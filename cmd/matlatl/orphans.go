package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stacklok/matlatl/internal/platform"
)

// newOrphansCommand implements `matlatl orphans [path]` — lists isolated
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
		Long: "orphans lists structurally weak documents: isolated orphans (no inbound " +
			"or outbound navigational links), unreachable documents (not reachable from " +
			"the root set), under-linked documents (fewer inbound links than the " +
			"discoverability threshold) and dead-ends (inbound links but nothing onward), " +
			"per ADR 0007 / ADR 0012. Use --isolated-only or --unreachable-only to filter. " +
			"This command always exits 0; gate CI with `check --strict`.",
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
			cfg, cerr := configFromFlags(cmd, args)
			if cerr != nil {
				return cerr
			}

			// Go through the same analyze→View path every other render command uses
			// (report, index). The View is the single place intentional-orphan
			// suppression and the sorted/presentation projection are applied, so
			// reading it here keeps `orphans` consistent with the rest of the CLI
			// and cannot drift from what the report shows.
			view, err := analyzeToView(cmd, cfg)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if view.Counts.Documents == 0 {
				_, _ = fmt.Fprintln(out, "matlatl orphans: no markdown documents found")
				return nil
			}

			showIsolated := !unreachableOnly
			showUnreachable := !isolatedOnly

			if showIsolated {
				_, _ = fmt.Fprintf(out, "Isolated orphans (%d):\n", len(view.Orphans))
				if len(view.Orphans) == 0 {
					_, _ = fmt.Fprintln(out, "  (none)")
				}
				for _, id := range view.Orphans {
					_, _ = fmt.Fprintf(out, "  %s\n", id)
				}

				_, _ = fmt.Fprintf(out, "Under-linked (%d):\n", len(view.UnderLinked))
				if len(view.UnderLinked) == 0 {
					_, _ = fmt.Fprintln(out, "  (none)")
				}
				for _, id := range view.UnderLinked {
					_, _ = fmt.Fprintf(out, "  %s\n", id)
				}

				_, _ = fmt.Fprintf(out, "Dead-ends (%d):\n", len(view.DeadEnd))
				if len(view.DeadEnd) == 0 {
					_, _ = fmt.Fprintln(out, "  (none)")
				}
				for _, id := range view.DeadEnd {
					_, _ = fmt.Fprintf(out, "  %s\n", id)
				}
			}
			if showUnreachable {
				if view.ReachabilityIndeterminate {
					_, _ = fmt.Fprintln(out,
						"Unreachable: indeterminate (no root set found; see notice on stderr)")
				} else {
					_, _ = fmt.Fprintf(out, "Unreachable (%d):\n", len(view.Unreachable))
					if len(view.Unreachable) == 0 {
						_, _ = fmt.Fprintln(out, "  (none)")
					}
					for _, id := range view.Unreachable {
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
