# 7. Graph node semantics and the document projection

Date: 2026-06-05
Status: Accepted (superseded in part by ADR 0012)

## Context

`matlatl` models a corpus as a graph and runs reachability, orphan/unreachable
classification, weak/strong components, HITS, and knowledge-gap detection over
it. ADR 0004 made both Documents *and* Sections first-class vertices ("full-fat,
mixed granularity"). That raises a precise-semantics hazard the panel flagged: if
a section can have edges, what does "orphan" or "reachable" mean for a *document*
whose sections carry the edges? This ADR pins the exact model so the analyses are
unambiguous and golden-testable, and records that the P3 analysis algorithms are
hand-rolled (not delegated to a third-party graph library).

## Decision

### Vertices

- **Document** vertex per `corpus.Document`: `NodeKind = NodeKindDocument`,
  `NodeID = "<DocumentID>"`.
- **Section** vertex per heading `corpus.Section` (the synthetic level-0 root is
  NOT a vertex): `NodeKind = NodeKindSection`,
  `NodeID = "<DocumentID>#<slug>"`.

### Edges (two kinds)

- **CONTAINS** (structural): `Document → its top-level Sections`, and
  `Section → its child Sections`. Mirrors the section tree / hierarchy.
- **REFERENCE** (navigational): one edge per *resolved, in-corpus* reference.
  - Target vertex: the target **Document** for a document-level link
    (`other.md`), or the target **Section** for an anchored link
    (`other.md#heading` → the `other.md#heading` section vertex).
  - Origin vertex: the **containing Section** when the reference's source line
    falls inside a section's byte span (we have section spans + the ref line);
    otherwise the **Document** (e.g. front-matter-derived or pre-heading refs).
  - Carries the `reference.LinkType`.

Only references with `Health == Valid` and an in-corpus target become REFERENCE
edges. (Anchored links to a *valid* heading point at the section vertex; an
anchored link whose document is valid but whose anchor is broken is a finding,
not an edge.)

### The document projection (resolves the section-edge ambiguity)

All graph **analysis** runs on the **document projection**, defined as:

1. Collapse every section vertex into its owning document. An edge touching any
   section of document D counts as touching D.
2. **Drop CONTAINS edges.** Keep only REFERENCE edges.
3. Keep only edges whose `LinkType` is in the **navigational set** and whose
   target is in-corpus with `Health == Valid`.
4. Drop self-loops (a document linking to its own section) — they do not affect
   reachability or degree-based orphan detection.

**Navigational set (default):** `RelativeLink, Wikilink, Anchor, ImageEmbed,
Transclusion, FrontmatterRelated`. It is configurable. `HealthExternal` (and any
non-`Valid` edge) is **never** counted — external links neither reach nor are
reached. Documented and enforced in `graphmodel.DefaultNavigationalTypes`.

### Analyses (all on the document projection)

- **Reachability:** BFS from the **root set** over the projection's out-edges. A
  document is *reached* iff some root has a path to it.
- **Unreachable:** an in-corpus document **not reached** from the root set. It may
  still have outbound edges (it links out but nothing links to it from the
  reachable set).
- **Orphan (isolated):** a document with **in-degree 0 AND out-degree 0** in the
  projection — no inbound and no outbound navigational references at all. Orphan
  and Unreachable are **distinct finding kinds** with distinct fixes:
  - Orphan → "link it in from a relevant page, or delete it."
  - Unreachable → "add an inbound link from a page reachable from a root."
  (A document can be both; we emit the more specific Orphan and suppress the
  redundant Unreachable for the same doc.)
  ADR 0012 refines this orphan notion into a graduated ladder — fully-isolated
  (this bullet), **dead-end** (in>0, out==0), and **under-linked** (out>0, few
  inbound links) — with **in-degree 0 AND out-degree 0** remaining the
  most-severe orphan tier.
- **Components:** Weakly-Connected Components (undirected projection) and
  Strongly-Connected Components (directed projection).
- **HITS:** hub/authority scores over the directed projection.
- **Gap detection:** a **gap** is a pair of two *distinct* weakly-connected
  components that could plausibly be bridged → a bridge-candidate suggestion
  (experimental; see below).

### Sections are NOT analyzed

Sections remain first-class vertices for **representation** (graph.json in P5),
section-level backlinks, and the CONTAINS tree — but are **never** subject to
orphan/reachability analysis. A "section orphan" would be noise (most sections
have no inbound links by nature). This is stated explicitly so reviewers do not
expect section-level orphan findings.

### Intentional orphans

A document whose front matter sets **`matlatl: orphan-intentional`** (in
`FrontMatter.Extra` under key `matlatl`, value `orphan-intentional`) is excluded
from **Orphan and Unreachable** findings but still appears as a vertex and in all
other metrics. The exact key/value is fixed here so authors can opt out of noise
(e.g. changelogs, license files).

### Root set

The root set is the union of:

- configured `--root` globs (matched against DocumentIDs), and
- conventions: any `README.md` or `index.md` at **any depth**, and any document
  whose front matter declares `type: index`.

If the resulting root set is **empty**, reachability is **INDETERMINATE**: emit a
notice, do **not** mark every document unreachable, and (per ADR 0005) this alone
does not fail `check`. A **disconnected** root set is fine — BFS simply starts
from multiple roots. Orphan (isolated) detection is independent of the root set
and still runs when reachability is indeterminate.

### Hand-rolled algorithms (supersedes part of ADR 0002 for the analysis path)

BFS, union-find (WCC), Tarjan (SCC), and HITS are implemented by hand in
`internal/domain/graphmodel`, pure stdlib, with **sorted vertex/edge iteration
everywhere** (Go map order is randomized). Rationale:

- `dominikbraun/graph` ships Tarjan (SCC) but **not** WCC or HITS, so we would
  hand-roll those regardless.
- We need total control over determinism (component IDs, HITS tie-breaks,
  iteration order) for byte-stable artifacts and golden tests.
- The domain must import **no** third-party graph library (ADR 0004 purity).

ADR 0002's "use `dominikbraun/graph` for graph structure + DOT" stands only for
the **P4 DOT drawing** path in `internal/infrastructure/emit`, never for domain
analysis.

## Consequences

- The riskiest design area (section→document projection) has one written
  definition and known-answer tests at the P3 gate.
- Determinism is a tested contract: every algorithm yields identical output on
  shuffled input (component IDs by sorted-min-member; HITS L2-normalized with
  sorted iteration and a fixed iteration cap + epsilon).
- Gap detection is **experimental** and conservative. A **gap** is defined as a
  pair of two *distinct* weakly-connected components: two clusters of
  documentation that, by construction, have **zero** navigational links between
  them (that disconnection is exactly what makes them separate weak components),
  and so could plausibly be bridged. There is therefore **no cross-link
  threshold knob** — a non-zero cross-link count would merge the two clusters
  into one component, so a distinct-WCC pair always has zero cross-links. The
  only tuning knob is `MinComponentSize`, which drops trivial clusters; the
  pipeline sets it to **2**, because singletons are already reported as
  **orphans** (linking the same isolated file as both an orphan and a gap would
  be redundant) and because an unbounded set of singletons would make the O(k²)
  pair enumeration explode. Gap detection is hard-capped at `MaxGaps` (1000)
  with an explicit truncation notice (mirroring the scanner's `MaxFiles`
  truncation), and gaps are labeled **Info** severity so they never fail a
  build.
