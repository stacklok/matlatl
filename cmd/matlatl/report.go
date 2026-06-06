package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/report"
	"github.com/stacklok/matlatl/internal/platform"
)

// newReportCommand implements `matlatl report [path]` — render a committable
// Markdown analysis report. With --out it writes report.md under the output
// directory (through the FSWriter safeJoin guard, ADR 0003); otherwise it prints
// to stdout. Like check, it never gates CI on its own (exits 0 on a clean run).
func newReportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report [path]",
		Short: "Render a human-readable Markdown analysis report",
		Long: "report scans, analyzes, and renders a committable GitHub-flavored " +
			"Markdown report (corpus overview, broken links/anchors, orphans, " +
			"unreachable, hubs/authorities, knowledge gaps, link suggestions).\n\n" +
			"With --out the report is written to report.md under the output directory; " +
			"otherwise it is printed to stdout.",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cerr := configFromFlags(cmd, args)
			if cerr != nil {
				return cerr
			}
			view, err := analyzeToView(cmd, cfg)
			if err != nil {
				return err
			}
			content := report.Markdown(view)
			return writeOrStdout(cmd.Context(), cmd, cfg.OutputDir, report.ReportMarkdownName, content)
		},
	}
	return cmd
}

// analyzeToView runs the pipeline and builds the render-ready View, mapping a
// runtime failure to the coded error. Notices go to stderr unless --quiet.
func analyzeToView(cmd *cobra.Command, cfg application.Config) (emit.View, error) {
	logSink := cmd.ErrOrStderr()
	if cfg.Quiet {
		logSink = nil
	}
	pipeline := buildPipeline(cfg, logSink)
	code, res, err := pipeline.Run(cmd.Context())
	if err != nil {
		return emit.View{}, exitCodeError{code: code, err: err}
	}
	return emit.BuildView(res), nil
}

// writeOrStdout writes content to name under outDir (via the FSWriter zip-slip
// guard) when outDir is set, else to stdout. A write failure is a runtime error.
func writeOrStdout(ctx context.Context, cmd *cobra.Command, outDir, name string, content []byte) error {
	if outDir == "" {
		if _, err := cmd.OutOrStdout().Write(content); err != nil {
			return exitCodeError{code: platform.ExitRuntime, err: err}
		}
		return nil
	}
	writer := emit.NewFSWriter(outDir)
	if err := writer.Write(ctx, []application.Artifact{{Name: name, Content: content}}); err != nil {
		return exitCodeError{code: platform.ExitRuntime, err: err}
	}
	return nil
}
