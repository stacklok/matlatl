package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/reference"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/platform"
)

// newCheckCommand implements `matlatl check [path]` — the CI gate (ADR 0005).
func newCheckCommand() *cobra.Command {
	var resolution string

	cmd := &cobra.Command{
		Use:   "check [path]",
		Short: "Validate links and anchors (CI gate)",
		Long: "check scans a repository's markdown, resolves every link and anchor, " +
			"and reports broken links, broken anchors and ambiguous targets.\n\n" +
			"Exit codes (ADR 0005): 0 clean, 1 findings at/above threshold " +
			"(broken links/anchors; --strict adds ambiguous), 2 usage, 3 runtime.",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromFlags(cmd, args)
			policy, perr := parseResolutionPolicy(resolution)
			if perr != nil {
				return exitCodeError{code: platform.ExitUsage, err: perr}
			}
			cfg.ResolutionPolicy = policy

			logSink := cmd.ErrOrStderr()
			if cfg.Quiet {
				logSink = nil
			}

			ctx := cmd.Context()
			pipeline := buildPipeline(cfg, logSink)
			runCode, res, err := pipeline.Run(ctx)
			if err != nil {
				return exitCodeError{code: runCode, err: err}
			}

			// Emit artifacts when --out is set (always, regardless of outcome).
			if cfg.OutputDir != "" {
				if werr := writeCheckArtifacts(ctx, cfg.OutputDir, res); werr != nil {
					return exitCodeError{code: platform.ExitRuntime, err: werr}
				}
			}

			printCheckSummary(cmd, res)

			if code := res.CheckExitCode(cfg.Strict); code != platform.ExitOK {
				return exitCodeError{code: code, err: fmt.Errorf("findings present")}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&resolution, "resolution", reference.DefaultResolutionPolicy.String(),
		"link resolution policy: exact | longest-suffix | basename")
	return cmd
}

// parseResolutionPolicy maps the --resolution flag to a ResolutionPolicy.
func parseResolutionPolicy(s string) (reference.ResolutionPolicy, error) {
	switch s {
	case "exact":
		return reference.Exact, nil
	case "longest-suffix":
		return reference.LongestSuffix, nil
	case "basename":
		return reference.Basename, nil
	default:
		return reference.DefaultResolutionPolicy,
			fmt.Errorf("invalid --resolution %q (want: exact, longest-suffix, basename)", s)
	}
}

// writeCheckArtifacts renders and writes findings.json + junit.xml under outDir.
func writeCheckArtifacts(ctx context.Context, outDir string, res application.Result) error {
	findingsJSON, err := emit.FindingsJSON(res.Report)
	if err != nil {
		return err
	}
	junitXML, err := emit.JUnitXML(res.Report)
	if err != nil {
		return err
	}
	writer := emit.NewFSWriter(outDir)
	return writer.Write(ctx, []application.Artifact{
		{Name: emit.FindingsJSONName, Content: findingsJSON},
		{Name: emit.JUnitXMLName, Content: junitXML},
	})
}

// printCheckSummary writes a human-readable summary to stdout.
func printCheckSummary(cmd *cobra.Command, res application.Result) {
	out := cmd.OutOrStdout()
	if res.DocumentCount == 0 {
		_, _ = fmt.Fprintln(out, "matlatl check: no markdown documents found (nothing to check)")
		return
	}
	_, _ = fmt.Fprintf(out,
		"matlatl check: %d documents, %d references — %d broken link(s), %d broken anchor(s), "+
			"%d ambiguous, %d orphan(s), %d unreachable\n",
		res.DocumentCount, res.ReferenceCount,
		res.BrokenLinkCount, res.BrokenAnchorCount, res.AmbiguousCount,
		res.OrphanCount, res.UnreachableCount)
}
