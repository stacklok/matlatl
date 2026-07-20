package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	idxemit "github.com/stacklok/matlatl/internal/infrastructure/emit/index"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/llmstxt"
	trailsemit "github.com/stacklok/matlatl/internal/infrastructure/emit/trails"
	"github.com/stacklok/matlatl/internal/platform"
)

// newEmitCommand implements `matlatl emit --out <dir>` — produce the COMPLETE
// LLM artifact bundle in one shot: index.md, llms.txt, llms-full.txt,
// llms-small.txt, graph.json, and findings.json, all under --out. It is the
// obvious "give me everything an agent needs" path; the per-artifact commands
// (index, graph) remain for stdout/single-file use. Every write goes through the
// FSWriter zip-slip guard (ADR 0003); --out is required.
func newEmitCommand() *cobra.Command {
	var title string
	cmd := &cobra.Command{
		Use:   "emit [path]",
		Short: "Emit the full LLM artifact bundle (--out required)",
		Long: "emit produces the complete LLM-facing artifact set under --out in one " +
			"run: index.md (navigation), llms.txt (curated importance-ordered index), " +
			"llms-full.txt (concatenated clean bodies of reachable docs), llms-small.txt " +
			"(hubs + getting-started for tight context windows), graph.json (the machine-" +
			"queryable manifest), trails.json (suggested reading orders), and findings.json " +
			"(actionable, agent-ready findings).\n\n" +
			"All artifacts are deterministic and written under --out (required) through " +
			"the path-safety guard (ADR 0003).",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cerr := configFromFlags(cmd, args)
			if cerr != nil {
				return cerr
			}
			if cfg.OutputDir == "" {
				return exitCodeError{code: platform.ExitUsage,
					err: fmt.Errorf("emit requires --out <dir>")}
			}

			logSink := cmd.ErrOrStderr()
			if cfg.Quiet {
				logSink = nil
			}
			pipeline := buildPipeline(cfg, logSink)
			code, res, err := pipeline.Run(cmd.Context())
			if err != nil {
				return exitCodeError{code: code, err: err}
			}
			// emitExclude (ADR 0019) filters only the consumption surfaces in the
			// bundle (index.md, the llms.txt family, trails.json); graph.json and
			// findings.json render the complete corpus from the same View.
			view := applyEmitExclude(cmd, cfg, emit.BuildView(res))

			artifacts, err := bundleArtifacts(view, res, cfg.RootPath, llmstxt.Options{Title: title})
			if err != nil {
				return exitCodeError{code: platform.ExitRuntime, err: err}
			}
			writer := emit.NewFSWriter(cfg.OutputDir)
			if werr := writer.Write(cmd.Context(), artifacts); werr != nil {
				return exitCodeError{code: platform.ExitRuntime, err: werr}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"matlatl emit: wrote %d artifact(s) to %s\n", len(artifacts), cfg.OutputDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "corpus title for the llms.txt H1 (default: derived from the root doc)")
	return cmd
}

// bundleArtifacts renders the full LLM bundle from the frozen View + Result. The
// llms-full/small emitters read doc bodies through a root-confined BodyReader so
// the domain stays pure and nothing is read outside the scan root (ADR 0003).
func bundleArtifacts(view emit.View, res application.Result, rootPath string, opts llmstxt.Options) ([]application.Artifact, error) {
	graphJSON, err := graphjson.JSON(view)
	if err != nil {
		return nil, err
	}
	trailsJSON, err := trailsemit.JSON(view)
	if err != nil {
		return nil, err
	}
	findingsJSON, err := emit.FindingsJSON(res.Report, emit.OKFVerdictFromResult(res))
	if err != nil {
		return nil, err
	}
	reader := llmstxt.NewBodyReader(rootPath)
	return []application.Artifact{
		{Name: idxemit.IndexMarkdownName, Content: idxemit.Markdown(view)},
		{Name: llmstxt.LLMSTxtName, Content: llmstxt.LLMSTxt(view, opts)},
		{Name: llmstxt.LLMSFullTxtName, Content: llmstxt.LLMSFull(view, reader, opts)},
		{Name: llmstxt.LLMSSmallTxtName, Content: llmstxt.LLMSSmall(view, reader, opts)},
		{Name: graphjson.GraphJSONName, Content: graphJSON},
		{Name: trailsemit.TrailsJSONName, Content: trailsJSON},
		{Name: emit.FindingsJSONName, Content: findingsJSON},
	}, nil
}
