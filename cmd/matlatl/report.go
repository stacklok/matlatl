package main

import (
	"context"
	"fmt"

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

// applyEmitExclude arms the View with the `.matlatl.yml emitExclude` patterns
// (ADR 0019). Called ONLY by the consumption-surface commands (`emit`, `index`):
// the filter affects the llms.txt family, index.md, and trails.json — never
// `check`, the reports, graph.json, findings.json, or junit.xml. A pattern that
// matches a reachability root is allowed (the root simply does not render;
// reachability is computed over the unfiltered corpus) but earns a stderr notice
// so it is not silent.
func applyEmitExclude(cmd *cobra.Command, cfg application.Config, view emit.View) emit.View {
	if len(cfg.EmitExclude) == 0 {
		return view
	}
	view = view.WithEmitExclude(cfg.EmitExclude)
	if !cfg.Quiet {
		sink := cmd.ErrOrStderr()
		for _, id := range view.EmitExcludedRoots() {
			_, _ = fmt.Fprintf(sink,
				"matlatl: notice [%s] %s: emitExclude matches reachability root %q; "+
					"it will not render on the consumption surfaces (reachability is unaffected)\n",
				application.NoticeConfig, ".matlatl.yml", id)
		}
	}
	return view
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
