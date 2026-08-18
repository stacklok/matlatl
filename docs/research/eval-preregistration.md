---
title: "Agent-outcome eval: pre-registration (unfrozen)"
matlatl: orphan-intentional
---

# Pre-registration: Experiment A

> Status: **UNFROZEN DRAFT.** This contract governs #17 Experiment A only and
> must be signed before the first paid measured run. Until then every field marked
> **PENDING MAINTAINER FREEZE** may change without constituting an outcome-aware
> amendment because no treatment outcome may be observed. After signature,
> deviations are append-only amendments in `eval/results.md`; this file is not
> silently edited. Completed Milestone 1 isolated fake execution freezes no
> measured container/image policy or outcome-study value. Nimbus,
> zero-cache/provider telemetry, qualification, real corpora, schedule/retries,
> and paid execution remain unimplemented.

## 1. Claim and research questions

**Finite benchmark:** the eligible and selected task families to freeze in
exactly two pinned repositories. The estimand and claims apply to that finite
benchmark, not all coding tasks or repositories; external validity is limited.

- **RQ1 (sole confirmatory question):** what is the pooled task-family-level
  change in deterministic private-verifier success for `+all` versus `baseline`
  on the frozen benchmark?
- **RQ2 (prespecified secondary cost):** what is the task-family-level change in
  mean billed spend per scheduled attempt for `+all` versus `baseline` on that
  benchmark?
- **RQ3 (descriptive heterogeneity/mechanism):** what are corpus and mutually
  exclusive primary-stratum estimates, and how often are `llms.txt`,
  `trails.json`, and each matlatl MCP tool accessed in `+all`?

Experiment A estimates matlatl's marginal bundle effect. It does not estimate the
causal effect of documentation health. A later Experiment B must manipulate
healthy/degraded documentation under controlled tasks. Adopted versus unadopted
repositories are not a causal comparison for Experiment B.

A null is reported as “no effect detected at the frozen detection floor,” not
equivalence. Correctness, #33 signal quality/link recovery, Nimbus calibration,
and focused artifact studies cannot enter these endpoints.

## 2. Defined terms

- **Task:** one authored coding change with one private verifier.
- **Task family:** the inferential cluster containing every task sharing any
  documented constraint, subsystem/change, mutation, or verifier lineage.
- **Repetition:** one scheduled execution of a family-arm; repetitions and
  multiple tasks within a family reduce noise but do not increase `N`.
- **Arm:** human labels `baseline` and `+all`; their machine/config identifiers
  are respectively `baseline` and `all`.
- **Treatment exposure:** sending the first model request for a scheduled run.
- **Family-level success:** successful scheduled attempts divided by all scheduled
  attempts in one family-arm; every post-exposure failure is unsuccessful.
- **Family-level finite cost:** total externally reconciled billed spend for all
  attempts assigned to a family-arm—including unsuccessful attempts and allowed
  pre-exposure retry spend—divided by scheduled attempts; missing post-exposure
  billed cost receives the frozen per-attempt cap.
- **Arm-level cost per success:** total arm billed spend divided by successful
  completions; descriptive only and infinite/undefined at zero successes.
- **Paired contrast:** the `+all − baseline` family-level difference after every
  task and repetition in the family is aggregated.
- **Pilot/qualification run:** any pre-freeze or baseline-only candidate-selection
  run. It supplies mechanics/cost inputs only and never an outcome endpoint.

## 3. Arms

Stage A contains exactly two conditions:

| Human label | Machine/config ID | Frozen contents |
| --- | --- | --- |
| `baseline` | `baseline` | Immutable prepared repository plus normalized native context, with all pre-existing matlatl artifacts and matlatl MCP entries removed |
| `+all` | `all` | Byte-identical baseline plus freshly generated root `llms.txt`, root `trails.json`, remote streamable-HTTP matlatl MCP, and one frozen availability notice |

The notice is part of the `+all` bundle and does not prescribe use. Prompts,
native instructions, source and documentation bytes, tools, model, limits, and
environment are otherwise identical. MCP is remote streamable HTTP at a loopback
`/mcp` endpoint; stdio/local MCP is prohibited.

