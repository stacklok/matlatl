package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	idxemit "github.com/stacklok/matlatl/internal/infrastructure/emit/index"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/llmstxt"
	"github.com/stacklok/matlatl/internal/platform"
)

// newIndexCommand implements `matlatl index [path]` — generate the navigation
// surface(s) for the corpus. By default it renders index.md (a human TOC + agent
// navigation surface). Flags select an LLM-facing artifact INSTEAD: --llms
// (llms.txt curated index), --full (llms-full.txt concatenated bodies), --small
// (llms-small.txt hubs + getting-started), or --graph (graph.json). Exactly one
// artifact is produced; with --out it is written under the output directory,
// otherwise printed to stdout. For the full bundle in one shot use `matlatl
// emit --out <dir>`.
func newIndexCommand() *cobra.Command {
	var (
		llms  bool
		full  bool
		small bool
		graph bool
		title string
	)
	cmd := &cobra.Command{
		Use:   "index [path]",
		Short: "Generate a navigation surface (index.md or an llms.txt artifact)",
		Long: "index renders a navigation surface for the corpus. By default it emits a " +
			"flat index.md grouped by category (a dual-purpose human TOC and agent " +
			"navigation surface).\n\n" +
			"Use a flag to emit an LLM-facing artifact instead: --llms (the spec-" +
			"compliant, importance-ordered llms.txt curated index), --full " +
			"(llms-full.txt, the concatenated clean bodies of every reachable doc), " +
			"--small (llms-small.txt, hubs + getting-started for tight context " +
			"windows), or --graph (graph.json). With --out the artifact is written " +
			"under the output directory; otherwise it prints to stdout.\n\n" +
			"To produce the complete LLM bundle (index.md + llms.txt + llms-full.txt + " +
			"llms-small.txt + graph.json + findings.json) in one shot, use `matlatl " +
			"emit --out <dir>`.",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			selected := 0
			for _, f := range []bool{llms, full, small, graph} {
				if f {
					selected++
				}
			}
			if selected > 1 {
				return exitCodeError{code: platform.ExitUsage,
					err: fmt.Errorf("--llms, --full, --small and --graph are mutually exclusive (use `emit` for the full bundle)")}
			}

			cfg, cerr := configFromFlags(cmd, args)
			if cerr != nil {
				return cerr
			}
			view, err := analyzeToView(cmd, cfg)
			if err != nil {
				return err
			}
			// emitExclude (ADR 0019) filters the consumption surfaces this command
			// renders (index.md, llms.txt family). --graph is unaffected: the
			// graph.json emitter never consults the matcher (machine surface).
			view = applyEmitExclude(cmd, cfg, view)

			opts := llmstxt.Options{Title: title}
			var content []byte
			var name string
			switch {
			case llms:
				content, name = llmstxt.LLMSTxt(view, opts), llmstxt.LLMSTxtName
			case full:
				content, name = llmstxt.LLMSFull(view, llmstxt.NewBodyReader(cfg.RootPath), opts), llmstxt.LLMSFullTxtName
			case small:
				content, name = llmstxt.LLMSSmall(view, llmstxt.NewBodyReader(cfg.RootPath), opts), llmstxt.LLMSSmallTxtName
			case graph:
				b, jerr := graphjson.JSON(view)
				if jerr != nil {
					return exitCodeError{code: platform.ExitRuntime, err: jerr}
				}
				content, name = b, graphjson.GraphJSONName
			default:
				content, name = idxemit.Markdown(view), idxemit.IndexMarkdownName
			}
			return writeOrStdout(cmd.Context(), cmd, cfg.OutputDir, name, content)
		},
	}
	cmd.Flags().BoolVar(&llms, "llms", false, "emit llms.txt (curated, importance-ordered index) instead of index.md")
	cmd.Flags().BoolVar(&full, "full", false, "emit llms-full.txt (concatenated clean bodies of reachable docs)")
	cmd.Flags().BoolVar(&small, "small", false, "emit llms-small.txt (hubs + getting-started, for tight context windows)")
	cmd.Flags().BoolVar(&graph, "graph", false, "emit graph.json (the machine-/LLM-queryable manifest)")
	cmd.Flags().StringVar(&title, "title", "", "corpus title for the llms.txt H1 (default: derived from the root doc)")
	return cmd
}
