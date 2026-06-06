# 14. Corpus navigability metrics

Date: 2026-06-06
Status: Accepted

## Context

matlatl already reports per-document structure signals (orphans, under-linked,
dead-ends), the macro-shape via the bow-tie classification (ADR 0012), and
unlinked-but-close pairs via link prediction (ADR 0013). What it did **not**
report is a small set of corpus-level *navigability* scalars that answer "how
navigable is this documentation as a whole?" — how connected, how hierarchical,
how many clicks apart typical pages are. These are standard, well-understood
graph metrics that compress the whole corpus into a handful of numbers an agent
or maintainer can read at a glance and track over time.

## Decision

Add a **navigability** analysis (P9) that computes a fixed set of corpus-level
SCALARS over the document projection. They are **pure data**, exactly like the
bow-tie report: they produce **no finding kind** and **never** affect the
`check` exit code (ADR 0005) — not even under `--strict`.

### Metrics and exact definitions

Let `N = len(documents)`. For `N <= 1` every metric is `0` (no ordered pairs;
no division by zero).

**Compactness `Cp` (directed projection):** the reachability-weighted
compactness in `[0,1]`. Over a single directed all-pairs pass, let
`sumCD = Σ_{i≠j} (reachable ? d(i,j) : K)` with the unreachable-pair penalty
`K = N` (the maximum sub-distance). Then

```
Cp = ((N²−N)·N − sumCD) / ((N²−N)·(N−1))
```

`Cp = 1` iff every ordered pair reaches the other in one hop (fully connected);
`Cp = 0` iff nothing reaches anything. Charging unreachable pairs `K = N` (the
finite maximum) rather than `+∞` keeps the score bounded and well-defined on a
disconnected corpus.

**Stratum (directed, same pass):** how linear/hierarchical the reachability is,
in `[0,1]`. Using only **finite** sub-distances (unreachable pairs contribute
nothing — NOT K-substituted): `statusOut[i] = Σ_{reachable j≠i} d(i,j)` and
`statusIn[j] += d(i,j)` for each reached `j`. Then `S(i) = statusIn[i] −
statusOut[i]`, `AP = Σ_i |S(i)|`, and the linear-graph normalizer
`LAP = N²/2` (N even) or `(N²−1)/2` (N odd). `Stratum = min(AP/LAP, 1)`. A pure
chain scores `1`; a pure cycle / fully symmetric structure scores `0`.

**Characteristic & median path length, diameter (undirected closure):** over a
second all-pairs pass on the undirected closure `N(x) = out(x) ∪ in(x)`, tally
the finite ordered-pair distances into a histogram (`[]int` of size `N+1`).
`CharacteristicPathLength` is the **mean** over finite pairs; `MedianPathLength`
is the **median** read from the cumulative histogram (no float sort);
`Diameter` is the largest finite distance; `ReachablePairs` is the count of
finite ordered pairs. Zero finite pairs → all zero.

**Clustering coefficient (undirected closure):** the Watts–Strogatz global
clustering coefficient — the mean *local* clustering over nodes with undirected
degree `k >= 2`. For such a node `v`, `local = (Σ_{u∈N(v)} |N(u)∩N(v)|) /
(k·(k−1))`. The numerator counts every neighbour–neighbour link twice
(`= 2·edges`) and the denominator is `2·maxpairs`, so `local = edges/maxpairs`
correctly. Nodes with `k < 2` are **EXCLUDED** from the mean (they have no
definable local clustering), per the Watts–Strogatz convention; they are *not*
counted as `0`.

Directed vs undirected is deliberate: compactness and stratum are about
*directed* reachability/flow, whereas path length and clustering describe the
undirected "can a reader get between these" shape.

### Streaming APSP helper

The two passes share one primitive: `ReferenceGraph.ForEachSourceDistances`
(`internal/domain/graphmodel/apsp.go`). It runs one BFS per source (in sorted
document order, over sorted neighbour lists) and invokes a callback with the
source and its single-source distance map, **reusing one distance map across
sources** (cleared between sources) and **never materializing a V² matrix**.
Transient memory is `O(V)`; time is `O(V·(V+E))`. The BFS queue uses an explicit
head index (not `queue = queue[1:]`) so a single backing array serves the whole
pass — reslicing the head pointer forward would force a reallocation on every
source, an `O(V²)` allocation blow-up the 5k-doc memory-ceiling test guards.

**P10 reuse contract:** betweenness centrality (P10) needs, in addition to
distances, the BFS discovery *order* and shortest-path *predecessor counts*
(sigma) for the dependency back-pass. Those are intentionally **not** computed
here, so unweighted distance consumers pay nothing for them. P10 should add a
*sibling* helper following this same per-source streaming shape (sorted source
order, sorted neighbour expansion, no stored V² state) rather than overloading
this function with extra out-parameters.

### Surfaces

- **graph.json** bumps schema **v3 → v4**: a new `summary.navigability` object
  (`compactness`, `stratum`, `characteristicPathLength`, `medianPathLength`,
  `clusteringCoefficient`, `diameter`, `reachablePairs`). The float fields reuse
  the fixed-precision `Float` type (the HITS determinism mechanism) so output is
  byte-stable. `findings.json` is **unchanged** (still v4) — navigability is not
  a finding. The `corpus-summary` MCP tool gets the block for free.
- **Human reports** (markdown/terminal): a `## Navigability` / "Navigability"
  section rendered from a shared `navigabilityLines` helper, floats `%.3f`.
- **llms.txt**: one terse clause in the summary blockquote (compactness +
  typical click distance), included in the determinism contract.
- **Low-compactness notice:** a single **non-gating** stderr notice
  `[low-compactness]` is emitted only when `Documents >= 10 AND Compactness <
  0.1` — a large but very poorly connected corpus — mirroring the existing
  `gaps-truncated` / `reachability-indeterminate` notices. It never changes the
  exit code.

### Determinism (ADR 0004)

Domain code imports stdlib only (`math` is fine). All iteration is over the
sorted `documents` and sorted neighbour lists; float sums are accumulated in
that fixed order; the median is read from a histogram (no float sort). Two runs
over the same corpus — or the same corpus with shuffled input order — yield
byte-identical scalars.

## Consequences

- A compact, trackable health summary of the whole corpus, queryable by agents
  over MCP and present in graph.json.
- Scalars, not findings: navigability never breaks a build, so it is safe to
  ship on by default.
- The streaming APSP helper is the foundation for P10 betweenness centrality.
- **Small-worldness `S` is DEFERRED.** `S` compares clustering and path length
  against a random-graph baseline (`S = (C/C_rand)/(L/L_rand)`); choosing a
  defensible, deterministic random baseline is a design decision of its own and
  is left to a later ADR. Clustering and characteristic path length — its two
  inputs — are reported now, so `S` can be added later without recomputation.
