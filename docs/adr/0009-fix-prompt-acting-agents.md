# 9. `fix-prompt` serves acting agents with an embedded, agent-agnostic prompt

Date: 2026-06-05
Status: Accepted

## Context

matlatl has, until now, rendered for two audiences: **humans** (terminal/Markdown
reports, diagrams, `index.md`) and **LLMs as readers/queriers** (`graph.json`,
the `llms.txt` family, `findings.json`, and the read-only MCP server). Both stop
at *describing* the corpus. A third audience has emerged in practice: an LLM
coding agent asked to **mutate the repository** — to actually fix the broken
links, anchors, and orphans matlatl reports.

That audience is poorly served by the existing artifacts. `findings.json` is
machine-actionable but expects the agent (or its harness) to read a file, parse
it, and carry the per-kind remediation guidance itself. The common, frictionless
shape people reach for is a single prompt piped straight into an agent:
`matlatl fix-prompt . | claude -p`. There was no command that produced that.

## Decision

Add `matlatl fix-prompt [path]`: it runs the same pipeline as `check`/`report`
and writes (stdout, or `fix-prompt.md` under `--out`) a self-contained prompt
instructing an LLM coding agent to fix the documentation findings.

- **Third audience, named.** This is an explicit acting-agent surface, distinct
  from the reader/querier surface. It is the only artifact whose intent is to
  drive a mutation of the repo.
- **Agent-agnostic.** The prompt names no harness-specific tools or flags (no
  `--allowedTools`, no Claude-only vocabulary). It works piped into any agent.
  Coupling the artifact to one harness would make it stale the moment another
  agent is used; guardrails are baked into the prose instead.
- **Findings embedded inline.** The prompt carries the findings (in the report's
  deterministic order) and the per-kind how-to text directly, rather than
  referencing a `findings.json` the agent must locate and parse. Self-containment
  beats a file dependency for a piped one-shot: the agent needs nothing but the
  prompt on its stdin. The remediation text is the same single-source-of-truth
  `remediationByKind` map that backs `findings.json`, so the two never drift.
- **Generator, not a gate.** A successful run always exits 0, regardless of how
  many findings it embeds (a clean or fully `--errors-only`-filtered report
  emits a short no-op prompt). `check` remains the only command that applies the
  ADR 0005 exit-code contract. `fix-prompt` produces input for a fix; it does not
  judge the result.

`--errors-only` narrows the embedded findings to severity `error` (broken
links/anchors) for agents that should only touch build-breaking issues. Output
is deterministic and byte-stable (golden-tested), like every other emitter.

## Consequences

- matlatl now addresses three audiences: human readers, agent readers/queriers,
  and **acting agents**.
- The prompt and `findings.json` share `remediationByKind`, so per-kind guidance
  stays consistent across the read and act surfaces.
- Because it is agent-agnostic, the prompt cannot grant or restrict tool
  permissions; the safety contract lives in the prose guardrails (fix only listed
  findings, do not invent files/headings/facts, skip intentional orphans and
  non-doc links, prefer skipping when the target is ambiguous, verify with
  `matlatl check`). The hosting harness still owns sandboxing.
- A new artifact name (`fix-prompt.md`) and a golden file are added; the emitter
  lives in the existing `emit` package alongside `findings.json`, reusing its
  unexported remediation map directly.
