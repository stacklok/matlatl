<!-- markdownlint-disable MD013 -->
# doctopus user guide

`doctopus` reads the markdown in a repository, works out how the documents link
to each other, and tells you what's **broken** (dead links, bad anchors),
what's **lost** (orphans and unreachable pages), and how the docs are
**shaped** (hubs, clusters, knowledge gaps). It produces output for people
*and* for LLMs/agents.

## Install

```console
$ go install github.com/stacklok/doctopus/cmd/doctopus@latest
# or, from a clone:
$ make build   # produces ./bin/doctopus
```

## The 30-second tour

```console
$ doctopus .              # colorized report of the current repo
$ doctopus check .        # CI gate: non-zero exit if links/anchors are broken
$ doctopus orphans .      # what nothing links to / what's unreachable
$ doctopus emit --out ai  # write the full human + LLM artifact bundle to ./ai
```

## Commands

| Command | What it does |
| --- | --- |
| `doctopus [path]` | Scan + analyze, print a colorized terminal report (default). `--quiet` for a one-line summary. |
| `doctopus check [path]` | Validate links/anchors as a **CI gate**. See exit codes below. |
| `doctopus report [path]` | Render a committable Markdown analysis report (`--out` to write `report.md`). |
| `doctopus graph [path]` | Emit the reference graph: `--format mermaid` (default), `dot`, or `json`. |
| `doctopus index [path]` | Emit a navigation surface: `index.md`, or an `llms.txt` family artifact (`--llms`, `--full`, `--small`, `--graph`). |
| `doctopus orphans [path]` | List orphaned (`--isolated-only`) and unreachable (`--unreachable-only`) docs. |
| `doctopus emit [path] --out <dir>` | Write the whole bundle: `index.md`, `llms.txt`, `llms-full.txt`, `llms-small.txt`, `graph.json`, `findings.json`. |
| `doctopus serve [path]` | Run the read-only MCP server (stdio) so an agent can query the graph. |
| `doctopus version` | Print version information. |

## Global flags

| Flag | Meaning |
| --- | --- |
| `--out <dir>` | Write artifacts into `<dir>` (paths are sanitized to stay inside it). |
| `--strict` | Promote orphan/ambiguous **warnings** to **failures** (affects `check`'s exit code). |
| `--check-external` | Opt in to HTTP liveness checks of external links (off by default; see below). |
| `--root <glob>` | Add reachability root(s) on top of the autodetected ones (`README.md`/`index.md`/`type: index`). Repeatable and/or comma-separated; matched against document paths with `path.Match` (a single `*` does **not** cross `/`, and `**` is unsupported — e.g. `docs/*.md`, not `docs/**`). |
| `--no-color` | Disable ANSI color. `NO_COLOR` env and non-TTY output are also honored. |
| `--quiet` / `--verbose` | Less / more output. |
| `--resolution <policy>` | (`check`) `exact` \| `longest-suffix` (default) \| `basename`. |

## What it detects

- **Broken links** — a link/wikilink whose target isn't a document in the repo.
- **Broken anchors** — `other.md#heading` where that heading doesn't exist. Slugs
  are GitHub-style (lowercase, spaces→`-`); see [ADR 0006](adr/0006-slug-dialect.md).
- **Ambiguous links** — e.g. `[[notes]]` when two `notes.md` exist. doctopus
  refuses to guess and shows you the candidates.
- **Orphans** — documents with **no** inbound or outbound links (truly isolated).
- **Unreachable** — documents you can't reach by following links from a root
  (`README.md`, `index.md`, or a `type: index` front-matter doc). They differ from
  orphans: an unreachable doc may link outward but nothing leads *to* it.
- **Knowledge gaps** — clusters of docs that are disconnected from each other.

Orphans and unreachable docs come with **different** remediation hints, because
the fix differs: link an orphan in (or delete it); give an unreachable doc an
inbound link from somewhere reachable.

### Keeping a doc intentionally unlinked

Add to its front matter:

```yaml
---
doctopus: orphan-intentional
---
```

It will still appear in the graph but won't be reported as an orphan/unreachable.

## Using it in CI

`doctopus check` is the gate. Exit codes ([ADR 0005](adr/0005-exit-code-contract.md)):

| Code | Meaning |
| --- | --- |
| `0` | Clean (or `--strict` not tripped). An empty repo is also `0`. |
| `1` | Broken links/anchors found (with `--strict`, also orphans/ambiguous). |
| `2` | Usage error (bad flags/args). |
| `3` | Runtime error (unreadable path, I/O failure). |

```yaml
# .github/workflows/docs.yml
- run: doctopus check . --out doctopus-out
- uses: actions/upload-artifact@v4
  if: always()
  with: { name: doctopus, path: doctopus-out }   # findings.json + junit.xml
```

`check --out <dir>` always writes `findings.json` and `junit.xml` — on pass *and*
fail — so dashboards get structured results either way.

## The LLM artifacts

`doctopus emit --out <dir>` produces a bundle designed for agents:

- **`graph.json`** — the machine-queryable corpus manifest: nodes (with
  importance scores), edges, orphans, broken links, components, gaps. Validated
  against [`docs/schemas/graph.schema.json`](schemas/graph.schema.json) and
  byte-stable run to run.
- **`llms.txt`** — a curated index, most-important docs first, with a
  "Known gaps" section flagging what's incomplete (per the `llms.txt` convention).
- **`llms-full.txt` / `llms-small.txt`** — concatenated clean bodies (each with a
  short context header) / a tight hubs-only subset for small context windows.
- **`findings.json`** — every finding is self-contained and actionable, plus a
  `remediationGuide` so an agent can fix issues without extra context. Validated
  against [`docs/schemas/findings.schema.json`](schemas/findings.schema.json)
  (schema version 2) and byte-stable run to run.

### Live queries for agents (MCP)

```console
$ doctopus serve .
```

Speaks MCP over stdio and exposes read-only tools: `what-links-to`,
`list-orphans`, `path-between`, `get-section`, and `corpus-summary`.

## Ignoring files

Create a `.doctopusignore` (gitignore syntax). `.git`, `node_modules`, and
`vendor` are ignored by default.

## Checking external links (opt-in)

`--check-external` enables HTTP liveness checks of external URLs. It's **off by
default** to keep runs fast and deterministic. When on, doctopus applies an SSRF
guard (refuses loopback, link-local, cloud-metadata, and private addresses, and
re-checks redirects against the resolved IP) — see
[ADR 0003](adr/0003-security-model.md).

## Troubleshooting

**"reachability indeterminate (no root found)"** — doctopus prints this notice
when it can't find any reachability root: no `README.md`/`index.md` at any depth
and no `type: index` front-matter doc, and you didn't pass `--root`. Rather than
flag *every* document as unreachable (which would be noise), doctopus skips
reachability analysis entirely and says so. Orphan (isolated) detection still
runs. Fix it by adding a `README.md`/`index.md`, marking an entry doc with
`type: index` front matter, or passing an explicit `--root <glob>`. This notice
alone never fails `check` (per [ADR 0005](adr/0005-exit-code-contract.md)).

**Orphan vs. unreachable** — they are distinct findings with distinct fixes. An
**orphan** has *no* navigational links at all (in-degree 0 **and** out-degree 0):
link it in from a relevant page, or delete it. An **unreachable** doc *does* link
out (or is linked from another unreachable cluster) but no path leads to it from
a root: give it an inbound link from a page that is itself reachable. A doc that
is both is reported only as the more specific orphan. To silence either
intentionally, add `doctopus: orphan-intentional` front matter.

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
