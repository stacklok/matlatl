---
title: "Nimbus: hermetic calibration fixtures for the agent-outcome evaluation"
matlatl: orphan-intentional
---

# Nimbus: hermetic calibration specification

> Status: **Milestone 2 synthetic/mechanical calibration infrastructure is
> complete.** Nimbus is instrumentation, isolation, and deterministic-scorer
> calibration only. Strict qualification telemetry schemas and synthetic examples
> are included, but provider candidates and thresholds remain unfrozen. Live
> provider cache proof, billing reconciliation, retry scheduling, ITT aggregation,
> and actual model selection are Milestone 3. Nimbus contributes no treatment-effect
> estimate and no external-validity evidence. The pending second human review is
> represented as `reviewStatus: pending`; no reviewer or date is fabricated.
> It is not `demo/corpus/nimbus-docs`, which remains an unrelated demonstration
> corpus.

## 1. Purpose and prohibitions

Nimbus de-risks the #17 harness before real paid measurement. Human treatment
label `+all` maps to machine/config ID `all`; `baseline` is `baseline` in both
forms. Nimbus spike/calibration records are never measured records. It verifies
that:

- baseline preparation removes matlatl surfaces and `+all` injects the intended
  bytes and remote MCP configuration;
- deterministic OpenCode/tool events produce exact filesystem and per-MCP-tool
  access counters (this does **not** claim kernel-level arbitrary-shell open counting);
- strict synthetic examples accept only explicit zero cache counters and reject
  malformed token/turn/tool/cost telemetry;
- hidden deterministic verifiers score known code changes correctly;
- containers isolate gold, verifier internals, host paths, and other attempts; and
- trajectory and append-only record mechanics can be exercised without a provider.

Live provider cache proof, billing reconciliation, retry scheduling, intention-to-
treat aggregation, candidate thresholds/lists, and actual model selection are not
Milestone 2 outputs.

Nimbus must not be used to claim that `+all` helps or harms agents, to select a
model based on treatment response, to estimate power from a treatment delta, or
to generalize to real repositories. A directional arm expectation is not a
calibration requirement.

## 2. Corpus and code fixtures

Build a small fictional repository, frozen and reproducible from checked-in
source, with:

- natural project documentation covering behavior, security, configuration, and
  architecture;
- source code, build metadata, and deterministic normal checks;
- coding task fixtures whose correct patches depend on documented constraints;
- enough linked documentation structure to generate nonempty root `llms.txt` and
  `trails.json` and exercise every read-only matlatl MCP tool;
- distinctive facts for direct-search controls and distributed constraints for
  synthesis fixtures; and
- independently authored graph/scorer expectations, never copied from matlatl
  output.

The fixtures exercise the same mutually exclusive classification procedure as
real tasks: grep-friendly control first; otherwise cross-document synthesis when
verifier correctness requires at least two documents; otherwise navigation-heavy
when the frozen burden criteria hold; otherwise single-document constraint. A
fixture receives one primary class, though exploratory multilabel descriptors may
be retained. Navigation-only, QA, and documentation-repair fixtures may exist for
component calibration but are not stand-ins for measured coding tasks.

Any generator is seeded and byte-stable. The freeze manifest lists every source,
documentation, task, expected patch/verifier input, and image hash. The demo
Nimbus tree is neither modified nor imported.

## 3. Coding tasks and scorers

Each qualification/calibration coding task includes:

1. an agent-visible instruction and repository snapshot;
2. at least one required code edit;
3. normal project checks available to the agent;
4. hidden tests and deterministic acceptance logic outside the sandbox; and
5. a documented constraint that distinguishes a correct implementation from a
   plausible but wrong one.

Use exact deterministic verification only. Nimbus does not calibrate a model
judge. Scorer tests include known passing and failing patches, timeout/budget
outcomes, malformed output, and verifier failure. Qualification tasks are
disposable, baseline-only, and cannot overlap real measured tasks.

## 4. Gold and isolation

Gold patches, hidden tests, expectation manifests, scorer code, and model-
qualification thresholds live outside the agent container. The agent receives
only the prepared repository, task instruction, normal tools, and arm-specific
public surfaces needed by a mechanical calibration case.

Isolation tests must prove that the agent process cannot read the gold tree,
host workspace, sibling attempt, container runtime socket, or retained provider
secret. Network policy permits only frozen provider access and, where required,
the loopback remote streamable-HTTP MCP endpoint. MCP is never stdio/local.

## 5. Mechanical coverage matrix

The matrix asserts observability and wiring, not directional arm outcomes.
Synthetic probes or deterministic fake agents may deliberately touch a surface;
the pass condition is that injection, access, and counters match the scripted
action.

