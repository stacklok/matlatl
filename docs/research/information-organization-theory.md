---
title: "Hyperlink theory & information organization — a design-research map for matlatl"
matlatl: orphan-intentional
---

# Hyperlink theory & information organization: what the field already knows, and how matlatl can use it

> Status: research note, not a binding decision. The aim is to place matlatl in
> the ~80-year lineage of people who have asked "how do you organize linked
> documents so a reader can find their way?" — and to convert that lineage into a
> prioritized, determinism-preserving feature roadmap.
>
> Marked `orphan-intentional` so matlatl doesn't flag its own research note.

## 0. The thesis

matlatl is not solving a new problem. It is solving a problem with a deep, named,
heavily-instrumented literature stretching from **1945 (Bush's memex)** through
**hypertext usability (1987–1992)**, **citation analysis (1955–1973)**,
**information architecture (1960–2005)**, and the **network science of the web
(1998–today)**. Almost every question matlatl asks — *is this doc reachable? is it
an orphan? which docs are authoritative? where are the gaps?* — has a formal name,
a metric, and decades of validation somewhere in that literature.

The practical upshot: matlatl today implements a strong *connectivity* core
(reachability, components, HITS, binary orphans) but a deliberately **naive
"knowledge-gap" heuristic** and **no navigability metrics**. The literature offers
a set of upgrades that are (a) directly on-point, (b) **computable from the graph
matlatl already builds**, and (c) **fully deterministic** — which matters because
determinism is a load-bearing project value (golden tests, byte-stable artifacts,
ADR 0007). That last property lets us reach for most of these *without* betraying
the architecture.

---

## 1. The intellectual lineage

### 1.1 The visionaries (1945–1965): association as the unit of thought

- **Vannevar Bush — "As We May Think" (1945), the Memex.** The founding idea: human
  memory works by *association*, and a knowledge machine should let you build
  **"associative trails"** — named, saved, *re-traversable* paths linking documents
  in the order a mind connected them. Bush's trail is not a single link; it is a
  *curated walk* through many. **This is the most under-exploited idea for matlatl**
  (see §4-I: trails for agents).
- **Douglas Engelbart — "Augmenting Human Intellect" (1962), NLS.** Linking,
  view-control, and structured outlines in service of *augmenting* a working mind,
  not replacing it. matlatl's "dual audience" framing (human + agent) is squarely in
  this tradition: the graph augments whoever is navigating.
- **Ted Nelson — Project Xanadu (1965–), coined "hypertext".** Three Nelson ideas
  bear directly on matlatl:
  1. **Two-way (bidirectional) links.** On Nelson's network every node knows what
     points *at* it. The web threw this away; matlatl quietly restores it
     (`what-links-to`, backlinks). Worth making first-class in *rendered* output,
     not just the MCP query (§4-K).
  2. **Transclusion** — content living in one canonical place, included by reference
     elsewhere. matlatl already models `Transclusion` as a navigational edge type.
  3. **The broken-link problem as the original sin.** Xanadu's obsession with
     *permanent* links is precisely the rot matlatl's `check` gate fights. We are,
     in a real sense, building the link-integrity layer Nelson wanted.
  - Nelson's **"intertwingularity"** ("everything is deeply intertwingled") is the
    honest counter-warning: knowledge resists clean hierarchy. matlatl's decision to
    overlay a **tree *and* a graph** (ADR 0007) is the right response — neither alone
    is faithful.

### 1.2 The hypertext-usability era (1987–1999): "lost in hyperspace"

This is the period most directly applicable to matlatl, because it asked: *given a
hyperdocument, is it actually navigable, and can we measure that?*

- **Jeff Conklin — "Hypertext: An Introduction and Survey" (IEEE Computer, 1987).**
  Named the two failure modes that still define the field:
  - **Disorientation** — "where am I, and how do I get to what I want?"
  - **Cognitive overhead** — the mental cost of deciding *which* link to follow.
  matlatl's orphan/unreachable findings are disorientation defects made
  machine-checkable. The metrics matlatl is *missing* (path length, scent) are the
  cognitive-overhead side.
- **Botafogo, Rivlin & Shneiderman — "Structural Analysis of Hypertexts: Identifying
  Hierarchies and Useful Metrics" (ACM TOIS, 1992).** The single most on-point paper
  for matlatl. It takes a hyperdocument graph and derives two scalar health metrics,
  both normalized to `[0,1]`:
  - **Compactness (Cp)** — how interconnected the hyperdocument is. Computed from the
    *converted distance matrix* (all-pairs shortest paths, with unreachable pairs
    assigned a finite conversion constant K), normalized between the
    fully-disconnected maximum and the fully-connected minimum.
    `Cp → 1` = everything reaches everything quickly (risk: a hairball with no
    structure); `Cp → 0` = fragmented islands. There is a healthy middle.
  - **Stratum (St)** — how *linear* the document is, i.e. how strongly it imposes a
    reading order (derived from Harary's node "prestige"). `St = 0` = no inherent
    order (a cycle or a complete graph); `St = 1` = a pure sequence (read A→B→C).
  - The paper also formalizes **converting a graph into a hierarchy** (finding the
    "best" tree over a tangled graph) — exactly matlatl's tree⊕graph overlay, 30
    years early.
- **Pirolli & Card — Information Foraging Theory (PARC, 1990s).** Borrowed optimal
  foraging from ecology: people (and now agents) navigating linked information behave
  like animals foraging between food **patches**, following **information scent** —
  the *proximal cues* (link text, titles, snippets) that predict the *distal* value
  of following a link. Two consequences for matlatl:
  - A link's **anchor text is a scent signal**. "click here" / "this" / "see" are
    *scent-free* — a measurable foraging defect (§4-G).
  - Docs that are *far* from everything else (high mean shortest-path) have weak
    scent trails leading to them — they are foraging-expensive even when technically
    reachable (§4-D).

### 1.3 Information & library science (1955–1973): documents relate through what they cite

The bibliometrics tradition solved "how do you tell two documents are related from
links alone?" — decades before PageRank, and **purely from topology** (no text, no
embeddings, fully deterministic):

- **Eugene Garfield — citation indexing (Science Citation Index, 1955).** Citations
  are a navigable, analyzable graph. The intellectual root of all link analysis.
- **Kessler — bibliographic coupling (1963).** Two documents are *coupled* if they
  **cite the same third document(s)**. Strength = size of the shared out-neighborhood.
  Coupling is *retrospective* and fixed at authoring time.
- **Small — co-citation (1973).** Two documents are related if they are **cited
  together** by the same later documents. Strength = size of the shared
  in-neighborhood. Co-citation is *prospective* and grows over time.
- **Ranganathan — faceted classification (Colon Classification, 1933).** Knowledge is
  better described by *multiple orthogonal facets* than one rigid tree — the
  intellectual basis for front-matter tags/facets (matlatl already parses front
  matter and could build facet indices).

**Why this matters enormously for matlatl:** bibliographic coupling and co-citation
are *link-prediction signals computable from the existing reference graph with zero
new dependencies and full determinism.* They are the principled replacement for
matlatl's current naive gap heuristic (§4-A).

### 1.4 Information architecture & wayfinding (1960–2005): docs are a *place*

- **Kevin Lynch — "The Image of the City" (1960).** People navigate cities via five
  elements: **paths, edges, districts, nodes, landmarks**. A city is *legible* when
  these are clear. The IA field (esp. **Rosenfeld & Morville**, the "polar bear book",
  and Morville's *Ambient Findability*, 2005) mapped this onto information spaces:
  - **Landmarks ↔ high-authority hub docs** (matlatl's HITS already finds these).
  - **Districts ↔ communities/clusters** (matlatl has hard components; soft
    communities are the upgrade — §4-H).
  - **Paths ↔ trails / reading sequences** (§4-I).
  - **Nodes ↔ decision points** = high-out-degree index pages.
  - *Legibility* is exactly what an orphan/unreachable report measures — and what a
    compactness/stratum score would summarize.

### 1.5 The network science of the web (1998–today): the graph is the object

- **Brin & Page — PageRank (1998).** Query-independent authority via the stationary
  distribution of a random walk. Robust global "importance."
- **Kleinberg — HITS (1999).** Hubs (pages that point to many authorities) and
  authorities (pages pointed to by many hubs). **matlatl already implements this**
  (`internal/domain/graphmodel/hits.go`) — it is ahead of most doc tools here.
- **Broder et al. — "Graph Structure in the Web" (2000), the bow-tie.** A large
  directed corpus decomposes around its **giant SCC (CORE)** into **IN** (reaches
  core, not reached by it), **OUT** (reached by core, doesn't return),
  **tendrils**, **tubes**, and **disconnected**. A far richer structural map than
  binary reachable/unreachable — and matlatl already computes SCC + reachability, so
  this is nearly free (§4-F).
- **Watts & Strogatz — small-world networks (1998).** Healthy navigable networks have
  **short characteristic path length** *and* **high clustering coefficient**. These
  are matlatl's missing "how many clicks apart is everything / how cliquey is it"
  metrics (§4-D).
- **Barabási & Albert — scale-free networks, preferential attachment (1999).** Real
  link graphs have a few mega-hubs and a long tail. Sanity-check for matlatl's HITS
  distribution; informs *expected* vs *anomalous* hub structure.
- **Community detection — Girvan-Newman modularity (2002), Louvain (2008), Leiden
  (2019).** Find **soft** clusters (densely intra-linked neighborhoods) that hard
  connected-components miss. The formal version of Lynch's "districts" (§4-H).
- **Liben-Nowell & Kleinberg — "The Link-Prediction Problem for Social Networks"
  (2003).** *Which absent edges should exist?* Validated that topology-only proximity
  scores — **common neighbors**, **Adamic/Adar** (common neighbors weighted by
  `1/log(degree)`), Jaccard, Katz — predict real future links. **This is the rigorous
  theory of "knowledge gaps,"** and Adamic/Adar is essentially weighted
  bibliographic-coupling + co-citation (§4-A).
- **Ronald Burt — "Structural Holes" (1992).** A **structural hole** is a gap between
  two clusters; the doc that **bridges** it has *brokerage* value. The bridge is
  captured by **betweenness centrality** (how often a node lies on shortest paths).
  Two readings for matlatl:
  - A high-betweenness doc is a **navigational single point of failure** — delete or
    orphan it and the corpus fragments (§4-B).
  - An *unfilled* structural hole between two topically-adjacent clusters is the
    *high-value* gap worth suggesting (the opposite of matlatl's current
    "any two islands" gap).

### 1.6 The modern note-linking movement (1990s–today)

- **Niklas Luhmann's Zettelkasten** and its software descendants (**Obsidian, Roam,
  Logseq, Foam, Dendron, Quartz**) re-popularized **backlinks**, **graph view**, and
  **emergent structure from linking**. matlatl's README explicitly positions against
  these as "visualize but not CI-oriented, emit nothing an agent can act on." The
  theory above is how matlatl can be *more* analytically serious than the graph-view
  toys while staying CI- and agent-first.

---

## 2. Where matlatl sits today (honest assessment)

| Question | Literature name | matlatl today | File |
| --- | --- | --- | --- |
| Reachable from an entry point? | Wikipedia orphan/dead-end; bow-tie IN/OUT | BFS reachability from root set | `orphans.go` |
| Truly isolated? | Orphan (no in-links) / dead-end (no out-links) | **Binary** `in==0 && out==0` | `orphans.go` |
| Which docs are important? | HITS, PageRank | **HITS (hub+authority)** ✅ | `hits.go` |
| What clusters exist? | Connected components; community detection | **Hard WCC/SCC** (union-find, Tarjan) | `components.go` |
| Where are the gaps? | Link prediction; structural holes | **Naive: every pair of distinct WCCs** ⚠️ | `gaps.go` |
| Is it navigable overall? | Compactness, stratum, small-worldness | **Nothing** ❌ | — |
| Do links give good cues? | Information scent | **Nothing** ❌ | — |
| Suggested reading order? | Associative trails; stratum | **Hierarchy tree only** | `hierarchy.go` |

**The three real limitations:**

1. **Gap detection is theory-naive.** `gaps.go` defines a gap as *any pair of
   distinct weakly-connected components above size 2* — an O(k²) enumeration that is
   correct-but-uninformative (every island pair is a "gap"). It has no notion of
   *which* pair is *worth* bridging. The literature's answer (coupling, co-citation,
   Adamic/Adar, structural holes) is both more useful and computable from the same
   graph.
2. **No navigability metrics.** There is no single number — or small panel — that
   says "this doc set is healthy/fragmented/a-hairball." Compactness + stratum +
   characteristic path length fill this and are the canonical, validated choices.
3. **The orphan model is binary.** Wikipedia (the largest live operationalization of
   this exact problem, 6M+ articles) uses *graduated* thresholds and separates
   *orphan* (no in-links) from *dead-end* (no out-links). matlatl requires *both*
   zero for "isolated," collapsing two distinct defects with distinct fixes.

---

## 3. The determinism constraint (the gate every idea must pass)

matlatl's identity is **deterministic, byte-stable, dependency-light, CI-grade**
output (ADR 0002, ADR 0007, golden tests). That rules some methods in and others out:

- **Fully deterministic, topology-only, no new deps — reach for these first:**
  bibliographic coupling, co-citation, Adamic/Adar, common-neighbor link prediction,
  betweenness centrality (Brandes), articulation points/bridges (Tarjan), compactness,
  stratum, characteristic path length, clustering coefficient, bow-tie classification,
  PageRank (same power-iteration discipline already used for HITS), graduated orphan
  tiers. **All of these are pure functions of the graph matlatl already builds.**
- **Deterministic but needs care:** community detection (Louvain/Leiden are
  order-sensitive — must fix iteration order and tie-breaks to keep golden tests
  stable, exactly as HITS already pins sorted iteration + epsilon).
- **Breaks determinism / adds a model dependency — keep opt-in and out of default
  output, like `--check-external` today:** embedding-based semantic similarity
  ("topically similar but unlinked"). Valuable, but it must live behind a flag and
  never enter the deterministic golden path. A **lexical** scent/similarity proxy
  (anchor-text vs target-title token overlap, TF-IDF over headings) is the
  deterministic middle ground.

---

## 4. The roadmap: theory → feature, prioritized

Ordered by (value × on-brand) ÷ effort. Each is annotated **[det]** if fully
deterministic (safe for the default golden path).

### A. Replace naive gaps with topology-based link prediction **[det]** — *highest leverage*
**Theory:** Liben-Nowell & Kleinberg link prediction; Kessler coupling; Small
co-citation; Burt structural holes.
**What:** Rank candidate *missing* edges. For every unlinked doc pair, score:
- **Bibliographic coupling** = `|out(A) ∩ out(B)|` (they cite the same docs), and
- **Co-citation** = `|in(A) ∩ in(B)|` (the same docs cite them), and
- **Adamic/Adar** = `Σ 1/log(deg(c))` over shared neighbors `c` (rare shared
  neighbors count more).
High score + currently unlinked = "these two docs are about the same thing and
should probably link." Replace (or gate behind) the current "every island pair"
gap. Optionally still report *disconnected* clusters, but rank bridge suggestions by
which *representative pair* has the highest coupling/co-citation — turning Info-noise
into ranked, actionable `findings.json` entries an agent can fix.
**Effort:** Medium. Pure additions to `graphmodel`; reuses `projAdj`/`projRev`.
**Risk note (Sandi Metz):** don't over-build into a full recommender. Coupling +
co-citation + Adamic/Adar is the right *next* step; embeddings can wait behind a flag.

### B. Betweenness centrality + articulation points → "broker / SPOF" findings **[det]**
**Theory:** Burt structural holes; Freeman betweenness; Tarjan bridges.
**What:** Compute betweenness (Brandes' algorithm, O(V·E)). High-betweenness docs are
navigational brokers — surface them as *load-bearing* ("X% of shortest paths route
through `architecture.md`; if it's removed or orphaned the corpus fragments"). Add
**articulation-point / bridge** detection (cheap, exact) → "this single doc/link is
the only connector between cluster X and the rest." Complements HITS (authority ≠
brokerage; a humble doc can be a critical bridge).
**Effort:** Medium. New file `centrality.go`; deterministic with sorted iteration.

### C. Compactness + stratum as corpus-health scalars **[det]** — *most on-brand*
**Theory:** Botafogo, Rivlin & Shneiderman (1992).
**What:** Two `[0,1]` numbers in the report and `graph.json`:
- **Compactness** — fragmented (→0) vs hairball (→1), with a healthy mid-band.
- **Stratum** — how linear/ordered the docs are (a tutorial series → high; a
  reference wiki → low). Lets matlatl say *what kind* of doc set this is.
Both come from one all-pairs-shortest-path pass (BFS from each node, O(V·E); the 5k
benchmark shows the corpus is small enough — peak heap ~32 MiB). This is the
"navigability score" matlatl currently lacks, from *the* canonical paper.
**Effort:** Medium. New `navigability.go`; one APSP helper feeds C + D.

### D. Small-world diagnostics: characteristic path length + clustering coefficient **[det]**
**Theory:** Watts & Strogatz (1998); Conklin's disorientation.
**What:** From the same APSP pass: **characteristic path length** (median mean
shortest path — "how many clicks between any two docs," a direct disorientation
proxy) and **clustering coefficient** (how cliquey). Flag individual docs whose mean
distance to the rest is an outlier — "reachable but far; weak scent." Report
small-worldness `S = (C/C_rand)/(L/L_rand)` as a one-line health read.
**Effort:** Low once C's APSP helper exists.

### E. Graduated orphan tiers, split orphan vs dead-end **[det]** — *cheap, high-value*
**Theory:** Wikipedia:Orphan & Wikipedia:Dead-end (the largest real operationalization).
**What:** Stop collapsing two defects:
- **Orphan / under-linked** = low *inbound* (configurable threshold; Wikipedia: 1
  removes the tag, 3+ ensures discoverability). Graduated, not binary.
- **Dead-end** = no *outbound* internal links (a foraging dead-end; distinct fix —
  "add onward links").
Keep `in==0 && out==0` as the most-severe "fully isolated." This matches matlatl's
existing finding-with-a-fix philosophy and the battle-tested Wikipedia thresholds.
**Effort:** Low. Mostly in `orphans.go` + finding kinds + config knob.

### F. Bow-tie classification of the corpus **[det]**
**Theory:** Broder et al. (2000).
**What:** Label every doc CORE / IN / OUT / tendril / disconnected relative to the
giant SCC. Richer than binary reachable/unreachable; gives the agent view a "here is
the navigable heart (CORE), here are the entry funnels (IN), here are the terminal
pages (OUT)" map. Almost free: matlatl already has SCC + reachability.
**Effort:** Low–Medium. Derive from existing SCC + BFS in/out of the giant SCC.

### G. Information-scent scoring of anchor text **[det, lexical]** — *novel, agent-relevant*
**Theory:** Pirolli & Card information scent.
**What:** Score each link by how well its **anchor text predicts the target** (token
overlap between anchor text and target title/headings; flag scent-free anchors like
"click here", "here", "this", "link", bare URLs). Low-scent links are a measurable
foraging defect and a concrete `findings.json` fix ("rename this link to name its
target"). Especially valuable for *agents*, which rely entirely on link text as scent
(no hover, no visual context). Deterministic — pure lexical, no model.
**Effort:** Medium. New analyzer over resolved references + heading inventory.

### H. Soft communities (modularity) → "districts" **[det with pinned order]**
**Theory:** Newman modularity; Louvain/Leiden; Lynch's districts.
**What:** Detect densely-linked topical neighborhoods *within* connected components
(WCC can't see these). Improves index.md grouping, feeds better gap suggestions
("doc is topically in district X but under-linked to it"), and gives the graph view
real "districts." Report modularity as a structure-strength scalar.
**Effort:** Medium–High. Must pin iteration order + tie-breaks for determinism
(Louvain is order-sensitive) — same discipline already applied to HITS.

### I. Associative trails — guided reading paths for agents **[det]** — *north-star*
**Theory:** Bush's memex trails; Engelbart; stratum.
**What:** Emit **ordered traversals** ("trails") from each root through its
cluster — a reading sequence that visits high-authority hubs early (HITS) and follows
the hierarchy/stratum, so an LLM onboarding to a repo gets an *optimal order* to read
the docs rather than a flat list. This is Bush's 1945 associative trail, reified as a
first-class artifact for acting agents — and it's the strongest expression of
matlatl's "graph-for-agents" positioning. Could ship as a new emitter
(`trails.json` / a `## Suggested reading order` block in `llms.txt`).
**Effort:** Medium. Composes existing hierarchy + reachability + HITS; new emitter.

### J. PageRank alongside HITS **[det]**
**Theory:** Brin & Page (1998).
**What:** Query-independent authority via random-walk stationary distribution. More
stable global "importance" than HITS; pairs well with it (HITS = hub/authority duo,
PageRank = single robust importance). Reuses the power-iteration + sorted-iteration +
epsilon discipline already in `hits.go`.
**Effort:** Low. Sibling of `hits.go`.

### K. First-class backlinks in rendered output **[det]**
**Theory:** Nelson's two-way links.
**What:** matlatl already answers `what-links-to` over MCP; render a **Backlinks**
section per doc in `index.md` (and optionally inject into `llms.txt`). Makes the
two-way nature of the graph visible to humans, not just queryable by agents.
**Effort:** Low. New rendering over the already-built reverse projection.

### L. (Opt-in, non-default) embedding-based semantic gaps **[NOT det — flag-gated]**
**Theory:** Vector space model (Salton); LSI (Deerwester 1990); modern embeddings.
**What:** "Topically similar but unlinked" via embeddings — the strongest gap signal,
but non-deterministic and model-dependent. Must live behind a flag and stay **out of
the default golden path**, exactly like `--check-external` keeps network results out
of deterministic output today. Document it as the explicit boundary of the
deterministic core.
**Effort:** High + an opt-in dependency. Lowest priority; the topology-only methods
(A) deliver most of the value deterministically first.

---

## 5. North star

Today matlatl is "a link checker that also builds a graph." The literature points at
something sharper:

> **matlatl as a navigability instrument and trail engine** — it doesn't just tell
> you what's broken; it scores how *findable* your docs are (compactness, stratum,
> path length), names the *load-bearing* and *missing* connections (betweenness,
> link prediction), and hands an agent an *optimal reading order* (trails). One
> graph, three readings: a human's legibility report, a machine's queryable model,
> and an agent's guided walk.

The sequencing that respects the architecture: **A → C/D → E → B/F** (all
deterministic, all from the existing graph) deliver the analytical leap first; **G,
I, H, J, K** round out scent, trails, districts, importance, and backlinks; **L**
(semantics) stays an opt-in frontier, never the default.

---

## 6. Primary sources

- Bush, V. (1945). *As We May Think.* The Atlantic. — memex, associative trails.
- Engelbart, D. (1962). *Augmenting Human Intellect: A Conceptual Framework.*
- Nelson, T. (1965– ). *Project Xanadu* — hypertext, transclusion, two-way links,
  intertwingularity.
- Conklin, J. (1987). *Hypertext: An Introduction and Survey.* IEEE Computer 20(9).
- Botafogo, R., Rivlin, E., Shneiderman, B. (1992). *Structural Analysis of
  Hypertexts: Identifying Hierarchies and Useful Metrics.* ACM TOIS 10(2), 142–180.
- Pirolli, P., Card, S. (1999). *Information Foraging.* Psychological Review;
  *Information Foraging Theory* (2007), PARC.
- Garfield, E. (1955). *Citation Indexes for Science.* Science.
- Kessler, M. M. (1963). *Bibliographic coupling between scientific papers.*
- Small, H. (1973). *Co-citation in the scientific literature.* JASIS 24(4).
- Ranganathan, S. R. (1933). *Colon Classification* — faceted classification.
- Lynch, K. (1960). *The Image of the City* — paths, edges, districts, nodes, landmarks.
- Rosenfeld, L., Morville, P. (1998– ). *Information Architecture for the WWW*;
  Morville, P. (2005). *Ambient Findability.*
- Brin, S., Page, L. (1998). *The Anatomy of a Large-Scale Hypertextual Web Search
  Engine* (PageRank).
- Kleinberg, J. (1999). *Authoritative Sources in a Hyperlinked Environment* (HITS).
- Broder, A. et al. (2000). *Graph Structure in the Web* (bow-tie).
- Watts, D., Strogatz, S. (1998). *Collective dynamics of 'small-world' networks.*
  Nature.
- Barabási, A.-L., Albert, R. (1999). *Emergence of scaling in random networks.*
- Newman, M., Girvan, M. (2004). *Finding and evaluating community structure*;
  Blondel et al. (2008) Louvain; Traag et al. (2019) Leiden.
- Liben-Nowell, D., Kleinberg, J. (2003/2007). *The Link-Prediction Problem for
  Social Networks.* JASIST; Adamic, L., Adar, E. (2003). *Friends and neighbors on
  the web.*
- Burt, R. (1992). *Structural Holes: The Social Structure of Competition.*
- Luhmann, N. — *Zettelkasten*; and the Obsidian/Roam/Foam/Dendron lineage.

## See also

- [matlatl-and-the-llm-wiki-pattern.md](matlatl-and-the-llm-wiki-pattern.md) —
  a related note: where matlatl fits when an LLM agent *writes and maintains* the
  wiki (the ingest/query/lint loop), rather than only reading it.
