package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/report"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/linkcheck"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
	"github.com/stacklok/matlatl/internal/platform"
)

// exitCodeError wraps an error with an explicit process exit code so main can
// honor the ADR 0005 contract for non-runtime outcomes (e.g. findings, usage).
type exitCodeError struct {
	code platform.ExitCode
	err  error
}

func (e exitCodeError) Error() string { return e.err.Error() }
func (e exitCodeError) Unwrap() error { return e.err }

// newRootCommand assembles the matlatl command tree.
func newRootCommand() *cobra.Command {
	var (
		strict        bool
		checkExternal bool
		outputDir     string
		quiet         bool
		verbose       bool
		roots         []string
	)

	var noColor bool

	root := &cobra.Command{
		Use:   "matlatl [path]",
		Short: "Analyze a repository's markdown documentation graph",
		Long: "matlatl scans a repository's markdown, resolves its links, builds a " +
			"reference graph, and analyzes it (reachability, orphans/unreachable, " +
			"weak/strong components, HITS, knowledge gaps), then renders a human " +
			"report.\n\n" +
			"The default invocation prints a colorized terminal report; use --quiet " +
			"for the one-line summary, or the report/graph/index subcommands for " +
			"committable artifacts.",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromFlags(cmd, args)

			// Notices (skipped symlinks, oversized files, truncation) go to stderr.
			var logSink = cmd.ErrOrStderr()
			if cfg.Quiet {
				logSink = nil
			}

			pipeline := buildPipeline(cfg, logSink)
			// The default command is DISPLAY-ONLY: it always exits 0 on a
			// successful run regardless of findings. `matlatl check` is the CI
			// gate (it applies the ADR 0005 exit contract via Result.CheckExitCode);
			// `matlatl .` just renders the human report. Pipeline.Run returns
			// (ExitOK, _, nil) on success, so the only non-nil error here is a
			// genuine runtime/usage failure, which we propagate with its code.
			_, res, err := pipeline.Run(cmd.Context())
			if err != nil {
				return exitCodeError{code: platform.ExitRuntime, err: err}
			}

			view := emit.BuildView(res)
			colorMode := report.ColorAuto
			if no, _ := cmd.Flags().GetBool("no-color"); no {
				colorMode = report.ColorNever
			}
			if terr := report.Terminal(cmd.OutOrStdout(), view, report.TerminalOptions{
				Color: colorMode,
				Quiet: cfg.Quiet,
			}); terr != nil {
				return exitCodeError{code: platform.ExitRuntime, err: terr}
			}
			return nil
		},
	}

	// Map flag parse errors to the usage exit code (ADR 0005).
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exitCodeError{code: platform.ExitUsage, err: err}
	})

	root.PersistentFlags().BoolVar(&strict, "strict", false, "promote orphan/ambiguous warnings to failures")
	root.PersistentFlags().BoolVar(&checkExternal, "check-external", false, "enable opt-in external link liveness checks")
	root.PersistentFlags().StringVarP(&outputDir, "out", "o", "", "artifact output directory")
	root.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress non-essential output")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable detailed logging")
	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable ANSI color (also honored: NO_COLOR env, non-TTY output)")
	root.PersistentFlags().StringSliceVar(&roots, "root", nil,
		"reachability root glob(s), matched against document paths; repeatable and/or "+
			"comma-separated. Added to the autodetected roots (README.md/index.md/type:index). "+
			"Globs use path.Match: a single `*` does NOT cross `/`, and `**` is not supported "+
			"(e.g. `docs/*.md`, not `docs/**`)")

	root.AddCommand(
		newCheckCommand(),
		newGraphCommand(),
		newIndexCommand(),
		newEmitCommand(),
		newReportCommand(),
		newFixPromptCommand(),
		newOrphansCommand(),
		newServeCommand(),
		newVersionCommand(),
	)

	return root
}

// configFromFlags builds an application.Config from the persistent flags and the
// optional positional path. Subcommands may further adjust the result (e.g.
// check sets ResolutionPolicy from its own flag).
func configFromFlags(cmd *cobra.Command, args []string) application.Config {
	cfg := application.DefaultConfig()
	if len(args) == 1 {
		cfg.RootPath = args[0]
	}
	flags := cmd.Flags()
	cfg.Strict, _ = flags.GetBool("strict")
	cfg.CheckExternal, _ = flags.GetBool("check-external")
	cfg.OutputDir, _ = flags.GetString("out")
	cfg.Quiet, _ = flags.GetBool("quiet")
	cfg.Verbose, _ = flags.GetBool("verbose")
	if r, err := flags.GetStringSlice("root"); err == nil && len(r) > 0 {
		cfg.Roots = r
	}
	return cfg
}

// buildPipeline wires the concrete scanner + parser factory into a Pipeline.
// Artifacts are rendered/written by the command layer (e.g. check) after Run, so
// the pipeline stays emitter-agnostic.
func buildPipeline(cfg application.Config, logSink io.Writer) *application.Pipeline {
	scanner := fsscanner.New(fsscanner.Config{OutputDir: cfg.OutputDir})
	parserFac := mdparser.NewFactory(mdparser.Config{})
	// Wire the external link checker only when --check-external is set so a
	// default run pays nothing for it and stays deterministic (ADR 0003). The
	// checker carries the mandatory SSRF guard.
	if cfg.CheckExternal && cfg.ExternalChecker == nil {
		cfg.ExternalChecker = linkcheck.New(linkcheck.Config{})
	}
	return application.NewPipeline(cfg, scanner, parserFac, logSink)
}

// usageArgs wraps a cobra positional-args validator so that an arg-count
// violation is reported as a usage error (exit 2, ADR 0005) rather than a
// generic runtime error.
func usageArgs(inner cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := inner(cmd, args); err != nil {
			return exitCodeError{code: platform.ExitUsage, err: err}
		}
		return nil
	}
}

// newVersionCommand prints build metadata.
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), platform.BuildInfo())
			return err
		},
	}
}
