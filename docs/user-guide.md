<!-- markdownlint-disable MD013 -->
# matlatl user guide

`matlatl` reads the markdown in a repository, works out how the documents link
to each other, and tells you what's **broken** (dead links, bad anchors),
what's **lost** (orphans and unreachable pages), and how the docs are
**shaped** (hubs, clusters, knowledge gaps). It produces output for people
*and* for LLMs/agents.

## Install

```console
$ go install github.com/stacklok/matlatl/cmd/matlatl@latest
# or, from a clone:
$ task build   # produces ./bin/matlatl
```

## The 30-second tour

```console
$ matlatl .              # colorized report of the current repo
$ matlatl check .        # CI gate: non-zero exit if links/anchors are broken
$ matlatl orphans .      # what nothing links to / what's unreachable
$ matlatl emit --out ai  # write the full human + LLM artifact bundle to ./ai
```

## Commands

| Command | What it does |
| --- | --- |
| `matlatl [path]` | Scan + analyze, print a colorized terminal report (default). `--quiet` for a one-line summary. |
| `matlatl check [path]` | Validate links/anchors as a **CI gate**. See exit codes below. |
| `matlatl report [path]` | Render a committable Markdown analysis report (`--out` to write `report.md`). |
| `matlatl graph [path]` | Emit the reference graph: `--format mermaid` (default), `dot`, or `json`. |
| `matlatl index [path]` | Emit a navigation surface: `index.md`, or an `llms.txt` family artifact (`--llms`, `--full`, `--small`, `--graph`). |
| `matlatl orphans [path]` | List isolated orphans, under-linked and dead-end docs (`--isolated-only`) and unreachable docs (`--unreachable-only`). |
| `matlatl emit [path] --out <dir>` | Write the whole bundle: `index.md`, `llms.txt`, `llms-full.txt`, `llms-small.txt`, `graph.json`, `findings.json`. |
| `matlatl fix-prompt [path]` | Emit an agent-agnostic prompt (findings embedded inline) that tells an LLM coding agent how to fix them. Pipe it to any agent; `--errors-only` for broken links/anchors only; `--out` to write `fix-prompt.md`. |
| `matlatl serve [path]` | Run the read-only MCP server (streamable HTTP) so an agent can query the graph. |
| `matlatl version` | Print version information. |

## Global flags

