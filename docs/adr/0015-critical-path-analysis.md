# 15. Critical-path analysis: betweenness centrality + articulation points and bridges

Date: 2026-06-06
Status: Accepted

## Context

matlatl reports per-document structure signals (orphans, under-linked,
dead-ends — ADR 0012), the macro-shape via the bow-tie classification
(ADR 0012), HITS hub/authority importance, unlinked-but-close pairs via link
prediction (ADR 0013), and corpus-level navigability scalars (ADR 0014). What it
did **not** report is *which documents and links are load-bearing* — the nodes
most navigation flows through, and the single points of failure whose removal
fragments the corpus. These are exactly the questions a maintainer or agent asks
before deleting, moving, or unlinking a page: "if this goes, what breaks?"

Two classical graph signals answer this:

- **Betweenness centrality** — how often a node lies on shortest paths between
  *other* nodes. High betweenness marks a relay/connector.
- **Articulation points (cut vertices)** and **bridges (cut edges)** — the
  vertices/edges whose removal increases the number of connected components.
  These are the literal single points of failure.

## Decision

Add a **critical-path analysis** (P10) over the already-built document graph.

### Betweenness = per-doc DATA (not a finding)

Betweenness is computed with **Brandes' algorithm** over the **DIRECTED**
projection (`projAdj`), exactly mirroring how HITS hub/authority scores are
treated: it is **pure data**, produces **no finding kind**, and **never** gates
the `check` exit code. It is surfaced as a per-node `betweenness` field on every
graph.json node and as a top-level `betweenness` block
(`{topDocs:[{id,score}]}`) parallel to `hits`, plus a "Load-bearing docs"
section in the human reports (reusing the existing `topN = 5`).

Scores are normalized by `(N−1)(N−2)` — the number of ordered `(s,t)` pairs that
exclude a given vertex — landing them in `[0,1]`. There is **no halving**: the
graph is directed. A corpus with `N < 3` has no vertex that can lie strictly
between two others, so every score is `0`.

### Articulation points & bridges = Info FINDINGS + data

Articulation points and bridges are computed with an **iterative Tarjan
low-link** DFS over the **UNDIRECTED** closure (`N(x) = out(x) ∪ in(x)`, the same
closure ADR 0014's path-length/clustering metrics use). The directed/undirected
split mirrors ADR 0014: importance/flow is directed; "is the corpus connected?"
is undirected.

Unlike betweenness, these are surfaced **both** as findings **and** as data:

- Two new finding kinds, `articulation-point` and `bridge`, both
  `analysis.Info`, each carrying a `SuggestedFix`. They **NEVER** gate
  `CheckExitCode`, not even under `--strict` — they are structural-resilience
  *hints*, not defects, exactly like `suggested-link` (ADR 0013) and
  `knowledge-gap` (ADR 0007). A corpus with a cut vertex is not a failed build.
- As graph.json data: a per-node `isArticulation` bool, top-level
  `articulationPoints []string` and `bridges [{from,to}]`, and
  `summary.articulationPoints` / `summary.bridges` counts. The human reports add
  a "Critical structure" section listing **both** vertices and edges.

Edge cases (pinned by tests): empty/single-node → none; a 2-node A-B → one
bridge, no articulation; a cycle → none/none; a path A-B-C-D → `{B,C}`
articulation and every edge a bridge. The DFS-tree root is an articulation point
**iff** it has `≥ 2` DFS-tree children.

### Streaming SSSP sibling: `ForEachSourceBFS`

Brandes needs, per source, the BFS discovery order, the shortest-path
predecessor lists, and the path counts (`σ`) — strictly more than the distances
`ForEachSourceDistances` (ADR 0014) yields. Rather than generalize that helper
with extra out-parameters (which would make every distance-only consumer pay for
state it does not use), we add a **sibling** `ForEachSourceBFS` that follows the
same streaming shape: sorted source order, sorted neighbour expansion, one reused
set of maps/slices per source, **no V² matrix**. The predecessor backing arrays
are re-sliced (not dropped) between sources so the `V·(V+E)` pass does not
reallocate a fresh slice per `(source, node)`. The callback **must not retain**
the reused maps.

### Iterative Tarjan (stack-safety)

The articulation/bridge DFS is driven over an **explicit stack** (a `[]abFrame`
work stack), never native recursion — matching `components.go`'s stack-safe SCC
transcription — so an arbitrarily long link chain (e.g. 20k documents in one
path) cannot overflow the goroutine stack (the P6 concurrency-readiness
contract).

### Determinism and purity (ADR 0004, 0007)

All iteration is over the sorted `g.documents` with sorted neighbour lists;
predecessor lists are appended in sorted order, so the float divisions/sums in
the Brandes back-pass run in a fixed order and are byte-stable; both
articulation and bridge outputs are sorted. Betweenness floats use the
fixed-precision `Float` wire type (the HITS determinism mechanism). The domain
imports only the standard library (`math` OK) and sibling domain packages.

### MCP

A new `critical-docs` tool returns the top-N load-bearing documents by
betweenness plus the articulation points and bridges. The `corpus-summary` tool
now also carries the betweenness/articulation/bridge data (it serves the full
graph.json manifest).

### Schema bumps

- graph.json `schemaVersion` 4 → **5** (per-node `betweenness`/`isArticulation`,
  top-level `betweenness`/`articulationPoints`/`bridges`, summary counts).
- findings.json `schemaVersion` 4 → **5** (two new kinds + summary counts).
- `llms.txt` is **unchanged** (no new clause).

## Consequences

- Maintainers and agents can see, at a glance and over time, which docs are
  load-bearing and which links/docs are single points of failure — and act
  (add a redundant path) before a deletion fragments the corpus.
- The two new finding kinds are non-gating, so they add signal without making
  green builds flaky or punishing legitimately tree-shaped documentation.
- Cost is `O(V·(V+E))` time (the inherent cost of all-pairs betweenness) with
  `O(V+E)` transient memory; the 5k-doc memory ceiling test still passes
  comfortably.
- Two new domain files (`centrality.go`, `articulation.go`) and one new APSP
  sibling keep the directed/undirected split explicit and mirror ADR 0014.
