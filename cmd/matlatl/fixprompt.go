package main

import (
	"github.com/spf13/cobra"

	"github.com/stacklok/matlatl/internal/infrastructure/emit"
)

// newFixPromptCommand implements `matlatl fix-prompt [path]` — write a
// self-contained, agent-agnostic prompt to stdout (or --out) that instructs an
// LLM coding agent to fix matlatl's documentation findings, with the findings
// embedded inline. Pipe it to any agent, e.g. `matlatl fix-prompt . | claude -p`.
//
// It is a GENERATOR, not a gate: a successful run always exits 0 regardless of
// how many findings it embedded. Only a genuine pipeline failure propagates its
// exit code.
func newFixPromptCommand() *cobra.Command {
	var errorsOnly bool

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
			"Use --errors-only to embed only severity=error findings (broken links/anchors). " +
			"With --out the prompt is written to fix-prompt.md under the output directory; " +
			"otherwise it is printed to stdout.",
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := configFromFlags(cmd, args)

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

			content := emit.FixPrompt(res.Report, emit.FixPromptOptions{ErrorsOnly: errorsOnly})
			return writeOrStdout(ctx, cmd, cfg.OutputDir, emit.FixPromptName, content)
		},
	}

	cmd.Flags().BoolVar(&errorsOnly, "errors-only", false,
		"include only severity=error findings (broken links/anchors)")
	return cmd
}
