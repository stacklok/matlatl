# 4. DDD layering and full-fat v1 scope

Date: 2026-06-05
Status: Accepted

## Context

The tool is a single-process, batch CLI. We want Domain-Driven Design's clarity
(a ubiquitous language, a dependency-pure core) without importing vocabulary that
fights Go idioms (transactional aggregates, repository indirection for
single-implementation collaborators).

The product owner chose **full-fat scope**: all analyses (orphan/unreachable,
weak + strong components, HITS, knowledge-gap detection), sections as first-class
graph vertices, the MCP server, and the full emitter set.

## Decision

**Layering.** Three layers with a strict dependency direction; the domain imports
nothing outward:

```
cmd/doctopus/            thin entrypoint; wires Cobra → application
internal/domain/         pure types + logic, no I/O, no goldmark
  corpus/                Document, Section, FrontMatter, Corpus, HeadingInventory, AliasTable
  reference/             Reference, LinkType, LinkHealth, ResolvedTarget, LinkResolver, ResolutionPolicy
  graphmodel/            ReferenceGraph, HierarchyTree, OrphanDetector, ComponentAnalyzer, HitsScorer, GapDetector, RootSetResolver
  analysis/              AnalysisReport, Finding, Diagnostic, Severity
internal/application/    pipeline orchestration, Config, ports (interfaces)
internal/infrastructure/ fsscanner, mdparser (goldmark), emit/*, mcpserver
internal/platform/       logging, exit-code mapping, version
```

**Go-idiomatic DDD, not ceremony.** We keep: ubiquitous language in package/type
names; the domain depending on nothing; goldmark quarantined to one package; data
built once and treated as immutable thereafter. We drop: transactional/aggregate
phrasing, and upfront `port` interfaces for collaborators that have exactly one
implementation. **Interfaces are introduced at real test seams only** (parser,
scanner, artifact writer), not as an architectural mandate.

**Sections as graph vertices (full-fat).** Both `Document` and `Section` are
vertices. Mixed-granularity semantics are specified precisely *before* Phase 3
implementation and covered by known-answer tests: a document's reachability/orphan
status is computed on a defined projection (an edge into any of a document's
sections counts as reaching the document for reachability; "isolated orphan" =
the document and all its sections have in=out=0). HITS and components run on the
full mixed graph with documented node-type handling.

**Determinism is part of the contract.** Every algorithm iterates vertices/edges in
sorted order; every emitter sorts its output. Golden tests assert byte-stable output.

## Consequences

- A larger surface to build and test, accepted deliberately for v1.
- The DDD structure aids navigation and testing without Go anti-patterns.
- The Section-as-vertex semantics are the riskiest design area and get an explicit
  spec + tests at the Phase 3 gate.