`pointer-only`, `+trails`, `+llms`, and `+MCP` are out of scope. The only
automatic trigger for a separately preregistered attribution study is the frozen
decision rule applied to the pooled frozen-benchmark success contrast. Cost,
corpus, and task-stratum results cannot trigger it. After a null, a default-bundle
study requires separate funding and signature. This contract cannot expand its
arms in response to a null.

## 4. Corpora and coding tasks

**PENDING MAINTAINER FREEZE:** select and pin exactly two outcome repositories.
The matlatl smoke row is not a third outcome corpus.

| Corpus | Role | Repository/SHA | License | Image/content hashes |
| --- | --- | --- | --- | --- |
| PENDING | outcome 1 | PENDING | PENDING | PENDING |
| PENDING | outcome 2 | PENDING | PENDING | PENDING |
| matlatl | smoke unless selected above under the same criteria | PENDING | Apache-2.0 | PENDING |

For each outcome repository, freeze the complete eligible-task sampling frame and
a reproducible selection procedure before scheduling. Adoption status and graph
health may be descriptive corpus attributes only.

Every measured task requires code edits and deterministic private verification
with hidden tests plus normal repository checks. Each task's intended solution
must depend on documented behavioral, security, configuration, or architecture
constraints. No headline endpoint uses a model judge.

Assign exactly one primary stratum per task by frozen objective precedence:

1. **grep-friendly coding control** when the frozen direct-search query/result
   burden criterion holds;
2. otherwise **cross-document synthesis** when verifier correctness requires
   constraints from at least two documents;
3. otherwise **navigation-heavy coding** when the frozen retrieval-burden
   criteria hold; and
4. otherwise **single-document constraint**.

Freeze the direct-search queries and thresholds, retrieval-burden criteria, and
document evidence. An independent reviewer confirms eligibility and the primary
classification before scheduling. Exploratory multilabel descriptors may be
retained, but cannot select confirmatory subgroups.

Before scheduling, group tasks into independent task families. Tasks sharing any
documented constraint, subsystem/change, mutation, or verifier lineage are one
family. If multiple tasks are retained in a family, schedule and bootstrap the
family as one cluster. `N` means the number of independent task families.

Task authors cannot inspect generated treatment artifacts. A second reviewer
checks the documented dependency, family assignment, instruction clarity, hidden
verifier, expected change scope, and absence of gold from the sandbox. Navigation-
only, QA, and document-repair tasks are excluded from the measured task set.

**PENDING MAINTAINER FREEZE:** eligible frames, selection procedure, task and
family inventory, objective classification rules/evidence, stratum balance,
task/gold/verifier hashes, normal-check commands, independent-review signatures,
and exclusion rules.

## 5. Model/provider qualification

Before treatment generation or exposure, qualify a frozen candidate list using
baseline-only disposable Nimbus coding tasks. Nimbus tasks used here are not real
outcome tasks and are never reused for treatment estimation.

The signed qualification manifest freezes:

- candidate model IDs/versions, providers, endpoints, and pinned OpenCode version;
- deterministic private-verifier competence threshold;
- tool/protocol reliability threshold;
- required telemetry completeness, including explicit zero cache counters;
- qualification spend cap and projected measured-run budget cap;
- projected-cost formula and sample used to estimate it; and
- deterministic tie-break order.

Candidates must pass every threshold, including observed explicit zero cache-read
and cache-write counters before any measured run. Among passers, choose the lowest
projected cost; use the tie-break only if projected costs are equal. Do not
generate or expose `+all`, compute treatment differences, or choose a candidate
based on matlatl responsiveness.

**PENDING MAINTAINER FREEZE:** all fields above and maintainer acceptance of the
selected model/provider.

## 6. Budget, power, tasks, and repetitions

Before schedule freeze, produce a signed joint simulation/calculation over
financially feasible `2 × N × r` designs (`N` independent task families, `r`
scheduled repetitions per family-arm). Begin comparisons at `r = 2`; there is no
fixed target family or repetition count.

