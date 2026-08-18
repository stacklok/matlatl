---
title: "Evaluation overview"
matlatl: orphan-intentional
---

# Evaluation overview

> Current implementation: completed deterministic Level 1 correctness and
> completed Milestone 1 isolated fake executor. Human labels `baseline` and
> `+all` map to machine/config IDs `baseline` and `all`. Fake OpenCode runs only
> inside a fresh locked-down scratch container and probes production remote
> streamable-HTTP MCP on container loopback. Container/image policy values remain
> unfrozen for measured runs. Nimbus, zero-cache/provider telemetry,
> qualification, real-corpus tasks, schedule/retries, paid execution, and outcome
> claims remain unimplemented.

## What matlatl provides

Matlatl turns repository Markdown into a deterministic graph and exposes it as
human reports, generated `llms.txt` and indexes, `trails.json`, machine-readable
artifacts, and a read-only streamable-HTTP MCP server. It also reports structural
facts and advisory signals such as broken references, reachability, graph
centrality, suggested links, and information scent.

Those outputs support three different claims that require separate evidence:

1. **Correctness:** the implementation matches its documented contracts.
2. **Signal quality:** advisory findings and rankings are useful rather than
   noisy.
3. **Agent outcomes:** supplying matlatl context changes coding-task success or
   cost.

Level 1 correctness is complete. The #33 signal-quality work remains separate.
Neither completes the agent-outcome study.

## Experiment A: the #17 headline

> On the frozen two-repository benchmark of coding-change tasks whose correct
> implementation depends on repository documentation, what is matlatl's marginal
> effect on deterministic private-verifier success and finite scheduled-attempt
> cost?

Every measured task requires code edits. Hidden tests plus the repository's
normal checks verify documented behavioral, security, configuration, or
architecture constraints. There is no headline model judge. Claims estimate the
finite frozen benchmark only, not all coding tasks or repositories; external
validity beyond these two repositories is limited.

Before scheduling, freeze each repository's eligible-task sampling frame and the
selection procedure applied to it. Group tasks into independent **task families**:
tasks sharing any documented constraint, subsystem/change, mutation, or verifier
lineage belong to one family. `N` is the number of independent task families. If
a family contains multiple tasks, schedule and bootstrap the whole family as one
inferential cluster.

Each task receives exactly one confirmatory stratum, using this frozen precedence:

1. **grep-friendly coding control** if the objective direct-search criterion holds;
2. otherwise **cross-document synthesis** if verifier correctness requires
   constraints from at least two documents;
3. otherwise **navigation-heavy coding** if the frozen retrieval-burden criteria
   hold; and
4. otherwise **single-document constraint**.

The objective criteria, evidence, and resulting assignment are independently
reviewed before scheduling. Exploratory multilabel retrieval descriptors may be
retained, but cannot select or redefine confirmatory subgroups.

Navigation questions, repository QA, and documentation repair may remain
calibration tasks or focused studies. They are not #17 headline evidence.

## First paid stage: baseline versus +all

Stage A has exactly two arms. The human treatment label `+all` has machine/config
identifier `all`; `baseline` is `baseline` in both human and machine records.

| Human label | Machine/config ID | Agent-visible environment |
| --- | --- | --- |
| `baseline` | `baseline` | Frozen repository and normalized native context, with pre-existing matlatl artifacts and MCP configuration removed |
| `+all` | `all` | The identical baseline plus freshly generated root `llms.txt`, root `trails.json`, remote streamable-HTTP matlatl MCP, and one frozen availability notice |

The notice is identical in every `+all` run and names the available surfaces
without prescribing a workflow. All other prompts, code, tools, limits, and
context remain identical.

`pointer-only`, `+trails`, `+llms`, and `+MCP` are not Stage A arms. They belong
to a separately preregistered attribution/default-bundle study. Its sole
automatic trigger is the frozen rule on pooled benchmark success; cost, corpus,
and stratum results cannot trigger it. After a null, that study requires separate
funding and signature; a null never silently expands the experiment.

## Execution and measurement

A pinned OpenCode model/provider is selected before treatment exposure. Candidate
qualification uses only disposable Nimbus coding tasks in the baseline arm and
frozen criteria for coding competence, tool/protocol reliability, telemetry
completeness, and budget. Among candidates that pass, select the lowest projected
cost, with a deterministic tie-break. Qualification never sees `+all` and never
selects on treatment delta.

Measured attempts run in fresh isolated containers over immutable snapshots.
Qualification must demonstrate that cache-read and cache-write counters are both
exactly zero before measurement begins. A scheduled attempt becomes exposed when
the first model request is sent. Only provisioning, provider, or evaluator
failures before exposure may receive a bounded fresh retry. After exposure, MCP,
runtime, provider, tool, telemetry, timeout, and evaluator failures remain
unsuccessful outcomes assigned to their randomized arm; they are neither retried
nor excluded from the intention-to-treat primary analysis.

Missing or nonzero cache counters after exposure likewise count as unsuccessful
assigned-arm outcomes. The preregistration freezes a stop/abort rule for
systematic or differential cache violations; they are never silently excluded.
A diagnostic per-protocol view may be reported only as secondary.

