# 16. Agent experience: PageRank, reading-order trails, backlinks, and information scent

Date: 2026-06-06
Status: Accepted

## Context

matlatl already maps a corpus's structure (orphans, components, bow-tie),
importance (HITS hub/authority), topology hints (link prediction, knowledge
gaps), navigability scalars, and critical-path structure (betweenness,
articulation points, bridges). What it did **not** answer are the questions an
*agent reading the corpus* asks:

- *Where should I start, and in what order should I read?* (a reading path)
- *What points at this document?* (incoming context, not just outgoing links)
- *Does this link tell me where it goes before I follow it?* (link quality)
- *Which documents are globally important?* — HITS answers a related but
  different question (hub vs. authority); the field's single global-importance
  scalar is **PageRank**, which we did not compute.

This ADR (P11) adds four agent-experience signals over the already-built graph,
all deterministic and pure-domain (ADR 0004).

## Decision

### PageRank = per-doc DATA (beside HITS), not a finding

PageRank is the random-surfer stationary distribution (Brin & Page 1998): a
single global-importance scalar. It is computed with **power iteration** over the
**directed** document projection and treated as pure data, exactly like HITS and
betweenness — **no finding kind**, **never** gates the exit code.

The load-bearing distinction from HITS: the per-edge term is
`PR[u]/outdeg(u)` — each contributing in-neighbour's score is **divided by its
out-degree** (HITS sums raw scores and L2-normalizes). Formula per node `v`:

```
newPR[v] = (1-d)/N + d*( Σ_{u→v} pr[u]/outdeg(u) + danglingSum/N )    d = 0.85
```

**Dangling nodes** (no out-links) have their mass redistributed uniformly
(`danglingSum/N`), so total mass is conserved (`Σ PR = 1`) and there is **no L2
normalization**. `danglingSum` is accumulated over the **sorted** document set
and the neighbour sum iterates the already-sorted `projRev[v]`, so every float
sum runs in a fixed order (CLAUDE.md). Convergence is the **L1** delta
`Σ|newPR-pr| < N·ε` (ε = 1e-6, max 100 iters). Empty graph → converged, no
scores; `N == 1` → 1.0.

It is surfaced as a per-node `pageRank` field and a top-level `pageRank` block
(`{topDocs:[{id,score}]}`) in graph.json — parallel to `betweenness` — plus an
"Importance (PageRank)" section in the human reports (reusing `topN = 5`).
We keep PageRank **beside** HITS rather than replacing it: HITS answers
"hub vs. authority" (two roles); PageRank answers "global importance" (one
scalar). They are complementary, and an agent benefits from both.

### Reading-order trails (Bush 1945), ranked by PageRank

Trails are per-weakly-connected-component **suggested reading orders** — the
modern realization of Vannevar Bush's associative *trails* ("As We May Think",
1945). The contract is precise:

> a **topologically-valid** reading order that prefers higher-authority
> (higher-**PageRank**) docs among the currently-available frontier.

This is **not** literal "hubs first": a high-authority **sink** is
topologically *late* and appears near the end — that is expected and correct
("prefer authority among the available frontier", not "most-important doc
first"). The algorithm is a priority Kahn topological sort over the **SCC
condensation** (so cycles cannot deadlock the order):

- `Condensation()` collapses each SCC to its representative (the sorted-min
  member = the existing `Component.ID`), guarding the self-edge case
  (`sv != sw`) so the condensation is acyclic; neighbour lists are sorted +
  de-duplicated.
- Per weak component, the **frontier** is the zero-in-degree SCC reps. Each step
  pops the rep with the highest **max-member PageRank** (tie: rep ID ascending),
  appends that SCC's members (a multi-node SCC emits members by PageRank DESC,
  then ID ASC), and decrements successors. The frontier is a **re-sorted slice**
  each pop (no heap, no map ranging for output) — fully deterministic.
- A trail's `Root` is the component's **highest-PageRank** doc (tie min-ID); it
  is the cluster's most-important member and is *not necessarily* the topological
  head of `Order`. A singleton component yields `[root]`.

Trails ship in the **emit bundle only** (no standalone CLI flag for v1): a new
`trails.json` (schema v1) and a `## Suggested reading order` block in `llms.txt`
(a `### {root}` heading per multi-doc trail + a numbered `[title](path)` list in
`Order`).