The calculation uses the exact proposed estimands and procedures. It jointly
simulates pooled family-level success and family-mean billed spend per scheduled
attempt, including their correlation, baseline success and paired deltas, within-
family agent variance, cost variance and tails, family/corpus/stratum
heterogeneity, post-exposure failures, allowed pre-exposure retry spend,
conservative cost-cap assignments, and qualification cost evidence. Each
simulation applies the frozen family clustering, corpus weighting, interval
method, confirmatory decision rule, and secondary finite-cost procedure.

The budget reserves qualification spend and bounded pre-exposure retry capacity.
Select the feasible design that meets the frozen success-power and finite-cost
precision/power requirements at lowest projected cost, favoring larger `N` over
larger `r` when performance is near-equal under the frozen tolerance. If targets
are unaffordable, raise and disclose the detection/precision floors and recompute.

**PENDING MAINTAINER FREEZE:** sensitivity grid and joint simulation code/hash,
near-equal tolerance, qualification/retry reserves, per-attempt cost cap, maximum
paid budget, chosen `N` and `r`, total run ceiling, target power, success and
finite-cost detection/precision floors, exact intervals/decision procedures, and
maintainer signature/date.

## 7. Randomization

Use balanced AB/BA order for each task family (`A = baseline`, `B = +all`, machine
ID `all`). A seeded schedule balances first position within corpus × primary-
stratum blocks where family counts permit and records unavoidable imbalance. All
tasks from one family remain linked to that scheduled cluster. Each execution
uses fresh isolated containers; no workspace or model session crosses arms or
repetitions.

The executor consumes the frozen schedule verbatim. Replacement attempts retain
the scheduled arm/order identity and link to the failed attempt.

**PENDING MAINTAINER FREEZE:** assignment algorithm/version, seed, schedule hash,
and balance diagnostics.

## 8. Isolation, caching, and telemetry

Caching is disabled in OpenCode and at the provider. Baseline-only qualification
must first prove `cache-read = 0` and `cache-write = 0`. Measured records retain
missing/nonzero values and apply the post-exposure intention-to-treat and frozen
stop/abort rules in §9; such records are not silently invalidated or excluded.

Every attempt records:

- uncached input tokens, output tokens, cache-read, cache-write, and monetary
  cost in the provider's billed currency;
- wall time, turns, and total tool calls;
- filesystem accesses to root `llms.txt` and root `trails.json`;
- separate call counts for `what-links-to`, `list-orphans`, `path-between`,
  `get-section`, `corpus-summary`, `suggest-links`, and `critical-docs`;
- full trajectory, first-model-request timestamp, final answer, edits/diff,
  process/check outputs, private verifier result, and terminal class; and
- every frozen identity listed in §12.

Fresh containers expose only the prepared task environment. Gold patches, hidden
verifier sources/expectations, scorer internals, host files, other attempts, and
provider credentials beyond the run's minimum secret channel are inaccessible.
Records are append-only and secrets are never logged.

**PENDING MAINTAINER FREEZE:** provider cache-disable proof, telemetry field
mapping, access-counter implementation, container/image policy, network policy,
gold-isolation tests, resource limits, and record schema/version.

## 9. Failures and retries

Treatment exposure begins when the first model request is sent. Only provisioning,
provider, or evaluator failures conclusively occurring before exposure may receive
a bounded fresh retry. Each retry has a new attempt ID, retry-parent link, fresh
container, and fresh provider request; all retry spend remains assigned to the
original scheduled run.

After exposure, `environment-failure`, `mcp-failure`, `provider-failure`,
`evaluator-failure`, tool/runtime/telemetry failure, agent timeout, budget
exhaustion, and protocol failure are unsuccessful outcomes in the assigned arm.
They are not retried or excluded from the primary intention-to-treat analysis.
Missing or nonzero cache counters after exposure follow the same rule.
`invalid-task` stops affected analysis; amendment/refreeze and the complete
affected schedule are required.

