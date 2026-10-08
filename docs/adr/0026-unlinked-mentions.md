# 26. Unlinked mentions are typed edges that count in the graph

Date: 2026-10-08
Status: Accepted

## Context

matlatl's graph is built from link syntax only: relative links, wikilinks,
images, transclusions and front-matter relations (ADR 0007). Prose references a
document in other ways too, and those references were invisible:

- a path in a code span or plain text: `` Per `.claude/rules/metrics.md`: dotted names ``
- a bare file name: ``see `frontdoor.md § V1 hardcoded` ``
- a name-prefixed invocation: ``Use the `triage-cve` skill (`/triage-cve CVE-YYYY-NNNNN`)``,
  or "run `/panel-review` before pushing"

A downstream repository enforces a rule over its doc graph (docs under `docs/`
must not reference agent context files) by post-processing `graph.json` edges.
Explicit links work, but about as many references are unlinked mentions, so it
maintains a bespoke regex scanner next to matlatl. The same blind spot affects
every graph metric: a document that is only ever mentioned reads as an orphan,
even though a reader (or an agent) can follow the mention.

The design constraints are the ones the rest of the tool already lives by:
identity is the repo-relative path (ADR 0001), resolution must not open new
root-escape paths (ADR 0003), the domain stays pure (ADR 0004), and matlatl
ships no tool-specific knowledge: path conventions belong in the repo's
`.matlatl.yml` (ADRs 0010 and 0011).

## Decision

### Mentions are on by default and are a new link type

An unlinked mention is a reference to another document written as text rather
than as link syntax. Mentions are extracted and resolved on every run, with no
opt-in. A resolved mention becomes a `reference.Mention` edge in the reference
graph. `Mention` joins `graphmodel.DefaultNavigationalTypes`, so mention edges
count in reachability, orphan / unreachable / under-linked / dead-end, hops from
root, components, bow-tie, HITS, PageRank, betweenness, navigability, link
prediction, backlinks and reading-order trails exactly as a link does.

Two analyses deliberately ignore mentions:

- Information scent (`low-scent-anchor`, ADR 0016) scores a link's label against
  its target. A mention has no label: its text is the target's own path or name.
- Link-health findings. A mention never produces a broken-link or ambiguous
  finding and never counts toward `summary.brokenLinks` or `summary.ambiguous`.
  Text that merely looks like a path is too common to fail a build on, so a
  mention can make `check` softer (an orphan becomes linked) but never stricter.
  Only Valid mentions become graph edges and count in metrics.

### Three kinds, classified by shape in the parser

The parser (`internal/infrastructure/mdparser/mention.go`) scans plain text,
code spans and HTML comments for tokens and classifies them:

| Kind | Shape | Examples |
|---|---|---|
| `path` | contains a `/` beyond a single leading one, or a leading `/` plus a markdown extension | `docs/guide.md`, `.claude/skills/foo/`, `../x.md`, `/docs/x.md` |
| `filename` | no `/`, ends in a markdown extension | `metrics.md` |
| `invocation` | a configured prefix followed by a name (`[A-Za-z0-9._:-]`) | `/panel-review` |

A path or file-name token is a maximal run of `[A-Za-z0-9._~+@/-]` with trailing
sentence dots removed, so `#fragment` suffixes and punctuation fall away. A token
starting with `//`, a token directly after a `:` (`scheme:path`), a token with no
letter or digit (`./`, `../`), a token touching a glob character (`*`, `?`, `[`,
`]`, `{`, `}`, as in `*-overlay.md` or `docs/*.md`), and every token in a
whitespace-delimited word containing a URL (`://`, `mailto:`, `www.`) are not
mentions.

An invocation only matches when the byte before the prefix is not a path, word or
URL character, so `https://host/panel-review` and `docs/x/panel-review` never
match. A name directly followed by `/` is a path, not an invocation, and a name
ending in a markdown extension is left to the path / file-name scan.

### What is never scanned

