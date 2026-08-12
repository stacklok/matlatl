---
title: "Agent-outcome evaluation harness: do matlatl's artifacts help agents?"
matlatl: orphan-intentional
---

# Agent-outcome evaluation harness (design)

> Status: **proposed design for [#17](https://github.com/stacklok/matlatl/issues/17), not yet run.**
> This is the design note; no harness code exists yet. It settles the decisions
> #17 leaves open (task set, corpora, conditions, agent harness, metrics, home,
> decision rules) with rationale and alternatives. The pre-outcome contract
> skeleton lives in [eval-preregistration.md](eval-preregistration.md); the
> hermetic calibration corpus is specified in
> [nimbus-eval-corpus.md](nimbus-eval-corpus.md).
>
> **Amended 2026-08 (methodology review).** Corrects the literature framing
> (§0), re-specifies Stage A as four pointer-normalized arms (§3), pins the
> execution + replay discipline (§4), replaces the success/cost/statistics
> sections with cache-aware, success-normalized, task-level-inference versions
> (§5), and splits the pre-registration out into its own document (§7). The
> link-recovery eval stays separate (§6).
>
> Marked `orphan-intentional` so matlatl doesn't flag its own research note.

## 0. The question, and why the null hypothesis is live

Every agent-facing claim matlatl makes — that `llms.txt`, reading-order trails,
backlinks, and the `serve` MCP tools make a repo more legible to agents — is
currently **unvalidated**. The 2026 evidence is **mixed**, which makes the null
hypothesis a real possibility rather than a strawman. The three load-bearing
sources, stated separately because they measure different things:

