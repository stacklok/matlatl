---
title: "Agent-outcome eval: pre-registration (pre-outcome contract)"
matlatl: orphan-intentional
---

# Pre-registration: agent-outcome evaluation harness

> Status: **skeleton, unfrozen.** This is the pre-outcome contract for the
> harness designed in [eval-harness-design.md](eval-harness-design.md) (issue
> [#17](https://github.com/stacklok/matlatl/issues/17)). It is written **before
> any measured run** and frozen before the first one; after freeze, deviations
> are logged in `eval/results.md`, not edited here. Every quantitative
> threshold below is marked **PENDING MAINTAINER FREEZE** until the maintainer
> signs the freeze checklist (§12).
>
> **Terminology discipline.** This contract bans the bare words *material* and
> *signal*. Every effect statement must name its metric, direction, threshold,
> and stratum. The defined terms are in §0; if a sentence needs a word that is
> not defined there, define it there first.
>
> Marked `orphan-intentional` so matlatl doesn't flag its own research note.

## 0. Defined terms

- **Run** — one execution of one task by the pinned agent in one sandbox.
- **Repetition** — one of the ≥4 repeated runs of the same (task, arm) pair.
- **Arm** — an experimental condition: `baseline`, `+all`, `pointer-only`, or
  `+trails` in Stage A (§2).
- **Block** — one (corpus × task × repetition) cell: the same task on the same
  corpus, repeated, with arm order randomized within the block (§4).
- **Task-level success rate** — for one (task, arm): the fraction of that
  task's repetitions scored successful. Range 0–1.
- **Task-level cost** — for one (task, arm): the mean total tokens across that
  task's repetitions, under the primary cache accounting (§8).
- **Success delta** — for an arm pair (A, B) on one task: A's task-level
  success rate − B's. Reported in **percentage points (pp)**.
- **Cost ratio** — for an arm pair (A, B): A's success-normalized cost (§8)
  ÷ B's. Reported as a percentage change.
- **Material success effect** — a success delta whose absolute value is **≥ 10
  pp** (PENDING MAINTAINER FREEZE) with a clustered-bootstrap CI excluding 0.
- **Material cost effect** — a cost ratio whose absolute change is **≥ 20%**
  (PENDING MAINTAINER FREEZE) with a clustered-bootstrap CI excluding 0.
- **Detectable effect** — either a material success effect or a material cost
  effect. Anything smaller is **below the detection floor**: reported, but
  never grounds a roadmap action.
- **Pilot run** — any execution before the freeze checklist (§12) is signed.
  Pilot runs validate plumbing only (§11); their outcome data is discarded.

## 1. Research questions

- **RQ1 (headline).** On the frozen real corpora, does `+all` change task-level
  success rate vs `baseline` by a material success effect, in either direction?
- **RQ2 (cost).** Does `+all` change success-normalized cost vs `baseline` by a
  material cost effect, in either direction?
- **RQ3 (trails exposure).** Does `+trails` change task-level success rate or
  success-normalized cost vs `pointer-only` by a material effect? (Contrasting
  against `pointer-only`, not `baseline`, differences out the pointer
  instruction.)
- **RQ4 (pointer confound).** Does `pointer-only` differ from `baseline` by any
  material effect? If yes, the pointer text itself is an active ingredient and
  all `+trails`/`+all` readings are reinterpreted against `pointer-only`.

Null results are in scope and reportable: "no detectable effect" means exactly
"no effect at or above the detection floor," per §0.

## 2. Stage A design

Four arms, exactly as specified in
[eval-harness-design.md](eval-harness-design.md) §3–§4:

| Arm | Injected content |
| --- | --- |
| `baseline` | raw checkout at the pinned SHA + normalized native context; no artifacts, no pointer |
| `pointer-only` | baseline + the frozen pointer text (hash recorded in §10) |
| `+trails` | `trails.json` + the identical pointer text |
| `+all` | root `llms.txt` + `trails.json` + MCP (`matlatl serve`) + the identical pointer text |

Native context (runner-CLI and repo-supplied defaults such as
`CLAUDE.md`/`AGENTS.md`) is byte-identical across arms. Arms differ only in the
injected matlatl artifacts and the pointer text. Stage B (`+llms.txt`, `+MCP`
arms) is triggered only by a Stage A detectable effect; its arms are specified
in the design note and frozen here only if Stage B is funded.

## 3. Corpora and tasks

- **Outcome set (frozen real corpora):** one adopted Stacklok repo (atrium or
  mecatl) and one un-adopted repo (toolhive or minder), each pinned by commit
  SHA below. **matlatl itself is smoke-only** — harness development and
  end-to-end plumbing checks; its numbers are never reported as outcomes.
- **Nimbus** ([nimbus-eval-corpus.md](nimbus-eval-corpus.md)) is a separate
  future calibration corpus for mechanics/internal validity. It is **not** part
  of the outcome set, contributes no runs to this contract's endpoints, and is
  not `demo/corpus/nimbus-docs`.
- **Task set:** ~25 tasks per corpus, balanced across the four shapes
  (navigation, comprehension QA, doc-maintenance, grep-favorable control) per
  the design note §1. Task packages are immutable directories in the
  Harbor-like shape documented at
  [harborframework.com/docs/task-format](https://harborframework.com/docs/task-format)
  (instruction, environment, verifier — Harbor-like packaging only; no Harbor
  schema or compatibility claimed), with gold answers held outside the
  sandbox.

| Corpus | Role | Pinned SHA |
| --- | --- | --- |
| TBD (adopted) | primary | PENDING FREEZE |
| TBD (un-adopted) | primary | PENDING FREEZE |
| matlatl | smoke only | PENDING FREEZE |

## 4. Randomization and repetitions

- **Blocking:** runs are grouped into blocks of (corpus × task × repetition).
  Within each block, every arm appears exactly once, and the **arm order is
  randomized** with a seeded, balanced assignment: a seeded RNG (seed recorded
  in §10) draws a permutation of the four arms per block, balanced so that
  across each task's blocks every arm occupies every schedule position equally
  often (a Latin-square balance over repetition index).
- **Repetitions:** ≥4 per (task, arm). Repetitions are aggregated to task-level
  values before inference (§7); they exist to average over agent
  nondeterminism, not to enlarge n.
- The assignment is computed once, recorded, and frozen; the run executor
  consumes it verbatim.

## 5. Limits, retries, and failures

- **Per-run limits:** agent turn, token, and wall-clock limits frozen per task
  shape before run 1; the frozen values are recorded in §10. Limits live in the
  frozen per-shape configuration, not in any task-package schema field.
- **Failure taxonomy:** every attempt ends in exactly one of the following
  nine classes. These names and meanings are identical to
  [eval-harness-design.md](eval-harness-design.md) §4.

| Class | Meaning | Retry and scoring treatment |
| --- | --- | --- |
| `completed` | the agent produced a scoreable answer | scorer separately records pass/fail; no retry |
| `agent-timeout` | the agent exceeded the frozen wall-clock limit | task failure (agent-caused); **never retried** |
| `budget-exhausted` | the agent exhausted a frozen token, turn, or tool-call budget | task failure (agent-caused); **never retried** |
| `agent-protocol-failure` | agent-caused malformed output, invalid tool use, or protocol violation prevented scoring | task failure (agent-caused); **never retried** |
| `environment-failure` | prepared image/package or sandbox infrastructure failed | **bounded fresh retry**, at most 3 total attempts |
| `mcp-failure` | the configured MCP service failed independently of agent behavior | **bounded fresh retry**, at most 3 total attempts |
| `provider-failure` | model-provider/API service failed or returned no usable response independently of agent behavior | **bounded fresh retry**, at most 3 total attempts |
| `evaluator-failure` | harness, judge, or scorer failed independently of the answer | **bounded fresh retry**, at most 3 total attempts |
| `invalid-task` | task, gold, rubric, or mutation is discovered to be invalid or ambiguous | stop the affected analysis; issue an **explicit amendment and new freeze** before rerunning the complete affected schedule — **never a selective rerun** |

- Agent-caused failures (`agent-timeout`, `budget-exhausted`,
  `agent-protocol-failure`) are outcome data; retrying them would bias success
  upward, so they are never retried. Only `environment-failure`, `mcp-failure`,
  `provider-failure`, and `evaluator-failure` are retried.
- A bounded retry is a **fresh attempt** with a new attempt id and a
  retry-parent link to the failed attempt. After the 3rd failed attempt the
  scheduled run is recorded `infra-exhausted`: excluded from numerator and
  denominator, with the exclusion and its cause logged.
- A task whose exclusions leave it with < 2 scored repetitions in any arm is
  dropped from all contrasts and the drop is logged.

## 6. Scoring and judge protocol

- **Navigation, grep-control:** exact repo-relative path / string match. No
  judge.
- **Doc-maintenance:** programmatic — targeted finding resolved, `matlatl
  check` exit 0, no collateral findings. Pinned matlatl version in §10.
- **Comprehension QA:** LLM-judge under the strict protocol:
  1. **Frozen rubric** — fixed criteria (0/1 or 0–2), versioned with the task
     set; changing the rubric after freeze is a logged deviation.
  2. **Blinding** — the judge sees gold + agent answer, never the arm, the
     corpus, or the run metadata.
  3. **Different judge model** from the model under test (self-preference
     control); judge model id + version in §10.
  4. **Human agreement check** — a human scores a **frozen audit subset
     stratified by corpus and task shape** (subset size and the agreement
     threshold frozen before run 1); inter-rater agreement against the judge
     is computed per corpus × task-shape stratum and reported.
  5. **Human fallback** — if a corpus × task-shape stratum falls below the
     frozen agreement threshold, **every answer in the affected stratum is
     human-scored, and those full human scores replace the judge scores** for
     that stratum. The audit subset alone never replaces unaudited judge
     results, and there is no averaging of judge and human scores.

## 7. Task-level analysis plan

- **Inferential unit: the task.** All inference uses task-level success rates
  and task-level costs (§0). Raw runs are never pooled.
- **Contrasts:** paired per-task deltas for the pre-registered arm pairs —
  (`+all` − `baseline`), (`+trails` − `pointer-only`), (`pointer-only` −
  `baseline`), and (`+all` − `pointer-only`) as secondary.
- **Uncertainty:** clustered bootstrap — resample tasks with their repetitions
  as clusters; 10,000 resamples; 95% percentile CIs.
- **Stratification:** every estimate reported pooled **and** stratified by
  corpus and by task shape. A pooled estimate that reverses within strata
  (Simpson's) is flagged, not hidden.
- **Multiplicity:** Holm correction across the primary contrasts.
- **Reporting:** effect sizes + CIs, never bare p-values. The power caveat
  (powered only for medium-to-large effects) is restated verbatim in
  `results.md`.

## 8. Cost metrics

- **Recorded per run:** uncached input tokens, cache-read tokens, cache-write
  tokens, output tokens (all from the runner JSON), plus tool-call count and
  wall time.
- **Primary cache accounting (PENDING MAINTAINER FREEZE):** total tokens
  **including** cache-read. The excluding-cache-read total is the pre-registered
  secondary; both are always reported.
- **Success-normalized cost (primary cost metric):** per (task, arm), total
  tokens across repetitions ÷ number of successful repetitions. If a task-arm
  has zero successes, its success-normalized cost is **undefined/infinite and
  reported explicitly**; it is never excluded, dropped, or silently omitted to
  make the arm look efficient. **Raw attempt cost is always reported
  alongside** the success-normalized figure (raw tokens-per-run is the
  secondary metric).
- Decision rules (§9) key on success-normalized cost under the primary cache
  accounting. If either arm in a contrast has zero successes, the cost ratio is
  not finite and receives no 20% cost-effect classification; report the
  undefined/infinite value and base the roadmap decision on the success
  contrast instead.

## 9. Material-effect thresholds and decision rules

Thresholds **PENDING MAINTAINER FREEZE**: **10 pp** success, **20%** cost
(definitions in §0). Rules are evaluated on Stage A only, per stratum and
pooled:

| Observed in Stage A | Roadmap action |
| --- | --- |
| `+all` vs `baseline`: no material success effect in either direction **and** a material cost increase | Bundle is neutral-to-harmful; keep analytics gated (#21–#23 stay parked); reframe artifacts as maintainer-lint only, consistent with #25. |
| `+trails` vs `pointer-only`: no material success gain **and** a material cost increase | **Demote trails from the default emit bundle** (make it opt-in). |
| `pointer-only` vs `baseline`: any material effect | Pointer text is an active ingredient; reinterpret all `+trails`/`+all` contrasts against `pointer-only` and flag the reinterpretation in `results.md`. |
| `+all` vs `baseline`: material success gain, esp. in the navigation stratum | Invest in the artifact surfaces — fund [#22](https://github.com/stacklok/matlatl/issues/22)/[#23](https://github.com/stacklok/matlatl/issues/23) (the `get-section`-dependent heuristics). |
| Navigation stratum gains while comprehension stratum shows a material cost increase | Keep llms.txt; document task-conditioning guidance (don't feed a global catalog to a narrow task). |
| Any arm: material success **decrease** vs its control (the controlled generated-context harm signature) | That artifact leaves the default bundle. |
| No detectable effect anywhere | "Neutral bundle at this detection floor" is the complete, reportable result; Stage B is not triggered. |

## 10. Execution metadata (the pinned tuple)

Every run record carries all of the following; a record missing any element is
invalid:

| Field | Value |
| --- | --- |
| Corpus repository URL | §3; recorded at freeze |
| Corpus commit SHA | §3 |
| Corpus license | recorded at freeze |
| Corpus content-manifest hash | PENDING FREEZE |
| Corpus package/image digest | PENDING FREEZE |
| Task hash | per task, computed at freeze |
| Gold hash | per task, computed at freeze |
| Mutation hash | per mutation, computed at freeze (none for unmutated corpora) |
| matlatl commit SHA (emit + check) | PENDING FREEZE |
| matlatl config hash | PENDING FREEZE |
| matlatl generated-artifact hashes | per corpus × arm, computed at freeze |
| Inspect AI version | PENDING FREEZE |
| Agent CLI + version | Claude Code, headless (`claude -p --output-format json`); version PENDING FREEZE |
| Model id + version, provider, API endpoint | PENDING FREEZE |
| System-prompt hash | PENDING FREEZE |
| Native-context hash | PENDING FREEZE (byte-identical across arms, §2) |
| MCP configuration hash | PENDING FREEZE |
| Tool configuration hash | PENDING FREEZE |
| Decoding settings | PENDING FREEZE |
| All limits (turn/token/wall-clock per shape) | §5; values PENDING FREEZE |
| Pointer text hash | PENDING FREEZE (identical for `pointer-only`, `+trails`, `+all`, §2) |
| Randomization block, repetition, seed, and schedule hash | §4; PENDING FREEZE |
| Scorer hash | computed at freeze |
| Judge model id + version and judge configuration hash | PENDING FREEZE |
| Rubric hash | computed at freeze |
| Attempt id + retry parent | recorded per attempt (§5) |

Trajectories are append-only and replayable per the design note §4.

## 11. Pilot firewall

- **Pilot runs validate plumbing only:** sandbox bring-up, artifact injection,
  trajectory recording, scorer wiring, judge blinding, retry accounting.
- **All pilot outcome data is discarded** — success, cost, and judge outputs
  from pilot runs are excluded from every analysis and deleted from the
  results store before the freeze checklist is signed.
- Pilot runs may use the matlatl smoke corpus and a **disposable task subset**
  that is replaced or mutated before freeze, so no measured run ever executes
  a task the agent harness has seen in final form.
- The firewall holds until §12 is signed; there is no "soft launch."

## 12. Freeze checklist

Each item is initialed and dated by the maintainer. **All items signed = the
contract is frozen; the first measured run may then start.**

- [ ] Corpora selected; SHAs recorded in §3.
- [ ] Immutable prepared corpus snapshots/images verified: repository URL,
      commit SHA, license, content-manifest hash, and package/image digest
      recorded in §10; measured attempts consume only the verified
      snapshot/image and never fetch moving remote state.
- [ ] Task set complete; gold answers verified by the second reviewer; task,
      gold, and mutation hashes recorded in §10.
- [ ] Arms, pointer text, and native-context normalization verified byte-identical
      where required (§2).
- [ ] Randomization seed and blocked assignment computed and recorded (§4).
- [ ] Per-shape run limits frozen (§5).
- [ ] Rubric frozen; judge model pinned; frozen stratified audit-subset size
      and agreement threshold set (§6).
- [ ] Primary cache accounting chosen (§8).
- [ ] Thresholds confirmed or amended: 10 pp success / 20% cost (§9).
- [ ] Execution tuple fields all filled (§10).
- [ ] Pilot outcome data discarded; disposable pilot tasks replaced (§11).
- [ ] Maintainer signature + date: ______________________