Each attempt records:

- uncached input and output tokens and externally reconciled provider billing;
- wall time, turns, and total tool calls;
- accesses to `llms.txt` and `trails.json`;
- calls to each matlatl MCP tool separately;
- treatment-exposure time, every retry and its spend, terminal class, and the
  complete trajectory and frozen execution identity.

Success is the deterministic private verifier result, with every post-exposure
failure scored unsuccessful. The primary finite cost endpoint is the task-family
mean billed spend per scheduled attempt, including every assigned attempt,
unsuccessful/post-exposure failures, and spend from allowed pre-exposure retries.
Billing is reconciled externally; if exact billed cost is missing after exposure,
the frozen per-attempt cost cap is assigned conservatively. Cost per successful
completion is a descriptive arm-level operational metric, not an ordinary paired
bootstrap endpoint. Zero successes remains visibly infinite/undefined.

## Design and analysis

The task-family count and repetitions are not fixed by convention. Before freeze,
a signed joint simulation/calculation evaluates feasible `2 × N × r` designs,
where `N` is independent task families and `r` is scheduled repetitions per
family-arm, beginning at `r = 2`. It models the exact success and finite-cost
estimands jointly, including their correlation, cost variance and tails,
failures, pre-exposure retry spend, conservative cost-cap assignment, and
family/corpus/stratum heterogeneity, and applies the exact frozen interval and
decision procedures. It reserves qualification and retry budget, favors more
independent families over more repetitions when power is nearly equal, and
raises the disclosed detection floor if the desired power is unaffordable.

Arm order is balanced AB/BA within task family. Multiple tasks and repetitions
are aggregated within family, and the family is the inferential cluster. The
pooled frozen-benchmark success contrast, `+all − baseline`, is the sole
confirmatory statistic and the only automatic trigger for a separately
preregistered attribution study. Family-clustered bootstrap resampling preserves
corpus strata and carries every task and repetition in a sampled family. The
finite cost contrast, per-corpus estimates, and mutually exclusive task-stratum
estimates are prespecified secondary/descriptive results and cannot trigger
attribution. A null means no effect detected at the frozen floor, not equivalence;
a post-null default-bundle study still requires separate funding and signature.

## Ordered delivery and dependencies

1. **Isolated executor and arm preparation — complete (Milestone 1):** fresh
   locked-down scratch-container execution, normalized deep-copied
   `baseline`/`all` preparation with common bytes/modes parity, production
   loopback streamable-HTTP MCP, and deterministic fake probes. Policy values
   remain unfrozen for measured runs.
2. **Source-bearing Nimbus — complete (Milestone 2):** the separate Cirrus Relay
   corpus, four coding strata, reversible mutations, private deterministic scorers,
   strict synthetic schemas/examples, freeze inventory, and mechanical probes.
   A second human review signature remains pending, so every manifest explicitly
   records `reviewStatus: pending`.
3. **Measurement substrate (Milestone 3):** live zero-cache/provider proof and
   billing reconciliation, treatment-exposure timestamps, intention-to-treat
   aggregation, bounded retry scheduling, conservative cost caps, and actual
   baseline-only qualification/model selection; depends on steps 1–2.
4. **Benchmark freeze preparation:** construct the eligible frames and task
   families for exactly two real corpora, independently review stratum assignment,
   then sign the joint power/budget calculation and still-unfrozen preregistration;
   depends on the qualified substrate in step 3.
5. **Paid Stage A:** run the frozen two-arm schedule and report the sole
   confirmatory pooled success contrast plus prespecified secondary/descriptive
   results; depends on signature and all earlier completion artifacts.
6. **Follow-up:** run attribution only if the confirmatory trigger fires, or run a
   post-null default-bundle study / Experiment B only under separate funding,
   preregistration, and signature.

Non-goals of steps 1–4 are treatment-effect claims, directional Nimbus evidence,
or external-validity claims. Attribution arms and Experiment B are not silently
added to Stage A. The completed correctness oracle and #33 remain unchanged.

## Experiment B is separate

Whether **healthy documentation itself** causally helps coding agents is a later
experiment. It requires controlled healthy/degraded documentation interventions
while holding task and code constant. Comparing matlatl-adopted and unadopted
repositories is observational and cannot answer that causal question. Adoption
status may describe corpora, but is not evidence for Experiment B.

## Nimbus and prior work

Nimbus is only for instrumentation, isolation, scorer, and synthetic qualification-
schema calibration. Milestone 2 contains source code and coding tasks whose
solutions depend on documented constraints. Its event-derived filesystem counters
do not claim kernel-level arbitrary-shell coverage. Live provider/cache proof,
billing reconciliation, retry scheduling, ITT aggregation, thresholds/candidates,
and model selection remain Milestone 3. Its checks concern mechanical injection
and access counters, per-tool MCP counters, strict zero-cache examples,
deterministic scoring, and gold isolation—not directional treatment outcomes or
external validity.

The completed Level 1 correctness work remains unchanged. The #33
signal-quality work also remains unchanged and separate from #17.
