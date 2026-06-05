# doctopus architecture

> Working overview of how `doctopus` is structured and why. The binding decisions
> live in the [ADRs](adr/); this document is the readable map over them.

## 1. What it does

`doctopus` runs a six-stage pipeline over a repository's markdown:

```
Scan ─▶ Parse ─▶ Resolve ─▶ Build graph/tree ─▶ Analyze ─▶ Emit
```

1. **Scan** — walk the repo, respect `.doctopusignore`, enforce the security
   boundary and resource caps (ADR 0003).
2. **Parse** — goldmark + front matter + the custom wikilink parser turn each file
   into a pure-domain `Document` (front matter, section tree, raw references).
3. **Resolve** — the `LinkResolver` turns each raw reference into a typed, health-
   classified edge (valid / broken / broken-anchor / non-note / ambiguous / external),
   using the `HeadingInventory` and `AliasTable`.
4. **Build** — assemble the directed `ReferenceGraph` (documents + sections as
   vertices, typed edges) and the `HierarchyTree`.
5. **Analyze** — reachability from the root set, orphan vs. unreachable
   classification, weak + strong components, HITS hub/authority, knowledge-gap
   detection → a frozen `AnalysisReport`.
6. **Emit** — render the report for humans (terminal, Markdown, Mermaid, DOT,
   index.md) and LLMs (graph.json, llms.txt family, findings.json + JUnit).

## 2. Layering

Three layers, strict inward dependency (see ADR 0004). The **domain imports nothing
outward** and never imports goldmark; the **infrastructure** layer owns all I/O and
third-party parsing. The **application** layer orchestrates the pipeline and defines
the few interfaces that mark real test seams.

```
cmd/doctopus → internal/application → internal/domain
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
- **Concurrency is earned** — the pipeline is single-threaded until a 5k-doc
  benchmark justifies fan-out parsing (merge single-threaded; per-worker parser).

## 5. Delivery

Built in phases P0–P6 (skeleton → scan/parse → resolve/check → graph/analysis →
human emitters → LLM emitters → MCP/concurrency). Each phase: Go-expert
implementation → expert panel review (concurrency, security, QA, duplication,
library-vs-handroll, idiomacy) → fixes → small commits → user gate.
