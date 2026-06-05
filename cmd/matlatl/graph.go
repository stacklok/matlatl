package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stacklok/matlatl/internal/infrastructure/emit/diagram"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	"github.com/stacklok/matlatl/internal/platform"
)

// newGraphCommand implements `matlatl graph [path]` — emit the reference graph
// as Mermaid (default) or DOT/Graphviz. With --out the diagram is written under
// the output directory; otherwise it prints to stdout. A --tree flag emits the
// hierarchy variant (Mermaid only).
func newGraphCommand() *cobra.Command {
	var (
		format string
		tree   bool
	)
	cmd := &cobra.Command{
		Use:   "graph [path]",
		Short: "Emit the reference graph (Mermaid or DOT)",
		Long: "graph renders the document-projection reference graph as a hand-rolled " +
			"Mermaid flowchart (default), Graphviz DOT, or the machine-/LLM-queryable " +
			"graph.json manifest.\n\n" +
			"Mermaid/DOT nodes are colored by connected component; orphans and broken-link " +
			"targets are visually distinct. A large graph (>200 nodes) falls back to a " +
			"focused subgraph of orphans/broken + their neighborhood, with a truncation " +
			"note. Use --format dot for Graphviz, --format json for graph.json (the " +
			"primary machine artifact), or --tree for the hierarchy variant.",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromFlags(cmd, args)
			view, err := analyzeToView(cmd, cfg)
			if err != nil {
				return err
			}

			var content []byte
			var name string
			switch format {
			case "mermaid":
				if tree {
					content = diagram.MermaidHierarchy(view)
				} else {
					content = diagram.Mermaid(view)
				}
				name = "graph.mmd"
			case "dot":
				if tree {
					return exitCodeError{code: platform.ExitUsage,
						err: fmt.Errorf("--tree is only supported with --format mermaid")}
				}
				content = diagram.DOT(view)
				name = "graph.dot"
			case "json":
				if tree {
					return exitCodeError{code: platform.ExitUsage,
						err: fmt.Errorf("--tree is only supported with --format mermaid")}
				}
				b, jerr := graphjson.JSON(view)
				if jerr != nil {
					return exitCodeError{code: platform.ExitRuntime, err: jerr}
				}
				content = b
				name = graphjson.GraphJSONName
			default:
				return exitCodeError{code: platform.ExitUsage,
					err: fmt.Errorf("invalid --format %q (want: mermaid, dot, json)", format)}
			}
			return writeOrStdout(cmd.Context(), cmd, cfg.OutputDir, name, content)
		},
	}
	cmd.Flags().StringVar(&format, "format", "mermaid", "output format: mermaid | dot | json")
	cmd.Flags().BoolVar(&tree, "tree", false, "render the hierarchy tree variant (mermaid only)")
	return cmd
}
