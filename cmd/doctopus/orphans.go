package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stacklok/doctopus/internal/platform"
)

// newOrphansCommand implements `doctopus orphans [path]` — lists isolated
// orphans and unreachable documents (ADR 0007). It does not gate CI on its own;
// exit-code gating on orphans/unreachable is `check --strict`.
func newOrphansCommand() *cobra.Command {
	var (
		unreachableOnly bool
		isolatedOnly    bool
	)

	cmd := &cobra.Command{
		Use:   "orphans [path]",
		Short: "List orphaned and unreachable documents",
		Long: "orphans lists isolated orphans (no inbound or outbound navigational " +
			"links) and unreachable documents (not reachable from the root set), per " +
			"ADR 0007. Use --isolated-only or --unreachable-only to filter. This " +
			"command always exits 0; gate CI with `check --strict`.",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if unreachableOnly && isolatedOnly {
				return exitCodeError{
					code: platform.ExitUsage,
					err:  fmt.Errorf("--isolated-only and --unreachable-only are mutually exclusive"),
				}
			}
			cfg := configFromFlags(cmd, args)
			logSink := cmd.ErrOrStderr()
			if cfg.Quiet {
				logSink = nil
			}

			pipeline := buildPipeline(cfg, logSink)
			code, res, err := pipeline.Run(cmd.Context())
			if err != nil {
				return exitCodeError{code: code, err: err}
			}

			out := cmd.OutOrStdout()
			if res.DocumentCount == 0 {
				_, _ = fmt.Fprintln(out, "doctopus orphans: no markdown documents found")
				return nil
			}
			m := res.Metrics

			showIsolated := !unreachableOnly
			showUnreachable := !isolatedOnly

			if showIsolated {
				_, _ = fmt.Fprintf(out, "Isolated orphans (%d):\n", len(m.Orphans.Isolated))
				if len(m.Orphans.Isolated) == 0 {
					_, _ = fmt.Fprintln(out, "  (none)")
				}
				for _, id := range m.Orphans.Isolated {
					_, _ = fmt.Fprintf(out, "  %s\n", id)
				}
			}
			if showUnreachable {
				if m.Orphans.Indeterminate {
					_, _ = fmt.Fprintln(out,
						"Unreachable: indeterminate (no root set found; see notice on stderr)")
				} else {
					_, _ = fmt.Fprintf(out, "Unreachable (%d):\n", len(m.Orphans.Unreachable))
					if len(m.Orphans.Unreachable) == 0 {
						_, _ = fmt.Fprintln(out, "  (none)")
					}
					for _, id := range m.Orphans.Unreachable {
						_, _ = fmt.Fprintf(out, "  %s\n", id)
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&unreachableOnly, "unreachable-only", false, "list only unreachable documents")
	cmd.Flags().BoolVar(&isolatedOnly, "isolated-only", false, "list only isolated orphans")
	return cmd
}