Freeze a stop/abort rule before measurement for systematic or differential cache
violations or missing telemetry. Every violation and cost remains reported. A
secondary per-protocol analysis may diagnose mechanically complete runs, but
cannot replace or modify assigned-arm primary outcomes.

**PENDING MAINTAINER FREEZE:** first-request timestamp semantics, maximum
pre-exposure attempts per scheduled run, responsibility mapping, systematic and
differential cache/telemetry stop thresholds, abort/report procedure, and
secondary per-protocol definition.

## 10. Scoring and primary measures

Success is binary and determined only by the frozen private verifier: hidden
tests and normal checks must pass. Verifier execution occurs outside agent
control. Every post-exposure failure is unsuccessful in its assigned arm. No
model judge contributes to success.

Prespecified endpoints:

1. **Sole confirmatory statistic:** pooled frozen-benchmark family-level success-
   rate difference, `+all − baseline`, under intention to treat.
2. **Secondary finite cost endpoint:** family-level difference in mean externally
   reconciled billed spend per scheduled attempt, including all assigned attempts,
   unsuccessful/post-exposure failures, and allowed pre-exposure retry spend. If
   exact billed cost is missing after exposure, assign the frozen per-attempt cost
   cap conservatively.

Corpus and mutually exclusive primary-stratum estimates are prespecified
secondary/descriptive. Raw tokens, wall time, turns, tool calls, artifact accesses,
and per-MCP-tool counts are descriptive. Cost per successful completion is an
arm-level operational metric only, not an ordinary paired family bootstrap
endpoint; zero-success arms remain visible as infinite/undefined.

**PENDING MAINTAINER FREEZE:** verifier composition; success and finite-cost
aggregation/contrast forms; external billing reconciliation; currency/conversion
timestamp policy; conservative per-attempt cost cap; and the meaningful pooled-
success threshold from §6.

## 11. Analysis

Aggregate every task and repetition within each family-arm before inference. Raw
runs and tasks sharing a family are not independent. Stage A's sole confirmatory
statistic is the pooled family-level intention-to-treat success contrast,
`+all − baseline`.

Use a family-clustered bootstrap that resamples complete families within corpus
strata and carries all tasks, repetitions, pre-exposure retries, post-exposure
failures, and costs for each sampled family. Apply the exact frozen weighting,
interval, and decision procedure. The pooled success interval/decision is
confirmatory and the only automatic attribution trigger.

Report the finite-cost contrast and corpus/primary-stratum estimates and intervals
as prespecified secondary/descriptive results. They cannot trigger attribution.
Exploratory multilabel descriptor analyses are labelled exploratory and cannot
redefine confirmatory subgroups. Do not compute cost per successful completion as
an ordinary paired bootstrap endpoint.

Publish arm summaries, family-level pairs, AB/BA diagnostics, retries, all
post-exposure assigned failures, cache violations, cost-cap assignments, zero-
success arms, and all access/tool counters. Publish any per-protocol diagnostic
separately. Flag pooled/stratum reversals. A null means no effect detected at the
frozen floor, not equivalence; there is no multiplicity ambiguity because only
the pooled success statistic is confirmatory.

**PENDING MAINTAINER FREEZE:** bootstrap algorithm/hash, resample count, interval
method/level, corpus weighting in pooled estimates, descriptive minimum-stratum
reporting rule, pooled-success decision rule, and null wording.

## 12. Execution tuple

Every measured record schema includes:

- corpus URL, SHA, license, content-manifest hash, and image digest;
- task, instruction, source fixture, gold, verifier, and normal-check hashes;
- matlatl SHA/config and generated `llms.txt`/`trails.json` hashes;
- smevals commit; OpenCode/model/provider/API identifiers and versions;
- prompt, native-context, availability-notice, MCP, and tool-config hashes;
- decoding, token, turn, tool, wall-time, filesystem, and network limits;
- schedule hash, seed, corpus, task, task family, primary stratum, exploratory
  descriptors, repetition, human arm label, machine arm ID, and AB/BA position;
