# Offline evaluation scaffold

`eval/` is the Go-only, standard-library command scaffold for evaluation issue
[#32](https://github.com/stacklok/matlatl/issues/32). Level 1 deterministic
correctness from issue [#34](https://github.com/stacklok/matlatl/issues/34) is
implemented by the oracle inventory below. It makes no network or model calls
and needs no credentials. Inspect AI and Claude integration are not implemented
here; they remain issue #36.

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
`oracle` first runs the canonical fixture through matlatl's real scanner,
parser, pipeline, and graph JSON emitter, then executes the separate correctness
contract described below. `smoke` runs the deterministic mock agent,
uses the private exact-path scorer, and creates one trajectory and result using
exclusive append-only writes. `report` validates records before rendering
byte-stable Markdown; with no `--records`, it renders an empty report.

## Layout and privacy boundary

- `tasks/`: versioned task manifests and agent instructions.
- `fixtures/`: agent-visible corpus files.
- `gold/`: private exact answers; never included in an agent package.
- `oracles/correctness/v1/`: the strict, separately versioned correctness inputs
  and expectations; this is not part of the task/result/trajectory manifest.
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
retaining content that would exceed a cumulative budget. Correctness-oracle
files are independently capped at 1 MiB, 256 cases, 1024 documents/edges per
case, and 16 KiB strings. Before execution, the complete correctness suite is
also capped at 128 cases, 4096 vertices, 8192 edges, 512 fixture files, 96
pipeline runs, and 1,000,000 aggregate graph-work units, where each run costs
`V * (V + E)`.

## Correctness-oracle v1 contract

`oracles/correctness/v1/` is deliberately independent from the v1 task, result,
and trajectory schema. Its `schemaVersion: 1` versions only the correctness
contract. The loader rejects unknown fields, duplicate keys, trailing JSON,
unsafe paths, duplicate or unstable ordering, unsupported enums, excessive
counts, and oversized input.

`graph.json` declares small directed graphs rather than storing matlatl output.
The runner constructs each graph through the public domain APIs and compares all
mechanisms with an eval-owned reference calculation: roots, reachability and
hops, structure tiers, SCC/WCC and bow-tie, HITS, PageRank, navigability,
betweenness, articulation points and bridges, trails, gaps, and suggestions.
The reference uses only the declared vertices and edges; emitted artifacts are
never fed back as expected data. `resolver.json` directly drives the domain
resolver and hand-authors each health, target, candidate, directory, and asset
probe expectation. Escape cases require an empty probe trace, proving no asset
lookup occurred.

`pipeline.json` runs the dedicated `fixtures/correctness-resolver/v1` tree through
matlatl's real scanner, parser, resolver, graph builder, findings analysis, and
emitter view. Its 22 hand-authored cases pin source path and line, parsed link
type, target and anchor text, resolution health and target data, projected edges,
and any reference or low-scent finding, including its `findings.json`
representation. The family covers deep relative and root-absolute paths,
local and cross-document fragments, duplicate heading slugs, directory links,
wikilink exact/alias/ambiguity behavior, external and protocol-relative links,
assets, missing and malformed empty destinations (which the parser emits as a
reference), percent-literal paths, and root escapes.
Default versus strict projection is asserted only for directory links, the
behavior currently controlled by `application.Config.Strict`.

The additional Phase C manifests isolate heuristic boundaries rather than
expanding the canonical graph snapshots:

- `scent.json` has 10 hand-authored cases for normalization and the strict
  Jaccard threshold, generic phrases and stopwords, code and stable-identifier
  exemptions, path-like anchors, section destinations, the `§` dialect, and
  source-line tie ordering. Token proofs list only the obvious tokens needed by
  each case; they do not duplicate the product phrase or stopword tables.
- `gaps.json` has 4 cases for WCC eligibility, the minimum component size, and
  lexicographic component-pair order. Large cap/truncation coverage remains in
  the normal CI test
  `internal/domain/graphmodel/gaps_test.go:TestDetectGaps_DoSBounded`; product
  test filenames are documentation only and are not part of machine manifests.
- `suggestions.json` has 7 cases covering candidate eligibility, coupling,
  co-citation, independently calculated Adamic/Adar values, linked-pair
  exclusion, the minimum-shared boundary, deterministic ties, and both sides of
  the fanout guard. The v1 oracle freezes defaults of 2 shared neighbours, 256
  maximum neighbour fanout, and 1000 results without importing product
  constants. Its eval-owned derivation enumerates the complete unordered pair
  universe, applies exclusions, thresholds, fanout filtering, ranking and caps,
  then compares the full result and flags with both hand-authored expectations
  and production output. Every case is rerun with reversed document and edge
  insertion and must produce byte-identical scores and order. Normal CI also
  runs `TestPredictLinks_CapTruncates` and
  `TestPredictLinks_MaxFanoutBoundary` in `linkprediction_test.go`; those test
  filenames are human-readable references, not machine-oracle fields.

`mutations.json` registers 8 small, separate fixture families under
`fixtures/correctness-mutations/v1`: hide/restore a sole inbound link,
remove/restore a sole outbound link, break/repair a document target,
break/repair an anchor, add/remove a redundant route, split/join components,
cross the far-from-root threshold, and weaken/restore an anchor. The runner
copies each immutable fixture to a fresh temporary directory. V1 records exact
transformations, not sampled/generated mutations, so no inert seed is stored.
The runner verifies both the whole-fixture tree SHA-256 and edited-file SHA-256
before changing it, requires one exact replacement, checks only the registered
local deltas, applies the inverse, and requires restoration
of the whole fixture tree hash, edited-file hash, normalized observations, and
emitted `graph.json` bytes. Unsafe paths, symlinks, absent or non-unique
replacements, and hash mismatches are rejected. Tests also hash the checked-in
fixture tree before and after the full oracle run.

The Phase D surface manifests complete the Level 1 contract:

- `backlinks.json` independently enumerates authored incoming edges for every
  document in `fixtures/correctness-surfaces/v1`. It pins sorted source paths,
  section-to-document collapse, duplicate-source de-duplication, exact
  `index.md` cells and `llms.txt` clauses, and the deliberate absence of any
  backlinks field in `graph.json`. Each rendered surface is compared directly
  with the authored expectation, never with another surface.
- `trails.json` has 4 hand-authored exact root/order cases spanning an SCC, a
  diamond DAG, symmetric frontier ties, and disconnected DAGs. The checks use
  the production emitter and pin trails schema v1 without serializing domain
  metrics back into the oracle.
- `determinism.json` runs the production pipeline and emitters three times over
  copies created in forward and reverse file order. `graph.json`,
  `findings.json`, `trails.json`, `index.md`, and `llms.txt` must be
  byte-identical. Before comparing bytes, each run proves the exact five artifact
  keys, machine schemas 7/8/1, known fixture document/finding/trail IDs, and
  independent text sentinels in both Markdown surfaces. Fixture mtimes are fixed,
  and no absolute path or timestamp is an oracle input. Every Phase A graph is
  also emitted after forward and reverse
  document/edge insertion; Phase C already performs the same reversal for every
  suggested-link case.

`eval oracle` reports 97 deterministic cases across 11 families, including the
canonical-navigation case. Product cap fixtures cited below remain independent
product tests and do not inflate the checked-in correctness-manifest count.

### Issue #34 Level 1 acceptance inventory

| Acceptance area | Independent oracle or explicit existing test |
| --- | --- |
| Identity, roots, reachability, hops, structure tiers, SCC/WCC, bow-tie | `graph.json` plus the eval-owned calculations in `reference.go`; 12 canonical graph cases |
| HITS, PageRank, navigability, betweenness, articulation points, bridges | `graph.json` plus independently calculated numeric/set expectations in `reference.go`; tolerance `1e-6` |
| Resolver containment, root-absolute/relative paths, fragments, directories, wikilinks, assets and external links | `resolver.json` (24 direct cases) and `pipeline.json` (22 scanner-to-view cases); escape cases require zero asset probes |
| Knowledge-gap eligibility/order and truncation cap | `gaps.json`; cap remains `internal/domain/graphmodel/gaps_test.go:TestDetectGaps_DoSBounded` |
| Suggested-link eligibility, scores, ties, fanout and cap | `suggestions.json`; cap/boundary remain `TestPredictLinks_CapTruncates` and `TestPredictLinks_MaxFanoutBoundary` in `internal/domain/graphmodel/linkprediction_test.go` |
| Information-scent normalization, threshold, exemptions, section candidates and ordering | `scent.json` (10 hand-authored cases) |
| Local sensitivity and inverse restoration | `mutations.json` (8 isolated reversible fixture families) |
| Backlinks on all shipped surfaces | `backlinks.json` and `fixtures/correctness-surfaces/v1`; exact independent checks of view, `index.md`, `llms.txt`, and graph omission |
| Trail roots/order and schema v1 | `trails.json` (4 exact SCC/DAG/tie/disconnected cases) |
| Full artifact and shuffled-input byte stability | `determinism.json`, all 12 Phase A graph cases, and all 7 Phase C suggestion cases |
| Bounds, malformed contracts and mutation safety | `correctness_test.go` loader, size, enum, path, hash, replacement and symlink tests |

There are no Level 1 acceptance gaps. This inventory references the existing
large cap and iteration tests instead of copying them into manifests.

The pipeline family is independent of agent evaluation: it is not a task corpus,
has no gold answer or agent outcome, does not use Nimbus or `demo/`, and does not
change the task/result/trajectory schemas. Expectations must be authored from
the documented parser/resolver contracts, never copied from matlatl output. The
runner may serialize actual observations only in-memory to test byte and order
determinism; those bytes are not a golden artifact. It reuses the real
`application.Result` and emitter `View` rather than defining an eval-owned mirror.

Set equality is exact and ranked output includes deterministic ID tie-breaking.
Numeric comparisons use the contract's frozen absolute tolerance of `1e-6`;
this accommodates iterative convergence without weakening discrete checks. Do
not replace these checks with full artifact snapshots or infer broad monotonic
quality claims from the canonical cases.

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
