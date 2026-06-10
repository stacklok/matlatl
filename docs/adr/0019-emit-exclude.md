# 19. `emitExclude`: in the corpus, off the navigation surfaces

Date: 2026-06-10
Status: Accepted

## Context

matlatl conflated two notions: a document being **in the corpus** (its links are
checked; it contributes PageRank, HITS, betweenness, backlinks) and being
**rendered** in the consumption artifacts (`llms.txt`, `index.md`). Real-world
repos carry large amounts of agent scaffolding — `.claude/agents/**`,
`.claude/skills/**`, `.agents/**` — that must STAY in the corpus (running
matlatl on a real repo found genuine link rot inside `.claude/rules/`, and
design docs earn PageRank from agent-file inlinks) but is pure noise as
navigation entries: skills and agent definitions are auto-discovered by the
agent harness itself; no LLM navigates to them via `llms.txt`. On one measured
repo, 79 of 209 `llms.txt` entries (38%) were scaffolding, and the
`linked from:` backlink clauses were dominated by `.claude/agents/...` paths.

`.matlatlignore` is not the answer: it removes docs from the corpus entirely,
forfeiting link-checking and ranking signal. [ADR 0011](0011-per-repo-config-file.md)'s
division of labor — the ignore file *removes* files, `.matlatl.yml` *declares
the role* of files that stay — points at the mechanism: a config key that
declares "in the corpus, but not a navigation entry".

## Decision

### A new `.matlatl.yml` key: `emitExclude`

```yaml
version: 1
emitExclude:
  - ".claude/agents/"
  - ".claude/skills/"
  - ".agents/"
```

A list of strings (any other shape is a HARD error, exit 2, per the ADR 0011
contract). Empty/absent = no filtering. It is a known key (no unknown-key
notice) and stays within config schema version 1 (additive).

### The split: navigation surfaces filter, diagnostic/machine surfaces don't

**Unchanged (complete corpus):** the scan, the graph, ALL analysis
(PageRank/HITS/betweenness/orphans/reachability), `graph.json`,
`findings.json`, `junit.xml`, the terminal/Markdown reports, the diagram
emitters, the MCP tools, and `check` behavior and exit codes. `emitExclude` has
ZERO effect on `matlatl check` — its output is byte-identical with and without
the key. Excluded docs keep their nodes/edges in `graph.json`: it is the machine
surface, and filtering belongs to renderings, so the graph/findings schema
versions are NOT bumped (their shapes are unchanged).

**Filtered (consumption surfaces):** `llms.txt`, `llms-full.txt`,
`llms-small.txt`, `index.md` — via both `matlatl emit` and `matlatl index`.
Excluded docs are dropped from entry lists AND from rendered backlink clauses
(`linked from:` in `llms.txt`, the Backlinks column in `index.md`): a surface
must not point at a doc it refuses to list. The header document count reflects
what is rendered, and the summary/header line states how many docs were
excluded, so the artifact is honest about the filter. The concatenated-body
artifacts drop the excluded doc's section; other docs' raw bodies are not
rewritten.

**`trails.json` filters too.** Trails exist for onboarding readers, so they are
a consumption surface despite being a machine artifact. Excluded docs are
dropped from each trail's order; a trail left empty is dropped; a trail whose
root is excluded is re-rooted at its most important remaining member (highest
PageRank, tie-broken by smallest ID — the domain's root definition). Dropping
entries is not a shape change, so the trails schema version stays 1.

### Matching semantics: gitignore, the `.matlatlignore` engine

`roots`-style `path.Match` globs are too weak here — users need subtree
exclusion (`.claude/agents/` at any depth). `emitExclude` patterns use
**gitignore syntax via the same engine `.matlatlignore` already uses**
(go-gitignore): a trailing-slash directory pattern excludes the subtree
wherever it appears, `*` and `!` negation behave as in `.gitignore`. Reusing
the engine means the two files cannot silently diverge in dialect. The patterns
are only ever string-matched against in-corpus DocumentIDs — never a
filesystem read — so a hostile pattern is inert (ADR 0003 posture, same as
`roots`).

### A pattern may match a root — loudly

Excluding a root/README is allowed: the root entry simply does not render;
reachability is computed over the unfiltered corpus and is unaffected. Because
silently dropping the corpus' front door is almost certainly a mistake, the
consumption-surface commands emit a `notice [config]` naming each excluded
root.

### Layering (honors ADR 0004)

The domain stays pure and ignorant of the feature. The key is parsed and
validated in `internal/infrastructure/config` (ADR 0011 contract: wrong type =
hard error, exit 2; 1 MiB cap), carried as an inert `application.Config` field
the pipeline never reads, and the filtering decision is applied at the emit
boundary: `emit.View.WithEmitExclude` compiles the matcher, and only the
consumption emitters (`llmstxt`, `index`, `trails`) consult it through
`EmitExcluded` / `RenderedBacklinks` / `RenderedTrails`. Determinism holds: the
corpus is already sorted and filtering preserves order (re-rooted trails are
re-sorted by root to keep the sorted-by-root invariant).

## Consequences

- A repo can keep its agent scaffolding link-checked and rank-contributing
  while its `llms.txt`/`index.md` list only docs an agent would actually
  navigate to; on the measured repo the entry list shrinks ~38% with an
  identical `graph.json` node set.
- `check`, `graph.json`, `findings.json`, and `junit.xml` are bit-for-bit
  unaffected — the gate cannot be weakened or strengthened by `emitExclude`
  (ADR 0005 untouched).
- Two config keys now carry two different matching dialects: `roots`
  (`path.Match`, per ADR 0011) and `emitExclude` (gitignore). This is
  deliberate — each reuses the dialect of the mechanism it extends — and each
  is documented at its key; unifying them would be a breaking schema change.
- ADR 0011's "v1 carries roots only" is superseded in part (as it already was
  by ADRs 0012/0013): the key set grows, the contract table's governing rule is
  unchanged.

## See also

- [ADR 0003](0003-security-model.md) — patterns are string-matched, never read.
- [ADR 0004](0004-ddd-layering-and-scope.md) — the emit-boundary placement.
- [ADR 0005](0005-exit-code-contract.md) — `check` is unaffected.
- [ADR 0011](0011-per-repo-config-file.md) — the config file and its error contract.
- [ADR 0016](0016-agent-experience.md) — backlinks and trails, the surfaces filtered here.
- [ADR 0020](0020-fix-prompt-scope.md) — `fix-prompt`'s curated default reuses these
  patterns to drop advisory findings on excluded docs (severity-keyed; `--all` bypasses).
- [docs/schemas/matlatl-config-v1.md](../schemas/matlatl-config-v1.md) — the schema reference.
