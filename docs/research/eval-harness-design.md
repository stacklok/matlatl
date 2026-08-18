---
title: "Agent-outcome evaluation harness: matlatl's marginal effect on coding agents"
matlatl: orphan-intentional
---

# Agent-outcome evaluation harness (design)

> Status: **amended design for [#17](https://github.com/stacklok/matlatl/issues/17); not yet run.**
> `eval/` contains completed Level 1 correctness and completed Milestone 1
> isolated fake execution, not a paid or causal result. The executor uses a fresh
> locked-down two-binary scratch container and real loopback streamable-HTTP MCP;
> its container/image policy values are still unfrozen for measured runs. The
> still-unfrozen execution contract is [eval-preregistration.md](eval-preregistration.md). Nimbus is
> specified in [nimbus-eval-corpus.md](nimbus-eval-corpus.md). Correctness and
> #33 signal-quality work remain separate in
> [heuristic-evaluation.md](heuristic-evaluation.md).

## 1. Claim and boundary

### Experiment A: #17

The headline question is:

> On the frozen two-repository benchmark of coding-change tasks whose correct
> implementation depends on repository documentation, what is matlatl's marginal
> effect on hidden-verifier success and finite scheduled-attempt cost?

This is an artifact-access intervention. It compares the same repository,
documentation, task, agent, and limits with and without matlatl's generated
agent-facing bundle. Its finite-benchmark estimand supports claims only about the
frozen eligible and selected task families in those two repositories, not all
real coding tasks or repositories; external validity is limited.

### Experiment B: documentation health

A separate later experiment may ask whether healthy documentation itself
causally helps coding agents. That requires controlled healthy/degraded
documentation interventions while task and code remain fixed. Adopted versus
unadopted repositories are confounded observational groups, not causal evidence
for Experiment B. Adoption status may be reported as corpus context only.

Level 1 correctness, #33 signal quality/link recovery, and focused one-surface
studies answer other questions. None is a substitute endpoint or completion
criterion for #17.

## 2. Measured tasks

Every headline task must:

1. require source-code or configuration-code edits, not only an answer or a
   documentation edit;
2. have deterministic private verification using hidden tests plus the
   repository's normal checks;
3. depend on one or more documented behavioral, security, configuration, or
   architecture constraints;
4. keep verifier code, gold patches, and hidden expectations outside the agent
   sandbox; and
5. be reviewed to ensure that the documented constraint is necessary to the
   intended correct implementation.

The task set to freeze uses exactly one primary retrieval stratum per task. Apply
these objective rules in precedence order:

| Precedence | Stratum | Requirement |
| --- | --- | --- |
| 1 | **Grep-friendly coding control** | The frozen direct-search test finds the decisive constraint within the frozen query/result burden |
| 2 | **Cross-document synthesis** | If not grep-friendly, verifier correctness requires constraints from at least two documents |
| 3 | **Navigation-heavy coding** | If neither above, the task meets the frozen objective retrieval-burden criteria |
| 4 | **Single-document constraint** | Every remaining eligible task whose decisive rule is in one document |

Freeze the direct-search queries, result/burden thresholds, navigation-burden
criteria, and required-document evidence. A reviewer independent of task
selection confirms each assignment before scheduling. Exploratory multilabel
descriptors may describe tasks, but cannot drive confirmatory subgroup selection.

Before selecting tasks, freeze the complete eligible-task sampling frame in each
repository and a reproducible selection procedure. Define independent **task
families** before scheduling: tasks sharing any documented constraint,
subsystem/change, mutation, or verifier lineage are in the same family. Multiple
tasks may be retained per family, but the family is scheduled and bootstrapped as
one inferential cluster.

Old find-the-document, comprehension QA, and documentation-repair tasks may be
used for calibration or separately preregistered focused studies. They do not
enter the headline sample. No headline score uses a model judge.

Task authors work from source code and source documentation, never generated
artifacts. A second reviewer checks eligibility, family assignment, primary
stratum, hidden verifier, and documented dependency. Frames, selection procedure,
tasks, families, fixtures, verifiers, and hashes freeze before measured runs.

## 3. Real corpora

Use exactly two pinned real repositories. Before observing outcomes, freeze each
repository's eligible-task frame and selection procedure as well as repository
URL, commit, license, content manifest, prepared image digest, and selected task-
family inventory. Matlatl itself may remain a smoke corpus but is not outcome
evidence unless it is one of the two repositories and independently satisfies
the frozen eligibility and selection criteria.

Committed matlatl artifacts are not treatment. Corpus preparation removes
pre-existing matlatl `llms.txt`, `trails.json`, generated indexes/manifests, and
matlatl MCP entries from the normalized base snapshot. Fresh artifacts are made
from the pinned matlatl version only for `+all`.

Include corpus topology and adoption status descriptively if useful. Do not infer
that adoption or healthier graph metrics caused an agent outcome; that belongs
to Experiment B.

## 4. Stage A conditions

The first paid Stage A has exactly two arms. Human label `+all` maps to the
machine/config identifier `all`; `baseline` maps to `baseline`.

| Human label | Machine/config ID | Injected content |
| --- | --- | --- |
| `baseline` | `baseline` | Frozen normalized repository plus byte-identical native agent context; all pre-existing matlatl artifacts and MCP configuration removed |
| `+all` | `all` | Baseline plus freshly generated root `llms.txt`, root `trails.json`, remote streamable-HTTP matlatl MCP, and one frozen availability notice |

Native repository instructions such as `AGENTS.md`, runner defaults, tools,
prompts, environment, and limits are normalized and byte-identical across arms.
The `+all` notice is fixed before measurement, names the three available surfaces,
and does not mandate their use. Its absence from baseline is part of the bundle
being tested.

MCP is configured only as remote streamable HTTP at a loopback `/mcp` endpoint;
local/stdio transport is forbidden. Each attempt receives a fresh service over
its immutable corpus copy.

`pointer-only`, `+trails`, `+llms`, and `+MCP` are deferred to a separately
preregistered attribution/default-bundle study. The sole automatic trigger is the
frozen decision rule applied to Stage A's pooled frozen-benchmark success
contrast. Cost, corpus, and task-stratum estimates cannot trigger attribution.
After a null, any default-bundle study requires separate funding and signature. A
Stage A null stops Stage A and never silently adds arms.

## 5. Model/provider qualification

Select the pinned OpenCode model/provider before any candidate sees treatment.
Qualification uses disposable Nimbus **coding** tasks under baseline only. These
tasks do not overlap measured real tasks and are discarded from outcomes.

Freeze before qualification:

- candidate model/provider/API list and exact OpenCode version;
- minimum coding competence on deterministic private verifiers;
- maximum tool/protocol failure rate;
- required telemetry fields, including explicit zero cache counters;
- qualification and projected measured-run budget ceilings;
- projected-cost calculation; and
- deterministic tie-break order.

Disqualify any candidate failing competence, reliability, telemetry, or budget.
Among passing candidates choose the lowest projected cost; apply the tie-break
only on equal projected cost. Never expose `+all`, calculate a treatment delta,
or select a candidate for responding favorably to matlatl.

## 6. Execution, isolation, and records

External smevals orchestrates pinned OpenCode headlessly. The current checked-in
fixture proves that boundary with fake OpenCode only. Measured attempts require
fresh container isolation because a run-local directory alone does not isolate
private gold or host paths.

Each run record pins the complete execution tuple: corpus/image and content
hashes; task, verifier, gold, and mutation hashes; matlatl SHA/config and artifact
hashes; smevals/OpenCode/model/provider/API versions; prompt, native-context,
notice, MCP, and tool configuration hashes; decoding settings and limits;
schedule, task, repetition, arm order, and seed; scorer hash; and attempt/retry
identity.

Trajectories and results are append-only. Retain every prompt/model event, tool
call and response, filesystem-access event used for treatment counters, final
answer, process result, verifier result, and telemetry record. Corrections create
new records.

Gold patches, hidden tests, scorer code, and qualification expectations never
enter the agent sandbox. Attempts consume verified local snapshots and do not
fetch moving repository state.

### Failure and retry policy

Record whether failure occurs before or after treatment exposure, defined as the
first model request being sent. Only pre-exposure provisioning, provider, or
evaluator failures may receive a bounded fresh retry under the frozen ceiling.
Retries use new sandboxes and attempt IDs linked to the original scheduled run;
their spend remains assigned to that run.

Once exposed, every terminal event—including MCP, runtime/environment, provider,
tool, telemetry, timeout, protocol, budget, and evaluator failures—is an
unsuccessful intention-to-treat outcome in the assigned arm and is not retried or
excluded. An invalid task stops affected analysis and requires amendment,
refreeze, and rerunning the complete affected schedule; it is not resolved by
selective replacement. Secondary diagnostic per-protocol views may identify
mechanically complete attempts, but cannot replace the primary analysis.

Qualification must prove explicit `cache-read = 0` and `cache-write = 0` counters
before measurement. Missing or nonzero cache counters after exposure are
unsuccessful assigned-arm outcomes. Freeze a stop/abort rule for systematic or
differential cache violations and report every violation; never silently exclude
them.

## 7. Zero-cache telemetry and cost

Caching is disabled at runner and provider. Qualification must show explicit
zero counters. Measurement retains cache counters even when missing/nonzero and
applies the post-exposure outcome and stop/abort rules above. Provider billing is
externally reconciled against runner events.

Record for every scheduled run and attempt:

- uncached input tokens, output tokens, externally reconciled billed cost, and any
  conservative cost-cap assignment;
- cache-read and cache-write counters;
- wall time, turns, and total tool calls;
- `llms.txt` access count and `trails.json` access count;
- separate call counts for `what-links-to`, `list-orphans`, `path-between`,
  `get-section`, `corpus-summary`, `suggest-links`, and `critical-docs`;
- first-model-request/treatment-exposure status and timestamp; and
- success/failure, retry, and intention-to-treat class.

The primary finite cost endpoint is task-family mean billed spend per scheduled
attempt. It includes all assigned attempts, unsuccessful and post-exposure
failures, and spend from allowed pre-exposure retries. If exact billed cost is
missing after exposure, assign the frozen per-attempt cost cap conservatively.
Cost per successful completion is reported only as a descriptive arm-level
operational metric; it is not an ordinary paired task-family bootstrap endpoint.
An arm with zero successes remains visibly infinite/undefined.

## 8. Budget and power

No fixed family count or repetition count is assumed. Before freeze, the
maintainer signs a joint simulation/calculation over feasible `2 × N × r`
designs, where `N` is independent task families and `r` scheduled repetitions per
family-arm.

The calculation must:

- jointly simulate the exact pooled family-level success estimand and finite
billed-spend-per-scheduled-attempt estimand, including their correlation, cost
variance and tails, family/corpus/stratum heterogeneity, post-exposure failures,
allowed pre-exposure retry spend, and conservative cost-cap assignments;
- execute the exact proposed clustering, interval, weighting, and confirmatory
decision procedures rather than a proxy test;
- begin design comparison at `r = 2`;
- reserve explicit budget for qualification and bounded pre-exposure retries;
- compare power and detection floors under every financially feasible design;
- favor larger `N` over larger `r` when power is near-equal; and
- raise and disclose the detectable-effect floor when target power is
unaffordable rather than pretending the original floor remains supported.

The signed calculation, code/hash, assumptions, chosen `N` and `r`, total run
ceiling, per-attempt cost cap, total cost ceiling, target power, exact interval and
decision procedures, and resulting success and finite-cost detection floors are
freeze artifacts.

## 9. Randomization and analysis

For each task family and repetition, use balanced AB/BA order (`A = baseline`,
`B = +all`, machine ID `all`). Freeze the seeded assignment so each arm appears
first equally often within every feasible corpus/primary-stratum block; all tasks
from one family remain in that family's scheduled cluster and unavoidable
imbalance is recorded.

Aggregate all tasks and repetitions into one value per family-arm before
inference. The task family is the inferential unit. The sole confirmatory
statistic is the pooled frozen-benchmark family-level success contrast:

> `+all − baseline`

Apply the frozen family-clustered bootstrap, resampling families within corpus
strata and carrying every task, repetition, failure, retry, and cost in each
sampled family. The primary success estimate and decision interval are
confirmatory. The finite cost contrast and per-corpus/per-primary-stratum effect
estimates and intervals are prespecified secondary/descriptive analyses; none can
trigger attribution. Cost per successful completion is arm-level descriptive
only and is not bootstrapped as an ordinary paired endpoint.

Also publish all raw arm summaries, failures, retries, cache violations,
zero-success arms, access counters, and per-MCP-tool counts, plus secondary
per-protocol diagnostics clearly separated from intention-to-treat results. A
null means no effect detected at the frozen detection floor, not proof of
equivalence.

## 10. Nimbus and calibration

Nimbus is instrumentation/isolation/scorer/model-qualification calibration only.
It must add source code and coding fixtures dependent on documented constraints.
Its coverage matrix verifies artifact injection and removal, access counters,
per-tool MCP counters, zero-cache rejection, deterministic hidden scoring, and
gold/host isolation. It neither needs nor may claim directional arm outcomes.
Nimbus contributes no treatment-effect estimate and no external-validity claim.

## 11. Reproducibility and completion

Raw records are append-only and feed deterministic aggregate data and a human
`eval/results.md`. #17 is complete only when the frozen two-arm study has run on
exactly the two selected repositories and reports the confirmatory pooled success
result plus finite-cost and registered descriptive results with every assigned
post-exposure failure. The result estimates this finite benchmark only. Link
recovery, #33 signal quality, Level 1 correctness, Nimbus, navigation QA, and
document repair do not count as #17 completion.

## 12. Pending maintainer freeze

The preregistration remains explicitly unfrozen. Before paid measurement the
maintainer must freeze:

- exactly two repositories, SHAs/images/licenses, eligible-task frames and
  selection procedure, task families, and whether matlatl qualifies as outcome or
  smoke only;
- coding tasks, mutually exclusive precedence-based strata, exploratory
  descriptors, independent review, hidden verifiers, normal checks, and all
  hashes;
- normalized native context, artifact-removal rules, `+all` notice, and remote
  MCP configuration;
- model/provider candidates, baseline-only Nimbus qualification thresholds,
  projected-cost formula, budget, and deterministic tie-break;
- zero-cache qualification proof, provider configuration, telemetry validation,
  treatment-exposure boundary, cache stop/abort rule, and conservative cost cap;
- signed joint simulation and budget/power calculation, selected `N`/`r`, retry
  reserve, target power, and success/finite-cost detection floors;
- AB/BA schedule and seed;
- run limits, bounded pre-exposure retry ceiling, post-exposure
  intention-to-treat rules, scorer, isolation tests, record schema/version, and
  complete execution tuple;
- sole confirmatory pooled-success estimand and trigger; finite-cost and
  descriptive estimands; family-clustered bootstrap settings; exact interval and
  decision procedures; and
- attribution-study trigger language and maintainer signature/date.
