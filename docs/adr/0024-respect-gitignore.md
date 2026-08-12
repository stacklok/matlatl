# 24. Opt-in exclusion of git-ignored files (`--respect-gitignore`)

Date: 2026-08-11
Status: Accepted

## Context

matlatl scans the **working tree**, so local-only, gitignored markdown files —
`HANDOFF.md`, `CLAUDE.local.md`, agent scratch/plan notes — are counted as
corpus documents and flagged as orphans/unreachable (issue #30). This bites
twice:

1. **`matlatl check --strict` fails locally** on orphans a clean CI checkout
   never sees (the files aren't committed), so local and CI disagree.
2. **Regenerating `llms.txt`** with those files present bakes a nonzero orphan
   count and inflated stats into the committed artifact; CI's freshness guard
   then regenerates on a clean checkout and fails on the mismatch.

The workarounds are all awkward: moving files out of the tree by hand, or
committing per-developer scratch paths to `.matlatlignore` (wrong — they differ
per machine), or a wrapper that unions `git status --ignored` output into
`.matlatlignore` (every consumer would have to reinvent it).

The corpus a repo *commits* and the corpus a developer *has on disk* differ
exactly by the git-ignored working files. git already maintains that set —
tracked and nested `.gitignore` rules, `.git/info/exclude`, and the global
`core.excludesFile` — with subtle precedence (nested negations, dir-vs-file
rules) that is easy to re-implement wrong.

## Decision

Add an **opt-in** `--respect-gitignore` persistent flag and a `.matlatl.yml`
`respectGitignore: true` key (`File.RespectGitignore *bool`, absent → nil,
bool → value, any other shape → hard error per ADR 0011). The effective mode is
`flag || config` — the same additive shape as `okf` (ADR 0023): enabling it can
only shrink the corpus, never grow it. **Off by default** (see "Why opt-in").

### The ignore set is derived from git itself

When the mode is on, the scanner runs one bounded subprocess:

```
git --no-optional-locks -C <root> -c alias.ls-files= \
    ls-files --others --ignored --exclude-standard --directory --no-empty-directory -z --
```

and converts the output into literal ignore lines (a trailing-slash directory
entry `d/` becomes `d` + `d/`; anything else is an exact path). Shelling out —
rather than linking a gitignore library and re-reading the rule files — is what
keeps the semantics exact: nested `.gitignore` files, `.git/info/exclude`,
global excludes, and every precedence/negation corner are resolved **by git**,
so matlatl's corpus matches `git check-ignore`'s own verdicts. Two shapes pin
this (`TestScan_RespectGitignoreNestedNegation`): a parent dir-ignore (`logs/`)
cannot be re-included by a nested `!` (git prunes the directory before reading
`logs/.gitignore`), while a file-level ignore (`build/*`) can — and
`--no-empty-directory` forces git to enumerate such a directory's contents
instead of collapsing it, so the re-included file survives.

`-z` gives NUL-terminated, unquoted paths, so filenames with spaces, quotes, or
newlines cannot corrupt the parse.

### Union semantics: `.matlatlignore` is the final word

The git-derived lines are compiled into the SAME matcher as `.matlatlignore`,
**prepended before its lines**. gitignore last-match-wins then yields exactly
the intended precedence: a committed `.matlatlignore` `!` rule can re-include a
path git ignores (reproducing the file in CI, where the developer's local
gitignore does not exist), while the git set can never re-include a path
`.matlatlignore` excludes. `.matlatlignore` remains the sole *committed* ignore
mechanism (ADR 0011's division of labor is unchanged); the git set is a
per-machine layer beneath it.

### Fail-open with a notice

Every collection failure — git not in `PATH`, the scan root not being a git
work tree, a git error, or output over the size cap — degrades to
"`.matlatlignore` only" with exactly one `gitignore` notice
(`NoticeGitignore`). A non-git directory is the documented no-op case, matching
the issue's "fall back to `.matlatlignore` only". Fail-open is the deliberate
direction (the same posture as ADR 0017's nested-repo check): the feature only
ever *removes* files from the corpus, so a failure restoring the pre-feature
corpus is the safe side, and the notice keeps it visible rather than silent.

### Security (extends ADR 0003)

The scanned repo is untrusted, and a repo can smuggle config into a git
invocation, so the subprocess is hardened:

- **No shell** — `exec.CommandContext` with an argv slice; the repo cannot
  inject arguments.
- **`-c alias.ls-files=`** resets repo-local alias shadowing for the one
  command; **`GIT_CONFIG_NOSYSTEM=1`** keeps machine-wide gitconfig out.
  `core.excludesFile` is deliberately NOT reset: it is user-global config
  (outside the repo's control) and is part of the effective ignore set the
  feature promises.
- **`--no-optional-locks`** takes no index lock, so a concurrent git is
  undisturbed and a read-only `.git` cannot fail the run.
- **Bounded** — a 30 s timeout and a 64 MiB output cap (invariant 3: the output
  is proportional to the attacker-controllable untracked-file count); overflow
  fail-opens with a notice.
- The subprocess **only supplies the ignore set**; the walk itself is unchanged
  (no symlink following, root containment, caps), so the ADR 0003 traversal
  invariants are untouched. git's output is used purely as match strings —
  never as paths to read.

### Monotonic softening (ADR 0005 unaffected)

Like ADR 0017, the change can only **remove** files from the corpus, never add
them — fewer files can only mean fewer findings, so the `check` gate softens
monotonically and can never newly fail a build that previously passed. There is
no schema or artifact-shape change: the feature only changes *which* files
enter the pipeline.

### Why opt-in (not auto-detect)

The issue offers auto-detect ("when the root is a git work tree, union the
ignore sets unless told otherwise") as the least-surprise end state, and an
opt-in flag/config as the safer first step. This ADR takes the first step:
respecting gitignore changes the corpus, and doing so by default would silently
change `check` results and committed `llms.txt` stats for every repo on
upgrade. Opt-in makes the behavior change an explicit, reviewable choice (one
flag in CI, or one committed config line). Auto-detect-by-default is deferred,
not rejected — if usage shows the flag is near-universal, flipping the default
is a future, separately-recorded decision.

## Consequences

- `matlatl check . --strict --respect-gitignore` locally matches a clean CI
  checkout: gitignored working files (`HANDOFF.md`, `CLAUDE.local.md`, scratch
  dirs) are out of the corpus, so local and CI agree and regenerated `llms.txt`
  carries no phantom orphan counts.
- A repo can make it durable with `.matlatl.yml respectGitignore: true`; a
  developer can make it per-invocation with the flag.
- Non-git directories keep working unchanged (fail-open + one notice).
- New notice kind `gitignore`; new `fsscanner.Config.RespectGitignore`,
  `application.Config.RespectGitignore`, and `.matlatl.yml` key — all
  additive, all off by default, so every existing run is byte-for-byte
  unchanged.
- Deferred (not adopted): auto-detect-by-default; a
  `.matlatlignore`-can-override-git precedence beyond the chosen
  final-word rule; honoring gitignore for the *config/ignore file reads*
  themselves (out of scope — those are fixed pre-walk reads).

## See also

- [ADR 0003](0003-security-model.md) — security model (caps, untrusted repos).
- [ADR 0011](0011-per-repo-config-file.md) — `.matlatl.yml` contract (the
  loud/tolerated split the new key follows).
- [ADR 0017](0017-nested-repo-scan-boundary.md) — the other
  git-structural scan-scope prune (same fail-open, monotonic-softening posture).
- [docs/schemas/matlatl-config-v1.md](../schemas/matlatl-config-v1.md) — the
  config schema reference.