- **[arXiv:2601.20404](https://arxiv.org/abs/2601.20404)** — an
  observational efficiency study covering 124 pull requests across 10
  repositories. It reports comparable task completion with lower median
  runtime and output-token use when repository context files were present.
  This is field evidence about observed efficiency, not a controlled estimate
  of the files' causal effect.
- **[arXiv:2602.11988](https://arxiv.org/abs/2602.11988)** — a controlled
  evaluation of repository context files. Generated context generally did not
  improve task success and raised inference cost by more than 20%; the study
  distinguishes generated context from developer-written instructions. Its
  controlled result is unfavorable to assuming generated artifacts help by
  default, but it does not erase the separate observational result above.
- **[Is Grep All You Need? (arXiv:2605.15184)](https://arxiv.org/abs/2605.15184)**
  — scoped to **retrieval and harness behavior**: coding agents discover
  primarily via grep-style direct corpus interaction rather than link-following.
  It says nothing about context files directly; its bearing here is that the
  reader-experience framing behind the structure heuristics is not safe to
  assume.

matlatl's emitted artifacts are generated context, so the unfavorable controlled
result applies to them prima facie. The
harness must answer: **do matlatl's artifacts and findings-driven fixes improve
agent task outcomes (success rate, tokens, tool calls) — or are they neutral or
harmful?** Both a positive and a negative result are wins: either matlatl
becomes the first tool in this space with agent-outcome evidence, or we learn
which artifacts to cut before investing further ([#25](https://github.com/stacklok/matlatl/issues/25)
governing principle: *validate before building*).

## 1. Task set

### The shapes, and why they discriminate

Four task shapes, chosen so each stresses a different artifact and so the null/
harm hypothesis is *detectable*:

| Shape | Example | Scoring | What it tests |
| --- | --- | --- | --- |
| **Find-the-doc navigation** | "Which file documents the exit-code contract?" | exact repo-relative path match (deterministic) | llms.txt catalog, trails, MCP `what-links-to`/`path-between` — the artifacts' strongest claim |
| **Repo-comprehension QA** | "What determines a document's identity, and why?" | LLM-judge vs gold answer (§5.1 safeguards) | general context assembly; where over-reading (the generated-context cost-rise signature) would show |
| **Doc-maintenance (fix-prompt)** | inject a broken link / stale § ref / orphan; "fix the doc findings" | programmatic: targeted finding resolved, `matlatl check` exit 0, no collateral findings | the fix-prompt + findings loop ([ADR 0009](../adr/0009-fix-prompt-acting-agents.md)/[0020](../adr/0020-fix-prompt-scope.md)) |
| **Grep-favorable control** | a fact trivially found by one `grep` | exact-match | *sentinel*: artifacts should give **no** benefit here; if they add tokens without success, that is the generated-context harm signature |

The control shape is the key methodological move: it is the internal check that
a measured "win" on the other shapes is real navigation value and not judge
noise, and it is where a token-cost regression is cleanest to read.

### Count, and an honest power statement

Target **~25 tasks per corpus**, balanced ~6–7 per shape. Combined with the
four Stage A arms (§3), ≥4 repetitions (§5.2), and 2–3 corpora, this is up to
~1,200 runs — tractable, but **powered only for medium-to-large effects**. A
small success delta would need substantially more tasks; this harness cannot
and does not claim to detect one. Its honest endpoints are: *rule out large
harm* and *detect a large win*. A null result means "no effect at or above the
pre-registered detection floor," not "proven equivalent." State this in the
results, do not launder it.

### Ground truth without contamination

- **The task author must not read the artifacts under test.** Authors write
  tasks and gold answers from the raw source docs/code only. A second reviewer
  verifies each gold answer against source.
- **Maintenance tasks need no human gold** — ground truth is the programmatic
  `matlatl check` result, immune to contamination.
- **Freeze tasks + gold answers before the first measured run** (see the
  [pre-registration](eval-preregistration.md) freeze checklist).
- Gold answers and the scoring rubric **never enter the agent sandbox** (§4).

## 2. Corpora

Sizes measured locally (markdown files, fixtures/vendor excluded): matlatl **34
real docs**, atrium **478**, toolhive **238**, mecatl **198**, minder **159**.

| Corpus | Role | Rationale |
| --- | --- | --- |
| **matlatl itself** | smoke / dev-loop | 34 docs, compactness 0.27, ~1.9 clicks apart — too shallow for navigation signal, but zero-friction for harness development. **Not** a primary-signal corpus. |
| **One adopted Stacklok repo** (atrium or mecatl) | primary | Already curated *using* matlatl → real, human-tuned nav surfaces; we control it and can inspect ground truth. |
| **One un-adopted repo** (toolhive or minder) | primary | Large real docs *not* yet matlatl-tuned — tests the artifact on a raw corpus. |

**The frozen real corpora are the outcome set.** External validity — "do the
artifacts help on real documentation?" — can only be established on the pinned
real repositories above. The synthetic **Nimbus** corpus
([nimbus-eval-corpus.md](nimbus-eval-corpus.md)) is a *separate, future*
artifact: a hermetic 40–60-document calibration corpus that validates harness
mechanics and internal validity (does the instrumentation move when it should,
and stay flat when it should?). It **cannot** establish external validity and
feeds no agent-outcome endpoint. Note also that Nimbus is **not**
`demo/corpus/nimbus-docs` — the demo seed is a deliberately-broken stage prop,
not a calibration instrument (see the spec's provenance section).

**The adoption confound (call it out loudly).** An adopted repo's `llms.txt`
may reflect *human curation done with matlatl's help*, not the raw generated
artifact — the generated-vs-developer-written distinction tested by
2602.11988 is directly relevant.

**DECIDED (maintainer, 2026-07-19; retained in the 2026-08 review): regenerate
all artifacts fresh (un-curated) on every corpus.** The eval measures the pure
generated artifact, not the curation story. Committed artifacts in adopted
repos are ignored; the harness runs the pinned matlatl version against each
pinned corpus SHA and injects only that output. Residual caveat to state in results: an adopted
repo's underlying *link structure* was still improved by matlatl-driven
maintenance, so adopted vs un-adopted remains a useful secondary lens on
structure (not on artifact curation), and keeping one of each in the corpus set
preserves that reading for free.

**Selection criteria:** substantial `docs/` with real link density; permissive
license (atrium/toolhive/minder carry LICENSE files; mecatl is internal — fine
since we control it but not "OSS"); we can inspect ground truth; variety in
navigability (one flat, one deep). **Pin each corpus by commit SHA** in the
pre-registration; do not vendor the trees.

**Synthetic corpora in general** add value *only* where the ground truth must
be injected by construction: the offline link-recovery eval (§6) and the Nimbus
mechanics calibration. They add nothing to agent-outcome external validity —
real corpora only there.

## 3. Conditions

Full matrix is 5 arms: baseline · +llms.txt · +trails.json · +MCP (`serve`) ·
+all. Running it on every corpus × task × repetition is 5× the first bill for a
question that is, at Stage 1, directional. **Staged design:**

- **Stage A (always), four arms:** `baseline` · `+all` · `pointer-only` ·
  `+trails`.
  - `baseline` vs `+all` answers the headline question (does the bundle move
    the needle, either way?).
  - `pointer-only` is the **control for the pointer confound**: the agent
    receives the same minimal pointer text used in `+trails`/`+all`, but no
    generated artifacts. Without this arm, a `+trails` win is uninterpretable —
    it could be the artifact or merely the instruction to look.
  - `+trails` is `trails.json` in the checkout **plus that identical pointer
    text** — the same pointer `pointer-only` and `+all` carry. It is included
    up front because trails is the most-exposed artifact (a global reading
    order is over-reading bait for task-conditioned sessions, per #17) and
    demoting it from the default emit bundle is the cheapest, highest-value
    pre-registered decision.
  - **Native context is normalized across arms.** If the runner CLI or the
    corpus repo supplies native context (e.g. a `CLAUDE.md`/`AGENTS.md` the
    agent reads by default), the identical native context is present in *every*
    arm — baseline included. Arms differ **only** in the injected matlatl
    artifacts and the pointer text; nothing else about the agent's default
    context may vary. An arm that silently adds or removes native context is an
    instrumentation bug, not a condition.
- **Stage B (conditional):** if Stage A shows a detectable effect under the
  pre-registered threshold (+ or −), add `+llms.txt` and `+MCP` arms to
  attribute the effect. If Stage A has no detectable effect, stop — "neutral at
  this detection floor" is a complete, cheap, reportable result.

## 4. Agent harness

**Runner: Inspect AI orchestrating pinned Claude Code, headless.** Pin the
Inspect AI version, Claude Code version, model id/version, and every setting in
the pre-registration's execution-metadata section. Inspect AI
([inspect.aisi.org.uk](https://inspect.aisi.org.uk/)), the UK AI Security
Institute / Meridian Labs evaluation framework, officially supports running
external agents including Claude Code inside its dataset/solver/scorer and
sandbox machinery. Claude Code remains the agent under test; Inspect supplies
the run, limit, log, and scoring envelope. A second agent for generalization is
**deferred** — noted as future work so a single-agent result is not
over-generalized.

Task packages follow the useful **Harbor-like shape** documented by Harbor
([harborframework.com/docs/task-format](https://harborframework.com/docs/task-format)):
an immutable task directory containing an instruction, task metadata, a
containerized environment, and a separate verifier. This design **does not
claim Harbor compatibility**: no Harbor schema version, registry, API, or
tooling is adopted or implied.

**Artifact injection per condition** (minimal and realistic — test the
artifact, not an instruction about it):

- `baseline`: immutable prepared corpus snapshot at the pinned SHA, plus the
  normalized native context (§3). No artifacts, no pointer.
- `pointer-only`: baseline + the minimal pointer text, verbatim identical to
  the pointer in `+trails` and `+all`.
- `+trails`: `trails.json` in the checkout + the pointer.
- `+all`: root `llms.txt` + `trails.json` + MCP configured + the pointer.
- (Stage B) `+llms.txt`: generated `llms.txt` at repo root, no pointer —
  adding "use llms.txt" would test the instruction, not the file.
- (Stage B) `+MCP`: agent configured with the `matlatl serve` endpoint via MCP
  config; `serve` started per-corpus (read-only, safe).

**Pin the full execution tuple.** Every run record carries the prepared corpus
identity (repository URL, commit SHA, license, content-manifest hash, and
package/image digest); task, gold, and mutation hashes; matlatl SHA, config,
and generated-artifact hashes; Inspect AI and Claude Code versions; exact
model, provider, and API endpoint; system-prompt, native-context, MCP, and tool
configuration hashes; decoding settings and all limits; randomization block,
repetition, seed, and schedule hash; scorer, judge, and rubric hashes; and the
attempt id plus retry parent. The tuple is filled field-by-field in the
pre-registration; a run whose record lacks any element is invalid, not
"approximately reproducible."

**Append-only, replayable trajectories.** Each run writes one immutable record:
the full event trajectory (every tool call and response), the final answer, the
runner's token/tool/turn/cost accounting, and the execution tuple. Records are
append-only — never edited in place; corrections are new records. Given the
same tuple and seed, a run must be re-executable (replayable) up to model
nondeterminism.

**Failure taxonomy and bounded retries.** Every attempt ends in exactly one
class; these names and meanings are identical in the pre-registration:

| Class | Meaning | Retry and scoring treatment |
| --- | --- | --- |
| `completed` | the agent produced a scoreable answer | scorer records pass/fail; no retry |
| `agent-timeout` | the agent exceeded the frozen wall-clock limit | task failure; never retry |
| `budget-exhausted` | the agent exhausted a frozen token, turn, or tool-call budget | task failure; never retry |
| `agent-protocol-failure` | agent-caused malformed output, invalid tool use, or protocol violation prevented scoring | task failure; never retry |
| `environment-failure` | prepared image/package or sandbox infrastructure failed | infrastructure failure; bounded fresh retry, at most 3 total attempts |
| `mcp-failure` | the configured MCP service failed independently of agent behavior | infrastructure failure; bounded fresh retry, at most 3 total attempts |
| `provider-failure` | model-provider/API service failed or returned no usable response independently of agent behavior | provider failure; bounded fresh retry, at most 3 total attempts |
| `evaluator-failure` | harness, judge, or scorer failed independently of the answer | evaluator failure; bounded fresh retry, at most 3 total attempts |
| `invalid-task` | task, gold, rubric, or mutation is discovered to be invalid or ambiguous | stop the affected analysis; issue an explicit amendment and new freeze before rerunning the complete affected schedule — never selectively rerun |

A bounded retry is a fresh attempt with a new attempt id and a retry-parent
link to the failed attempt. After three failed infrastructure/provider/
evaluator attempts, record `infra-exhausted` and exclude that scheduled run
from numerator and denominator with the exclusion reported.

**Contamination avoidance:** each attempt gets a fresh isolated copy made from
the immutable prepared snapshot/image — never a runtime remote checkout. The
agent receives only the corpus snapshot + task prompt. Gold answers, rubric,
and scoring code live in `eval/` and are **never** in the sandbox. When matlatl
is the corpus, `eval/` is excluded via `.matlatlignore` so it never pollutes
matlatl's own artifacts or the dogfood gate.

## 5. Metrics + scoring

### 5.1 Success (per shape)

- **Navigation & grep-control:** exact repo-relative path / string match — no
  judge.
- **Comprehension QA:** LLM-judge under a **strict protocol**: a **frozen
  rubric** (fixed 0/1 or 0–2 criteria, versioned with the task set); the judge
  sees gold + agent answer but **not** the condition (**blinding**); the judge
  model differs from the model under test (reduce self-preference). A human
  audits a frozen subset stratified by corpus and task shape, and agreement is
  compared with a frozen threshold. **If a corpus/shape stratum falls below the
  threshold, humans score every answer in that stratum before those full human
  scores replace the judge results** — the audit subset alone never replaces
  unaudited results.
- **Maintenance:** programmatic — targeted finding resolved, `matlatl check`
  exit 0, no new collateral findings.

### 5.2 Cost, cache, and repetitions

Primary cost metric: **total tokens** (input+output, from the runner JSON) —
the generated-context harm signature was a cost rise. Also record tool-call count and wall
time. Two refinements, both pre-registered:

- **Cache-aware accounting.** Record uncached input, cache-read, cache-write,
  and output tokens per run, and report totals both **including** and
  **excluding** cache-read tokens. The first reflects all processed context;
  the second isolates newly billed/processed context under the pinned provider
  accounting. Which view is primary is frozen in the pre-registration, not
  chosen after seeing the data.
- **Success-normalized cost.** For each task-arm, report raw attempt cost and
  tokens per successful repetition (total tokens ÷ successful repetitions).
  If the task-arm has zero successes, success-normalized cost is
  **undefined/infinite and reported explicitly**; it is never dropped or
  silently excluded to make the arm look efficient. Report raw and
  success-normalized cost together; decision rules use the pre-registered view.

**≥4 repetitions per (task, arm)** to average over agent nondeterminism;
repetitions are **aggregated to a single per-task value per arm** (success rate
over the task's repetitions; mean tokens) **before any inference** — the task,
not the individual run, is the inferential unit (§5.3).

### 5.3 Statistics honest at this n

- **The task is the inferential unit.** Inference runs on per-task aggregated
  values (§5.2), never on the pool of raw runs — pooling repetitions would
  inflate n by the repetition count and fake precision.
- **Paired task-level contrasts.** Each contrast is the per-task delta between
  two arms over the same tasks (e.g. `+all` − `baseline` per task), so task
  difficulty is differenced out. Report effect sizes with **clustered
  bootstrap** confidence intervals — resample tasks (with their repetitions as
  the cluster), not individual runs.
- **Stratify by corpus and by task shape.** A bundle that helps navigation and
  hurts comprehension must not average out to "neutral"; report each
  corpus × shape stratum alongside the pooled estimate.
- Multiple-comparison correction across arms. **Report effect sizes + CIs, not
  bare p-values.** Re-state the §1 power caveat: this detects large effects and
  large harm, nothing subtle.

## 6. Home + reproducibility

- **In-repo `eval/`** (harness, task specs, scoring), excluded from dogfood via
  a new `.matlatlignore` entry and from emit. Rationale: the eval *is* the
  tool's central thesis; versioning it with matlatl keeps it discoverable and
  honest. Alternative — a sibling repo — keeps the main tree lean but loses
  cohesion; rejected for the first run.
- **Real corpora are immutable prepared snapshots, not runtime checkouts.** For
  each corpus the freeze manifest records repository URL, commit SHA, license,
  content-manifest hash, and task-package/image digest. Preparation may fetch
  the pinned commit before freeze; measured attempts consume only the verified
  snapshot/image and run without fetching moving remote state.
- **Results:** append-only raw per-run JSON (§4) → aggregated `results.json` →
  a human `eval/results.md` stating the pre-registered endpoints and the
  verdict.
- **Link-recovery stays a separate eval.** The link-recovery experiment
  designed in §5 of
  [semantic-similarity-and-determinism.md](semantic-similarity-and-determinism.md)
  lives in `eval/link-recovery/` as a **deterministic, agent-free, token-free**
  offline eval (hide real links, measure recovery — validates `suggested-link`,
  [ADR 0013](../adr/0013-topology-link-prediction.md)). It shares corpora and
  tooling but is a *different claim* (offline signal quality, not agent
  outcome), so it stays out of the agent-outcome endpoints and out of the
  pre-registration's decision rules. It can run first as a cheap warm-up.
- **"Done" for #17:** one credible directional result on 2 real corpora (+ the
  matlatl smoke corpus), Stage A only, with a `results.md` verdict feeding the
  #25 roadmap. **Not** a permanent CI benchmark.

## 7. Decision rules and pre-registration

The pre-outcome contract is a separate document:
[eval-preregistration.md](eval-preregistration.md). It freezes, before the
first measured run: the research questions, the Stage A arms, the corpora SHAs
and task/gold set, the randomization and repetition scheme, the limits/retries/
failure taxonomy, the scoring and judge protocol, the task-level analysis plan,
the cost metrics, the **material-effect thresholds and decision rules**
(proposed: 10 percentage points success, 20% cost — marked *pending maintainer
freeze*), the execution metadata tuple, the pilot firewall, and the freeze
checklist. Deviations after freeze are logged in `results.md`.

The decision-rule table itself lives in the pre-registration so this note can
keep evolving without touching the frozen contract.

## 8. Open questions for the maintainer — ANSWERED (2026-07-19)

1. **Adoption confound** (§2) — **regenerate fresh artifacts everywhere.** The
   eval measures the pure generated artifact (strict comparison with the controlled generated-context study); adopted
   vs un-adopted is retained only as a secondary structural lens.
2. **Single harness vs a second agent** (§4) — **single (pinned Claude Code,
   headless)** for the first run; a second harness is future work and results
   are stated as single-agent.
3. **Power vs cost** (§1/§5.3) — **"rule out large harm, detect a large win" is
   accepted as done-for-#17.** The directional Stage A design stands; a
   more-powered design is not funded unless Stage A results argue for it.

## 9. First-run cost estimate

Stage A ≈ 4 arms × 3 corpora × ~25 tasks × 4 repetitions ≈ **~1200 agent
runs**. At an assumed ~80k tokens/run (comprehension tasks dominate; caching
helps) ≈ **~100M tokens**, roughly **$200–500**. LLM-judge scoring adds a few
dollars. Stage B, if triggered, is a comparable increment. Dominant cost
levers: repetitions × tasks × tokens-per-run — tune repetitions down to 3 or
tasks to ~18 to roughly halve it.
