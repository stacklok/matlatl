# `.matlatl.yml` — per-repo configuration (schema v1)

`.matlatl.yml` is an **optional** per-repo configuration file. It lives at the
**scan root** (a sibling of `.matlatlignore`) and is read **only there** — it is
not discovered up the directory tree. When absent, matlatl runs with its current
defaults; the file changes nothing else.

This is a prose-plus-example reference rather than a machine schema (JSON Schema):
one optional structured key does not yet warrant a machine schema. If the key set
grows, a machine schema may follow — and `version` is the field that lets it.

See [ADR 0011](../adr/0011-per-repo-config-file.md) for the rationale and the full
decision record.

## Annotated example

```yaml
# .matlatl.yml — matlatl per-repo configuration (optional; absent = current defaults).
version: 1

# Additional reachability roots — path globs matched against document IDs
# (repo-root-relative, slash-separated). UNIONED with the auto-detected
# conventions (README.md / index.md / type:index / SKILL.md) and any --root
# flags.
roots:
  - ".claude/agents/*.md"

# Discoverability threshold for the under-linked finding (ADR 0012). A document
# with fewer than this many inbound navigational links (but at least one outbound
# link) is reported as under-linked. Default 3; 0 = the default, negative is a
# hard error.
inboundThreshold: 3

# Severity of the graduated structure findings (under-linked, dead-end), ADR 0012.
# "info" (default) reports them but never fails `check`, even --strict; "warning"
# promotes BOTH so they fail `check --strict` like orphans/unreachable.
structureFindingsSeverity: info

# Hop-distance threshold for the far-from-root finding (ADR 0021). A document
# reachable from a root but at or beyond this many hops from the NEAREST root is
# reported as far-from-root (reachable but hard to discover by link traversal).
# Always Info; never fails `check`, even --strict. Default 6; 0 = the default,
# negative is a hard error.
farFromRootThreshold: 6

# Docs to keep IN the corpus (link-checked, ranked) but DROP from the
# consumption surfaces (llms.txt family, index.md, trails.json), ADR 0019.
# Gitignore syntax, same engine as .matlatlignore. Zero effect on `check`,
# graph.json, findings.json, junit.xml, or the reports.
emitExclude:
  - ".claude/agents/"
  - ".claude/skills/"
  - ".agents/"

# Enable OKF v0.1 conformance mode (ADR 0023). When true, `check` also validates
# the corpus against the Open Knowledge Format conformance rules and reports a
# CONFORMANT / NOT CONFORMANT verdict; a conformance violation fails `check`
# (exit 1) regardless of --strict. Off by default. Equivalent to the --okf flag
# (effective mode = flag OR this key). Must be a boolean; any other type is a
# hard error.
okf: false

# Exclude git-ignored files from the corpus (ADR 0024). When true, the repo's
# effective git ignore set (tracked and nested .gitignore rules,
# .git/info/exclude, global excludes) is unioned with .matlatlignore, so
# local-only working files stay out of the corpus and local runs match a clean
# CI checkout. Off by default. Equivalent to the --respect-gitignore flag
# (effective mode = flag OR this key). No-op when the scan root is not a git
# work tree. Must be a boolean; any other type is a hard error.
respectGitignore: false
```

## Fields

### `version` (integer, recommended)

The config schema version. The only supported value is **`1`**.

- Present and `== 1` — proceed.
- **Missing** (in an otherwise present file) — matlatl assumes `1` and emits a
  notice nudging you to pin `version: 1`. Pinning it is recommended: it is the
  forward-compatibility anchor.
- An integer **`> 1`** — a hard error: the repo expects a newer matlatl
  (`config version N is newer than this matlatl supports (max 1); upgrade
  matlatl`).
- A **non-integer** (e.g. `"one"`) — a hard error.

### `roots` (list of strings, optional)

Additional reachability **roots** — the documents from which graph reachability
starts. Each entry is a path **glob** matched against document IDs
(repo-root-relative, slash-separated).

