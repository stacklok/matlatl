# matlatl report

## Corpus overview

| Metric | Count |
| --- | --- |
| Documents | 18 |
| Headings | 29 |
| References | 31 |
| Components | 6 |
| Broken links | 3 |
| Broken anchors | 1 |
| Ambiguous links | 1 |
| Orphans | 2 |
| Unreachable | 9 |
| Far from root | 0 |
| Under-linked | 8 |
| Dead-ends | 1 |
| Knowledge gaps | 3 |

**Structure: 3 core, 3 in, 2 out, 1 tendril, 9 disconnected**

## Navigability

- Compactness: 0.119 (0 = disconnected, 1 = fully connected)
- Stratum: 0.494 (0 = cyclic/symmetric, 1 = pure hierarchy)
- Characteristic path length: 2.000 (mean clicks between linked docs)
- Median path length: 2.000
- Diameter: 4 (longest shortest path)
- Clustering coefficient: 0.648
- Reachable pairs: 86

## Load-bearing docs

_Documents on the most shortest paths between other docs (betweenness centrality). These are the corpus' key connectors (ADR 0015)._

| Rank | Document | Betweenness |
| --- | --- | --- |
| 1 | Overview | 0.037 |
| 2 | Downstream Branch | 0.026 |
| 3 | User Guide | 0.018 |
| 4 | Project Home | 0.015 |
| 5 | Changelog | 0.000 |

## Critical structure

_Single points of failure in the link graph: articulation points (documents) and bridges (links) whose removal fragments the corpus (ADR 0015)._

**Articulation points** (removing one fragments the corpus):

- `README.md` — Project Home
- `docs/flow/branch.md` — Downstream Branch
- `docs/guide.md` — User Guide
- `docs/sub/overview.md` — Overview

**Bridges** (the only link between two clusters):

| From | To |
| --- | --- |
| README.md | docs/stray.md |
| docs/README.md | docs/guide.md |
| docs/cycle/alpha.md | docs/cycle/beta.md |
| docs/flow/aside.md | docs/flow/branch.md |
| docs/flow/branch.md | docs/flow/terminal.md |
| docs/flow/branch.md | docs/sub/overview.md |

## Broken links and anchors

| File | Line | Kind | Detail | Suggested fix |
| --- | --- | --- | --- | --- |
| docs/links.md | 13 | broken-link | wikilink link target "does-not-exist" does not resolve to a document in the corpus | Check that "does-not-exist" exists and is spelled correctly relative to "docs/links.md"; if it lives elsewhere, fix the path or move the file. |
| docs/links.md | 14 | broken-link | relative-link link target "nope.md" does not resolve to a document in the corpus | Check that "nope.md" exists and is spelled correctly relative to "docs/links.md"; if it lives elsewhere, fix the path or move the file. |
| docs/links.md | 22 | broken-link | relative-link link target "../../../../etc/passwd" does not resolve to a document in the corpus | Check that "../../../../etc/passwd" exists and is spelled correctly relative to "docs/links.md"; if it lives elsewhere, fix the path or move the file. |
| docs/links.md | 14 | broken-anchor | anchor #no-such-heading does not exist in "docs/guide.md" | Add a heading that slugifies to "no-such-heading" in "docs/guide.md", or update the fragment to match an existing heading (slugs are GitHub-style: lowercase, spaces to dashes). |

## Ambiguous links

| File | Line | Detail | Suggested fix |
| --- | --- | --- | --- |
| docs/links.md | 18 | link target "notes" is ambiguous; it matches 2 documents: docs/project/notes.md, docs/team/notes.md | Disambiguate "notes" by using a longer, unique path (e.g. one of: docs/project/notes.md, docs/team/notes.md). |

## Isolated orphans

_No inbound or outbound navigational links. Link them in from a relevant page, or delete them. To keep one intentionally unlinked, add front matter `matlatl: orphan-intentional`._

- `docs/project/notes.md` — Project Notes
- `docs/team/notes.md` — Team Notes

## Under-linked

_Fewer inbound links than the discoverability threshold. Add inbound links from related, more-connected pages so readers and agents can find them. To keep one intentionally sparse, add front matter `matlatl: orphan-intentional`._

- `docs/cycle/alpha.md` — Cycle Alpha
- `docs/cycle/beta.md` — Cycle Beta
- `docs/flow/aside.md` — Tendril Aside
- `docs/flow/branch.md` — Downstream Branch
- `docs/island/four.md` — Island Four
- `docs/island/three.md` — Island Three
- `docs/links.md` — Link Showcase
- `docs/stray.md` — Stray Page

## Dead-ends

_Have inbound links but link to nothing onward. Add onward internal links to related documents. To keep one intentionally terminal, add front matter `matlatl: orphan-intentional`._

- `docs/flow/terminal.md` — Terminal Page

## Unreachable

_Not reachable from any root. Add an inbound link from a page that is itself reachable from a root._

- `docs/cycle/alpha.md` — Cycle Alpha
- `docs/cycle/beta.md` — Cycle Beta
- `docs/flow/aside.md` — Tendril Aside
- `docs/island/four.md` — Island Four
- `docs/island/one.md` — Island One
- `docs/island/three.md` — Island Three
- `docs/island/two.md` — Island Two
- `docs/links.md` — Link Showcase
- `docs/stray.md` — Stray Page

## Far from root

_Reachable from a root but at or beyond the hop-distance threshold, so hard to discover by link traversal. Link them closer to an entry point (an index or a root). To keep one intentionally deep, add front matter `matlatl: orphan-intentional`._

None.

## Hubs and authorities

| Rank | Hub | Authority |
| --- | --- | --- |
| 1 | Link Showcase (0.654) | User Guide (0.639) |
| 2 | Project Home (0.471) | Overview (0.588) |
| 3 | User Guide (0.409) | Project Home (0.478) |
| 4 | Overview (0.296) | Downstream Branch (0.133) |
| 5 | Docs Index (0.245) | Island One (0.000) |

## Importance (PageRank)

_Documents ranked by PageRank: the random-surfer stationary distribution (Brin & Page 1998). High PageRank marks a globally-important doc (ADR 0016)._

| Rank | Document | PageRank |
| --- | --- | --- |
| 1 | Island One | 0.164 |
| 2 | Island Two | 0.164 |
| 3 | User Guide | 0.094 |
| 4 | Cycle Alpha | 0.089 |
| 5 | Cycle Beta | 0.089 |

## Knowledge gaps

_Experimental: pairs of disconnected document clusters that may warrant a bridge (ADR 0007)._

| Cluster A | Cluster B |
| --- | --- |
| README.md | docs/cycle/alpha.md |
| README.md | docs/island/four.md |
| docs/cycle/alpha.md | docs/island/four.md |

## Suggested links

_Experimental: unlinked document pairs that share neighbours, ranked by Adamic/Adar; topology suggests they may warrant a link (ADR 0013)._

| From | To | Shared | A/A score |
| --- | --- | --- | --- |
| docs/island/four.md | docs/island/three.md | 2 | 1.820478 |
