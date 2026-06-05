<!-- markdownlint-disable MD041 -->
# 🕸️ matlatl

> Map the markdown in your repo into trees and graphs, find the orphans, and emit
> artifacts that are equally readable by **humans** and **LLMs**.

`matlatl` is Nahuatl for *net*: the tool casts a net over your repo's markdown —
every link a knot, every doc a node — then shows you the holes and what slipped
through (the orphans).

`matlatl` recursively reads the markdown documents in a repository, parses their
front matter, headings, and links (relative links, wikilinks, anchors, embeds),
and builds two overlaid structures:

- a **tree** — the folder / front-matter hierarchy plus intra-document section nesting, and
- a **graph** — the typed cross-reference relationships between documents and sections.

From those it detects **orphans** (truly isolated docs) and **unreachable** docs
(no path from a root such as `README.md`), **broken links**, and **broken anchors**,
then renders the result for two audiences:

- **Humans** — a colorized terminal report, a committable Markdown report, Mermaid and
  Graphviz/DOT diagrams, and a navigable `index.md`.
- **LLMs** — a compact, queryable `graph.json`, an `llms.txt` family, and a
  `findings.json` where every finding is a self-contained, actionable fix instruction.

## Status

✅ Feature-complete across phases P0–P6: secure scan + parse, link/anchor
resolution, the reference graph with orphan/unreachable/component/HITS/gap
analysis, human emitters (terminal, Markdown, Mermaid, DOT, index), LLM
emitters (`graph.json`, the `llms.txt` family, actionable `findings.json`),
fan-out parsing, and a read-only MCP server. See [`docs/architecture.md`](docs/architecture.md)
and the [ADRs](docs/adr/).

## Quick start

```console
$ matlatl .                 # scan + analyze, print the terminal report
$ matlatl check .           # CI lint mode: non-zero exit on broken links/anchors
$ matlatl graph . --format mermaid
$ matlatl index .           # emit index.md + llms.txt
$ matlatl orphans .         # list orphans / unreachable docs
$ matlatl serve .           # MCP server exposing graph queries to agents
```

## Why another markdown tool?

Link checkers (lychee, markdown-link-check) validate links but build no graph.
Knowledge tools (Obsidian, Foam, Dendron, Quartz) visualize a graph but are not
CI-oriented and emit nothing an LLM can act on. `matlatl` treats a
**machine-readable, LLM-queryable graph as a first-class output** — the gap all of
that prior art leaves open.

## Documentation

- [User guide](docs/user-guide.md) — commands, flags, CI usage, the LLM artifacts
- [Developer guide](docs/dev-guide.md) — layout, rules, testing, how to contribute
- [Architecture](docs/architecture.md)
- [Architecture Decision Records](docs/adr/)
- [graph.json schema](docs/schemas/graph.schema.json) · [findings.json schema](docs/schemas/findings.schema.json)
- [Contributing](CONTRIBUTING.md) · [Changelog](CHANGELOG.md) · [Agent guide](AGENTS.md)

## License

Apache-2.0 © 2026 Stacklok, Inc. See [`LICENSE`](LICENSE).

`SPDX-License-Identifier: Apache-2.0`
