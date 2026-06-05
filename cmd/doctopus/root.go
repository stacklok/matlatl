package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
	"github.com/stacklok/doctopus/internal/infrastructure/emit/report"
	"github.com/stacklok/doctopus/internal/infrastructure/fsscanner"
	"github.com/stacklok/doctopus/internal/infrastructure/mdparser"
	"github.com/stacklok/doctopus/internal/platform"
)

// exitCodeError wraps an error with an explicit process exit code so main can
// honor the ADR 0005 contract for non-runtime outcomes (e.g. findings, usage).
type exitCodeError struct {
	code platform.ExitCode
	err  error
}

func (e exitCodeError) Error() string { return e.err.Error() }
func (e exitCodeError) Unwrap() error { return e.err }

// newRootCommand assembles the doctopus command tree.
func newRootCommand() *cobra.Command {
	var (
		strict        bool
		checkExternal bool
		outputDir     string
		quiet         bool
		verbose       bool
	)

	var noColor bool

	root := &cobra.Command{
		Use:   "doctopus [path]",
		Short: "Analyze a repository's markdown documentation graph",
		Long: "doctopus scans a repository's markdown, resolves its links, builds a " +
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
			code, res, err := pipeline.Run(cmd.Context())
			if err != nil {
				return exitCodeError{code: code, err: err}
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

			if code != platform.ExitOK {
				return exitCodeError{code: code, err: fmt.Errorf("findings present")}
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

	root.AddCommand(
		newCheckCommand(),
		newGraphCommand(),
		newIndexCommand(),
		newReportCommand(),
		newOrphansCommand(),
		newStubCommand("serve", "Run the MCP server", "P6"),
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
	return cfg
}

// buildPipeline wires the concrete scanner + parser factory into a Pipeline.
// The artifact writer is nil: artifacts are rendered/written by the command
// layer (e.g. check) after Run, so the pipeline stays emitter-agnostic.
func buildPipeline(cfg application.Config, logSink io.Writer) *application.Pipeline {
	scanner := fsscanner.New(fsscanner.Config{OutputDir: cfg.OutputDir})
	parserFac := mdparser.NewFactory(mdparser.Config{})
	return application.NewPipeline(cfg, scanner, parserFac, nil, logSink)
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

// newStubCommand returns a wired-but-unimplemented subcommand that accepts an
// optional path, prints a planned-phase notice, and returns cleanly.
func newStubCommand(name, short, phase string) *cobra.Command {
	return &cobra.Command{
		Use:           name + " [path]",
		Short:         short,
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"doctopus %s: not yet implemented (planned: %s)\n", name, phase)
			return err
		},
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