- **Glob semantics are identical to the `--root` flag** — Go
  [`path.Match`](https://pkg.go.dev/path#Match): a single `*` does **not** cross
  `/`, and `**` is **not** supported. So `.claude/agents/*.md` matches
  `.claude/agents/foo.md` but not `.claude/agents/sub/bar.md`.
- A malformed glob matches nothing and is surfaced as a notice (it is not a hard
  error) — the same `BadGlobs` path the `--root` flag uses.
- Must be a **list of strings**. A bare string (`roots: "docs/*.md"`) or a
  non-string element is a hard error.

#### Precedence: an additive union

Roots from every source are **unioned** — none overrides another:

```
roots used = auto-detected conventions
           ∪ .matlatl.yml `roots`
           ∪ --root flags
```

The auto-detected conventions are `README.md` / `index.md` / `SKILL.md` (by
filename, case-insensitive) and any document with front-matter `type: index`.
Order does not matter; the set is sorted and de-duplicated.

#### What declaring a root does

A root is **exempt from both the unreachable and the isolated-orphan findings**
(ADR 0010). So declaring `.claude/agents/*.md` as roots stops edgeless agent
files — entry points that nothing links to by design — from being reported as
isolated orphans. This can only **remove** findings; it never adds any, so the
`check` gate only softens.

### `inboundThreshold` (integer, optional)

The discoverability threshold for the **under-linked** structure finding
(ADR 0012). A non-exempt document with **outbound** links but **fewer than
`inboundThreshold` inbound** links is reported as under-linked.

- Default **`3`** (Wikipedia's "discoverable" heuristic).
- `0` is accepted and treated as unset (the default `3` applies); a **negative**
  value or a **non-integer** is a hard error. There is no "off" value.
- The `--inbound-threshold` CLI flag overrides this key when set explicitly.

### `structureFindingsSeverity` (string, optional)

The severity assigned to **both** graduated structure findings (under-linked and
dead-end), ADR 0012.

- `"info"` (**default**) — reported in every artifact but **never** fails
  `check`, even under `--strict`.
- `"warning"` — promotes both so they fail `check --strict` exactly like
  orphan/unreachable/ambiguous.
- Any other value is a hard error. There is no CLI flag for this; it is
  config-only.

### `farFromRootThreshold` (integer, optional)

The hop-distance threshold for the **far-from-root** finding (ADR 0021). A
non-exempt document reachable from a root but **at or beyond** this many hops
from the **nearest** root is reported as far-from-root: reachable, but so deep in
the link graph that a reader or agent traversing from an entry point is unlikely
to discover it.

- Default **`6`**.
- `0` is accepted and treated as unset (the default `6` applies); a **negative**
  value or a **non-integer** is a hard error. There is no "off" value.
- Always **Info** — it **never** fails `check`, even under `--strict`. There is
  no CLI flag for this; it is config-only.
- Root-set members and intentional orphans are exempt; unreachable documents are
  reported as `unreachable`, not far-from-root.

### `emitExclude` (list of strings, optional)

Documents to keep **in the corpus** — scanned, link-checked, ranked, present in
`graph.json`/`findings.json` and every diagnostic surface — but **drop from the
consumption surfaces**: `llms.txt`, `llms-full.txt`, `llms-small.txt`,
`index.md`, and the `trails.json` reading orders (entries *and* rendered
backlink clauses). The filtered artifacts state how many docs were excluded
([ADR 0019](../adr/0019-emit-exclude.md)). The motivating case is agent
scaffolding (`.claude/agents/**`, `.claude/skills/**`, `.agents/**`): it must
stay link-checked, but no LLM navigates to it via `llms.txt`.

- **Gitignore syntax, the same engine as `.matlatlignore`** (NOT the
  `path.Match` globs `roots` uses): a trailing-slash directory pattern excludes
  the subtree at any depth (`.claude/agents/` matches
  `.claude/agents/sub/deep.md`), and `!` negation re-includes.
- Must be a **list of strings**; a bare string or a non-string element is a
  hard error. Empty or absent = no filtering.
- **Zero effect on `matlatl check`** — output and exit codes are byte-identical
  with and without the key. There is no CLI flag; it is config-only.
- A pattern matching a reachability root is allowed (the root simply does not
  render; reachability is computed over the unfiltered corpus) but emits a
  notice.
- Patterns are only string-matched against in-corpus document IDs — never a
  filesystem read.

### `okf` (boolean, optional)

Enables **OKF v0.1 conformance mode** ([ADR 0023](../adr/0023-okf-conformance-mode.md)).
When true, `matlatl check` additionally validates the corpus against the
[Open Knowledge Format](../research/okf-spec-pinned.md) §9 conformance rules and
reports a CONFORMANT / NOT CONFORMANT verdict.

- Default **`false`**. Must be a **boolean**; any other type (`okf: "yes"`) is a
  hard error.
- Equivalent to the `--okf` flag — the effective mode is `--okf` **OR** this key,
  so a repo that *is* an OKF bundle can turn the mode on for every invocation by
  setting it here (this is why, unusually, a run-mode has a config equivalent:
  "this repo is an OKF bundle" is a property of the repo's shape).
- When on, a conformance violation fails `check` (exit `1`) **regardless of
  `--strict`**, and the health gate (broken links etc.) still applies unchanged —
  a superset gate, never a relaxation.

### `respectGitignore` (boolean, optional)

Excludes **git-ignored files** from the corpus
([ADR 0024](../adr/0024-respect-gitignore.md)). When true, matlatl derives the
repo's effective git ignore set — tracked and nested `.gitignore` rules,
`.git/info/exclude`, and the global `core.excludesFile` — by asking git itself,
and unions it with `.matlatlignore`, so local-only working files
(`HANDOFF.md`, `CLAUDE.local.md`, scratch notes) are not scanned.

- Default **`false`**. Must be a **boolean**; any other type is a hard error.
- Equivalent to the `--respect-gitignore` flag — the effective mode is the flag
  **OR** this key.
- The committed `.matlatlignore` stays the **final word**: its `!` can
  re-include a path git ignores; the git set can never re-include a path
  `.matlatlignore` excludes.
- A **no-op with one `gitignore` notice** when the scan root is not a git work
  tree or git is unavailable (fail-open to `.matlatlignore` only).
- Enabling it can only **shrink** the corpus, never grow it — so `check` only
  softens, matching a clean CI checkout.

## What v1 does NOT configure

- **Ignoring files** — `.matlatlignore` remains the sole ignore mechanism. The
  division of labor: `.matlatlignore` *removes* files from the corpus;
  `.matlatl.yml` *declares the role* of files that stay in it.
- **Run behavior** — `--strict`, `--out`, `--resolution`, `--check-external`,
  etc. stay flag-only. The config describes the repo's *shape*, not how a given
  run *behaves*. (`okf` is the one exception: "this repo is an OKF bundle" is a
  shape property, so it has both a flag and a config key.)
- **Disabling the built-in conventions** — v1 is additive-only.

These are deferred, not rejected; the `version` field is how a future schema
would introduce them.

## Error behavior at a glance

| Condition | Behavior | Exit |
|---|---|---|
| Missing / empty / comments-only file | silent no-op | — |
| Oversized file (> 1 MiB) | skipped + notice (not read past the cap) | run continues |
| Malformed YAML | hard error | 2 (usage) |
| `version` missing | assume 1 + notice | run continues |
| `version` > 1 or wrong type | hard error | 2 (usage) |
| `roots` wrong type | hard error | 2 (usage) |
| `inboundThreshold` negative or wrong type | hard error | 2 (usage) |
| `structureFindingsSeverity` not `info`/`warning` or wrong type | hard error | 2 (usage) |
| `farFromRootThreshold` negative or wrong type | hard error | 2 (usage) |
| `emitExclude` wrong type (a bare string, a non-string element) | hard error | 2 (usage) |
| `emitExclude` matches a reachability root | notice (renders nothing for it; reachability unaffected) | run continues |
| `okf` / `respectGitignore` wrong type | hard error | 2 (usage) |
| `respectGitignore: true` on a non-git root | fail-open + `gitignore` notice (`.matlatlignore` only) | run continues |
| Unknown non-version key (typo / future key) | ignored + notice | run continues |
| Bad glob in `roots` | notice (matches nothing) | run continues |

## Security caps

- The file is read **only** at `<scanRoot>/.matlatl.yml` — never outside the scan
  root.
- A file larger than **1 MiB** is skipped without being read into memory (ADR
  0003 resource-cap invariant). That cap plus YAML's alias-expansion budget bound
  the decode against "billion laughs" alias bombs.
- The globs in `roots` are only **string-matched** against document IDs already
  in the corpus — they never trigger a filesystem read, so a hostile
  `roots: ["/etc/**"]` is inert.
