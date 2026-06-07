# 10. How matlatl treats agent-tooling scaffolding

Date: 2026-06-06
Status: Accepted (superseded in part by ADR 0018)

## Context

Running `matlatl` on a repository that uses agent coding tools (Claude Code and
peers) floods the report with noise about the agent's own scaffolding. Two
distinct symptoms, one root cause — the tool has no concept of "this directory is
agent harness, not authored documentation":

1. **Orphan/unreachable spam for entry-point docs.** An agent-skills manifest
   (`SKILL.md`) and its reference cluster, or an agent definition under
   `.claude/`, are *entry points by design*: nothing inside the repo links *to*
   them, because the harness loads them by convention. ADR 0007 reports a
   document with no inbound links as **unreachable**, and a document with no
   inbound *and* no outbound links as an **isolated orphan**. So agent scaffolding
   lights up the report even though it is working as intended.
2. **Corpus pollution / duplication.** `.claude/worktrees` holds Claude Code
   agent worktrees, where **each entry is a full copy of the repository**. Left
   in, they multiply the corpus by the number of live worktrees (observed: a
   215-doc repo scanning as 1,536). `.claude/plans` holds transient scratch plans
   that are not authored documentation.

This ADR records one cohesive decision — **how matlatl treats agent-tooling
scaffolding** — via two mechanisms. It **extends ADR 0007** (graph node
semantics: roots, orphans, unreachable) and **ADR 0003** (security model:
scan scope and default-ignored directories). It does not restate those ADRs; it
adds to them.

The unifying idea is "entry points = roots": treat agent-harness entry points as
reachability roots, and make root-set membership the single concept that quiets
the orphan/unreachable noise — with no Claude-Code-specific path baked into the
tool.

## Decision

### Section A — Entry points are roots (extends ADR 0007)

#### `SKILL.md` is an auto-root by filename convention

`SKILL.md` (the agent-skills manifest) is added to the auto-detected root-set
conventions, matched by **filename** (base name), **case-insensitively** — a peer
to `README.md`, `index.md`, and front-matter `type: index`. It joins the
convention set in `graphmodel.ResolveRootSet`.

#### Root-set members are exempt from orphan AND unreachable findings

Previously, root-set membership exempted a document only from the **unreachable**
finding. It now also exempts it from the **isolated-orphan** finding. This applies
to **all roots — configured via `--root` OR detected by convention**
(`README.md` / `index.md` / `SKILL.md` / `type: index`).

Behavior delta: an **edgeless** `README.md` / `index.md` / `SKILL.md` (or any
declared root) is no longer reported as an isolated orphan. A declared entry
point having no inbound links is its *purpose*, not a defect. The change is narrow
by construction: a root that has outbound edges is already non-isolated by degree,
so this only affects edgeless roots.

#### The boundary rule (verbatim)

> Filename conventions may be auto-detected in the domain; directory/path
> conventions must be repo-config-declared, never baked into the tool.

This is the deliberate line that keeps the domain tool-agnostic: `SKILL.md`
auto-roots regardless of where it lives (filename convention), but
`.claude/agents/*.md` does **not** auto-root — a directory/path convention is
per-repo config (future work), not a hard-coded behavior. No `.claude/...` path
is baked into the roots mechanism.

#### Monotonic softening (ADR 0005 unaffected)

This change can only **remove** findings, never add them: it newly exempts some
nodes from orphan/unreachable, and exempts nothing into a finding. The `check`
gate therefore softens **monotonically** — it can never newly fail a build that
previously passed. The ADR 0005 exit-code contract is unaffected.

### Section B — Default-ignored directories (extends ADR 0003)

Two scopes of built-in skip, applied before any `.matlatlignore`:

- **By base name, anywhere:** `.git`, `node_modules`, `vendor`, plus the common
  Python virtualenv + tooling caches `.venv`, `.tox`, `__pycache__`,
  `.mypy_cache`, `.pytest_cache`, `.ruff_cache` — conventional non-source trees
  (installed packages, tool scratch state) that would only add noise. Their
  markdown is never the repo's own authored documentation.
- **By scan-root-relative path:** `.claude/worktrees` and `.claude/plans`.
  `.claude/worktrees` is **structural corruption** of the corpus — each entry is a
  full repository copy, producing phantom duplicate nodes, false orphans, and a
  meaningless graph. `.claude/plans` is **transient scratch** (agent plan files),
  not authored documentation.

The line is drawn deliberately narrow. Only **structural-corruption**
(`worktrees`) and **clear scratch** (`plans`) are defaults. Everything else under
`.claude/` stays in the corpus:

- `.claude/agents` and `.claude/agent-memory` are **deliberately NOT** defaults —
  whether they are documentation is a judgment call, deferred to per-repo config
  (`.matlatlignore`). Hard-coding them would prejudge the answer.
- `.claude/rules` are real docs that authored documentation commonly links into.
- `.claude/skills` form real graphs — and with Section A, a `SKILL.md` there is
  now a reachability root, so the cluster is reachable rather than orphaned.

The same "deliberately narrow" line governs the base-name list: only
**package/tool caches that are never authored docs** (`node_modules`, `vendor`,
and the Python `.venv`/`.tox`/`__pycache__`/`.mypy_cache`/`.pytest_cache`/
`.ruff_cache`) are defaults. Build-output directories — `dist`, `build`,
`target`, `site`, `out` — and editor dirs are **deliberately NOT** defaults:
they can legitimately hold generated markdown a repo wants scanned (e.g. a
`site/` of rendered docs), so suppressing them is a per-repo judgment call left
to `.matlatlignore`. Anything that might be real documentation stays in scope by
default.

The path scope (a scan-root-relative path, not a bare base name) keeps each skip
deliberate: a stray `plans/` or `worktrees/` elsewhere in the tree is not
silently dropped. Both lists live in `internal/infrastructure/fsscanner`.

## Consequences

- A repo using Claude Code (or a peer harness) no longer reports its `SKILL.md`
  clusters as unreachable, nor its declared entry points as orphans.
- The orphan/unreachable exemption flows through one concept — root-set membership
  — so configured roots and convention roots behave identically, and the human
  emitter / `graph.json` view stay consistent with `Orphans.Isolated`.
- The domain stays tool-agnostic: no `.claude/` path appears in the roots
  mechanism (ADR 0004 purity, Section A boundary rule). Path-based scaffolding
  recognition remains per-repo config.
- The scanner drops only the two unambiguous cases (worktrees, plans); judgment
  calls (`agents`, `agent-memory`) and real content (`rules`, `skills`) remain in
  the corpus, where the roots mechanism handles the entry-point ones.
- ADR 0007's invariants are otherwise preserved (orphan = degree-based; the root
  exemption only removes edgeless roots from the isolated list). ADR 0003's
  scan-scope and security invariants are otherwise preserved (the new skip is the
  same scan-root-relative-path mechanism already used for `.claude/worktrees`).
- The change softens `check` monotonically; ADR 0005 is unaffected.
