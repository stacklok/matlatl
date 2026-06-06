# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Agent-experience analyses (P11, [ADR 0016](docs/adr/0016-agent-experience.md)).**
  Four new signals over the built graph, all deterministic and non-gating:
  - **PageRank** — the random-surfer global-importance scalar (Brin & Page 1998),
    computed beside HITS. Surfaced as a per-node `pageRank` and a top-level
    `pageRank` block in `graph.json`, and an "Importance (PageRank)" section in
    the human reports.
  - **Reading-order trails** (Bush 1945) — per-cluster, topologically-valid
    reading orders that prefer higher-PageRank docs among the available frontier.
    Shipped in the `emit` bundle as a new **`trails.json`** (schema version 1) and
    a "Suggested reading order" block in `llms.txt`.
  - **Backlinks** (Nelson/Xanadu two-way links) — every document now shows what
    links to it, in `index.md` (a Backlinks column) and `llms.txt` (a
    "linked from" clause). Derived from the existing edges; no array added to
    `graph.json`.
  - **Information scent** (Pirolli & Card 1999) — a new non-gating Info finding
    `low-scent-anchor` flags links whose anchor text barely previews the target
    (Jaccard of anchor tokens vs. the target's title below 0.20), with a suggested
    rename. Anchor/display text is now threaded from the parser through to the
    graph edge. Stable-identifier anchors (e.g. "ADR 0010") and directory-link
    expansion edges (ADR 0008) are deliberately not flagged.

### Changed

- **Machine-artifact schemas bumped (additively).** `graph.json` is now
  **schema version 6** (adds per-node `pageRank` and a top-level `pageRank` block,
  on top of v5's per-node `bowtie` / `underLinked` / `deadEnd` / `betweenness` /
  `isArticulation`; top-level `suggestedLinks`, `articulationPoints`, `bridges`;
  `summary.navigability` + `summary.betweenness`), and `findings.json` is now
  **schema version 6** (adds the `low-scent-anchor` kind + `lowScentAnchor`
  summary count, on top of v5's `under-linked`, `dead-end`, `suggested-link`,
  `articulation-point`, `bridge`). A new **`trails.json`** (schema version 1) is
  emitted in the bundle. Existing consumers keep working; the new fields are
  additive. Emitter types and the published JSON Schemas are kept in lockstep and
  validated in tests.
- **`matlatl serve` now speaks MCP over streamable HTTP instead of stdio.** The
  endpoint is served at `/mcp` on `--address` (default `127.0.0.1:8080`); the
  serving context drives a graceful drain of in-flight requests on shutdown.
  Containerized deployments should bind `--address 0.0.0.0:PORT`.
- **Reachability roots are now exempt from the isolated-orphan finding** as well
  as from the unreachable finding. This applies to **any root — configured via
  `--root` or detected by a convention** (`README.md`/`index.md`/`SKILL.md` or
  `type: index`): a declared entry point with no inbound links is its purpose, not
  a defect. In practice this only affects edgeless roots (a root with outbound
  links is already non-isolated). The change softens the `check` gate
  monotonically (it can only remove findings, never newly fail a build). See
  [ADR 0010](docs/adr/0010-agent-scaffolding-roots-and-default-ignores.md).
- **Renamed the project from `doctopus` to `matlatl`** (Nahuatl for *net*; the
  tool casts a net over a repo's markdown). The module path is now
  `github.com/stacklok/matlatl`, the binary and command are `matlatl`, the ignore
  file is `.matlatlignore`, and the intentional-orphan front-matter key is
  `matlatl: orphan-intentional`. The `tool` field in `graph.json`/`findings.json`
  and the MCP server name are now `matlatl`.

### Added

- **Graduated structure findings + bow-tie classification** — the binary orphan
  check is now a tiered ladder: **orphan** (no inbound *and* no outbound links,
  most severe) → **dead-end** (inbound but nothing onward) → **under-linked**
  (fewer inbound links than the discoverability threshold). `under-linked` and
  `dead-end` default to **Info** severity (never fail `check`) and can be promoted
  to `warning` via the `structureFindingsSeverity` config key; `inboundThreshold`
  (default 3, plus `--inbound-threshold`) sets the under-linked line. Every doc is
  also classified into a **bow-tie** bucket (core / in / out / tendril /
  disconnected) relative to the giant SCC, surfaced as data in `graph.json`, the
  report, and MCP. See [ADR 0012](docs/adr/0012-graduated-structure-and-bowtie.md).
- **Topology-based link prediction (`suggested-link`)** — ranked "these two docs
  should link" suggestions from bibliographic coupling (`|out∩out|`), co-citation
  (`|in∩in|`), and Adamic/Adar, over currently-unlinked pairs. Info severity;
  **augments** (does not replace) the disconnected-cluster knowledge-gap signal.
  New read-only MCP tool `suggest-links` (doc-scoped or global top-N). See
  [ADR 0013](docs/adr/0013-topology-link-prediction.md).
- **Navigability metrics** — a corpus-level health panel reported as scalars
  (never gate `check`): **compactness** and **stratum** (Botafogo/Rivlin/
  Shneiderman) over the directed projection, plus **characteristic / median path
  length**, **diameter**, and **clustering coefficient** over the undirected
  closure. A non-gating `[low-compactness]` notice fires only on a non-trivial
  corpus (≥10 docs) with compactness < 0.1. See
  [ADR 0014](docs/adr/0014-navigability-metrics.md).
- **Critical-path analysis** — **betweenness centrality** (Brandes, directed)
  surfaces the **load-bearing docs** that navigation flows through (per-doc data +
  top-N, like HITS), and **articulation points** + **bridges** (iterative Tarjan
  over the undirected closure) flag the single docs/links whose removal fragments
  the corpus as Info findings (`articulation-point`, `bridge`) plus `graph.json`
  data. New read-only MCP tool `critical-docs`. See
  [ADR 0015](docs/adr/0015-critical-path-analysis.md).
- **`.matlatl.yml` per-repo configuration file** — an optional, committed file at
  the scan root (sibling of `.matlatlignore`) that declares **additional
  reachability `roots`** (path globs, same `path.Match` semantics as `--root`),
  **unioned** with the auto-detected conventions and any `--root` flags. It
  carries an explicit integer `version` (supported: `1`). This lets a repo declare
  e.g. `.claude/agents/*.md` as roots so its edgeless agent docs stop being
  reported as isolated orphans — with zero tool-specific knowledge baked into
  matlatl. v1 is roots-only (`.matlatlignore` stays the sole ignore mechanism; run
  behavior stays flag-only). A malformed/unsupported config exits 2 (usage); an
  unknown key or a missing version is tolerated with a notice. See
  [ADR 0011](docs/adr/0011-per-repo-config-file.md) and
  [the schema reference](docs/schemas/matlatl-config-v1.md).
- **`matlatl fix-prompt [path]`** — emits a self-contained, agent-agnostic prompt
  (findings and per-kind how-to embedded inline) that instructs an LLM coding
  agent to fix the documentation findings: `matlatl fix-prompt . | claude -p`.
  `--errors-only` narrows it to broken links/anchors; `--out` writes
  `fix-prompt.md`. It is a generator, not a gate (always exits 0). See
  [ADR 0009](docs/adr/0009-fix-prompt-acting-agents.md).
- Persistent `--root <glob>` flag to designate extra reachability roots on top of
  the autodetected ones (`README.md`/`index.md`/`SKILL.md`/`type: index`), feeding
  `graphmodel.ResolveRootSet` (ADR 0007).
- **`SKILL.md` is now an auto-detected reachability root** by filename
  (case-insensitive), peer to `README.md`/`index.md` — the tool-agnostic
  agent-skills manifest convention. Only the *filename* is matched; no directory
  path (e.g. `.claude/...`) is baked in (those remain per-repo config). See
  [ADR 0010](docs/adr/0010-agent-scaffolding-roots-and-default-ignores.md).
- **`.claude/plans/` is now default-ignored** (scan-root-relative), alongside
  `.claude/worktrees/` — transient agent scratch that would otherwise add noise.
  `.claude/agents`, `.claude/agent-memory`, `.claude/skills`, and `.claude/rules`
  are deliberately left in the corpus. See
  [ADR 0010](docs/adr/0010-agent-scaffolding-roots-and-default-ignores.md).
- Published `docs/schemas/findings.schema.json` (schema version 2) for the
  `findings.json` artifact, validated against emitted output in tests.
- `LICENSE` (Apache-2.0), `CONTRIBUTING.md`, `AGENTS.md`, and this changelog.
- Repo-root `.matlatlignore` (excludes test fixtures) and a committed `llms.txt`
  generated from the project's own docs; `task llms` / `task dogfood` regenerate
  it and run the strict doc-link-rot gate, mirrored in CI.

## [0.1.0] - 2026-06-05

Initial release. Feature-complete across phases P0–P6.

### Added

- **Scan + parse** — secure recursive markdown scan for untrusted repos (root
  containment, resource caps, `.matlatlignore`), goldmark + front matter + a
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

[Unreleased]: https://github.com/stacklok/matlatl/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/stacklok/matlatl/releases/tag/v0.1.0