| Flag | Meaning |
| --- | --- |
| `--out <dir>` | Write artifacts into `<dir>` (paths are sanitized to stay inside it). |
| `--strict` | Promote orphan/ambiguous **warnings** to **failures** (affects `check`'s exit code). |
| `--check-external` | Opt in to HTTP liveness checks of external links (off by default; see below). |
| `--root <glob>` | Add reachability root(s) on top of the autodetected ones (`README.md`/`index.md`/`SKILL.md` by filename, plus `type: index`). Repeatable and/or comma-separated; matched against document paths with `path.Match` (a single `*` does **not** cross `/`, and `**` is unsupported — e.g. `docs/*.md`, not `docs/**`). |
| `--no-color` | Disable ANSI color. `NO_COLOR` env and non-TTY output are also honored. |
| `--quiet` / `--verbose` | Less / more output. |
| `--resolution <policy>` | (`check`) `exact` \| `longest-suffix` (default) \| `basename`. |

## What it detects

- **Broken links** — a link/wikilink whose target isn't a document in the repo.
- **Broken anchors** — `other.md#heading` where that heading doesn't exist. Slugs
  are GitHub-style (lowercase, spaces→`-`); see [ADR 0006](adr/0006-slug-dialect.md).
- **Ambiguous links** — e.g. `[[notes]]` when two `notes.md` exist. matlatl
  refuses to guess and shows you the candidates.
- **Orphans** — documents with **no** inbound or outbound links (truly isolated).
- **Under-linked** — documents that link onward but have **fewer inbound links
  than the discoverability threshold** (default **3**; see `inboundThreshold`).
  Hard to discover, even though not isolated. *Info severity by default* — never
  fails `check` unless you promote it (see `structureFindingsSeverity`).
- **Dead-ends** — documents that have inbound links but **link to nothing
  onward**. Navigation stops there. Also *Info severity by default*.
  (These three — orphan, under-linked, dead-end — form a single **graduated
  ladder**: each non-exempt doc lands in at most one, most-severe first; see
  [ADR 0012](adr/0012-graduated-structure-and-bowtie.md).)
- **Unreachable** — documents you can't reach by following links from a root
  (`README.md`, `index.md`, `SKILL.md`, or a `type: index` front-matter doc).
  They differ from orphans: an unreachable doc may link outward but nothing leads
  *to* it. Unreachable is **orthogonal** to under-linked/dead-end (a doc can be
  both); only a fully-isolated orphan suppresses it. **Any root** — whether
  configured with `--root` or detected by a convention
  (`README.md`/`index.md`/`SKILL.md`/`type: index`) — is itself exempt from
  **all** of the orphan/under-linked/dead-end/unreachable findings: a declared
  entry point with no inbound links is its purpose, not a defect
  ([ADR 0010](adr/0010-agent-scaffolding-roots-and-default-ignores.md)).
- **Knowledge gaps** — clusters of docs that are disconnected from each other.
- **Suggested links** — pairs of docs that **share navigational neighbours but
  don't link to each other**, surfaced by topology-based link prediction
  (Adamic/Adar over the undirected neighbour closure, plus bibliographic coupling
  and co-citation). An *additive* signal alongside knowledge gaps: gaps flag
  wholly-disconnected clusters; suggestions flag concrete unlinked pairs that look
  related. *Info severity, experimental* — **never** fails `check`, even under
  `--strict`. See [ADR 0013](adr/0013-topology-link-prediction.md).
- **Bow-tie structure** — a one-line read of the corpus's macro-shape relative to
  its giant strongly-connected core: how many docs are *core* / *in* (feed the
  core) / *out* (lead away from it) / *tendril* / *disconnected*. Reported in the
  human report, `graph.json`, and over MCP — it is descriptive **data**, not a
  finding.

Orphans, under-linked, dead-end and unreachable docs come with **different**
remediation hints, because the fix differs: link an orphan in (or delete it); add
inbound links to an under-linked doc; add onward links from a dead-end; give an
unreachable doc an inbound link from somewhere reachable.

### Tuning the structure findings

- `--inbound-threshold N` (or `inboundThreshold: N` in `.matlatl.yml`) sets the
  under-linked discoverability threshold. Default `3`; `<=0` normalizes to `3`.
- `structureFindingsSeverity: warning` in `.matlatl.yml` promotes **both**
  under-linked and dead-end from Info to Warning, so they fail `check --strict`
  like orphans/unreachable. Default `info` (never fails the build).
- `linkSuggestionMinShared: N` in `.matlatl.yml` sets the minimum shared-neighbour
  count an unlinked pair must have to be surfaced as a **suggested link**
  (ADR 0013). Config-only (no CLI flag); default `2`, `<=0` normalizes to `2`.
  Lower it to `1` for more (noisier) suggestions; raise it to tighten the signal.

#### Reading the bow-tie summary

- **core** — the central cycle every other tier orbits; a healthy hub-and-spoke
  corpus has a sizeable core.
- **in** — docs that reach the core but the core doesn't reach back (entry
  funnels).
- **out** — docs the core reaches but that don't lead back (terminal branches).
- **tendril** — attached to the core's neighbourhood but neither feeding nor fed
  by it.
- **disconnected** — a separate island entirely.
- A report that says **"no cyclic core"** means the corpus is acyclic (every SCC
  is a singleton) — common and not a problem; the in/out/tendril/disconnected
  counts still describe the shape.

### Keeping a doc intentionally unlinked

Add to its front matter:

```yaml
---
matlatl: orphan-intentional
---
```

It will still appear in the graph but won't be reported as an orphan/unreachable.

## Using it in CI

`matlatl check` is the gate. Exit codes ([ADR 0005](adr/0005-exit-code-contract.md)):