- scorer and record-schema hashes; externally reconciled bill record/cost-cap flag;
  and
- run, scheduled-run, attempt, retry-parent, timestamps including first model
  request, exposure status, and terminal class.

**PENDING MAINTAINER FREEZE:** exact value or missing-value encoding for every
field above. Missing required data after exposure is retained as an unsuccessful
assigned-arm outcome with conservative cost-cap assignment and may activate the
frozen stop/abort rule; it is never silently excluded.

## 13. Pilot firewall and Nimbus

Nimbus supports only instrumentation, isolation, deterministic scoring, and
baseline-only model qualification. It contains code-edit fixtures dependent on
documented constraints. Mechanical calibration verifies injection/removal,
`llms.txt`/`trails.json` access counters, every MCP counter, zero-cache
qualification and pre/post-exposure failure paths, scorer behavior, conservative
cost-cap accounting, and gold/host isolation. It does not need to show a
directional arm outcome and contributes no external-validity or treatment-effect
evidence.

All pre-freeze runs and qualification results are excluded from endpoints.
Disposable Nimbus tasks may inform reliability and projected cost but never
success/cost treatment deltas. Final real tasks are not run before signature.

## 14. Attribution-study trigger

A separate attribution/default-bundle study may preregister `pointer-only`,
`+trails`, `+llms`, and `+MCP` only if:

1. the frozen decision rule for Stage A's **pooled frozen-benchmark success
   contrast** triggers; or
2. after a null, the maintainer separately funds and signs that study.

No cost, corpus, task-stratum, exploratory, or per-protocol result can trigger the
study. It receives its own power calculation, endpoints, arms, notices, and
multiplicity plan. No Stage A result is reanalyzed as if those arms existed.

**PENDING MAINTAINER FREEZE:** exact pooled-success trigger and post-null separate-
funding/signature authorization rule.

## 15. Pending-maintainer-freeze inventory

All boxes must be initialed and dated. Until then this preregistration is
explicitly unfrozen.

- [ ] Exactly two outcome repositories, roles, SHAs, licenses, snapshots/images,
      eligible-task frames, selection procedures, and hashes (§4).
- [ ] Task-family inventory (`N`), mutually exclusive precedence-based strata,
      objective criteria/evidence, exploratory descriptors, hidden verifiers,
      normal checks, independent-review signatures, exclusions, and hashes (§4).
- [ ] Baseline normalization/removal rules; fresh artifact generation; frozen
      `+all` notice; remote streamable-HTTP MCP config (§3).
- [ ] Qualification candidates, baseline-only Nimbus tasks, competence,
      reliability, telemetry, budget, projected-cost formula, tie-break, and
      selected model/provider (§5).
- [ ] Signed joint simulation and budget/power calculation; chosen `N`, `r`, run
      and cost ceilings, conservative per-attempt cap, retry reserve, target
      power, exact procedures, and detection/precision floors (§6).
- [ ] AB/BA algorithm, seed, schedule, and balance diagnostics (§7).
- [ ] Zero-cache qualification proof, telemetry mapping, all artifact/MCP access
      counters, exposure timestamp, cache stop/abort rule, isolation/network
      policy, external billing reconciliation, and record schema (§8).
- [ ] Limits, bounded pre-exposure retry ceiling, post-exposure intention-to-treat
      rules, failure responsibility, and per-protocol diagnostic definition (§9).
- [ ] Deterministic scoring, sole confirmatory pooled-success contrast, secondary
      finite-cost endpoint, cost-cap/currency policy, zero-success descriptive
      rule, and meaningful pooled-success threshold (§10).
- [ ] Family-clustered bootstrap, resamples, interval/decision procedure,
      weighting, descriptive stratum reporting, and null language (§11).
- [ ] Complete pinned execution tuple (§12).
- [ ] Pilot/qualification firewall verified; final tasks never exposed (§13).
- [ ] Attribution trigger and post-null separate-funding rule (§14).
- [ ] Maintainer signature and freeze date: ______________________________
