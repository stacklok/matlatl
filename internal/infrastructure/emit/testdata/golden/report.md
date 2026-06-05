# matlatl report

## Corpus overview

| Metric | Count |
| --- | --- |
| Documents | 13 |
| Headings | 24 |
| References | 24 |
| Components | 6 |
| Broken links | 3 |
| Broken anchors | 1 |
| Ambiguous links | 1 |
| Orphans | 2 |
| Unreachable | 6 |
| Knowledge gaps | 3 |

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

## Unreachable

_Not reachable from any root. Add an inbound link from a page that is itself reachable from a root._

- `docs/cycle/alpha.md` — Cycle Alpha
- `docs/cycle/beta.md` — Cycle Beta
- `docs/island/one.md` — Island One
- `docs/island/two.md` — Island Two
- `docs/links.md` — Link Showcase
- `docs/stray.md` — Stray Page

## Hubs and authorities

| Rank | Hub | Authority |
| --- | --- | --- |
| 1 | Link Showcase (0.665) | User Guide (0.626) |
| 2 | Project Home (0.475) | Overview (0.603) |
| 3 | User Guide (0.423) | Project Home (0.494) |
| 4 | Docs Index (0.242) | Cycle Alpha (0.000) |
| 5 | Overview (0.242) | Cycle Beta (0.000) |

## Knowledge gaps

_Experimental: pairs of disconnected document clusters that may warrant a bridge (ADR 0007)._

| Cluster A | Cluster B |
| --- | --- |
| README.md | docs/cycle/alpha.md |
| README.md | docs/island/one.md |
| docs/cycle/alpha.md | docs/island/one.md |
