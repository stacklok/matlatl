# 5. `check` exit-code contract

Date: 2026-06-05
Status: Accepted

## Context

`doctopus check` is the CI gate. CI pipelines depend on a precise, stable mapping
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

`check` always emits `findings.json` and JUnit XML (when `--out` is set) regardless
of exit code, so CI dashboards get structured results on both pass and fail.

## Consequences

- Each outcome above ships with a golden integration test asserting both the exit
  code and the emitted artifacts.
- The threshold knob (`--strict`, severity config) maps cleanly onto codes `0`/`1`.
