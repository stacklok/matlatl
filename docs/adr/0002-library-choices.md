# 2. Library choices

Date: 2026-06-05
Status: Accepted

## Context

We prefer well-maintained libraries over hand-rolled code, but only where a library
genuinely fits. A pre-code design review found several proposed libraries that do
**not** provide the capabilities attributed to them; those claims are corrected here.

## Decision

Use these libraries:

| Concern                     | Choice                                   | Notes |
| --------------------------- | ---------------------------------------- | ----- |
| Markdown → AST              | `github.com/yuin/goldmark`               | CommonMark, de-facto standard, stdlib-only deps. **Quarantined to `internal/infrastructure/mdparser`** — no other package imports it. |
| Front matter (YAML + TOML)  | `go.abhg.dev/goldmark/frontmatter`       | Single parse pass within goldmark; handles `---` YAML and `+++` TOML. |
| CLI                         | `github.com/spf13/cobra`                 | Multi-verb command tree. |
| Ignore-file matching        | `github.com/sabhiram/go-gitignore`       | `.doctopusignore` with gitignore semantics. |
| MCP server                  | `github.com/mark3labs/mcp-go`            | Optional, isolated in `internal/infrastructure/mcpserver`; migrate to the official SDK when stable. |
| External link liveness      | stdlib `net/http`                        | Opt-in only; bounded concurrency + SSRF guard (ADR 0003). |

Hand-roll these (a library does **not** provide them, or the semantics are too
specific to delegate):

- **Graph structure, components, and the DOT emitter** — `github.com/dominikbraun/graph`
  was initially chosen for the directed graph, `StronglyConnectedComponents` (Tarjan),
  and `draw.DOT`. In implementation we hand-rolled the whole graph layer instead and the
  dependency is **not** in `go.mod`. Rationale:
  - The graph is a tiny, purpose-built directed structure over `DocumentID`/`Section`
    vertices with typed edges and a navigational *projection* (ADR 0007). Adjacency,
    reverse adjacency, BFS reachability, and Tarjan SCC are a few dozen deterministic
    lines each; a generics graph lib adds a dependency without removing meaningful code.
  - **Determinism** is a hard contract: every traversal iterates sorted slices so output
    is byte-stable. Go map iteration is randomized, and a third-party graph's internal
    iteration order is not ours to control — owning it is simpler than constraining it.
  - **DOT** is hand-rolled so we keep a single choke-point for the ADR 0003 hostile-label
    escaping contract and full control over custom attributes (vertex size ∝ in-degree,
    fill ∝ component, red broken-target placeholders). `draw.DOT` renders from vertex
    attributes and gives us neither. See the DOT-library decision note in
    `internal/infrastructure/emit/diagram/dot.go`.
  - Keeping the graph hand-rolled also keeps the domain free of any graph library
    (ADR 0004 / 0007): the algorithms live in `internal/domain/graphmodel` with no
    third-party imports.
- **Wikilink inline parser** — a ~100-line goldmark `InlineParser` for `[[target]]`,
  `[[target|alias]]`, `[[target#anchor]]`, and `![[embed]]`. Keeps everything in one
  AST pass and yields our typed edges directly.
- **Weakly-connected components** — computed via union-find over the undirected
  projection (Tarjan only yields strong components).
- **HITS hub/authority scoring** — not provided by any chosen lib; iterative power
  method with deterministic (sorted) vertex iteration and fixed normalization.
- **Mermaid emitter** — no mature Go library emits Mermaid; `emicklei/dot` (initially
  proposed for "Mermaid mode") has none, so it is **not** used. Mermaid is a small
  custom emitter; DOT is hand-rolled too (see above).
- **Link/anchor resolver** and the **dual-audience emitters** — the core domain value.

## Consequences

- The domain layer is testable without goldmark.
- We own the trickiest correctness surfaces (wikilink parsing, resolution, slug
  fidelity) where library mismatches would otherwise bite.
- Determinism is our responsibility in every hand-rolled algorithm (sorted iteration),
  since Go map iteration order is randomized.
