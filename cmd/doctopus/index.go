package main

import (
	"github.com/spf13/cobra"

	idxemit "github.com/stacklok/doctopus/internal/infrastructure/emit/index"
)

// newIndexCommand implements `doctopus index [path]` — generate a navigable flat
// index.md of every document (canonical DocumentID, description, category,
// mod-date). With --out it writes index.md under the output directory; otherwise
// it prints to stdout.
func newIndexCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index [path]",
		Short: "Generate a navigable index.md for the corpus",
		Long: "index renders a flat, navigable index of every document grouped by " +
			"category, with a one-line description (front matter, falling back to the " +
			"first heading) and mod-date. It is a dual-purpose human TOC and agent " +
			"navigation surface.\n\n" +
			"With --out the index is written to index.md under the output directory; " +
			"otherwise it is printed to stdout.",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromFlags(cmd, args)
			view, err := analyzeToView(cmd, cfg)
			if err != nil {
				return err
			}
			content := idxemit.Markdown(view)
			return writeOrStdout(cmd.Context(), cmd, cfg.OutputDir, idxemit.IndexMarkdownName, content)
		},
	}
	return cmd
}
