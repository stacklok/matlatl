# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **OKF v0.1 conformance mode ([ADR 0023](docs/adr/0023-okf-conformance-mode.md)).**
  An opt-in `--okf` flag (and `.matlatl.yml okf: true` key) checks a repo against
  Google's [Open Knowledge Format](https://github.com/GoogleCloudPlatform/knowledge-catalog)
  v0.1 §9 conformance rules and reports a CONFORMANT / NOT CONFORMANT verdict:
  R1 every non-reserved `.md` has a parseable YAML frontmatter block; R2 that
  frontmatter carries a non-empty `type` (the value is never validated against a
  list — OKF forbids a central registry); R3 reserved files (`index.md`/`log.md`
  only — `README.md` is a concept doc) follow their §6/§7 structure. Three new
  Error-severity finding kinds (`okf-missing-frontmatter`, `okf-missing-type`,
  `okf-reserved-file-structure`) are produced **only** in the mode and gate
  `check` (exit 1) regardless of `--strict`. The verdict is reported **separately**
  from the health gate — a broken link is conformant per OKF, so it never makes a
  bundle NOT CONFORMANT, and `--okf` never relaxes the health checks (a superset
  gate). `findings.json` bumps to **schema v8**: the three kinds/counts plus an
  always-present top-level `okfConformance` object (`checked:false` when off);
  `graph.json` is unchanged (v7). Matlatl's own repo is not an OKF bundle, so
  `task dogfood` does not run `--okf`.

- **`emitExclude` ([ADR 0019](docs/adr/0019-emit-exclude.md)).** A new
  `.matlatl.yml` key that keeps documents **in the corpus** — link-checked,
  ranked, present in `graph.json`/`findings.json` — while dropping them from the
  **consumption surfaces**: `llms.txt`/`llms-full.txt`/`llms-small.txt`,
  `index.md` (via `emit` and `index`), and the `trails.json` reading orders,
  entries and rendered backlink clauses alike. Patterns use gitignore syntax
  (the same engine as `.matlatlignore`), so `.claude/agents/` excludes the
  subtree at any depth. Zero effect on `matlatl check` (byte-identical output
  and exit codes); the filtered artifacts state how many docs were excluded.
  The motivating case is agent scaffolding (`.claude/agents/**`,
  `.claude/skills/**`, `.agents/**`): it must stay link-checked, but no LLM
  navigates to it via `llms.txt`.

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

- **Hops-from-root discoverability signal ([ADR 0021](docs/adr/0021-hops-from-root.md)).**
  Each document now carries `hopsFromRoot` (shortest distance from the nearest
  root, `-1` when unreachable/indeterminate) in `graph.json`, and a non-gating
  `far-from-root` Info finding flags docs at/beyond the config-only
  `farFromRootThreshold` (default 6). Bumps `graph.json`/`findings.json` to
  **schema version 7** (additively).

### Changed

- **Root-absolute links resolve from the scan root ([ADR 0022](docs/adr/0022-root-absolute-links.md)).**
  A link with a single leading `/` (e.g. `/tables/orders.md`) now resolves from
  the scan root, independent of the linking document's directory — previously it
  was misresolved origin-relative (folded under the origin's folder) and reported
  as a broken link from any non-root document. `//host/...` stays external. This
  softens the `check` gate monotonically (it can only newly-resolve links, never
  newly break one) and needs no schema or version change.
- **Nested git repositories are skipped by default.** Submodules, linked
  worktrees, and nested clones are pruned from the scan wherever they live,
  detected by the presence of a `.git` entry inside a directory (a file for
  submodules/worktrees, a directory for nested clones). Each pruned tree emits one
  `skipped-nested-repo` notice. The scan root's own `.git` is exempt, so a normal
  repo never skips itself, and pointing matlatl directly at a submodule still
  scans it. The change softens the `check` gate monotonically (it can only remove
  files, never newly fail a build). See
  [ADR 0017](docs/adr/0017-nested-repo-scan-boundary.md).
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

### Fixed

- **Front-matter `name:` is now treated as a wikilink alias.** Alongside the
  `aliases:` list, a document's single-valued `name:` field is indexed into the
  alias table, so `[[name]]` resolves to that document (a `name`/alias shared by
  two docs is reported `Ambiguous`, never guessed). `name` is now a typed
  `FrontMatter` field and no longer leaks into `Extra`. Note this is **not**
  purely monotonic: turning a former broken wikilink into a valid edge **adds a
  graph edge**, which can shift derived values (orphan / unreachable /
  under-linked sets, PageRank, reading-order trails). It can also reclassify an
  already-broken `[[name#anchor]]` link from broken-link to broken-anchor (the
  name now matches a doc, but the fragment may not), but never turns a passing
  gate into a failing one — it cannot create a finding where there was none, so
  the `check` verdict stays monotonic (ADR 0005 unaffected). See
  [ADR 0001](docs/adr/0001-document-identity.md).
- **Links to an existing non-markdown directory now resolve to a NonNote asset,
  not a broken link.** A relative link to a real directory that holds no corpus
  markdown (e.g. `[examples](examples/)` pointing at a folder of code/assets)
  previously reported as a broken link; it now resolves to `NonNote` (it exists
  → not rot; confers no reachability), mirroring how an existing non-markdown
  *file* asset resolves. Symlinks remain excluded (the asset check uses `Lstat`,
  so a symlink is neither a regular file nor a directory — the load-bearing
  containment property, ADR 0003). A `.md`-named directory still resolves Broken.
  See [ADR 0008](docs/adr/0008-directory-links.md). The change softens `check`
  monotonically; ADR 0005 is unaffected.
- **Default-ignore the Python virtualenv + tooling caches.** `.venv`, `.tox`,
  `__pycache__`, `.mypy_cache`, `.pytest_cache`, and `.ruff_cache` are now
  skipped wholesale during the walk (matched by base name anywhere, like
  `node_modules`/`vendor`), so installed-package and tool-scratch markdown no
  longer pollutes the corpus. Build-output directories (`dist`, `build`,
  `target`, `site`, `out`) are deliberately left in scope — they can hold
  generated docs — and remain a per-repo `.matlatlignore` decision. The change
  softens `check` monotonically (it can only remove findings); ADR 0005 is
  unaffected. See
  [ADR 0010](docs/adr/0010-agent-scaffolding-roots-and-default-ignores.md).

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
