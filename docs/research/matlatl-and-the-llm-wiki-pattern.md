---
title: "matlatl as the structural layer of an LLM-maintained wiki"
matlatl: orphan-intentional
---

# matlatl and the "LLM Wiki" pattern

> A note on where matlatl fits when an LLM agent writes and maintains a wiki of
> markdown files, and the LLM is responsible for the bookkeeping — the
> cross-references, the filing, the summaries, the staleness. The pattern is
> described in an external idea document (Karpathy, *LLM Wiki* gist, 2026); this
> page does not reproduce it, only relates it to matlatl's surfaces.
>
> Marked `orphan-intentional` so matlatl doesn't flag its own research note.

## 0. The pattern, in one sentence

Instead of retrieving from raw documents at query time, an LLM **incrementally
builds and maintains a persistent, interlinked wiki of markdown files** that sits
between you and the raw sources. You curate and ask; the LLM summarizes,
cross-references, files, and keeps it current. The wiki is a compounding
artifact — the synthesis is built once and kept, not re-derived on every
question.

The pattern names three operations the wiki owner performs:

- **Ingest** — drop a source; the LLM writes a summary page, updates entity and
  concept pages across the wiki, updates the index, appends to a log.
- **Query** — ask against the wiki; the LLM reads the index, drills into pages,
  synthesizes an answer with citations. Good answers get filed back as new pages.
- **Lint** — health-check the wiki: orphans, missing cross-references, dead-ends,
  concepts mentioned but lacking a page, weak "click here" links, stale claims.

## 1. What matlatl is, and is not, in that picture

matlatl is a **read-only** analyzer. It never writes the wiki; the acting agent
does. What it does is turn the wiki's markdown into a graph, measure that graph,
and emit the surfaces the pattern's operations lean on. Concretely:

| Pattern layer | What it is | matlatl's role |
| ------------- | ---------- | -------------- |
| Raw sources   | Immutable inputs the LLM reads | Out of scope — keep `raw/` in `.matlatlignore` so it doesn't pollute the wiki's link structure |
| The wiki      | The LLM-generated markdown dir | **This is what matlatl scans.** Identity is the repo-relative path (see [ADR 0001](../adr/0001-document-identity.md)); goldmark + front matter + the wikilink parser turn each file into a graph vertex |
| The schema    | `CLAUDE.md` / `AGENTS.md` config | matlatl's own `AGENTS.md` is an instance; the per-repo config file ([ADR 0011](../adr/0011-per-repo-config-file.md)) describes the wiki's shape |

## 2. The operations, mapped

### Ingest — coexistence with agent-maintained markdown

matlatl is built to scan repos that an agent is actively mutating without
flooding the report with the agent's own scaffolding:

- `SKILL.md` is an auto-detected reachability root by filename, a peer to
  `README.md` and `index.md` ([ADR 0010](../adr/0010-agent-scaffolding-roots-and-default-ignores.md)).
  An entry point having no inbound links is its purpose, not a defect.
- `.claude/worktrees` (full repo copies) and `.claude/plans` (transient scratch)
  are default-ignored; `.claude/agent-memory` is deliberately left to per-repo
  config, not hard-coded.
- Root-set members are exempt from both the unreachable and the isolated-orphan
  findings, so the wiki's catalog and entry pages don't light up as false
  positives.

matlatl does not do the ingest. It does not read your sources, extract entities,
or write summary pages. That is the agent's job.

### Query — the index, the reading order, and live graph tools

The pattern says the agent reads `index.md` first, then drills in. matlatl emits
exactly that catalog, and the navigation data behind it:

- `index.md` with a **Backlinks column** — the content-oriented catalog the
  pattern describes, plus what points *at* each page.
- The `llms.txt` family with a reading-order block and backlinks — the
  agent-onboarding surface.
- `trails.json` — reading-order trails ranked by PageRank over the SCC
  condensation ([ADR 0016](../adr/0016-agent-experience.md)).
- `graph.json` — the full machine manifest: navigability scalars
  (compactness/stratum/path-length/clustering/diameter), per-node PageRank and
  betweenness, articulation points and bridges.