| Code | Meaning |
| --- | --- |
| `0` | Clean (or `--strict` not tripped). An empty repo is also `0`. |
| `1` | Broken links/anchors found (with `--strict`, also orphans/ambiguous). |
| `2` | Usage error (bad flags/args). |
| `3` | Runtime error (unreadable path, I/O failure). |

```yaml
# .github/workflows/docs.yml
- run: matlatl check . --out matlatl-out
- uses: actions/upload-artifact@v4
  if: always()
  with: { name: matlatl, path: matlatl-out }   # findings.json + junit.xml
```

`check --out <dir>` always writes `findings.json` and `junit.xml` — on pass *and*
fail — so dashboards get structured results either way.

## The LLM artifacts

`matlatl emit --out <dir>` produces a bundle designed for agents:

- **`graph.json`** — the machine-queryable corpus manifest (schema **version
  3**): nodes (with importance scores, per-node `bowtie`/`underLinked`/`deadEnd`),
  edges, orphans, under-linked/dead-end, a `bowtie` structure summary, broken
  links, components, gaps, and `suggestedLinks` (topology-based suggestions, each
  with `sharedNeighbours`/`coupling`/`coCitation`/`adamicAdar`). Validated against
  [`docs/schemas/graph.schema.json`](schemas/graph.schema.json) and byte-stable
  run to run.
- **`llms.txt`** — a curated index, most-important docs first, with a
  "Known gaps" section flagging what's incomplete (per the `llms.txt` convention).
- **`llms-full.txt` / `llms-small.txt`** — concatenated clean bodies (each with a
  short context header) / a tight hubs-only subset for small context windows.
- **`findings.json`** — every finding is self-contained and actionable, plus a
  `remediationGuide` so an agent can fix issues without extra context. Validated
  against [`docs/schemas/findings.schema.json`](schemas/findings.schema.json)
  (schema version 4) and byte-stable run to run.

### Fixing findings with an agent

`matlatl fix-prompt` writes a single, self-contained prompt that tells an LLM
coding agent how to fix the findings — the findings (and a per-kind how-to) are
embedded inline, so the prompt needs no other context. Pipe it straight into any
agent:

```console
$ matlatl fix-prompt . | claude -p
$ matlatl fix-prompt . --errors-only   # only broken links/anchors (severity=error)
$ matlatl fix-prompt . --out ai        # write ai/fix-prompt.md instead of stdout
```

The prompt is **agent-agnostic** — it names no harness-specific tools — and bakes
its guardrails into the text: fix only the listed findings, don't invent files/
headings/facts, skip intentional orphans and links into code/directories, prefer
skipping when a target is ambiguous, and verify with `matlatl check` afterward.
`fix-prompt` is a generator, not a gate: it always exits 0 (a clean corpus yields
a short no-op prompt). The hosting agent/harness still owns sandboxing.

### Live queries for agents (MCP)

```console
$ matlatl serve .
$ matlatl serve . --address 0.0.0.0:9000   # bind elsewhere (e.g. for containers)
```

