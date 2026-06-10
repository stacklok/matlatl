package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/platform"
)

// newFixPromptCommand implements `matlatl fix-prompt [path]` — write a
// self-contained, agent-agnostic prompt to stdout (or --out) that instructs an
// LLM coding agent to fix matlatl's documentation findings, with the findings
// embedded inline. Pipe it to any agent, e.g. `matlatl fix-prompt . | claude -p`.
//
// It is a GENERATOR, not a gate: a successful run always exits 0 regardless of
// how many findings it embedded. Only a genuine pipeline failure propagates its
// exit code.
//
// The default scope is curated (ADR 0020): all errors and warnings, plus the
// advisory findings that survive the `.matlatl.yml emitExclude` rule, with the
// two corpus-scaling kinds (suggested-link, low-scent-anchor) capped. --kinds
// selects exact kinds (caps lifted), --all is the unfiltered escape hatch, and
// --errors-only keeps its historical meaning. The three mode flags are mutually
// exclusive (exit 2).
func newFixPromptCommand() *cobra.Command {
	var (
		errorsOnly bool
		all        bool
		kindNames  []string
	)

	cmd := &cobra.Command{
		Use:   "fix-prompt [path]",
		Short: "Emit an agent-ready prompt to fix the documentation findings",
		Long: "fix-prompt scans, analyzes, and writes a self-contained, agent-agnostic " +
			"prompt that instructs an LLM coding agent to fix matlatl's documentation " +
			"findings. The findings (and a per-kind how-to) are embedded inline, so the " +
			"prompt needs no other context. Pipe it to any agent:\n\n" +
			"  matlatl fix-prompt . | claude -p\n\n" +
			"The prompt is agent-agnostic — it names no harness-specific tools — and bakes " +
			"its guardrails into the text (fix only listed findings, don't invent files/" +
			"headings/facts, skip intentional orphans, verify with `matlatl check`).\n\n" +
			"By default the scope is curated: every error and warning, plus advisory " +
			"findings not on `.matlatl.yml emitExclude` documents, with suggested-link " +
			"capped at 20 and low-scent-anchor at 50. The prompt's Scope block reports " +
			"exactly what was omitted; findings.json always has everything.\n\n" +
			"Use --errors-only to embed only severity=error findings (broken links/anchors), " +
			"--kinds to embed exactly the named kinds (all of them — caps lifted), or " +
			"--all for the complete, unfiltered report. The three are mutually exclusive. " +
			"With --out the prompt is written to fix-prompt.md under the output directory; " +
			"otherwise it is printed to stdout.",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Mode exclusivity + kind validation happen BEFORE the pipeline runs
			// (fail fast, exit 2). Checked here in RunE — not via cobra flag groups,
			// whose errors bypass the root SetFlagErrorFunc and would not map to the
			// ADR 0005 usage exit code.
			modes := 0
			for _, set := range []bool{errorsOnly, all, len(kindNames) > 0} {
				if set {
					modes++
				}
			}
			if modes > 1 {
				return exitCodeError{code: platform.ExitUsage,
					err: fmt.Errorf("--errors-only, --kinds and --all are mutually exclusive")}
			}
			kinds, kerr := parseFindingKinds(kindNames)
			if kerr != nil {
				return exitCodeError{code: platform.ExitUsage, err: kerr}
			}

			cfg, cerr := configFromFlags(cmd, args)
			if cerr != nil {
				return cerr
			}

			logSink := cmd.ErrOrStderr()
			if cfg.Quiet {
				logSink = nil
			}

			ctx := cmd.Context()
			pipeline := buildPipeline(cfg, logSink)
			runCode, res, err := pipeline.Run(ctx)
			if err != nil {
				return exitCodeError{code: runCode, err: err}
			}

			content := emit.FixPrompt(res.Report, emit.FixPromptOptions{
				ErrorsOnly:  errorsOnly,
				Kinds:       kinds,
				All:         all,
				EmitExclude: cfg.EmitExclude,
			})
			return writeOrStdout(ctx, cmd, cfg.OutputDir, emit.FixPromptName, content)
		},
	}

	cmd.Flags().BoolVar(&errorsOnly, "errors-only", false,
		"include only severity=error findings (broken links/anchors)")
	cmd.Flags().StringSliceVar(&kindNames, "kinds", nil,
		"include exactly these finding kinds, all of them (comma-separated, e.g. suggested-link,orphan)")
	cmd.Flags().BoolVar(&all, "all", false,
		"include the complete, unfiltered report (every kind, every severity, no caps)")
	return cmd
}

// parseFindingKinds maps --kinds values to FindingKinds, deduping silently and
// preserving first-seen order (mirrors parseResolutionPolicy: an unknown or
// empty name is a usage error that lists every valid kind name, exit 2).
func parseFindingKinds(names []string) ([]analysis.FindingKind, error) {
	if len(names) == 0 {
		return nil, nil
	}
	seen := make(map[analysis.FindingKind]bool, len(names))
	out := make([]analysis.FindingKind, 0, len(names))
	for _, n := range names {
		k, ok := analysis.ParseFindingKind(strings.TrimSpace(n))
		if !ok {
			return nil, fmt.Errorf("invalid --kinds value %q (want: %s)", n, validFindingKindNames())
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out, nil
}

// validFindingKindNames lists every defined finding-kind name, comma-separated,
// derived from the domain enum so the usage message can never drift from it.
func validFindingKindNames() string {
	var names []string
	for k := analysis.BrokenLink; k.Valid(); k++ {
		names = append(names, k.String())
	}
	return strings.Join(names, ", ")
}
