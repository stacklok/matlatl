# 20. `fix-prompt` scope: curated default, kind selection, and emitExclude

Date: 2026-06-10
Status: Accepted

## Context

`fix-prompt` (ADR 0009) embedded EVERY finding. On a real repo that meant a
prompt dominated by advisory noise: ~950 `suggested-link` entries and dozens of
Info findings on `.claude/skills/**` scaffolding the repo had already declared
uninteresting via `emitExclude` (ADR 0019). An acting agent fed that prompt
burns its context on hints instead of defects, and `--errors-only` was the only
alternative — all-or-nothing.

## Decision

### Curated default scope

The bare `matlatl fix-prompt .` now embeds: **all Error and all Warning
findings**, plus the **advisory (Info) findings** that survive two filters:

- **emitExclude (severity-keyed, either endpoint).** An advisory finding is
  dropped iff it touches a `.matlatl.yml emitExclude` document — its Location,
  or a named pair endpoint (`suggestedTarget`, `representativeA`/`B`,
  `bridgeEndpoint`). Gate-capable findings (Error + Warning) ALWAYS render:
  fix-prompt's contract is "make `matlatl check` pass" and check ignores
  emitExclude (ADR 0019). `low-scent-anchor` filters by source Location only —
  renaming an anchor in a rendered doc improves it regardless of destination.
  Cluster labels (`componentA`/`B`) are deliberately not matched (they would
  systematically over-drop). The matcher is the shared gitignore engine
  (`compileExclude`), string-matched against DocumentIDs only (ADR 0003).
- **Caps for the two corpus-scaling kinds.** `suggested-link` keeps the top
  **20** by Adamic/Adar score (ties: `sharedNeighbours` desc, then report
  order); `low-scent-anchor` keeps the **50** weakest by scent score (ties:
  report order). Selection stable-sorts candidate indices by score only and
  re-emits the survivors in report order, so output stays byte-deterministic.
  The caps are named constants in `emit`, NOT config keys — config is
  repo-shape, caps are run-behavior (ADR 0011).

### Modes

`--errors-only` (unchanged), `--kinds k1,k2` (exactly these kinds, ALL of them
— caps lifted, emitExclude still applies; unknown name = exit 2 listing the
valid names, validated before the pipeline runs), and `--all` (the single
escape hatch: every kind, every severity, no filtering, no caps — byte-identical
to the pre-0020 default). The three are mutually exclusive (exit 2, checked in
RunE so the error maps to the ADR 0005 usage code).

### Honesty

The prompt's Scope block states the mode and — only when something was dropped
— one accounting line per drop (cap totals, emitExclude count), each pointing
at `--kinds`/`--all`/`findings.json`. A report filtered to empty yields an
honest "0 of N selected" no-op, never the clean-corpus text. `findings.json`
always carries everything; exit codes are unchanged (generator, not a gate,
ADR 0009). The domain gains only `ParseFindingKind` (the pure inverse of
`FindingKind.String`); the selection lives in the emit layer (ADR 0004).

## Consequences

- On a measured repo the default prompt shrinks from ~1300 findings to ~120,
  with zero `.claude/skills/**` entries, while `--all` reproduces the old
  output byte-for-byte (golden-pinned).
- The exclusion rule is severity-keyed, so it self-adjusts if ADR 0012's knob
  promotes under-linked/dead-end to Warning. Accepted impurity: `dead-link`
  (Warning, never gates, opt-in) renders on excluded docs.
- Scores tie at 6 decimals (the Details' fixed formatting); the stable
  report-order tiebreak keeps cap selection deterministic anyway.

## See also

- [ADR 0009](0009-fix-prompt-acting-agents.md) — fix-prompt's contract.
- [ADR 0019](0019-emit-exclude.md) — emitExclude and the shared matcher.
- [ADR 0005](0005-exit-code-contract.md) — the usage exit code.