Speaks MCP over **streamable HTTP** at `/mcp` on `--address` (default
`127.0.0.1:8080`) and exposes read-only tools: `what-links-to`,
`list-orphans`, `path-between`, `get-section`, `corpus-summary`, and
`suggest-links` (topology-based suggested links — pass a `doc` to scope it to one
document's partners, or omit it for the global top suggestions). The server runs
until interrupted and drains in-flight requests on shutdown.

## Ignoring files

Create a `.matlatlignore` (gitignore syntax). `.git`, `node_modules`, and
`vendor` are ignored by default (matched by directory name, anywhere in the
tree). `.claude/worktrees/` (Claude Code agent worktrees — each is a full copy of
the repo, which would otherwise multiply the corpus many times over) and
`.claude/plans/` (transient scratch plans) are ignored too, matched only at the
scan root — so `.claude/rules/`, `.claude/skills/`, and the rest of `.claude/`
stay in the graph
([ADR 0010](adr/0010-agent-scaffolding-roots-and-default-ignores.md)).

## Declaring extra roots (`.matlatl.yml`)

For roots you want **committed and durable** — not retyped on every command line
— add a `.matlatl.yml` at the scan root (a sibling of `.matlatlignore`; read only
there, never up the tree). It's optional: absent means current defaults.

```yaml
# .matlatl.yml
version: 1
roots:
  - ".claude/agents/*.md"
```

Its `roots` are path globs (same `path.Match` semantics as `--root`: a single
`*` does not cross `/`, `**` is unsupported) **unioned** with the autodetected
conventions and any `--root` flags — `roots = conventions ∪ .matlatl.yml ∪
--root`. Because a root is exempt from both the unreachable and the
isolated-orphan findings ([ADR 0010](adr/0010-agent-scaffolding-roots-and-default-ignores.md)),
declaring `.claude/agents/*.md` as roots stops edgeless agent files — entry
points nothing links to by design — from being reported as isolated orphans, with
no tool-specific path baked into matlatl.

**`.matlatlignore` vs `.matlatl.yml`** — different jobs: `.matlatlignore`
**removes** files from the corpus; `.matlatl.yml` **declares the role** of files
that stay in it (entry-point roots). Use the ignore file to drop noise, the config
to mark entry points.

The `version` field (integer; supported: `1`) is the forward-compatibility
anchor — pin it. A missing version is assumed `1` (with a notice); a malformed
file, a wrong-typed field, or a `version` newer than this matlatl supports is a
usage error (exit 2); an unknown key (a typo, or a key from a newer matlatl) is
tolerated with a notice. The file is read only at the scan root, capped at 1 MiB,
and its globs are string-matched against in-corpus documents (never a filesystem
read). v1 is **roots only**: `.matlatlignore` stays the sole ignore mechanism and
run behavior (`--strict`/`--out`/…) stays flag-only. See
[ADR 0011](adr/0011-per-repo-config-file.md) and the
[schema reference](schemas/matlatl-config-v1.md).

## Checking external links (opt-in)

`--check-external` enables HTTP liveness checks of external URLs. It's **off by
default** to keep runs fast and deterministic. When on, matlatl applies an SSRF
guard (refuses loopback, link-local, cloud-metadata, and private addresses, and
re-checks redirects against the resolved IP) — see
[ADR 0003](adr/0003-security-model.md).

## Troubleshooting

**"reachability indeterminate (no root found)"** — matlatl prints this notice
when it can't find any reachability root: no `README.md`/`index.md`/`SKILL.md` at
any depth and no `type: index` front-matter doc, and you didn't pass `--root`.
Rather than flag *every* document as unreachable (which would be noise), matlatl
skips reachability analysis entirely and says so. Orphan (isolated) detection
still runs. Fix it by adding a `README.md`/`index.md`, marking an entry doc with
`type: index` front matter, or passing an explicit `--root <glob>`. This notice
alone never fails `check` (per [ADR 0005](adr/0005-exit-code-contract.md)).

**Orphan vs. unreachable** — they are distinct findings with distinct fixes. An
**orphan** has *no* navigational links at all (in-degree 0 **and** out-degree 0):
link it in from a relevant page, or delete it. An **unreachable** doc *does* link
out (or is linked from another unreachable cluster) but no path leads to it from
a root: give it an inbound link from a page that is itself reachable. A doc that
is both is reported only as the more specific orphan. To silence either
intentionally, add `matlatl: orphan-intentional` front matter.

**`--strict` and directory links** — a directory link like `[the ADRs](adr/)`
always *resolves* (it is never a broken link). Under the default policy it also
confers reachability on the folder's direct children, so they aren't flagged. But
under `--strict` a directory link does **not** vouch for the folder's non-index
siblings ([ADR 0008](adr/0008-directory-links.md)): those docs surface as
orphans/unreachable and `check --strict` exits 1. The fix is to link the
individual docs explicitly (e.g. from an index or from prose) rather than relying
on the bare directory link.

## See also

- [Architecture overview](architecture.md)
- [Developer guide](dev-guide.md)
- [Architecture Decision Records](adr/)