Fenced and indented code blocks, raw HTML other than comments (block and
inline), and everything inside link syntax: link and image labels, autolinks and
wikilinks. Link reference definitions (`[id]: target`) are consumed by goldmark
before the AST exists, so they never surface as text. This is what keeps a link
a link edge with no double counting. Front matter is not scanned. Headings are.

An HTML comment (`<!-- ... -->`, block or inline) is scanned. It never renders,
but it is prose that a reader of the source, and an agent in particular, follows:
templates and maintainer notes routinely point at other files from comments.

### Resolution reuses the link machinery

`reference.MentionResolver` (pure domain) wraps the link `Resolver`:

- `path`: resolved the way a link resolves (relative to the origin, or from the
  scan root or content root for a single leading `/`, ADRs 0022 and 0025), then,
  if that names nothing, relative to each ancestor directory of the origin,
  nearest first, ending at the repo root. Prose writes paths relative to the
  enclosing project (`.claude/rules/x.md` in a subproject's docs) or to the
  repository. Every attempt goes through `resolveInRoot` or the ADR 0003
  root-containment guard. A path naming a directory (ADR 0008) resolves to its
  `README.md` / `index.md`, else to its `SKILL.md` (the filename convention ADR
  0010 already auto-detects), else to nothing. A mention never fans out to a
  folder's children: one mention, at most one target. Fragments are ignored.
- `filename`: matched by basename against every in-corpus document.
- `invocation`: the name is looked up in the corpus alias index that wikilinks
  already use (front-matter `name:` and `aliases:`), then filtered to documents
  matching the rule's `targets` globs.

When a file name or invocation matches several documents and every one of them
lives under a dot-directory, the nearest wins. A candidate's scope is the
directory holding its first dot-directory (`app` for
`app/.claude/skills/x/SKILL.md`, the repo root for `.claude/skills/x/SKILL.md`).
Candidates whose scope does not enclose the origin are discarded, and the
deepest enclosing scope wins. When a plain document is among the candidates
(`docs/architecture.md` beside `.claude/skills/x/reference/architecture.md`),
nothing is picked: a tool file must not win over a doc just because tool files
carry a scope. This is how project-scoped tool files behave: a
subproject's `.claude/skills/x` shadows the repository's for docs inside that
subproject. matlatl knows no tool's layout; it only reads the dot-directory
boundary.

Each mention then gets a health:

- Valid: exactly one document, other than the origin.
- Ambiguous: several documents and no single nearest one. The target is the
  name as written, and every candidate is kept.
- Broken: a `path` or `filename` mention whose token has a markdown extension
  and names no document. A doc that names a deleted or renamed file is exactly
  the stale reference a consumer wants to see. The target is the repo-root
  reading of the path, or the file name as written.
- Dropped: any other mention that names nothing (a token without a markdown
  extension, such as `internal/domain/reference`, or an invocation matching no
  target), and a mention of its own origin or of a shared name one of whose
  candidates is the origin, mirroring the projection's self-loop rule. Text that merely looks like a path is too common to keep.

Duplicates (same target, kind, line, text and health) collapse.

### Invocations are repo-declared

The prefix and the eligible documents are tool knowledge, so they live in
`.matlatl.yml` and nowhere in matlatl:

```yaml
mentions:
  invocations:
    - prefix: "/"
      targets: ["**/.claude/skills/*/SKILL.md"]
    - prefix: "@"
      targets: [".claude/agents/*.md"]
```

`targets` is required. Without it, `/build` in prose would bind to any document
whose front matter says `name: build`, and since mentions count in the metrics,
that wrong edge would also move orphans and rankings. `targets` globs support
`**` (zero or more whole path segments); every other segment uses `path.Match`.
The matcher (`reference.MatchGlob`) is stdlib-only. A prefix is 1 to 4 ASCII
punctuation characters. With no rules configured, invocation matching is off;
path and file-name mentions need no configuration and have no off switch.

The config contract follows ADR 0011: a wrong shape, an invalid prefix, or an
empty / malformed `targets` is a hard error (exit 2); an unknown key inside
`mentions` or inside a rule is ignored with a notice. The config schema stays at
`version: 1` because the key is additive.

### graph.json schema v8

`edges` keeps one `"reference"` edge per explicit-link document pair (the
link-only projection, `ReferenceGraph.LinkProjectionOut`), and adds one
`"mention"` edge per resolved mention occurrence:

```json
{ "from": "docs/guide.md", "to": ".claude/rules/metrics.md", "type": "mention",
  "health": "valid", "kind": "path", "line": 3, "text": ".claude/rules/metrics.md" }
```

`kind`, `line` and `text` appear only on mention edges, and the schema requires
them when `type` is `"mention"`. A mention edge's `health` is `valid`, `broken`
or `ambiguous`. A broken mention's `to` names a document that does not exist.
An ambiguous mention is one edge whose `to` is the name as written and whose
`candidates` lists the documents it may name, so a common name such as
`SKILL.md` adds one edge, not one per file carrying it. Only valid mention edges
count in the graph. A pair connected by both a link and a mention carries both
edges. Edges sort by `(from, to, type, kind, line, text)`.
`summary.edges` counts reference edges and the new required `summary.mentions`
counts mention edges, so their sum is `len(edges)`. Because mentions now count
in every metric, node scores and corpus scalars shift wherever mentions exist.
`findings.json` and `trails.json` keep their shapes.

### Layering and security

Extraction lives in `mdparser` (it needs the goldmark AST to know what is code,
link or HTML). Classification by shape happens there; deciding what a token
names happens in the pure domain. The parser receives only the configured
prefixes; the resolver receives the full rules. The CLI wires both from the same
config.

Mentions add no filesystem access. Resolution is path arithmetic plus catalog
lookups, every path runs through the existing root-containment guard, and
`targets` globs are only string-matched against in-corpus document IDs. Token
scanning is a single forward pass per text run, linear in its length.

## Consequences

- References written as text appear in the graph and in `graph.json` as typed
  edges with a source line, so a consumer can enforce rules over them without a
  separate scanner.
- Existing users see different output with no config change. `graph.json`
  becomes v8, and wherever docs mention each other, metrics, backlinks and
  orphan / unreachable findings reflect it. The `check` gate can only soften:
  mentions add edges and never findings.
- A fixture or doc that names a file in prose to describe it as unlinked now
  links it. The test corpus's `island` fixture was reworded for this reason.
- The repo has no off switch for path and file-name mentions. A heuristic edge a
  repo disagrees with can be avoided by rewording or by `.matlatlignore`. A
  resolved mention always names a real in-corpus document, which bounds the
  noise.
- `matlatl serve` does not read `.matlatl.yml`, so its analysis carries path and
  file-name mentions but not invocations.
- Stale and ambiguous mentions are reported in `graph.json` but never in
  `findings.json`, so a consumer that wants to enforce on them reads the edges.
- Deferred: matching invocations by directory name, and semantic references
  ("the owning rule"), which need an LLM rather than a parser.

## See also

- [ADR 0001](0001-document-identity.md): identity and the alias index invocations reuse.
- [ADR 0003](0003-security-model.md): root containment, which path mentions go through.
- [ADR 0007](0007-graph-node-semantics.md): the navigational set mentions join.
- [ADR 0008](0008-directory-links.md): directory resolution.
- [ADR 0010](0010-agent-scaffolding-roots-and-default-ignores.md): the boundary rule and the `SKILL.md` filename convention.
- [ADR 0011](0011-per-repo-config-file.md): the config contract `mentions` follows.
- [ADR 0016](0016-agent-experience.md): information scent, which mentions skip.
- [ADR 0022](0022-root-absolute-links.md) / [ADR 0025](0025-content-roots-and-docusaurus-anchors.md): root-absolute and content-root resolution.
- [docs/schemas/graph.schema.json](../schemas/graph.schema.json) and [docs/schemas/matlatl-config-v1.md](../schemas/matlatl-config-v1.md).
