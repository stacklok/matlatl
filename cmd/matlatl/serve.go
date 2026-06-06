package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stacklok/matlatl/internal/infrastructure/mcpserver"
	"github.com/stacklok/matlatl/internal/platform"
)

// defaultServeAddress is the default host:port the MCP server listens on. It
// binds loopback by default; containerized deployments (e.g. ToolHive) should
// pass --address 0.0.0.0:8080 so the endpoint is reachable from outside.
const defaultServeAddress = "127.0.0.1:8080"

// newServeCommand implements `matlatl serve [path]` — run the read-only MCP
// server over streamable HTTP. It builds the analysis once over the path, then
// exposes the graph-query tools (what-links-to, list-orphans, path-between,
// get-section, corpus-summary) to an MCP client. The MCP dependency is isolated
// in internal/infrastructure/mcpserver; nothing else in the tool imports it.
func newServeCommand() *cobra.Command {
	var address string
	cmd := &cobra.Command{
		Use:   "serve [path]",
		Short: "Run the read-only MCP server (streamable HTTP)",
		Long: "serve builds the matlatl analysis once over the given path and exposes " +
			"read-only MCP tools over streamable HTTP for an agent: what-links-to, list-orphans, " +
			"path-between, get-section, and corpus-summary (the graph.json manifest). " +
			"All tools return the same structured data as the file artifacts.\n\n" +
			"The endpoint is served at " + mcpserver.EndpointPath + " on --address (default " +
			defaultServeAddress + "). The server runs until interrupted, draining in-flight " +
			"requests on shutdown.",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cerr := configFromFlags(cmd, args)
			if cerr != nil {
				return cerr
			}
			if !cfg.Quiet {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "matlatl serve: MCP endpoint on http://%s%s\n",
					address, mcpserver.EndpointPath)
			}
			if err := mcpserver.Serve(cmd.Context(), cfg.RootPath, address); err != nil {
				return exitCodeError{code: platform.ExitRuntime, err: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&address, "address", defaultServeAddress,
		"host:port the streamable-HTTP MCP endpoint listens on (use 0.0.0.0:PORT for containers)")
	return cmd
}
