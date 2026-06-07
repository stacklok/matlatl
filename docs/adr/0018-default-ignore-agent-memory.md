# 18. Default-ignore `.claude/agent-memory`

Date: 2026-06-07
Status: Accepted

## Context

[ADR 0010](0010-agent-scaffolding-roots-and-default-ignores.md), Section B, drew
the default-ignore line deliberately narrow: only **structural corruption**
(`.claude/worktrees`, full repo copies) and **clear scratch** (`.claude/plans`)
are default-ignored, while `.claude/agent-memory` was left as a **judgment call
deferred to per-repo config** — hard-coding it would prejudge whether it is
documentation.

Running `matlatl check .` on a repo that uses Claude Code agent memory shows that
the judgment call is not actually open: `.claude/agent-memory` structurally
**cannot** be part of the navigable corpus. On this very repo it produced 1
broken-link + 13 unreachable findings, all inside `.claude/agent-memory/`, e.g.:

```
.claude/agent-memory/software-architect/project_config_seam.md:21
  wikilink target "matlatl-layering" does not resolve to a document in the corpus
```

Three properties make `agent-memory` like `plans`/`worktrees`, not like real docs:

- It is **agent-generated, transient scaffolding**, not part of the
  human/agent-facing documentation corpus.
- It uses its own internal **`[[slug]]` memory-link convention** that is **not
  repo-relative**, so it structurally cannot pass link resolution — it is
  *guaranteed* to produce false `broken-link`/`unreachable` findings, no matter
  how well-formed the notes are.
- It is **commonly gitignored**, so it never reaches CI — meaning a local run and
  a CI run disagree, which is confusing precisely when a maintainer is trying to
  trust the gate.

The conservative, precedent-matching fix is to add `.claude/agent-memory` to the
default-ignored relative paths (issue #7, option 1) — matching the
`plans`/`worktrees` precedent rather than the broader "ignore all of `.claude/`"
alternative.

## Decision

Add `.claude/agent-memory` to `defaultIgnoredRelPaths` in
`internal/infrastructure/fsscanner`, alongside `.claude/plans` and
`.claude/worktrees`. It is matched as a scan-root-relative path (not a bare base
name), so a stray `agent-memory/` elsewhere in the tree is not silently dropped —
the same scoped mechanism ADR 0010 already uses.

This **supersedes in part** [ADR 0010](0010-agent-scaffolding-roots-and-default-ignores.md)'s
Section B default-ignore list: where 0010 enumerated `agents` and `agent-memory`
together as deliberately-NOT-default judgment calls, `agent-memory` is now a
default. The rest of 0010's Section B stands.

After this change, **`.claude/agents` is the only `.claude/*` subtree still
deferred to per-repo config** — whether an agent-definition tree is documentation
remains a genuine per-repo judgment call, so it is left to `.matlatlignore`.
`.claude/rules` (real docs that authored documentation links into) and
`.claude/skills` (real graphs, rooted by the `SKILL.md` filename convention from
ADR 0010 Section A) stay in the corpus. The "deliberately narrow" line of ADR
0010 is preserved, not abandoned: this moves exactly one subtree, on the strength
of a structural guarantee (the non-repo-relative `[[slug]]` convention), not a
blanket "ignore all of `.claude/`".

### Monotonic softening (ADR 0005 unaffected)

The change can only **remove** files from the corpus, never add them — but "fewer
files" is *not* in general "fewer findings". Topology findings (`orphan`,
`unreachable`, per ADR 0007/0012) depend on edges: removing a node also removes
its outbound edges, and a document reachable *only* through the removed node would
newly surface as `unreachable`/`orphan`. So node removal is not monotonic in the
abstract.

What makes *this* prune monotonic is the structural property that motivates it:
agent-memory notes use the non-repo-relative `[[slug]]` convention, so their links
**never resolve to a corpus document**. The pruned subtree therefore has no
resolving outbound edges into the rest of the corpus — the only edges removed are
(a) already-broken, non-resolving links (themselves the false findings we are
eliminating) and (b) edges internal to the pruned subtree. Neither can have been
the sole reachability path for any real document, so no previously-reachable doc
can be orphaned. The `check` gate softens monotonically and never newly fails a
build that previously passed; the ADR 0005 exit-code contract is unaffected.

The guarantee is therefore **"pruning a subtree with no resolving outbound edges
into the corpus is monotonic"**, not "fewer files ⇒ fewer findings". This
distinction is load-bearing for any *future* default-ignore decision — notably the
remaining `.claude/agents` judgment call, whose definition files *can* author
resolving links into the corpus. Default-ignoring such a subtree could orphan a
doc, so it would not be a free monotonic softening and must not be justified by a
naive file-count argument.

## Consequences

- A repo using Claude Code agent memory no longer reports false
  `broken-link`/`unreachable` findings from `.claude/agent-memory/`'s
  non-resolving `[[slug]]` wikilinks; local and CI runs converge.
- `.claude/agents` remains the sole `.claude/*` per-repo-config judgment call; a
  repo that wants its agent definitions in the corpus needs no config, and one
  that does not adds a single `.matlatlignore` line.
- ADR 0010's boundary rule and Section A roots mechanism are untouched — this is
  purely an infrastructure default-ignore addition (the domain stays pure, ADR
  0004); no `.claude/` path enters the roots mechanism.
- The change softens `check` monotonically; ADR 0005 is unaffected.
