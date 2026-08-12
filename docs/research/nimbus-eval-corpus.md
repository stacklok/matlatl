---
title: "Nimbus: a hermetic calibration corpus for the agent-outcome eval"
matlatl: orphan-intentional
---

# Nimbus: hermetic calibration-corpus specification

> Status: **specification, not yet built.** Nimbus is a future, separate
> 40–60-document synthetic corpus for the agent-outcome harness
> ([eval-harness-design.md](eval-harness-design.md),
> [eval-preregistration.md](eval-preregistration.md), and the sibling
> [heuristic-evaluation.md](heuristic-evaluation.md), issue
> [#17](https://github.com/stacklok/matlatl/issues/17)). It supports
> correctness fixtures, seeded mutations, and harness mechanics only — it
> checks whether the harness detects differences planted by construction and
> stays flat where it plants none. It **cannot establish external validity**: nothing measured on Nimbus generalizes to real documentation.
> The frozen real corpora remain the outcome set; matlatl itself is smoke only.
>
> **Provenance — Nimbus is not `demo/corpus/nimbus-docs`.** The nine-doc demo
> tree is a deliberately-broken stage prop (broken links, an orphan,
> unreachable docs) for lighting up the matlatl report on stage. Nimbus
> borrows the fictional name only; it is a new, independently frozen artifact
> built to this spec. The demo corpus is a seed/reference and is not expanded
> or modified for Nimbus.
>
> Marked `orphan-intentional` so matlatl doesn't flag its own research note.

## 1. Purpose

Nimbus exists to answer **instrumentation questions** about the harness before
and alongside the real-corpus runs:

- **Mechanics:** does artifact injection, trajectory recording, scoring, and
  the judge pipeline work end-to-end on a corpus whose ground truth is known
  by construction?
- **Internal validity:** when a navigation effect is *planted* (§5 mutations),
  do the harness's arms detect it, in the right stratum, at the right sign?
  When nothing is planted, does the harness report flat?
- **Scorer trust:** do the deterministic scorers and the LLM-judge agree with
  the constructed gold answers, so that disagreement on real corpora is
  interpretable as task ambiguity rather than scorer drift?

## 2. Non-goals

- **No external validity.** Nimbus results never appear in agent-outcome
  endpoints, decision rules, or the pre-registration's contrasts. A sentence
  like "the bundle improved success on Nimbus, therefore it helps on real
  repos" is a category error this spec exists to prevent.
- **Not a benchmark.** No leaderboard, no cross-model comparison, no CI gate.
- **Not a demo.** It does not replace or extend `demo/corpus/nimbus-docs`; it
  is not optimized to "light up" a report.
- **Not a power fix.** Nimbus does not raise the harness's detection floor on
  real corpora; it de-risks the plumbing, not the statistics.

## 3. Corpus construction

- **Size: 40–60 markdown documents**, natural in register — real prose with
  headings, paragraphs, and cross-references, written to read like a small
  project's documentation (install, configure, deploy, operate, concepts,
  troubleshooting), not lorem ipsum or link farms. Naturalness matters because
  the agent and the judge must behave as they would on real docs; size is
  bounded so the whole corpus is inspectable by one reviewer.
- **Controlled topology.** The link graph is designed, not emergent: a frozen
  adjacency list defines every link. The base topology includes, by
  construction:
  - a maximum root-to-leaf depth of **at least 7** (so `hopsFromRoot`,
    `far-from-root`, and navigation tasks have room to vary),
  - **at least one cycle / strongly connected component**, so SCC handling and
    path-finding over cycles are exercised,
  - **a controlled weak or disconnected component** where useful, so
    `hopsFromRoot = -1` (unreachable) and reachability-indeterminate cases
    exist by construction and are enumerable,
  - **known hubs and authorities** (high in-/out-degree docs), so PageRank and
    HITS rankings can be checked against constructed expectations,
  - **a known number of articulation points and bridges**, derived from the
    construction manifest and checked by hand or an independent graph oracle,
    so `critical-docs` can be checked independently of the ranking expectations
    above — never copy either expectation from prior matlatl output,
  - **a deliberate minority of orphans, under-linked docs, and dead ends**
    (known, enumerable, and distinct from the demo corpus's accidental
    breakage),
  - **similar vocabulary among both linked and unlinked doc pairs**, so future
    lexical baselines and the shipped topology-only `suggested-link` predictor
    can be compared on true-positive and look-alike-negative pairs,
  - **grep-easy unique facts**: rare, distinctive strings planted in exactly
    one doc each, retrievable with a single `grep` — the control-shape
    substrate (§7),
  - **facts distributed across docs for synthesis**: comprehension questions
    whose answers require combining statements from two or more linked docs,
    so synthesis tasks reward navigation rather than single-doc lookup,
  - link density tuned so matlatl's navigability scalars land mid-range —
    neither the matlatl smoke corpus's shallowness nor a hairball.
- **The clean base may reserve broken links/anchors for mutations.** The base
  corpus is not required to be finding-clean: it may carry deliberately
  reserved broken-link and broken-anchor sites that exist only so the §5
  mutations have known, enumerable instances to repair. Such reserved sites
  are listed in the expectation manifest like any other constructed feature.
- **Deterministic generation.** Any templated or generated content is produced
  by a seeded, checked-in generator; regenerating from the same seed is
  byte-identical. Hand-written content is simply frozen. Either way, the
  corpus's bytes are reproducible from the repo.
- **Independent expectations.** Every expected graph value comes from the
  construction manifest, hand enumeration, or a frozen independent oracle.
  Prior or current matlatl output is never used as matlatl's own oracle;
  comparing it with the expectation is the test.

## 4. Task shapes

Nimbus carries the same four shapes as the real-corpus task set
([design note §1](eval-harness-design.md)), so calibration reads transfer
shape-by-shape:

| Shape | Nimbus instance | Gold |
| --- | --- | --- |
| Find-the-doc navigation | "Which file documents X?" for planted X | exact repo-relative path, known by construction |
| Repo-comprehension QA | single-document retrieval and cross-document synthesis questions | gold answer text with atomic claims tied to the source docs |
| Doc-maintenance | the deterministic mutations of §5 | programmatic `matlatl check` result |
| Grep-favorable control | facts greppable in one call | exact string match |

## 5. Deterministic mutations

The calibration lever: **seeded, reversible mutations** applied to copies of
the frozen base corpus (the base itself never mutates). Each mutation is a
function of the mutation seed and is logged in the run record:

- **link-break:** rewrite a chosen link target to a nonexistent path → a known
  broken-link finding.
- **orphan-create:** remove the sole inbound link to a chosen doc → a known
  orphan.
- **staleness-inject:** rewrite a section reference to a renamed heading → a
  known broken-anchor finding.
- **nav-degrade:** remove a hub doc's outbound links → a known navigability
  change (measurable in compactness/stratum).
- **redundant-path-add/remove (future):** add or remove an alternate route while
  preserving endpoints → known articulation, bridge, and shortest-path deltas.
- **hidden-link (future):** remove an eligible real edge while retaining its
  source/target labels in the private manifest → a known link-recovery positive;
  look-alike unlinked pairs remain explicit negative/uncertain controls.

Because the mutations are enumerated, the harness's expected findings are
computable *before* any run: that is what makes "did the harness detect what
we planted?" a deterministic question.

## 6. Gold and scorer isolation

- Gold answers, mutation manifests, and all scoring code live **outside the
  agent sandbox**, in the eval tree — the same contamination rule as the real
  corpora ([design note §4](eval-harness-design.md)).
- The agent sees only the (possibly mutated) corpus checkout plus the task
  prompt. It can never read the adjacency list, the mutation manifest, or the
  gold answers.
- Scorers for Nimbus are the same code paths as the real-corpus scorers — that
  identity is the point of the calibration.

## 7. Treatment/task coverage matrix

Every harness surface and every task shape has at least one representative
Nimbus task whose expected calibration behavior is known by construction. The
matrix below is a **mechanics and harness-sensitivity contract**, not evidence
that a surface is useful on real documentation. Any directional difference is
created deliberately by the task construction; detecting it shows only that the
harness can detect its planted treatment. At freeze, each row exists as at least
one task, and the expected behavior column is what the harness must show for
that calibration check to pass.

| Treatment / surface | Representative task (feature) | Expected relevant arm | Scorer | Planted mechanics / sensitivity check |
| --- | --- | --- | --- | --- |
| **`llms.txt`** (catalog + backlinks) | find-the-doc navigation to a mid-depth doc ("which file documents X?") | `+all` | exact repo-relative path | A deliberately artifact-addressable navigation task makes `+all` > `baseline`; a grep control with no planted artifact advantage stays flat. |
| **`trails.json`** (reading-order trails) | comprehension task whose answer requires synthesizing facts distributed across adjacent trail docs | `+trails` (vs `pointer-only`) | LLM-judge vs gold (strict protocol) | A task constructed around the trail makes `+trails` ≥ `pointer-only`; the instrumentation also records any over-reading on single-fact tasks. |
| **Pointer-only instruction** | any navigation or comprehension task | `pointer-only` (vs `baseline`) | per shape | A task with no planted pointer advantage stays approximately flat; a pointer effect invalidates calibration of pointer-carrying arms. |
| **MCP `what-links-to` / `path-between`** | navigation tasks whose gold path crosses a cycle/SCC, plus a disconnected pair whose gold result is “no path” | `+all` | exact path or exact no-path result | The planted reachable path and unreachable pair are returned correctly, and a healthy `serve` produces no `mcp-failure`. |
| **MCP `get-section`** | anchor-resolution task: identify the title, level, and section node for a known `doc#slug` | `+all` | exact structured metadata | A task constructed for section lookup exercises the tool and token instrumentation without claiming that `get-section` returns the section body. |
| **MCP `corpus-summary`** | corpus-level orientation question ("which docs are the most load-bearing here?") | `+all` | LLM-judge vs gold (gold = constructed hubs/articulation points) | The arm can recover the independently constructed hubs/articulation points through the surface. |
| **MCP `suggest-links`** | doc-maintenance adjacent: choose the doc pair topology predicts (the planted `suggested-link` case) | `+all` | programmatic vs the constructed `suggested-link` pair | The planted pair is exposed and the look-alike unlinked pair is scored separately to exercise false-positive accounting. |
| **MCP `critical-docs`** | "which docs are articulation points / bridges?" | `+all` | exact set match vs constructed topology | The surface exactly matches the independently constructed articulation-point/bridge sets. |
| **findings / `fix-prompt`** | doc-maintenance tasks over the §5 mutations (link-break, orphan-create, staleness-inject, nav-degrade) | separate focused `+findings` or `+fix-prompt` condition, outside Stage A | programmatic: targeted finding resolved, `matlatl check` exit 0, no collateral findings | A planted mutation is resolved when its finding or fix prompt is supplied; reserved break sites (§3) change only when targeted. This row calibrates the future focused condition, not `+all`. |
| **Direct grep (control)** | grep-favorable tasks over the planted unique facts | none — control | exact string match | With no planted artifact advantage, success stays flat; instrumentation must expose any added token cost in artifact arms. |

The **expected relevant arm** column names where the surface is present; it does
not predict real-world benefit. Surfaces absent from an arm (e.g. MCP tools in
`+trails`, trails in `pointer-only`) must show no planted surface-specific
response there — that flatness is a mechanics check. Passing any row establishes
harness sensitivity only, never signal quality or external usefulness.

Coverage is verified at freeze time by running `matlatl emit` on the base
corpus and diffing the findings against the constructed-expectation manifest,
and by walking this matrix against the frozen task list: every row has at
least one task, and every task maps to exactly one row.

## 8. Hermeticity and freeze manifest

- **Hermetic:** the corpus plus its generator, mutation functions, gold
  answers, and expectation manifest are fully self-contained in the eval tree.
  No network, no external checkout, no dependency on any repo state at run
  time.
- **Freeze manifest:** a checked-in manifest lists every corpus file with its
  SHA-256, plus the generator seed and the mutation seed. Freeze = the
  manifest matches the tree byte-for-byte; any later edit is a new manifest
  version, never an in-place change.
- The manifest is what the pre-registration's execution tuple references when
  Nimbus runs are recorded.

## 9. Limitations

- **Synthetic prose.** However natural the writing, a constructed corpus
  under-represents the ambiguity, redundancy, and rot of real documentation.
  Judge agreement calibrated on Nimbus is an upper bound on judge agreement in
  the wild.
- **Constructed difficulty.** Task difficulty is chosen, not discovered; the
  corpus cannot reveal tasks that are hard for reasons nobody planted.
- **Single topology family.** One base graph plus mutations samples a narrow
  slice of documentation shapes — even though the §3 topology is built so
  every finding kind and metric the harness consumes has at least one known
  instance. Negative calibration results ("harness missed a planted effect")
  are informative; positive ones are necessary-but-not-sufficient.
- **No outcome claims.** Restating §2: Nimbus feeds no endpoint, contrast, or
  decision rule in the pre-registration.

## 10. Freeze checklist

- [ ] 40–60 documents written; register reviewed as natural by one reviewer.
- [ ] Base adjacency list frozen; topology properties (depth, articulation
      points, bridges, density) derived from the construction manifest and
      verified by hand or an independent oracle, then compared with `matlatl
      emit` output.
- [ ] Generator (if any) checked in; regeneration from seed verified
      byte-identical.
- [ ] Mutation functions implemented and enumerated; expected-findings
      manifest computed.
- [ ] Gold answers authored from source docs only; second-reviewer verified;
      confirmed absent from the sandbox.
- [ ] Signal-coverage diff (§7) clean.
- [ ] Freeze manifest written; SHA-256s match the tree.
- [ ] Recorded in the pre-registration as calibration-only, with the
      external-validity disclaimer restated in any document that cites a
      Nimbus number.
