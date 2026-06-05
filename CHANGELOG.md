# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Persistent `--root <glob>` flag to designate extra reachability roots on top of
  the autodetected ones (`README.md`/`index.md`/`type: index`), feeding
  `graphmodel.ResolveRootSet` (ADR 0007).
- Published `docs/schemas/findings.schema.json` (schema version 2) for the
  `findings.json` artifact, validated against emitted output in tests.
- `LICENSE` (Apache-2.0), `CONTRIBUTING.md`, `AGENTS.md`, and this changelog.
- Repo-root `.doctopusignore` (excludes test fixtures) and a committed `llms.txt`
  generated from the project's own docs; `make llms` / `make dogfood` regenerate
  it and run the strict doc-link-rot gate, mirrored in CI.

## [0.1.0] - 2026-06-05

Initial release. Feature-complete across phases P0–P6.

### Added

- **Scan + parse** — secure recursive markdown scan for untrusted repos (root
  containment, resource caps, `.doctopusignore`), goldmark + front matter + a
  custom wikilink parser into a pure-domain corpus.
- **Resolve** — link/anchor resolution with `exact` / `longest-suffix` /
  `basename` policies, GitHub-style anchor slugs, and directory-link reachability
  (ADR 0008).
- **Analyze** — the reference graph over a document projection (ADR 0007):
  reachability, orphan vs. unreachable classification, weak/strong components,
  HITS hub/authority, and experimental knowledge-gap detection — all
  deterministic and hand-rolled.
- **Human emitters** — colorized terminal report, committable Markdown report,
  Mermaid and Graphviz/DOT diagrams, and `index.md`.
- **LLM emitters** — `graph.json` (schema v1), the `llms.txt` family
  (`llms.txt`/`llms-full.txt`/`llms-small.txt`), and an actionable `findings.json`
  (schema v2) with a self-contained `remediationGuide`.
- **CLI** — `check` (CI gate with the ADR 0005 exit-code contract), `report`,
  `graph`, `index`, `orphans`, `emit`, and `serve` (read-only MCP server).
- **Concurrency** — bounded fan-out parsing with a deterministic single-threaded
  merge (byte-identical output at any worker count).

[Unreleased]: https://github.com/stacklok/doctopus/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/stacklok/doctopus/releases/tag/v0.1.0
