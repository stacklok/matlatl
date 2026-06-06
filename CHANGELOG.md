# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

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