### Backlinks (Nelson/Xanadu two-way links)

Every document can now show **what links to it**, not just where it points
(Ted Nelson's Xanadu two-way links). Backlinks are derived from the existing
document projection's in-neighbours (`ProjectionIn(id)` — already sorted by path,
self-excluded), so there is **no redundant backlinks array in graph.json** (a
consumer derives them from the existing `edges`). They render in **both**:

- `index.md` — a new "Backlinks" column listing the source paths (or `-`).
- `llms.txt` — a terse ` (linked from: a.md, b.md)` clause on each curated entry.

### Information scent → `low-scent-anchor` Info finding (Pirolli & Card 1999)

A link's anchor text should preview where it leads ("information scent", Pirolli
& Card 1999). A generic "click here" or a label unrelated to the destination
gives a reader or agent weak scent. The analysis runs **on the graph** (the
resolved reference edges carry the anchor text + source line — we thread anchor
text through the parser → `reference.Reference` → `graphmodel.Edge`; we did
**not** add a `refs` parameter to `Analyze()`), per navigational in-corpus link:

- Normalize the anchor (lowercase, collapse whitespace, trim). If it is in the
  in-source **scent-free phrase set** (here, click here, this, read more, learn
  more, see, view, … — the full list lives in `scent.go`) → score 0.0. If the
  raw anchor is wholly **backtick-wrapped** (a code identifier) → **skip** (no
  finding).
- Tokenize anchor and the target's **title** (front-matter title → first H1 →
  DocumentID, via the shared `corpus.Document.Title` so emit and scent cannot
  drift): lowercase, split on non-(letter|digit), drop stopwords + length-1
  tokens, sort+dedup. Empty anchor token set (bare URL/numeric) → 0.0. If the
  title yields no tokens, fall back to the union of the target's heading texts.
- `score = Jaccard(anchorTokens, titleTokens) = |∩|/|∪|` via a sorted
  merge-walk (the single division is the only float). Emit a finding when
  `score < 0.20`.

It is a new finding kind `low-scent-anchor`, **Info**, that **NEVER** gates the
exit code (even `--strict`) — a discoverability hint, not a defect, mirroring
`suggested-link` (ADR 0013) and `articulation-point`/`bridge` (ADR 0015). The
suggested fix is to rename the anchor to the target's title.

**No count cap on scent findings.** Unlike link prediction (whose candidate
space is super-linear), scent findings are bounded by the link count (one per
navigational edge at most), so a cap is unnecessary. We state this explicitly
here, mirroring the no-silent-cap convention: scent is never silently truncated.

### Determinism and purity (ADR 0004, 0007)

All four analyses iterate sorted `g.documents` / sorted neighbour lists; PageRank
and the Jaccard/dangling sums run in fixed float order; trails use a re-sorted
frontier slice (no map ranging for output); scent findings are sorted by
`(Source, Line, Target, AnchorText)`. Float scores use the fixed-precision
`Float` wire type in graph.json. The domain imports only the standard library
(`math`, `unicode` OK) and sibling domain packages.

### Schema bumps

- graph.json `schemaVersion` 5 → **6** (per-node `pageRank` + top-level
  `pageRank` block).
- findings.json `schemaVersion` 5 → **6** (the `low-scent-anchor` kind +
  `lowScentAnchor` summary count).
- **New** `trails.json` `schemaVersion` **1**.

## Consequences

- Agents get a reading path (where to start, in what order), incoming context
  (backlinks), a global-importance scalar (PageRank), and a link-quality signal
  (scent) — the experience signals the structural analyses did not provide.
- The new finding kind is non-gating, so it adds signal without making green
  builds flaky.
- Five new domain files (`pagerank.go`, `trails.go`, `scent.go`, plus
  `Condensation()` in `components.go` and `Document.Title/HeadingTexts` in
  corpus) and one new emit subpackage (`emit/trails`) keep the additions
  isolated and mirror the existing analysis/emit shapes.
- Anchor text is now carried on `reference.RawReference` → `Reference` →
  `graphmodel.Edge`; it is pure data the resolver ignores (identity stays keyed
  on the target, ADR 0001).
