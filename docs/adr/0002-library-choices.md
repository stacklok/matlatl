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
| Graph structure + DOT       | `github.com/dominikbraun/graph`          | Generics-based; provides BFS, `StronglyConnectedComponents` (Tarjan), adjacency/predecessor maps, and `draw.DOT`. Wrapped behind a domain interface. |
| CLI                         | `github.com/spf13/cobra`                 | Multi-verb command tree. |
| Ignore-file matching        | `github.com/sabhiram/go-gitignore`       | `.doctopusignore` with gitignore semantics. |
| MCP server                  | `github.com/mark3labs/mcp-go`            | Optional, isolated in `internal/infrastructure/mcpserver`; migrate to the official SDK when stable. |
| External link liveness      | stdlib `net/http`                        | Opt-in only; bounded concurrency + SSRF guard (ADR 0003). |

Hand-roll these (a library does **not** provide them, or the semantics are too
specific to delegate):

- **Wikilink inline parser** — a ~100-line goldmark `InlineParser` for `[[target]]`,
  `[[target|alias]]`, `[[target#anchor]]`, and `![[embed]]`. Keeps everything in one
  AST pass and yields our typed edges directly.
- **Weakly-connected components** — `dominikbraun/graph` ships Tarjan (strong) only.
  WCC is computed via union-find over the undirected projection.
- **HITS hub/authority scoring** — not provided by any chosen lib; iterative power
  method with deterministic (sorted) vertex iteration and fixed normalization.
- **Mermaid emitter** — no mature Go library emits Mermaid; `emicklei/dot` (initially
  proposed for "Mermaid mode") has none, so it is **not** used. DOT comes from
  `draw.DOT`; Mermaid is a small custom emitter.
- **Link/anchor resolver** and the **dual-audience emitters** — the core domain value.

## Consequences

- The domain layer is testable without goldmark.
- We own the trickiest correctness surfaces (wikilink parsing, resolution, slug
  fidelity) where library mismatches would otherwise bite.
- Determinism is our responsibility in every hand-rolled algorithm (sorted iteration),
  since Go map iteration order is randomized.
