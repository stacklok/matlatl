# matlatl architecture

> Working overview of how `matlatl` is structured and why. The binding decisions
> live in the [ADRs](adr/); this document is the readable map over them.

## 1. What it does

`matlatl` runs a six-stage pipeline over a repository's markdown:

```
Scan ─▶ Parse ─▶ Resolve ─▶ Build graph/tree ─▶ Analyze ─▶ Emit
```

1. **Scan** — walk the repo, respect `.matlatlignore`, enforce the security
   boundary and resource caps (ADR 0003).
2. **Parse** — goldmark + front matter + the custom wikilink parser turn each file
   into a pure-domain `Document` (front matter, section tree, raw references).
3. **Resolve** — the `LinkResolver` turns each raw reference into a typed, health-
   classified edge (valid / broken / broken-anchor / non-note / ambiguous / external),
   using the `HeadingInventory` and `AliasTable`.
4. **Build** — assemble the directed `ReferenceGraph` (documents + sections as
   vertices, typed edges) and the `HierarchyTree`. Node/edge semantics and the
   document projection that analysis runs over are pinned in
   [ADR 0007](adr/0007-graph-node-semantics.md); directory links (`adr/`) resolve
   and confer reachability per [ADR 0008](adr/0008-directory-links.md).
5. **Analyze** — reachability from the root set, the graduated structure ladder
   (isolated orphan → dead-end → under-linked) plus orthogonal unreachable
   classification, weak + strong components, bow-tie classification relative to
   the giant SCC, HITS hub/authority, knowledge-gap detection, topology-based
   link prediction (the additive `suggested-link` signal), corpus-level
   navigability metrics, and critical-path analysis (betweenness centrality +
   articulation points / bridges) → a frozen
   `AnalysisReport` + `GraphMetrics`
   ([ADR 0007](adr/0007-graph-node-semantics.md),
   [ADR 0012](adr/0012-graduated-structure-and-bowtie.md),
   [ADR 0013](adr/0013-topology-link-prediction.md),
   [ADR 0014](adr/0014-navigability-metrics.md),
   [ADR 0015](adr/0015-critical-path-analysis.md)). `GraphMetrics`
   carries `Graph`, `Hierarchy`, `RootSet`, `Reachability`, `Degrees`, `Orphans`
   (isolated / dead-end / under-linked / unreachable), `WCC`, `SCC`, `Bowtie`,
   `HITS`, `Gaps`, `SuggestedLinks`, `Navigability`, `Betweenness`, and
   `Critical` (articulation points + bridges). Navigability and betweenness reuse
   a shared streaming APSP family in `apsp.go`: `ForEachSourceDistances` runs one
   BFS per source reusing a single distance map, never materializing a V² matrix
   (`O(V·(V+E))` time, `O(V)` transient memory); the sibling `ForEachSourceBFS`
   adds the discovery order, shortest-path predecessors and path counts that
   Brandes' betweenness needs (`centrality.go`), keeping the same streaming
   shape. Articulation points / bridges run an **iterative** Tarjan over the
   undirected closure (`articulation.go`), stack-safe like the SCC pass — the
   directed (betweenness) / undirected (cut structure) split mirrors ADR 0014.
6. **Emit** — render the report for humans (terminal, Markdown, Mermaid, DOT,
   index.md) and LLMs (graph.json, llms.txt family, findings.json; JUnit via `check`).

## 2. Layering

Three layers, strict inward dependency (see ADR 0004). The **domain imports nothing
outward** and never imports goldmark; the **infrastructure** layer owns all I/O and
third-party parsing. The **application** layer orchestrates the pipeline and defines
the few interfaces that mark real test seams.

```
cmd/matlatl → internal/application → internal/domain
                         │
                         └── internal/infrastructure (implements ports)
```

## 3. Core model

| Concept            | Meaning |
| ------------------ | ------- |
| `DocumentID`       | Canonical repo-relative path — the only identity (ADR 0001). |
| `Document`         | One markdown file: front matter, root `Section`, outbound raw references. |
| `Section`          | A heading-scoped node (level, text, anchor slug, span). A graph vertex (ADR 0004). |
| `Reference`        | A directed edge: link type, raw target, fragment, resolved target, health. |
| `HeadingInventory` | `map[DocumentID]set[Anchor]` — drives cross-file anchor validation (ADR 0006). |
| `ReferenceGraph`   | Directed graph over documents + sections; typed edges. |
| `HierarchyTree`    | Folder / front-matter `parent` tree, overlaid with section nesting. |
| `AnalysisReport`   | Immutable result: findings + projections every emitter renders from. |

## 4. Cross-cutting principles

