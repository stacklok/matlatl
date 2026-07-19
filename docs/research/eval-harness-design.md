---
title: "Agent-outcome evaluation harness: do matlatl's artifacts help agents?"
matlatl: orphan-intentional
---

# Agent-outcome evaluation harness (design)

> Status: **proposed design for [#17](https://github.com/stacklok/matlatl/issues/17), not yet run.**
> This is the design note; no harness code exists yet. It settles the seven
> decisions #17 leaves open (task set, corpora, conditions, agent harness,
> metrics, home, decision rules) with rationale and alternatives, and pins a
> lightweight pre-registration so the first run's verdict is honest.
>
> Marked `orphan-intentional` so matlatl doesn't flag its own research note.

## 0. The question, and why the null hypothesis is live

Every agent-facing claim matlatl makes — that `llms.txt`, reading-order trails,
backlinks, and the `serve` MCP tools make a repo more legible to agents — is
currently **unvalidated**. The 2026 evidence makes the null hypothesis a real
possibility, not a strawman:

- ETH Zurich's AGENTS.md studies ([arXiv:2602.11988](https://arxiv.org/html/2602.11988v1),
  [arXiv:2601.20404](https://arxiv.org/html/2601.20404v2)) measured **LLM-generated
  context files reducing task success ~3% and raising inference cost >20%**; only
  curated developer-written files helped, modestly. matlatl's emitted artifacts
  are in the *generated* class.
- [Is Grep All You Need? (arXiv:2605.15184)](https://arxiv.org/abs/2605.15184)
  found coding agents discover primarily via grep-style direct corpus
  interaction, not link-following — so the reader-experience framing behind the
  structure heuristics is not safe to assume.

The harness must answer: **do matlatl's artifacts and findings-driven fixes
improve agent task outcomes (success rate, tokens, tool calls) — or are they
neutral or harmful?** Both a positive and a negative result are wins: either
matlatl becomes the first tool in this space with agent-outcome evidence, or we
learn which artifacts to cut before investing further ([#25](https://github.com/stacklok/matlatl/issues/25)
governing principle: *validate before building*).

## 1. Task set

### The shapes, and why they discriminate

Four task shapes, chosen so each stresses a different artifact and so the null/
harm hypothesis is *detectable*:

| Shape | Example | Scoring | What it tests |
| --- | --- | --- | --- |
| **Find-the-doc navigation** | "Which file documents the exit-code contract?" | exact repo-relative path match (deterministic) | llms.txt catalog, trails, MCP `what-links-to`/`path-between` — the artifacts' strongest claim |
| **Repo-comprehension QA** | "What determines a document's identity, and why?" | LLM-judge vs gold answer (§5.1 safeguards) | general context assembly; where over-reading (ETH cost rise) would show |
| **Doc-maintenance (fix-prompt)** | inject a broken link / stale § ref / orphan; "fix the doc findings" | programmatic: targeted finding resolved, `matlatl check` exit 0, no collateral findings | the fix-prompt + findings loop ([ADR 0009](../adr/0009-fix-prompt-acting-agents.md)/[0020](../adr/0020-fix-prompt-scope.md)) |
| **Grep-favorable control** | a fact trivially found by one `grep` | exact-match | *sentinel*: artifacts should give **no** benefit here; if they add tokens without success, that is the ETH harm signature |

The control shape is the key methodological move: it is the internal check that a
measured "win" on the other shapes is real navigation value and not judge noise,
and it is where a token-cost regression is cleanest to read.

### Count, and an honest power statement

Target **~25 tasks per corpus**, balanced ~6–7 per shape. Combined with the
staged conditions (§3), ≥4 reps (§5.2), and 2–3 corpora, this is a few hundred to
~900 agent runs — tractable, but **powered only for medium-to-large effects**.
Detecting the ETH ~3pp success delta would need hundreds of tasks per corpus;
this harness cannot and does not claim to. Its honest endpoints are: *rule out
large harm* and *detect a large win*. A null result means "no large effect," not
"proven equivalent." State this in the results, do not launder it.

### Ground truth without contamination

- **The task author must not read the artifacts under test.** Authors write tasks
  and gold answers from the raw source docs/code only. A second reviewer verifies
  each gold answer against source.
- **Maintenance tasks need no human gold** — ground truth is the programmatic
  `matlatl check` result, immune to contamination.
- **Freeze tasks + gold answers before the first run** (§7 pre-registration).
- Gold answers and the scoring rubric **never enter the agent sandbox** (§4).

## 2. Corpora

Sizes measured locally (markdown files, fixtures/vendor excluded): matlatl **34
real docs**, atrium **478**, toolhive **238**, mecatl **198**, minder **159**.

| Corpus | Role | Rationale |
| --- | --- | --- |
| **matlatl itself** | smoke / dev-loop | 34 docs, compactness 0.27, ~1.9 clicks apart — too shallow for navigation signal, but zero-friction for harness development. **Not** a primary-signal corpus. |
| **One adopted Stacklok repo** (atrium or mecatl) | primary | Already curated *using* matlatl → real, human-tuned nav surfaces; we control it and can inspect ground truth. |
| **One un-adopted repo** (toolhive or minder) | primary | Large real docs *not* yet matlatl-tuned — tests the artifact on a raw corpus. |

**The adoption confound (call it out loudly).** An adopted repo's `llms.txt`
reflects *human curation done with matlatl's help*, not the raw generated
artifact — exactly the ETH "curated beats generated" axis.

**DECIDED (maintainer, 2026-07-19): regenerate all artifacts fresh (un-curated)
on every corpus.** The eval measures the pure generated artifact — the strict
ETH-comparable claim — not the curation story. Committed artifacts in adopted
repos are ignored; the harness runs `matlatl emit` at the pinned SHA and injects
only its own output. Residual caveat to state in results: an adopted repo's
underlying *link structure* was still improved by matlatl-driven maintenance, so
adopted vs un-adopted remains a useful secondary lens on structure (not on
artifact curation), and keeping one of each in the corpus set preserves that
reading for free.

**Selection criteria:** substantial `docs/` with real link density; permissive
license (atrium/toolhive/minder carry LICENSE files; mecatl is internal — fine
since we control it but not "OSS"); we can inspect ground truth; variety in
navigability (one flat, one deep). **Pin each corpus by commit SHA** in the
pre-registration; do not vendor the trees.

**Synthetic corpus:** adds value *only* for the offline link-recovery eval (§6),
where injecting known link structure at scale is the point. It adds nothing to
agent-outcome external validity — real corpora only there.

## 3. Conditions

Full matrix is 5 arms: baseline · +llms.txt · +trails.json · +MCP (`serve`) ·
+all. Running it on every corpus × task × rep is 5× the first bill for a question
that is, at Stage 1, directional. **Staged design:**

- **Stage A (always):** `baseline` vs `+all` vs `+trails-only`.
  - `baseline` vs `+all` answers the headline question (does the bundle move the
    needle, either way?).
  - `+trails-only` is included up front because trails is the most-exposed
    artifact (a global reading order is over-reading bait for task-conditioned
    sessions, per #17) and demoting it from the default emit bundle is the
    cheapest, highest-value pre-registered decision. Getting that one ablation in
    Stage A is worth the extra arm.
- **Stage B (conditional):** if Stage A shows any signal (+ or −), add
  `+llms.txt` and `+MCP` arms to attribute the effect. If Stage A is flat, stop —
  "neutral bundle" is a complete, cheap, reportable result.

## 4. Agent harness

**Runner: `claude -p --output-format json` (headless).** It emits token counts,
tool-call counts, turn counts, and cost natively; it is reproducible; and it
matches how Stacklok agents actually run. Pin the model id + version, settings,
and record them in every run record. A second harness (Agent SDK, or a different
model) for generalization is **deferred** — noted as future work so a single-
agent result is not over-generalized.

**Artifact injection per condition** (minimal and realistic — test the artifact,
not an instruction about it):

- `baseline`: raw checkout at the pinned SHA. No artifacts, no pointer.
- `+llms.txt`: generated `llms.txt` at repo root (its conventional home). No
  CLAUDE.md pointer — adding "use llms.txt" would test the instruction, not the
  file, and confound against the ETH design.
- `+trails.json`: `trails.json` in the checkout **plus** a minimal pointer, since
  nothing makes an agent read `trails.json` unprompted. This pointer *is* a
  confound (it advantages trails); note it, and interpret a trails win
  cautiously.
- `+MCP`: agent configured with the `matlatl serve` endpoint via MCP config;
  `serve` started per-corpus (read-only, safe).
- `+all`: root `llms.txt` + `trails.json` (+ pointer) + MCP configured.

**Contamination avoidance:** each run in a fresh clone/worktree at the pinned
SHA; the agent receives only the corpus checkout + the task prompt. Gold answers,
rubric, and scoring code live in `eval/` and are **never** in the sandbox. When
matlatl is the corpus, `eval/` is excluded via `.matlatlignore` so it never
pollutes matlatl's own artifacts or the dogfood gate.

## 5. Metrics + scoring

### 5.1 Success (per shape)

- **Navigation & grep-control:** exact repo-relative path / string match — no
  judge.
- **Comprehension QA:** LLM-judge, with safeguards: judge sees gold + agent
  answer but **not** the condition; a fixed rubric (0/1 or 0–2); the judge model
  differs from the model under test (reduce self-preference); a human audits a
  random subset and inter-rater agreement is reported. If agreement is weak, fall
  back to human scoring for that corpus.
- **Maintenance:** programmatic — targeted finding resolved, `matlatl check`
  exit 0, no new collateral findings.

### 5.2 Cost & reps

Primary cost metric: **total tokens** (input+output, from the JSON) — the ETH
result was a >20% cost rise. Also record tool-call count and wall time.
**≥4 reps per (task, condition)** to average over agent nondeterminism; report
mean + per-task variance.

### 5.3 Statistics honest at this n

Paired per-task deltas (same task across conditions), reported as effect sizes
with bootstrap confidence intervals; Wilcoxon signed-rank / paired sign test on
per-task success-rate and token deltas. Multiple-comparison correction across
arms. **Report effect sizes + CIs, not bare p-values.** Re-state the §1 power
caveat: this detects large effects and large harm, nothing subtle.

## 6. Home + reproducibility

- **In-repo `eval/`** (harness, task specs, scoring), excluded from dogfood via a
  new `.matlatlignore` entry and from emit. Rationale: the eval *is* the tool's
  central thesis; versioning it with matlatl keeps it discoverable and honest.
  Alternative — a sibling repo — keeps the main tree lean but loses cohesion;
  rejected for the first run.
- **Corpora are not vendored** — a manifest pins each by commit SHA; the harness
  checks them out.
- **Results:** append-only raw per-run JSON → aggregated `results.json` → a
  human `eval/results.md` stating the pre-registered endpoints and the verdict.
- **Link-recovery (§5 of the semantic-similarity note)** lives in
  `eval/link-recovery/` as a **separate, deterministic, agent-free, token-free**
  offline eval (hide real links, measure recovery — validates `suggested-link`,
  [ADR 0013](../adr/0013-topology-link-prediction.md)). It shares corpora and
  tooling but is a *different claim* (offline signal quality, not agent outcome),
  so it stays out of the agent-outcome endpoints. It can run first as a cheap
  warm-up.
- **"Done" for #17:** one credible directional result on 2 real corpora (+ the
  matlatl smoke corpus), Stage A only, with a `results.md` verdict feeding the
  #25 roadmap. **Not** a permanent CI benchmark.

## 7. Decision rules (pre-registration)

Frozen before the first run; deviations logged in `results.md`.

| Observed in Stage A | Roadmap action |
| --- | --- |
| `+all` no success gain over baseline **and** materially higher tokens | Bundle is neutral-to-harmful; keep analytics gated (#21–#23 stay parked); reframe artifacts as maintainer-lint only, consistent with #25. |
| `+trails` ≤ baseline on success while adding tokens | **Demote trails from the default emit bundle** (make it opt-in). |
| `+MCP` improves success (esp. navigation) | Invest in the MCP surface — fund [#22](https://github.com/stacklok/matlatl/issues/22)/[#23](https://github.com/stacklok/matlatl/issues/23) (the `get-section`-dependent heuristics). |
| `+llms.txt` helps navigation but hurts comprehension | Keep llms.txt; document task-conditioning guidance (don't feed a global catalog to a narrow task). |
| Any arm shows large harm (ETH signature) | That artifact leaves the default bundle. |

**Pre-registration fixes (freeze before run 1):** the arms, the primary
endpoints (per-task success delta; per-task token delta), the corpora SHAs, the
task+gold set, the reps count, and the thresholds in the table above.

## 8. Open questions for the maintainer — ANSWERED (2026-07-19)

1. **Adoption confound** (§2) — **regenerate fresh artifacts everywhere.** The
   eval measures the pure generated artifact (strict ETH comparison); adopted vs
   un-adopted is retained only as a secondary structural lens.
2. **Single harness vs a second agent** (§4) — **single (`claude -p`)** for the
   first run; a second harness is future work and results are stated as
   single-agent.
3. **Power vs cost** (§1/§5.3) — **"rule out large harm, detect a large win" is
   accepted as done-for-#17.** The directional Stage A design stands; a
   more-powered design is not funded unless Stage A results argue for it.

## 9. First-run cost estimate

Stage A ≈ 3 arms × 3 corpora × ~25 tasks × 4 reps ≈ **~900 agent runs**. At an
assumed ~80k tokens/run (comprehension tasks dominate; caching helps) ≈ **~70M
tokens**, roughly **$150–400**. LLM-judge scoring adds a few dollars. Stage B, if
triggered, is a comparable increment. Dominant cost levers: reps × tasks ×
tokens-per-run — tune reps down to 3 or tasks to ~18 to roughly halve it.
