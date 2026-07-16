# 21. Hops-from-root: distance from the entry points as the discoverability metric

Date: 2026-07-16
Status: Accepted

## Context

`under-linked` (ADR 0012) flags a document with fewer than N inbound links
(default 3) — Wikipedia's human-discoverability heuristic, calibrated for a 6M-
article corpus navigated by clicking. For a repo corpus consumed by agents, raw
in-degree measures the wrong thing: **in-degree 1 from the README beats in-degree
5 from five leaf pages.** What predicts whether a document enters an agent's (or a
reader's) context via link traversal is not how many pages point at it but how
**far it sits from the entry points** — how many links you must follow from a
README/index to reach it.

matlatl already resolves the **root set** (README.md / index.md / SKILL.md /
`type: index` / configured globs, ADR 0007/0010) and computes reachability from
it (`unreachable` for what the root set cannot reach at all). What it did not
answer is the graduated question *between* "reachable" and "unreachable": **how
deep** is a reachable document? A document 8 links down a chain is reachable but
effectively undiscoverable — the research note's §4-D "reachable but far" idea,
now made per-document and actionable.

## Decision

### Hops-from-root = per-doc DATA + a non-gating `far-from-root` finding

Per document, compute the **shortest directed distance from the nearest root**
to it over the document projection — its *hops-from-root*. It is surfaced two
ways, mirroring how `betweenness`/`pageRank` are data while
`articulation-point`/`bridge` are also findings:

- **Per-node data** in graph.json (`hopsFromRoot`), beside `pageRank`.
- **A `far-from-root` Info finding** for the outliers: a document with a FINITE
  distance at or beyond a configurable threshold. Always **Info**, **NEVER**
  gates the exit code (even `--strict`) — a discoverability hint, not a defect,
  mirroring `under-linked`'s intent but keyed on distance-from-entry-point rather
  than raw in-degree.

### One multi-source BFS

The distance is a single **multi-source breadth-first search**
(`ComputeHopsFromRoot`, `internal/domain/graphmodel/hops.go`): all root-set
members are seeded at distance 0 into one queue (in sorted root order), then BFS
expands over the sorted document projection, so the first time a document is
dequeued its distance is the **minimum over all roots** (edges are unweighted, so
BFS gives exact shortest paths). It mirrors `ComputeReachability`'s shape — same
root seeding, same sorted-neighbour expansion — but records distances instead of
a reached set, and uses the explicit-head-index queue idiom from `apsp.go` (never
`queue = queue[1:]`, which reallocates the backing array), so the pass is
**O(V+E)** time and **O(V)** memory.

### Sentinel and indeterminate handling

The domain stores `map[DocumentID]int`; **absence means unreachable** (BFS never
visited it). graph.json renders `hopsFromRoot: -1` for an unreachable document
**and** for every node when the root set is **indeterminate** (empty) — the same
posture as `unreachable`: an empty root set skips the computation entirely
(`Indeterminate: true`, empty maps), and callers must not treat every document as
far (ADR 0007). The graph.schema type is `integer, minimum -1`.

### Threshold and exemptions

The threshold defaults to **6** (`DefaultFarFromRootThreshold`): a reader or
agent following links from an entry point is unlikely to reach a document that
deep. It is configurable via a **config-only** `.matlatl.yml farFromRootThreshold`
key (`>= 0`; negative is a hard error; `<= 0` is normalized up to the default in
the domain). There is **no CLI flag** — like `linkSuggestionMinShared`
(ADR 0013), this describes the repo's shape, not a run's behavior (ADR 0011).

A `far-from-root` finding fires only on a **finite** distance `>= threshold` for
a **non-exempt** document. The exemption set is the **same** `structureExemptSet`
the structure ladder uses — root-set members ∪ intentional orphans (front matter
`matlatl: orphan-intentional`) — extracted into one shared helper so
`DetectOrphans` and `ComputeHopsFromRoot` cannot drift on what counts as exempt.
Root members are distance 0 anyway; the exemption matters for a deliberately deep
intentional orphan, which stays unflagged. Unreachable documents are never
flagged (they are absent from the distance map, reported as `unreachable`
instead).

### Relationship to under-linked

This is **additive**, not a replacement. Once the eval harness (P13) can
arbitrate, we may revisit whether `under-linked`'s default threshold earns its
keep or whether hops-from-root should become the primary discoverability signal
with in-degree as detail data. Until then both ship.

### Determinism and purity (ADR 0004)

The BFS seeds roots in sorted order and expands sorted neighbour lists; the
far-from-root outliers are collected by iterating sorted `g.documents`, so the
list is sorted without a re-sort. Distances are integers — no float-order
concern. The domain imports only the standard library and sibling domain
packages.

### Schema bumps

- graph.json `schemaVersion` 6 → **7** (per-node `hopsFromRoot`, top-level
  `farFromRoot` array, `summary.farFromRoot` count).
- findings.json `schemaVersion` 6 → **7** (the `far-from-root` kind +
  `farFromRoot` summary count).

## Consequences

- Agents and maintainers get a distance-from-entry-point signal that predicts
  link-traversal discoverability better than raw in-degree, per document
  (`hopsFromRoot`) and as actionable outliers (`far-from-root`).
- The new finding kind is non-gating, so it adds signal without making green
  builds flaky — consistent with the other advisory kinds (ADR 0013/0015/0016).
- One new domain file (`hops.go`), one extracted shared helper
  (`structureExemptSet`), and the usual emit/config/MCP plumbing keep the
  addition isolated and mirror the existing analysis shapes.
- The `list-orphans` MCP tool now also returns `farFromRoot`, and
  `corpus-summary` exposes per-node `hopsFromRoot`, so a live agent can query the
  signal without parsing artifacts.
