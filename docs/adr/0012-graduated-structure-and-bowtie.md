# 12. Graduated structure findings and bow-tie classification

Date: 2026-06-06
Status: Accepted (gap/discoverability-framing augmented in part by ADR 0013)

## Context

ADR 0007 modelled a single orphan signal: a document with **in-degree 0 AND
out-degree 0** in the navigational document projection. That binary "isolated or
not" view misses two common, distinct documentation-health problems:

- A document that links onward but has **few inbound links** is hard to discover
  (readers and agents rarely reach it), even though it is not isolated.
- A document that has inbound links but **links to nothing onward** is a
  navigational dead-end (the reader's path stops there).

Neither is well-served by the existing isolated-orphan finding, and neither is
necessarily a build-breaking defect — a glossary leaf is intentionally sparse, a
changelog is intentionally terminal. Separately, agents asking "what is the macro
shape of this corpus?" had no answer: the SCC/WCC lists are raw, not a
human-readable structure summary.

## Decision

### Graduated structure ladder (per-document, single bucket)

Each non-exempt document is classified into **at most one** structure bucket, in
strict priority order over the document projection's in/out degree:

1. `in==0 && out==0` → **Orphan** (fully-isolated; the most-severe tier,
   unchanged from ADR 0007, Warning severity).
2. else `out==0` (in>0) → **Dead-end**.
3. else `in < inboundThreshold` (out>0) → **Under-linked**.

A document with `in >= inboundThreshold` and `out > 0` is healthy and produces no
structure finding.

**Exemptions** are unchanged from ADR 0007 and apply to **all** tiers:
intentional orphans (front matter `matlatl: orphan-intentional`) and root-set
members (configured or convention) are exempt from orphan/under-linked/dead-end.
A declared entry point with few inbound links is its purpose, not a defect.

**Unreachable** stays orthogonal. It is computed independently (only when the
root set is determinate) and is suppressed **only** by a fully-isolated Orphan —
dead-end and under-linked do **not** suppress unreachable. A document can be both
under-linked and unreachable; both are reported.

### Default threshold

`inboundThreshold` defaults to **3** (Wikipedia's "discoverable" heuristic: a
page reachable from at least a few others). A configured value `<= 0` is
normalized (floored) to 3 in the domain. Under-linked findings carry the actual
inbound count in their `details.inboundCount`.

### Configurable severity

Under-linked and dead-end default to **Info** severity: they are reported in
every artifact but **never** fail `check`, even under `--strict`. A single config
key `structureFindingsSeverity` (`info` | `warning`, default `info`) promotes
**both** to **Warning**, which then fails `check --strict` exactly like
orphan/unreachable/ambiguous. The key lives in `.matlatl.yml` and the application
Config; the threshold additionally has a `--inbound-threshold` CLI flag (parity
with `--root`); the severity is config-only.

### Bow-tie classification (corpus-level data, not findings)

Relative to the **giant SCC** `S` (the SCC with the most members; ties broken by
the smallest sorted-min component ID), every document is bucketed:

- **CORE** — a member of `S`.
- **IN** — can reach CORE but is not reachable from it.
- **OUT** — reachable from CORE but cannot reach it.
- **TENDRIL** — in the same weak component as `S` but neither reaches nor is
  reached by CORE.
- **DISCONNECTED** — in a weak component that does not contain `S`.

This is pure **classification data**: it is surfaced in `graph.json` (per-node
`bowtie` plus a corpus `bowtie` summary), the human report ("Structure: N core, N
in, N out, N tendril, N disconnected"), and over MCP — but it produces **no
per-document finding**. When the giant SCC has a single member (every SCC is a
singleton, i.e. an acyclic corpus) there is **no cyclic core**; the buckets are
still populated deterministically and the human report says "no cyclic core".

All of this is deterministic: sorted component selection, sorted-neighbour BFS
(forward over out-edges for OUT, reverse over in-edges for IN), and sorted seed
order, so the report is identical regardless of map iteration order.

## Consequences

- Additive schema bumps: `graph.json` schemaVersion 1→2 (per-node
  `bowtie`/`underLinked`/`deadEnd`, top-level `underLinked`/`deadEnd` arrays, a
  `bowtie` summary, summary counts); `findings.json` schemaVersion 2→3 (new kinds
  `under-linked`/`dead-end`, summary counts). Both are backward-compatible
  additions.
- The default `check` exit contract (ADR 0005) is unchanged: the new findings are
  Info and never gate CI unless a repo opts in via `structureFindingsSeverity:
  warning`.
- A two-document corpus where each doc has a single inbound link is now reported
  as under-linked at the default threshold; a repo that wants such a corpus to be
  "clean" sets `inboundThreshold: 1`.
