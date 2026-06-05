package main

import (
	"github.com/spf13/cobra"

	"github.com/stacklok/doctopus/internal/infrastructure/mcpserver"
	"github.com/stacklok/doctopus/internal/platform"
)

// newServeCommand implements `doctopus serve [path]` — run the read-only MCP
// server over stdio. It builds the analysis once over the path, then exposes the
// graph-query tools (what-links-to, list-orphans, path-between, get-section,
// corpus-summary) to an MCP client. The MCP dependency is isolated in
// internal/infrastructure/mcpserver; nothing else in the tool imports it.
func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve [path]",
		Short: "Run the read-only MCP server (stdio)",
		Long: "serve builds the doctopus analysis once over the given path and exposes " +
			"read-only MCP tools over stdio for an agent: what-links-to, list-orphans, " +
			"path-between, get-section, and corpus-summary (the graph.json manifest). " +
			"All tools return the same structured data as the file artifacts.",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromFlags(cmd, args)
			if err := mcpserver.Serve(cmd.Context(), cfg.RootPath); err != nil {
				return exitCodeError{code: platform.ExitRuntime, err: err}
			}
			return nil
		},
	}
}
