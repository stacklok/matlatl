# Offline evaluation scaffold

`eval/` is the Go-only, standard-library command scaffold for evaluation issue
[#32](https://github.com/stacklok/matlatl/issues/32). It makes no network or
model calls and needs no credentials. Inspect AI and Claude integration are not
implemented here; they remain issue #36.

## Commands

From the repository root:

```console
go run ./eval/cmd/eval validate --root eval
go run ./eval/cmd/eval oracle --root eval
go run ./eval/cmd/eval smoke --root eval --out /tmp/matlatl-eval-records
go run ./eval/cmd/eval report --records /tmp/matlatl-eval-records --out /tmp/matlatl-eval-report
```

Equivalent Taskfile entries are `eval:validate`, `eval:oracle`, `eval:smoke`,
and `eval:report`. Smoke records are temporary; `eval:report` leaves the
inspectable derived report at `eval/out/report.md` (git-ignored).

`validate` strictly decodes v1 manifests and checks task packages. With
`--records`, it also verifies hashes, matching result/trajectory identities,
contiguous trajectory events, duplicate attempts, and retry-parent rules. The v1
retry policy is fixed in the manifest package: only `environment-failure`,
`mcp-failure`, `provider-failure`, and `evaluator-failure` may be parents of a
retry; tasks cannot override that taxonomy.
`oracle` runs the canonical fixture through matlatl's real scanner, parser,
pipeline, and graph JSON emitter, then compares observed values with an
independently hand-authored oracle. `smoke` runs the deterministic mock agent,
uses the private exact-path scorer, and creates one trajectory and result using
exclusive append-only writes. `report` validates records before rendering
byte-stable Markdown; with no `--records`, it renders an empty report.

## Layout and privacy boundary

- `tasks/`: versioned task manifests and agent instructions.
- `fixtures/`: agent-visible corpus files.
- `gold/`: private exact answers; never included in an agent package.
- `internal/`: filesystem guards, manifest validation, harness, oracle, report.
- `out/`: local generated records (git-ignored).

An agent-visible package contains exactly two fields: the instruction and the
corpus files. Gold, scorer code, and oracle data stay outside it. Tests place a
sentinel in private gold and verify it cannot appear in the package, and reject
a task that tries to name gold as its corpus.

This allowlist is **not hostile-process sandboxing**. It is a cooperative,
in-process data boundary for the offline mock. Process/container isolation is
work for the future real-agent runner.

All filesystem paths are root-relative and containment checked; existing
symlink components are rejected. Enumeration accepts only regular files and is
sorted. File/tree hashes and JSON encoding are deterministic. Result and
trajectory files are created with exclusive writes and are never updated in
place.

Resource budgets are fixed for this small v1 scaffold. Each filesystem file is
limited to 1 MiB and an agent-visible instruction-plus-corpus package to 2 MiB.
Manifest identifiers and paths are limited to 16 KiB; instructions, answers,
and each event payload to 64 KiB. A trajectory may contain at most 256 events.
Loading a result collection retains at most 2 MiB of result and trajectory JSON
and 4096 aggregate events. The loaders reject the next file or record before
retaining content that would exceed a cumulative budget.

## V1 manifest contract

The task, result, and trajectory records intentionally share `schemaVersion: 1`
as one atomic first-version family. An incompatible field or invariant change
requires v2; this scaffold has no migration framework or old-version decoder.

- A task contains `schemaVersion`, `id`, `version`, `kind`, `instruction`,
  `corpusRef`, private `goldRef`, and `answerFormat`.
- A trajectory contains `schemaVersion`, `runId`, `attemptId`, `taskHash`, `arm`,
  `agentId`, ordered `events`, and its canonical `hash`.
- A result contains `schemaVersion`, the same run/attempt/task/arm/agent identity,
  `taskId`, `attempt`, terminal `status`, `answer`, binary `score`, optional
  `retryParent`, and canonical `hash`.

Recognized statuses are `completed`, `agent-timeout`, `budget-exhausted`,
`agent-protocol-failure`, `environment-failure`, `mcp-failure`,
`provider-failure`, `evaluator-failure`, `invalid-task`, and `infra-exhausted`.
Agents may report only the first four. A completed result has a binary score of
0 or 1; richer judge scores require a later manifest version. Every other
terminal status has score -1 and an empty answer. Events are contiguous from 1,
result and trajectory identities must match, retry parents must be earlier
retryable attempts in the same run/task/arm, hashes cover canonical hashless
records, and persisted result/trajectory files are append-only.

## Contributor workflow

1. Add a versioned task under `tasks/`, its Markdown corpus under `fixtures/`,
   and its private exact answer under `gold/`; never copy gold into the corpus.
2. Hand-author or review any oracle expectation independently of emitted output.
3. Run `task eval:validate` and `task eval:oracle`, then smoke/report. Task edits
   change the canonical task hash and stable attempt ID; update any explicitly
   pinned expected hashes after reviewing that change.
4. Extend only the real seams: `Agent` returns an `AgentOutcome`, `Scorer` owns
   private gold, and `Canonical.Check` remains concrete. The package allowlist is
   not process sandboxing, and provider/Inspect behavior belongs to issue #36.

## Canonical fixture

`fixtures/canonical-graph/v1` is a three-document cycle:

```text
README.md -> docs/install.md -> docs/operate.md -> README.md
```

`README.md` is the root and the expected hop distances are 0, 1, and 2. The
unique task marker occurs only in `docs/operate.md`; its private exact-path
answer is hand-authored separately. The graph oracle independently enumerates
the documents, directed edges, root, hop distances, counts, and graph schema
version. Oracle expectations must never be generated or updated from matlatl
output.
