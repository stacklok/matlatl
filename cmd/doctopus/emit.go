package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
	"github.com/stacklok/doctopus/internal/infrastructure/emit/graphjson"
	idxemit "github.com/stacklok/doctopus/internal/infrastructure/emit/index"
	"github.com/stacklok/doctopus/internal/infrastructure/emit/llmstxt"
	"github.com/stacklok/doctopus/internal/platform"
)

// newEmitCommand implements `doctopus emit --out <dir>` — produce the COMPLETE
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
			"queryable manifest), and findings.json (actionable, agent-ready findings).\n\n" +
			"All artifacts are deterministic and written under --out (required) through " +
			"the path-safety guard (ADR 0003).",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromFlags(cmd, args)
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
			view := emit.BuildView(res)

			artifacts, err := bundleArtifacts(view, res, cfg.RootPath, llmstxt.Options{Title: title})
			if err != nil {
				return exitCodeError{code: platform.ExitRuntime, err: err}
			}
			writer := emit.NewFSWriter(cfg.OutputDir)
			if werr := writer.Write(cmd.Context(), artifacts); werr != nil {
				return exitCodeError{code: platform.ExitRuntime, err: werr}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"doctopus emit: wrote %d artifact(s) to %s\n", len(artifacts), cfg.OutputDir)
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
	findingsJSON, err := emit.FindingsJSON(res.Report)
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
		{Name: emit.FindingsJSONName, Content: findingsJSON},
	}, nil
}
