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
| Under-linked | 8 |
| Dead-ends | 1 |
| Knowledge gaps | 3 |

**Structure: 3 core, 3 in, 2 out, 1 tendril, 9 disconnected**

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

## Hubs and authorities

| Rank | Hub | Authority |
| --- | --- | --- |
| 1 | Link Showcase (0.654) | User Guide (0.639) |
| 2 | Project Home (0.471) | Overview (0.588) |
| 3 | User Guide (0.409) | Project Home (0.478) |
| 4 | Overview (0.296) | Downstream Branch (0.133) |
| 5 | Docs Index (0.245) | Island One (0.000) |

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