- **Determinism** — sorted iteration everywhere; byte-stable artifacts; golden tests.
- **Security first** — root containment, resource caps, output-path sanitization,
  label escaping (ADR 0003), tested with adversarial fixtures.
- **Dual audience** — every analysis result has a human shape and a machine shape.
- **Concurrency is earned** — only the **parse** stage fans out (P6). Scan
  produces a file list; a bounded worker pool (default `GOMAXPROCS`, capped at
  16, configurable via `Config.ParseWorkers`) parses files in parallel, each
  worker using its **own** parser from `DocumentParserFactory.Clone()` (goldmark
  instances are not safe to share). Results are **sorted by `DocumentID` and
  merged on a single goroutine**, so the corpus, heading inventory, and every
  artifact are **byte-identical** to the single-threaded path at any worker
  count (proven by `TestPipeline_Determinism_AcrossWorkerCounts` at 1/2/8
  workers, run under `-race`). Resolution, graph build and analysis stay
  single-threaded over the **frozen** corpus (`Corpus.Freeze()` enforces it).

## 4a. Performance (P6 benchmark)

`BenchmarkPipeline_5kDocs` measures a full scan→analyze over ~5,000 synthetic
cross-linked docs (~27k references). On a 12-core arm64 machine:

| workers           | wall-time / op | total alloc / op | allocs / op |
| ----------------- | -------------- | ---------------- | ----------- |
| 1 (single-thread) | ~280 ms        | ~249 MB          | ~1.43 M     |
| auto (GOMAXPROCS) | ~235 ms        | ~249 MB          | ~1.43 M     |

Peak resident heap for the run is ~32 MiB (`TestPipeline_5kDocs_MemoryCeiling`,
asserted under a 1 GiB total-allocation ceiling) — the model is **linear**
(O(V+E)), not the feared in-memory-everything blow-up. Wall-time is `O(V+E)`:
5k docs analyze in ~0.24 s end-to-end (`time matlatl <5k-dir>`).

**Recommendation.** Fan-out parsing yields a **modest ~15–20%** wall-time
improvement at 5k docs (parsing is a minority of the total, and the
single-threaded merge + analysis dominate); allocation/memory is unchanged
(merge is sequential). The win grows with corpus size and parse cost, and there
is **no determinism or memory cost**, so concurrency is **ON by default**
(`ParseWorkers: 0` → `GOMAXPROCS`). `ParseWorkers: 1` forces the sequential path
for debugging or pathological small corpora.

## 4b. Opt-in external link checking (`--check-external`)

OFF by default (determinism + speed). When enabled, an infrastructure HTTP
checker (`internal/infrastructure/linkcheck`, the only outbound-network package)
validates `HealthExternal` http(s) links with bounded concurrency, per-host rate
limiting, a redirect cap, and URL de-duplication, producing `DeadLink` findings
that are kept **out** of the default deterministic output. A **mandatory SSRF
guard** (ADR 0003) refuses loopback/link-local/metadata (`169.254.169.254`) and
private RFC1918/ULA ranges and non-http(s) schemes, re-checks every redirect
target, and resolves the host and checks the **resolved IP** (defeating
DNS-rebinding-to-internal). The resolver and transport are injectable so tests
prove internal targets are refused **without a network call**.

## 4c. MCP server (`matlatl serve`)

`matlatl serve [path]` runs the analysis once and exposes read-only MCP tools
over **streamable HTTP** (`github.com/mark3labs/mcp-go`, isolated in
`internal/infrastructure/mcpserver` — the only package importing the MCP lib):
`what-links-to`, `list-orphans`, `path-between`, `get-section`,
`corpus-summary` (the graph.json manifest), `suggest-links` (topology-based
suggested links, doc-scoped or global top-N; ADR 0013), and `critical-docs` (the
critical-path structure — top load-bearing docs by betweenness, plus articulation
points and bridges; ADR 0015). The endpoint is served at `/mcp` on
`--address` (default `127.0.0.1:8080`); the serving context drives a graceful
drain on shutdown. Tools reuse the `emit.View` + `emit/graphjson` layers and
validate every `DocumentID` against the corpus.

## 5. Delivery

Built in phases P0–P6 (skeleton → scan/parse → resolve/check → graph/analysis →
human emitters → LLM emitters → MCP/concurrency), then extended by phases P7–P10
(graduated structure + bow-tie → topology link prediction → navigability metrics
→ critical-path analysis). Each phase: architect plan → separate implementation →
expert panel review (spec conformance, domain correctness, QA/test-adequacy; plus
concurrency, security, duplication, library-vs-handroll, idiomacy) → fixes →
small commits → user gate.