| Contract | Calibration action | Mechanical assertion |
| --- | --- | --- |
| Baseline normalization | Prepare a fixture containing stale matlatl artifacts/config | Agent-visible baseline contains none; native context and unrelated files are unchanged |
| `+all` / `all` injection | Generate bundle from frozen fixture | Root `llms.txt` and `trails.json` hashes match; one frozen notice is present; one remote MCP entry points to loopback `/mcp` |
| `llms.txt` access | Script zero, one, and repeated opens | Counter reports exactly 0, 1, and the repeated count |
| `trails.json` access | Script zero, one, and repeated opens | Counter reports exactly 0, 1, and the repeated count |
| MCP transport | Connect to the configured endpoint | Streamable HTTP succeeds; stdio/local configuration is rejected |
| `what-links-to` | Script one valid call | Tool response is retained and its individual counter increments once |
| `list-orphans` | Script one valid call | Tool response is retained and its individual counter increments once |
| `path-between` | Script one reachable and one unreachable call | Both responses are retained and only this tool's counter increments twice |
| `get-section` | Script one valid section lookup | Response and one call are recorded |
| `corpus-summary` | Script one call | Response and one call are recorded |
| `suggest-links` | Script one call | Response and one call are recorded |
| `critical-docs` | Script one call | Response and one call are recorded |
| Total tool calls | Mix agent, filesystem, and MCP actions | Frozen total equals the event-derived total; per-tool counts reconcile without conflation |
| Zero cache / exposure | Feed explicit `0/0`, missing, and nonzero telemetry before and after scripted first-request exposure | Qualification requires explicit `0/0`; only pre-exposure eligible failures retry; post-exposure missing/nonzero counters become unsuccessful assigned-arm outcomes and exercise the frozen stop/abort rule |
| Monetary/token telemetry | Feed known runner and external provider billing records | Uncached input/output and externally reconciled billed spend map exactly; missing post-exposure billing receives the frozen cap and allowed pre-exposure retry spend stays assigned |
| Deterministic scorer | Apply known passing and failing code patches | Hidden verifier produces the authored binary outcomes and normal checks run |
| Gold isolation | Attempt reads of gold, verifier internals, host, and sibling run | Every access fails and no private sentinel enters prompt, workspace, env, event stream, or answer |
| Fresh attempt | Retry a scripted eligible pre-exposure provider/provisioning/evaluator failure | New sandbox/attempt ID is linked to its retry parent; no prior state survives; scripted post-exposure failures are not retried |
| Append-only records | Repeat an attempt ID/write | Existing records cannot be overwritten; correction requires a new record |

A coverage row passes because its exact mechanical invariant holds. Agent success
under one arm versus another is ignored and never appears in a Nimbus result
summary.

## 6. Model/provider qualification use

Before any treatment exposure, run disposable Nimbus coding tasks in `baseline`
only. Apply the preregistered competence, tool/protocol reliability, telemetry,
and budget thresholds. Estimate projected measured-run cost using the frozen
formula. Among passing candidates select the lowest projected cost and apply the
deterministic tie-break if needed.

Do not generate `+all` for qualification candidates. Qualification records are
labelled calibration-only and excluded from every #17 contrast. They may supply
budget inputs but never a treatment effect.

## 7. Reproducibility

The canonical `eval/nimbus/v1/freeze.json` inventories sorted paths, modes, sizes,
and hashes while excluding itself from its tree hash. It freezes task, mutation,
verifier, patch, topology, probe, recipe, and static-binary recipe hashes. The
separately pinned Go verifier is prepared explicitly and then run with
`--pull=never`; Docker and rootless Podman each record their own immutable local
image ID. No portable cross-runtime image digest is claimed. There is no inert
seed, and generated matlatl output is never copied back as an oracle. Matlatl
output is an actual value compared with independent expectations.

## 8. Limitations

Synthetic code and prose under-represent real repositories, and scripted access
probes do not predict autonomous agent behavior. Passing Nimbus establishes only
that measurement machinery can observe known events and protect private data.
Real pinned coding tasks are required for #17 evidence.

## 9. Freeze checklist

- [x] Source code, normal checks, and approximately fifteen natural documents are written.
- [ ] A second human reviewer signs the corpus and task classifications (`reviewStatus`
      remains `pending` until then).
- [x] Coding fixtures exercise all four mutually exclusive precedence-based primary
      strata and require documented constraints.
- [x] Hidden correct, semantically alternative, and plausible-wrong cases are isolated.
- [x] Baseline/`+all`, event-derived filesystem and all seven MCP-tool counters,
      transport, strict telemetry, fresh-attempt, append-only, and isolation mechanics
      have deterministic probes.
- [x] The sorted schema-v1 manifest freezes files and per-runtime immutable image IDs.
- [x] Calibration-only reports contain no directional arm statistic.
- [ ] Candidate list, qualification thresholds, and live provider/cache/billing proof
      are frozen in Milestone 3.
- [ ] Retry scheduling, ITT aggregation, real corpora, paid runs, and model selection
      are completed in Milestone 3.