- `matlatl serve` — read-only MCP tools for live drilling: `what-links-to`,
  `list-orphans`, `path-between`, `get-section`, `corpus-summary`,
  `suggest-links`, `critical-docs`. These are the "drill in" step as a native
  tool the agent calls, instead of re-parsing files. See the
  [architecture overview](../architecture.md#4c-mcp-server-matlatl-serve).

### Lint — the direct hit

The pattern's lint checklist vs. matlatl's finding kinds
(`internal/domain/analysis/analysis.go`):

| Lint item (pattern) | matlatl finding |
| ------------------- | --------------- |
| Orphan pages with no inbound links | `orphan` (nothing links to it) + `unreachable` (not reachable from the root set) |
| Missing cross-references | `suggested-link` — topology-based prediction of two unlinked but structurally-close docs ([ADR 0013](../adr/0013-topology-link-prediction.md)); `under-linked` — below the discoverability threshold |
| Concepts mentioned but lacking a page | `knowledge-gap` — a partial, deliberately naive coverage heuristic |
| Terminal / dead-end nodes | `dead-end` — inbound links but no outbound navigational links |
| Weak "click here" links | `low-scent-anchor` — anchor text shares too few tokens with the destination title ([ADR 0016](../adr/0016-agent-experience.md)); the pattern's "generic click here" complaint, detected structurally |
| Broken links / anchors | `broken-link`, `broken-anchor`, `ambiguous` |
| Structural resilience | `articulation-point` / `bridge` — which docs, if removed, fragment the wiki ([ADR 0015](../adr/0015-critical-path-analysis.md)) |

The finding the pattern asks for that matlatl **cannot** produce: *"contradictions
between pages, stale claims that newer sources have superseded."* That is
semantic content comparison. matlatl is strictly graph topology — it sees links,
not truth. The domain-purity rule
([ADR 0004](../adr/0004-ddd-layering-and-scope.md)) keeps it a structural tool;
adding an embedding model would fight the determinism guarantee that makes the
artifacts byte-stable. That gap is the agent's job (or a separate tool's).

### Indexing and logging

- `index.md` (content catalog) maps directly; matlatl emits it.
- `log.md` (chronological) is not matlatl's concern. But the navigability
  metrics give you the wiki's **shape as data** — the Obsidian graph view,
queryable and committed rather than visual-only. Track compactness over time
as a CI trend: a dropping number means the wiki is fragmenting as pages are
added without being linked in.

## 3. The closed loop: `fix-prompt`

The pattern's thesis is *"the LLM does the maintenance."* matlatl closes that
loop with `matlatl fix-prompt .` ([ADR 0009](../adr/0009-fix-prompt-acting-agents.md)):

- Runs the full pipeline, embeds findings **inline** with per-kind how-to text,
  writes an **agent-agnostic** prompt to stdout.
- Guardrails are baked into prose: fix only listed findings, do not invent
  files/headings/facts, skip intentional orphans, verify with `matlatl check`.
- `matlatl fix-prompt . | <agent>` is the ingest/maintain cycle the pattern
  describes — matlatl finds, the agent fixes, `matlatl check` gates.

The scope is curated by default ([ADR 0020](../adr/0020-fix-prompt-scope.md)):
all errors and warnings, plus advisory findings that survive the
`emitExclude` rule, with the two corpus-scaling kinds (`suggested-link`,
`low-scent-anchor`) capped so they can't drown the prompt. `--kinds` lifts the
caps for a focused pass; `--all` is the unfiltered escape hatch.

## 4. Why it fits rather than fights

1. **Determinism + git.** The pattern says *"the wiki is just a git repo."*
   matlatl's artifacts are byte-stable and golden-tested, so a committed
   `llms.txt` / `index.md` won't churn on re-run. The repo's own `task dogfood`
   regenerates `llms.txt` and runs `check --strict` as a CI gate — the proof.
2. **Root set quiets entry-point noise.** An LLM wiki's `index.md` / `README.md`
   are entry points by design — nothing links *to* them. The root-set exemption
   means the catalog page is not a false-positive orphan.
3. **`emitExclude`.** Advisory findings on docs you don't want surfaced get
   dropped from `fix-prompt` and findings, while gate-capable ones always render
   ([ADR 0019](../adr/0019-emit-exclude.md)) — so the agent isn't told to "fix"
   intentional orphans.
4. **Scale.** ~5,000 docs analyze end-to-end in ~0.24 s at ~32 MiB peak heap
   (see the [performance section](../architecture.md#4a-performance-p6-benchmark)).
   A personal wiki of hundreds of pages is trivial.

## 5. Where matlatl stops

- **Semantic contradiction / staleness detection** — out of scope (topology,
  not NLP). The pattern's "noting where new data contradicts old claims" is the
  agent's job.
- **Embedding / vector search at scale** — the pattern suggests a separate tool
  for this once the index file isn't enough. matlatl is graph navigation, not
  BM25/vector retrieval. They compose: matlatl for structure and health, the
  search tool for content.
- **Writing the wiki** — matlatl never mutates the repo. It is a read-only
  analyzer plus a prompt generator. The acting agent does the writing.

---

**The through-line:** point matlatl at the wiki directory (ignore `raw/`), and
it becomes the structural health layer of an LLM Wiki — `check` for lint,
`emit` for the `index.md` / `llms.txt` catalog plus `graph.json` / `trails.json`
navigation data, `serve` for live agent queries, and `fix-prompt` to drive the
maintenance loop. What it does not give you is semantic contradiction detection
or content search — those need the LLM itself, or a complementary tool.

## See also

- [information-organization-theory.md](information-organization-theory.md) —
  the ~80-year lineage behind matlatl's connectivity core and the determinism
  boundary that keeps the semantic frontier flag-gated.
- [semantic-similarity-and-determinism.md](semantic-similarity-and-determinism.md)
  — the companion note on why embedding-based similarity (the one thing an LLM
  Wiki wants that matlatl cannot do deterministically) lives behind a flag.
