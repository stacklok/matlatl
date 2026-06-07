# 17. Nested git repositories are out of scan scope

Date: 2026-06-07
Status: Accepted

## Context

Running `matlatl` on a repository that vendors other repositories — git
submodules, linked worktrees (`git worktree add`), or plain nested clones —
ingests the nested repo's documentation as if it were the outer repo's own. A
submodule's `README.md`, its `docs/` tree, and its broken links all light up the
outer corpus: phantom nodes, false orphans, and findings the outer repo's
maintainers neither own nor can fix. This mirrors the `.claude/worktrees`
corpus-corruption symptom (ADR 0010, Section B), but the offending tree is not at
a known path — a submodule can live anywhere the outer repo's `.gitmodules`
places it.

The defining structural signal of a nested working tree is git's own marker:
the working tree contains a `.git` entry. git materializes it as a **file** (a
gitfile, `gitdir: …`) for submodules and linked worktrees, and as a **directory**
for a plain nested clone. The outer repo's `.git` is already pruned by name
(`defaultIgnoredDirs`), but that name match only ever sees a directory literally
named `.git`; it never sees a submodule's `.git` FILE, and it never prunes the
submodule's *working tree* — the docs sit beside the `.git` file, not inside it.

## Decision

During the filesystem walk, a directory **below the scan root** that contains a
`.git` entry is treated as a nested git repository and **pruned wholesale**
(`fs.SkipDir`), emitting exactly one `skipped-nested-repo` notice for that
directory. Detection is a single `os.Lstat(filepath.Join(dir, ".git"))` —
presence of either a file or a directory suffices.

- **Root is exempt.** The scan root's own `.git` is short-circuited before the
  check (the existing `if path == realRoot { return nil }`). So scanning a normal
  repo never skips itself, and pointing matlatl **directly at a submodule** scans
  it normally (it becomes the root). That direct-scan path is the intentional
  escape hatch — there is no opt-in flag (deferred; see Future work).
- **Explicit ignore wins, silently.** The check runs **after** `shouldSkipDir`,
  so an explicit `.matlatlignore` match prunes the directory with no
  `skipped-nested-repo` notice — the user already said "ignore this", and we do
  not second-guess it with a notice.
- The check lives in `internal/infrastructure/fsscanner`, alongside the existing
  default-ignore mechanisms.

### Why this does NOT violate ADR 0010's boundary rule

ADR 0010 draws the line: *"Filename conventions may be auto-detected in the
domain; directory/path conventions must be repo-config-declared, never baked into
the tool."* That rule guards against hard-coding **path/name conventions** (e.g.
`.claude/agents`) into the tool. This decision hard-codes **no path and no name
convention**: it keys off a **git-structural / content signal** — the presence of
git's own `.git` marker that git itself writes into every working tree — not a
hardcoded location or a project-specific naming choice. A submodule is recognized
wherever it lives. The check is infrastructure-only (the filesystem scanner); the
domain stays pure (ADR 0004) — no new domain code, no new import.

### Extends ADR 0003 (scan scope / security)

This narrows scan scope. The cost is a single non-following `Lstat` per directory,
and the effect is to **reduce** traversal (an entire nested working tree is
pruned). We use `Lstat`, not `Stat`, so a symlinked `.git` is detected by its
presence and **not followed** — the no-follow containment stance is preserved, and
traversal still only shrinks.

### Monotonic softening (ADR 0005 unaffected)

The change can only **remove** files from the corpus, never add them. Fewer files
can only mean fewer findings, so the `check` gate softens monotonically — it can
never newly fail a build that previously passed. The exit-code contract is
unaffected.

### Edge cases

- An **uninitialized submodule** is an empty placeholder directory with no `.git`
  and no docs — nothing to scan, nothing to prune; the check is a no-op for it.
- A nested repo with many markdown files produces **exactly one** notice (the
  `SkipDir` prunes the subtree before any of its contents are visited).

## Consequences

- A repo with submodules / worktrees / nested clones no longer ingests the nested
  repos' docs; the outer corpus reflects only the outer repo's authored
  documentation.
- Maintainers see one `skipped-nested-repo` notice per nested repo, so the prune
  is visible, not silent.
- The escape hatch is "scan the submodule directly" — point matlatl at it and it
  becomes the (exempt) root.
- **Future work:** if demand appears, add an opt-in override —
  `--include-submodules` or a `.matlatl.yml` `scanNestedRepos: true` — to keep
  nested working trees in scope. Deferred until there is a concrete use case.
