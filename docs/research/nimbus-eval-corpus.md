---
title: "Nimbus: a hermetic calibration corpus for the agent-outcome eval"
matlatl: orphan-intentional
---

# Nimbus: hermetic calibration-corpus specification

> Status: **specification, not yet built.** Nimbus is a future, separate
> 40–60-document synthetic corpus for the agent-outcome harness
> ([eval-harness-design.md](eval-harness-design.md),
> [eval-preregistration.md](eval-preregistration.md), issue
> [#17](https://github.com/stacklok/matlatl/issues/17)). It **calibrates
> mechanics and internal validity** — it proves the harness detects effects it
> plants and stays flat where it plants none. It **cannot establish external
> validity**: nothing measured on Nimbus generalizes to real documentation.
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
  - **known hubs and authorities** (high in-/out-degree docs), so
    PageRank/HITS ranking and `critical-docs` output can be checked against
    constructed expectations,
  - **a known number of articulation points and bridges** (verifiable against
    matlatl's own critical-path output),
  - **a deliberate minority of orphans, under-linked docs, and dead ends**
    (known, enumerable, and distinct from the demo corpus's accidental
    breakage),
  - **similar vocabulary among both linked and unlinked doc pairs**, so the
    `suggested-link` lexical-scent and topology signals are tested against
    both true-positive and look-alike-negative pairs,
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
  regression (measurable in compactness/stratum).

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
matrix below is the coverage contract: at freeze, each row exists as at least
one task, and the expected behavior column is what the harness *must* show for
calibration to pass.

| Treatment / surface | Representative task (feature) | Expected relevant arm | Scorer | Expected calibration behavior |
| --- | --- | --- | --- | --- |
| **`llms.txt`** (catalog + backlinks) | find-the-doc navigation to a mid-depth doc ("which file documents X?") | `+all` | exact repo-relative path | `+all` > `baseline` success on navigation tasks; no success change on grep controls |
| **`trails.json`** (reading-order trails) | comprehension task whose answer requires synthesizing facts distributed across adjacent trail docs | `+trails` (vs `pointer-only`) | LLM-judge vs gold (strict protocol) | `+trails` ≥ `pointer-only` on synthesis tasks; token cost on single-fact tasks does not fall (over-reading is measurable if it occurs) |
| **Pointer-only instruction** | any navigation or comprehension task | `pointer-only` (vs `baseline`) | per shape | approximately flat success vs `baseline`: the pointer text alone is not an active ingredient; a material `pointer-only` effect invalidates the Nimbus calibration of every pointer-carrying arm |
| **MCP `what-links-to` / `path-between`** | navigation task whose gold path crosses a cycle/SCC or a disconnected component (`hopsFromRoot = -1` doc) | `+all` | exact repo-relative path | `+all` resolves cross-component and in-cycle questions correctly; no attempt classifies as `mcp-failure` under a healthy `serve` |
| **MCP `get-section`** | section-targeted QA: answer is inside one named heading's section of a long doc | `+all` | LLM-judge vs gold | `+all` answers section questions with fewer tokens than full-doc reading; success flat or better vs `baseline` |
| **MCP `corpus-summary`** | corpus-level orientation question ("which docs are the most load-bearing here?") | `+all` | LLM-judge vs gold (gold = constructed hubs/articulation points) | `+all` names the constructed hubs/articulation points; `baseline` guesses or over-reads |
| **MCP `suggest-links`** | doc-maintenance adjacent: choose the doc pair topology predicts (the planted `suggested-link` case) | `+all` | programmatic vs the constructed `suggested-link` pair | `+all` identifies the planted pair; the look-alike unlinked pair with similar vocabulary is a known false-positive risk and is scored separately |
| **MCP `critical-docs`** | "which docs are articulation points / bridges?" | `+all` | exact set match vs constructed topology | `+all` matches the constructed articulation points/bridges exactly |
| **findings / `fix-prompt`** | doc-maintenance tasks over the §5 mutations (link-break, orphan-create, staleness-inject, nav-degrade) | `+all` (and any arm carrying findings) | programmatic: targeted finding resolved, `matlatl check` exit 0, no collateral findings | mutation tasks are resolved in the findings-carrying arms; the unmutated reserved break sites (§3) are repaired only when targeted, never as collateral |
| **Direct grep (control)** | grep-favorable tasks over the planted unique facts | none — control | exact string match | **grep controls do not benefit from artifacts**: success flat across all arms; any token-cost rise in artifact arms on these tasks is the planted generated-context harm signature and must be visible as such |

The **expected relevant arm** column names the arm in which the surface is
present and should help; surfaces absent from an arm (e.g. MCP tools in
`+trails`, trails in `pointer-only`) must show no corresponding effect there —
that flatness is itself a calibration check.

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
      points, bridges, density) verified against `matlatl emit` output.
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
