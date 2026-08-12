---
title: "Heuristic evaluation: correctness, signal quality, and agent outcomes"
matlatl: orphan-intentional
---

# Heuristic evaluation: correctness, signal quality, and agent outcomes

> Status: **approved design extension for [#17](https://github.com/stacklok/matlatl/issues/17), not yet run.** This workstream is a sibling to the Stage A
> agent-outcome evaluation in
> [eval-harness-design.md](eval-harness-design.md), not a replacement or an
> expansion of its arms, endpoints, thresholds, or completion criteria. Nimbus
> supplies only controlled mechanics and correctness cases; real corpora supply
> usefulness evidence.
>
> Marked `orphan-intentional` so matlatl does not flag its own research note.

## 0. Claims and evidence levels

A deterministic implementation can be wrong, a mathematically correct signal
can be useless, and a useful-looking signal can fail to improve an agent's
work. Evaluate those claims separately:

1. **Deterministic correctness:** does the resolver, graph operation, metric, or
   emitted surface exactly implement its documented contract on independently
   specified input? This level uses canonical fixtures, hand-enumerated or
   independently computed oracles, invariants, and seeded reversible mutations.
2. **Signal quality and noise:** on labelled real documentation, does a finding,
   ranking, or recommendation identify something reviewers judge relevant and
   actionable without overwhelming them? This level uses hidden-link recovery,
   should-link labels, blind review, ranking baselines, and held-out data.
3. **Focused one-surface agent outcome:** after the cheaper levels pass, does
   exposing one surface change task success, repair quality, collateral edits,
   or cost against a matched control? Each experiment ablates one surface only;
   it is not the full factorial Stage A bundle study.

Evidence at one level **must not imply either of the others**. Exact PageRank
values do not prove that PageRank is a useful reading order; reviewer approval
does not prove an agent outcome; a task win does not repair a failed correctness
oracle. Report each level and each mechanism independently.

## 1. Evaluation units and independent oracles

### Canonical graph and resolver fixtures

Freeze small, inspectable fixtures covering relative, root-absolute, directory,
fragment-only, percent-encoded, external, escaping-root, missing-document, and
missing-anchor references; duplicate headings and the canonical slug dialect;
directed paths, cycles/SCCs, disconnected components, isolated documents,
multiple roots, hubs, authorities, diamonds, redundant paths, articulation
points, and bridges. Identity and graph expectations follow
[ADR 0001](../adr/0001-document-identity.md),
[ADR 0003](../adr/0003-security-model.md),
[ADR 0006](../adr/0006-slug-dialect.md),
[ADR 0007](../adr/0007-graph-node-semantics.md),
[ADR 0008](../adr/0008-directory-links.md), and
[ADR 0022](../adr/0022-root-absolute-links.md).

Apply seeded, reversible mutations to immutable copies: hide or restore one
link; break a path or anchor; remove the sole inbound or outbound edge; add a
redundant path; move a document beyond the far-from-root boundary; split or
join a component; weaken an anchor; and repair exactly one targeted finding.
The manifest records fixture bytes, seed, mutation, inverse, and expected delta.
Mutations must preserve root containment and the resource/security constraints
of [ADR 0003](../adr/0003-security-model.md).

Expected values come from construction, hand enumeration, or an independent
reference implementation with its algorithm and version frozen. **Never use a
previous or current matlatl artifact as matlatl's own oracle.** Comparing two
matlatl surfaces can detect disagreement, but cannot certify either one.

### Real-corpus labels

For recommendation and finding quality, sample both emitted items and eligible
non-emitted controls. Reviewers see source context but are blinded to mechanism,
score, threshold, and treatment. At least two reviewers independently label:
valid/invalid, should-link/no-link/uncertain, actionable/not-actionable/uncertain,
and likely repair scope. Adjudicate disagreements without revealing the
mechanism; retain original labels, adjudicated label, uncertainty, and reasons.
Report agreement and uncertain rates rather than forcing ambiguous examples
into positive or negative classes.

Hidden-link recovery removes known eligible links and asks whether rankings
recover them. It is only a proxy: existing links can be historical accidents,
and absent links are not negatives. Pair it with a labelled should-link/noise
review of hidden positives, unlinked high-ranked pairs, and sampled low-ranked
pairs. The eligibility rules, sampling frame, and exclusions are frozen before
scoring.

## 2. Inventory and treatment

The table is the complete shipped inventory plus explicitly gated future work.
“Outcome” always means a focused one-surface ablation after correctness and
quality pass; it does not add an arm or endpoint to Stage A.

| Mechanism or surface | Objective correctness | Empirical usefulness / noise | Focused causal outcome |
| --- | --- | --- | --- |
| **Reference resolution; broken links and anchors** | Exact resolved-edge and finding sets over canonical path/anchor fixtures; escaping-root and external-reference classification; mutation adds/removes only the expected edge/finding. | Blind review of real reported failures and sampled resolved controls for validity and actionability, with parser/dialect ambiguity retained as uncertainty. | With only broken-reference findings exposed, measure targeted repair, task success, cost, and collateral changes against raw-source/direct-grep control. |
| **Orphan, unreachable, under-linked, dead-end, bow-tie** | Hand-enumerated sets and bow-tie classes on directed fixtures, including exemptions and indeterminate reachability, per [ADR 0012](../adr/0012-graduated-structure-and-bowtie.md). | Label whether each finding represents a real discoverability problem and a feasible link action; stratify by class and corpus role. | Expose one finding kind at a time for a navigation or maintenance task; measure repair and downstream task success, not merely finding removal. |
| **`hopsFromRoot` / `far-from-root`** | Exact multi-source shortest-path distances, `-1` cases, threshold set, exemptions, and mutation deltas per [ADR 0021](../adr/0021-hops-from-root.md). | Review whether distant documents are genuinely hard to discover; compare labels and noise directly with under-linked on the same documents. | Ablate only hops/far findings in matched navigation or repair tasks; compare with an under-linked-only condition rather than treating either as ground truth. |
| **`knowledge-gap`** | Exact weakly connected component pairs, ordering, and caps on hand-enumerated graphs. | Label whether disconnected components should actually be connected; report actionability and noise rather than treating disconnection as a defect. | Expose only knowledge-gap advice when labels justify a focused repair task; measure correct link choice, collateral links, task success, and cost. |
| **`suggested-link`** | Exact eligible unlinked document pairs, Adamic/Adar, coupling, co-citation, sorted ties, truncation, and hub-skip behavior per [ADR 0013](../adr/0013-topology-link-prediction.md). | Hidden-link recovery plus labelled should-link/noise review. Compare random, alphabetical, degree, and common-neighbour rankings; report incremental value over each. | Expose only `suggest-links`; measure correct link choice, repair success, collateral links, task success, and cost. |
| **HITS, PageRank, trails, backlinks** | Independent numeric oracle within a frozen tolerance; exact stable tie/order checks; exact trail paths and backlinks over constructed graphs per [ADR 0016](../adr/0016-agent-experience.md). | Review whether top-ranked documents, backlink context, and trail order support plausible reading tasks. Compare random, alphabetical, degree, and topology-only trail order. | Remove or add exactly one of PageRank ordering, trails, or backlinks while holding content/pointer constant; measure navigation/comprehension success and cost. |
| **Navigability scalars** | Independently compute compactness, stratum, characteristic/median path length, clustering, and diameter on enumerated graphs per [ADR 0014](../adr/0014-navigability-metrics.md); compare exact values or frozen numeric error. | Test association with labelled navigability and task difficulty across held-out real corpora; do not call correlation a causal benefit. | Only a justified intervention that changes one structural property while controlling content may test an agent outcome; otherwise remain at Levels 1–2. |
| **Betweenness, articulation points, bridges** | Independent exact sets and numeric betweenness oracle on path, cycle, diamond, redundant-path, and disconnected fixtures per [ADR 0015](../adr/0015-critical-path-analysis.md). | Blindly assess whether ranked/set members are operationally load-bearing; include high-degree non-critical and low-degree critical controls. | Expose only `critical-docs` (or its absence) for a dependency/navigation task; measure exact task success and cost. |
| **Information scent / `low-scent-anchor`** | Exact tokenization, phrase/stopword handling, score, threshold classification, and stable source location over canonical anchors per [ADR 0016](../adr/0016-agent-experience.md). | Blind review of false-positive rate and whether replacement text would better predict the target; retain ambiguous generic-but-local anchors as uncertain. | Show only scent findings for anchor-rewrite tasks; measure valid repairs, target preservation, collateral edits, later navigation success, and cost. |
| **Findings / `fix-prompt`** | Exact finding selection, severity/kind ordering, requested scope, stable rendering, and exit behavior per [ADR 0005](../adr/0005-exit-code-contract.md), [ADR 0009](../adr/0009-fix-prompt-acting-agents.md), and [ADR 0020](../adr/0020-fix-prompt-scope.md). | Per-kind validity, noise, and actionability review; distinguish a correct prompt rendering from a useful underlying finding. | Raw findings versus the same findings in `fix-prompt`, with repair, remaining findings, new findings, collateral changes, task success, and cost. |
| **OKF mode** | Exact R1–R3 verdicts and mode-scoped findings on specification examples per [ADR 0023](../adr/0023-okf-conformance-mode.md). | **Not a heuristic:** this is specification conformance. Report conformance correctness and specification ambiguities, not precision, usefulness, or popularity. | No heuristic outcome claim. An agent repair study may test conformance remediation, but cannot redefine the OKF verdict. |
| **Title quality; section self-containedness** | **Future, gated, unshipped.** No shipped contract or correctness claim exists. | No evaluation until eligibility, oracle/label rubric, false-positive review, and holdout threshold contract are frozen. | No outcome experiment until a mechanism ships and cheaper levels pass. |

## 3. Metrics and baselines

Use metrics appropriate to the claim, always by mechanism, corpus, and relevant
stratum:

- **Correctness:** exact set equality; missing/extra elements; exact stable
  order; maximum/mean numeric error against the independent oracle; mutation
  sensitivity and reversibility.
- **Ranked signals:** Precision@K, Recall@K, MRR, and nDCG at frozen K values,
  with uncertainty and coverage. Compare **random, alphabetical, degree,
  common-neighbour, topology-only trail order, and direct grep** wherever each
  is applicable. A sophisticated method must beat the cheapest applicable
  baseline for the same candidate set and information access.
- **Human quality:** false-positive/noise rate, actionability rate, uncertain
  rate, inter-reviewer agreement, and adjudicated label distributions. Report
  selection and prevalence assumptions; do not silently discard uncertainty.
- **Focused outcomes:** task success, targeted repair rate, unresolved target,
  collateral changes (files, lines, links, and new findings), total tokens,
  tool calls, turns, wall time, and monetary cost under the frozen accounting.

For justified interventions, freeze the expected direction before applying the
mutation. Examples include adding an alternative route should not create an
articulation point, restoring the only inbound edge should remove an orphan, or
moving a document one edge farther from every root should not reduce its exact
shortest-path distance. These are local consequences of explicit preconditions,
not universal quality laws. **Do not assert universal monotonicity** such as
“more links always improve compactness or agent outcomes,” “higher PageRank is
always better,” or “removing a bridge always improves resilience.” Graph edits
can alter denominators, components, shortest paths, ranks, and legitimate
workflow boundaries in opposing directions.

## 4. Corpora, splits, and threshold discipline

Use three corpus roles:

1. **Canonical fixtures:** exhaustive correctness and mutation tests; prose
   naturalness is irrelevant.
2. **Nimbus:** constructed mechanics, reversible mutations, and independently
   enumerated expectations as specified in
   [nimbus-eval-corpus.md](nimbus-eval-corpus.md). It supplies no external-validity
   evidence.
3. **Pinned real repositories:** empirical usefulness and focused outcomes.
   Include adopted and un-adopted corpora where feasible; report that status and
   domain/size/topology strata.

Split real examples by repository, or by a frozen grouped split that prevents
near-duplicate documents and mutations of the same source from crossing
boundaries. Development data is available for candidate eligibility, thresholds,
K, and presentation tuning. Holdout labels stay sealed until code, baselines,
metrics, exclusions, and analysis are frozen. Never tune on held-out errors.

This design deliberately sets **no hard numeric acceptance thresholds**. Before
held-out scoring, publish a separate frozen manifest with justified correctness
tolerances, minimum quality/coverage, K values, uncertainty treatment, baseline
comparisons, focused-outcome detection rule, and costs. No manifest means no
held-out verdict.

Apply the decision sequence per mechanism:

- **Keep:** correctness passes and held-out quality beats applicable baselines
  with acceptable noise; retain the current exposure only if focused outcome
  evidence is required by its claim and does not show harm.
- **Tune:** correctness passes but development data identifies a pre-specified
  threshold/ranking/presentation change; re-freeze before one holdout read.
- **Demote:** correct but noisy, non-actionable, baseline-equivalent, or costly;
  reduce prominence, make opt-in, or present as data rather than advice.
- **Remove:** objectively incorrect and not repaired, or held-out/focused
  evidence shows unacceptable harm under the frozen rule.

For **under-linked versus hops/far-from-root**, evaluate both on the identical
eligible document set. Report overlap and discordant cases, per-signal label and
actionability rates, incremental ranking value, and focused navigation/repair
results. Keep both only if each contributes distinct held-out value; tune or
demote the redundant/noisier signal. Do not select a winner from constructed
correctness alone.

## 5. Reproducibility and reporting contract

Freeze and publish, before scoring:

- repository URL and commit SHA; license; file-content manifest and corpus role;
- matlatl SHA/config and artifact hashes; fixture/oracle implementation and
  version; mutation seeds, inverses, and hashes;
- candidate universe, eligibility/exclusion rules, split assignment, sampling
  weights, labels, reviewer protocol, blinding, adjudication, and uncertainty;
- every baseline, threshold candidate, K, metric, numeric tolerance, random seed,
  focused arm, task/gold/rubric, model/agent/harness version, limits, and cost
  accounting;
- immutable run records and trajectories, software/environment versions, and a
  deterministic regeneration command.

Results must identify the evidence level and never collapse levels into one
“validated” verdict. Publish all registered mechanisms, nulls, failures,
uncertainty, exclusions, baseline results, development choices, holdout results,
and deviations. Report aggregate and per-corpus/per-kind results; preserve raw
review labels and append-only run records. A correction creates a new version,
not a silent overwrite.

## 6. Implementation sequence

1. Freeze canonical graphs, resolver cases, mutation schema, and independent
   oracles; make every shipped mechanism pass the correctness gate.
2. Build the candidate/label export and blinded review protocol; pilot only the
   mechanics, then discard pilot labels or keep them development-only.
3. Freeze real corpora and grouped development/holdout splits.
4. Run hidden-link recovery and false-positive/actionability review on
   development data; compare all applicable cheap baselines.
5. Tune only registered parameters on development data; publish and sign the
   separate scoring manifest.
6. Score held-out quality once and assign keep/tune/demote/remove under the
   frozen rules.
7. Run a focused one-surface ablation only for surfaces that passed cheaper
   levels and whose causal claim justifies agent cost. Do not build a factorial
   substitute for Stage A.
8. Publish level-separated results and feed roadmap decisions without changing
   Stage A's completion contract.

## 7. Freeze checklist

- [ ] Binding ADR versions and shipped inventory verified; future/unshipped
      mechanisms remain gated.
- [ ] Canonical fixture bytes, construction manifests, independent oracles,
      numeric tolerances, mutation seeds/inverses, and expected deltas frozen.
- [ ] Security/root-containment cases included; generated outputs are not used
      as their own oracle.
- [ ] Candidate universe, hidden-link eligibility, exclusions, applicable
      baselines, K values, and metrics frozen.
- [ ] Real corpora pinned and grouped development/holdout split sealed; no
      source family or mutation crosses the split.
- [ ] Blind multi-reviewer rubric, sampling, adjudication, agreement, and
      uncertainty treatment frozen.
- [ ] Correctness gate and keep/tune/demote/remove thresholds justified in the
      separate scoring manifest; **no held-out data inspected yet**.
- [ ] Under-linked versus hops/far comparison registered on the same eligible
      documents.
- [ ] Every intervention has explicit preconditions and a justified local
      direction; no universal monotonic claim is registered.
- [ ] Any focused experiment changes exactly one surface, has a matched control,
      and records task success, repair, collateral changes, and cost.
- [ ] Reproduction tuple, immutable record format, reporting strata, deviations
      policy, and maintainer signature/date frozen.

## 8. Binding sources

The evaluation does not redefine product behavior. The binding contracts remain
[ADR 0001](../adr/0001-document-identity.md),
[ADR 0003](../adr/0003-security-model.md),
[ADR 0005](../adr/0005-exit-code-contract.md),
[ADR 0006](../adr/0006-slug-dialect.md),
[ADR 0007](../adr/0007-graph-node-semantics.md),
[ADR 0008](../adr/0008-directory-links.md),
[ADR 0009](../adr/0009-fix-prompt-acting-agents.md),
[ADR 0012](../adr/0012-graduated-structure-and-bowtie.md),
[ADR 0013](../adr/0013-topology-link-prediction.md),
[ADR 0014](../adr/0014-navigability-metrics.md),
[ADR 0015](../adr/0015-critical-path-analysis.md),
[ADR 0016](../adr/0016-agent-experience.md),
[ADR 0020](../adr/0020-fix-prompt-scope.md),
[ADR 0021](../adr/0021-hops-from-root.md),
[ADR 0022](../adr/0022-root-absolute-links.md), and
[ADR 0023](../adr/0023-okf-conformance-mode.md). If this design and an ADR
differ, the ADR wins; amend the evaluation contract rather than its oracle.
