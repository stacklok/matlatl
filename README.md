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

From those it detects a graduated set of structure problems — **orphans** (truly
isolated docs), **under-linked** docs (too few inbound links to be discoverable),
**dead-ends** (nothing onward) and **unreachable** docs (no path from a root such
as `README.md`) — plus **broken links** and **broken anchors**, and maps the
corpus's macro-shape with a **bow-tie** classification (core / in / out / tendril
/ disconnected) and a set of corpus-level **navigability metrics** (compactness,
stratum, characteristic/median path length, clustering coefficient, diameter) —
how connected, how hierarchical, and how many clicks apart the docs are. It also
runs **critical-path analysis**: the **load-bearing docs** (betweenness
centrality — the connectors most navigation flows through) and the **critical
structure** (articulation points and bridges — the single docs and links whose
removal fragments the corpus). It then renders the result for three audiences:

- **Humans** — a colorized terminal report, a committable Markdown report, Mermaid and
  Graphviz/DOT diagrams, and a navigable `index.md`.
- **LLMs (reading)** — a compact, queryable `graph.json`, an `llms.txt` family, and a
  `findings.json` where every finding is a self-contained, actionable fix instruction.
- **Acting agents** — `fix-prompt` emits a self-contained, agent-agnostic prompt
  that turns the findings into fixes: `matlatl fix-prompt . | claude -p`.

## Status

✅ Feature-complete across phases P0–P6: secure scan + parse, link/anchor
resolution, the reference graph with orphan/unreachable/component/HITS/gap
analysis plus topology-based link suggestions, human emitters (terminal,
Markdown, Mermaid, DOT, index), LLM
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
$ matlatl fix-prompt . | claude -p   # agent-ready prompt to fix the findings
$ matlatl serve .           # MCP server exposing graph queries to agents
```

## Why another markdown tool?

Link checkers (lychee, markdown-link-check) validate links but build no graph.
Knowledge tools (Obsidian, Foam, Dendron, Quartz) visualize a graph but are not
CI-oriented and emit nothing an agent can act on. `matlatl` builds the graph and
treats it as a **machine-readable, queryable first-class output**.

Broken-link and orphan checking is the way in: a `check` gate that fails a PR on
the rot a link checker would catch, plus the orphans, unreachable pages, and weak
spots in the graph that a flat link check can't see. But the real point is what
surrounds that graph. Your docs are now written and read by agents as much as by
people, so `matlatl` emits the graph (`graph.json`), a navigable `llms.txt`, and
findings an agent can fix straight from (`findings.json`, `fix-prompt`), and it
serves the whole thing over MCP. One graph, rendered for whoever needs it: a
human, an LLM reading, or an agent acting.

So, what happens when an agent has been scribbling in your repo? `matlatl` knows
the difference between your docs and an agent's scaffolding. A `SKILL.md` is an
entry point, not an orphan; agent worktrees and scratch plans are ignored by
default; and a `.matlatl.yml` lets a repo declare its own roots (say, your
sub-agent definitions) so the health check stays accurate instead of burying you
in false orphans.

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
