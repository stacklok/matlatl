# 5. `check` exit-code contract

Date: 2026-06-05
Status: Accepted

## Context

`matlatl check` is the CI gate. CI pipelines depend on a precise, stable mapping
from analysis outcome to process exit code. Leaving this implicit causes flaky or
surprising pipeline behavior, so it is specified up front and golden-tested.

## Decision

Exit codes:

| Code | Meaning                                                                 |
| ---- | ----------------------------------------------------------------------- |
| `0`  | Success — no findings at or above the failure threshold.                |
| `1`  | Findings present at/above threshold (broken links/anchors; orphans when `--strict`). |
| `2`  | Usage error — bad flags/arguments.                                      |
| `3`  | Runtime error — unreadable path, I/O failure, internal error.           |

Semantics:

- A **clean** repo (parses, no failing findings) → `0`.
- A repo with broken links or broken anchors → `1`. Orphans and ambiguous links are
  warnings by default (exit `0`) and become failures (`1`) only under `--strict`.
- **No markdown found** under the scan root → `0` with a clear "no markdown documents
  found" notice (an empty corpus is not an error). A future `--fail-on-empty` may opt in.
- **No root found** for reachability (no `README.md`/`index.md`/`type:index`, none
  configured) → reachability is reported as indeterminate; this alone does **not**
  fail the build. Broken-link/anchor findings still apply.

- **External dead links are out of the exit contract.** `--check-external` is
  opt-in and inherently non-deterministic (network state, transient timeouts,
  rate limits, DNS). `DeadLink` findings are therefore **reported** (in
  findings.json, JUnit, and the human report) but **never** change the exit code,
  **even under `--strict`** — gating CI on them would make a green build flaky.
  CI that wants to fail on dead external links should consume `findings.json`
  explicitly. (Enforced in `Result.CheckExitCode`, which never reads
  `DeadLinkCount`, and pinned by the `dead-link` rows in `TestCheckExitCode`.)

- **The default command (`matlatl [path]`) is display-only.** It renders the
  human report and **always exits 0** on a successful run regardless of findings;
  only a genuine runtime/usage failure yields a non-zero code. `matlatl check`
  is the CI gate that applies the table above. (`Pipeline.Run` returns `ExitOK`
  on success; the gating decision lives only in `check` via `CheckExitCode`.)

`check` always emits `findings.json` and JUnit XML (when `--out` is set) regardless
of exit code, so CI dashboards get structured results on both pass and fail.

## Consequences

- Each outcome above ships with a golden integration test asserting both the exit
  code and the emitted artifacts.
- The threshold knob (`--strict`, severity config) maps cleanly onto codes `0`/`1`.
