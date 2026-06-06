# 11. Per-repo configuration file (`.matlatl.yml`)

Date: 2026-06-06
Status: Accepted

## Context

ADR 0010 drew a deliberate line for how matlatl recognizes agent-tooling
scaffolding (its **boundary rule**, verbatim):

> Filename conventions may be auto-detected in the domain; directory/path
> conventions must be repo-config-declared, never baked into the tool.

That ADR auto-roots `SKILL.md` by **filename** but explicitly refuses to bake any
**directory/path** convention (e.g. `.claude/agents/*.md`) into the tool, naming
per-repo config as the future home for path conventions. With ADR 0010 also
extending the root→exemption so that **edgeless roots are exempt from the
isolated-orphan finding**, a repo now needs only a durable way to *declare* such
a path as a root for its edgeless agent files to stop being flagged.

There was no such mechanism. `--root` globs are per-invocation (and absent from
committed CI config unless every command line carries them); `.matlatlignore`
*removes* files from the corpus rather than *declaring their role* in it. A repo
needs a committed, durable place to say "these paths are entry points," carrying
the path knowledge that the tool deliberately does not.

This ADR records that mechanism. It **builds on ADR 0010** (the conventions and
the root→isolated-orphan exemption it relies on), **extends ADR 0003** (security
model: resource caps, scan-scope), and honors **ADR 0004** (DDD layering) and
**ADR 0005** (exit-code contract). It does not restate them; it adds to them.

## Decision

### A file named `.matlatl.yml`, read once at the scan root

matlatl reads an **optional** `.matlatl.yml` from the **scan root only** — a
sibling of `.matlatlignore`. It is **not** discovered up the directory tree: the
scan root is the one repo boundary the tool already trusts, and tree-walking
config invites the "whose config wins" ambiguity that dotfile discovery is
notorious for. Absent file = current defaults (a pure no-op).

Format is **YAML**, matching the front-matter and `.matlatlignore` ecosystem the
tool already parses; `gopkg.in/yaml.v3` is already in the module graph (front
matter), so this adds no dependency — it is promoted from indirect to a direct
require.

### v1 carries ONE thing: additional reachability roots

```yaml
version: 1
roots:
  - ".claude/agents/*.md"
```

`roots` are path globs matched against document IDs (repo-root-relative,
slash-separated) with the **same `path.Match` semantics as the `--root` flag**: a
single `*` does not cross `/`, and `**` is unsupported. They are **UNIONED** with
the auto-detected conventions (`README.md`/`index.md`/`SKILL.md`/`type: index`)
and any `--root` flags. The precedence rule is purely additive:

> roots = conventions ∪ `.matlatl.yml` roots ∪ `--root` flags

Order is irrelevant; the domain's `graphmodel.ResolveRootSet` sorts and
de-duplicates the union. **The domain does not change** — it already takes root
globs and is source-agnostic. The file is merely a new *source* feeding the
existing `application.Config.Roots` seam, which is the proof the seam was right.

#### Why roots only; why NOT ignore

`.matlatlignore` remains the **sole** ignore mechanism. The two files have a
clean division of labor: `.matlatlignore` **removes** files from the corpus;
`.matlatl.yml` **declares the role** of files that stay in it. Folding ignore
into the config would create two ways to express the same exclusion (and a
precedence question between them); that subsumption is **deferred**, not adopted.

#### Config governs repo-SHAPE, not run-BEHAVIOR

The config declares *what the repo is* (its entry-point shape). It deliberately
cannot set run behavior — `--strict`, `--out`, `--resolution`, etc. stay
**flag-only**. A committed file silently changing whether CI fails would be a
footgun; behavior is the caller's per-invocation choice. Disabling the built-in
conventions from config is likewise **deferred** (v1 is additive-only).

### An explicit, integer `version` field

The file carries an explicit integer `version`. The supported version is **1**.
This is the durable forward-compat seam: a repo can pin the schema it was written
against, and a tool can refuse a schema from the future rather than
mis-interpret it.

### The error / version / unknown-key contract

The governing rule: **a mistake in something matlatl UNDERSTANDS is LOUD; a thing
it does not understand yet is TOLERATED with a notice.** A loud failure is a real
error the CLI maps to `ExitUsage` (2, per ADR 0005); a tolerated one is a stderr
notice and the run continues.

| Condition | Behavior | Exit |
|---|---|---|
| Missing file | silent no-op, zero config | n/a |
| Empty file (or comments only) | silent no-op | n/a |
| Oversized file (> 1 MiB) | skip + notice (not read past the cap) | run continues |
| Malformed YAML (syntax) | HARD error | ExitUsage (2) |
| `version` missing (file present) | assume 1 + notice (`no version field; assuming 1; pin version: 1`) | run continues |
| `version` present and == 1 | proceed | n/a |
| `version` integer > 1 / unsupported | HARD error (`config version N is newer than this matlatl supports (max 1); upgrade matlatl`) | ExitUsage (2) |
| `version` wrong type (e.g. `"one"`) | HARD error | ExitUsage (2) |
| `roots` wrong type (e.g. a string, or a non-string element) | HARD error | ExitUsage (2) |
| Unknown NON-version key (e.g. `rootz:` typo, or a future key) | ignore + notice | run continues |
| Bad glob inside `roots` | flows through the existing `RootSet.BadGlobs` notice path | run continues |

Unknown-key tolerance does double duty: it **surfaces typos** (`rootz:` earns a
notice rather than silently doing nothing) while **tolerating future additive
keys** (an old tool reading a newer file ignores keys it does not know, rather
than failing). A bad glob is not special-cased — it rides the same
`RootSet.BadGlobs` notice path `--root` globs already use.

### Security (extends ADR 0003)

Loading mirrors `fsscanner.loadIgnore`'s guard, because the config is read
*before/outside* the per-file scan cap: `os.Stat` first (missing/not-regular →
zero config); a file larger than `maxConfigBytes` (1 MiB, mirroring
`maxIgnoreBytes`) is **skipped, not read** — a hostile repo cannot OOM the load
with a multi-GB config. The 1 MiB source cap plus yaml.v3's built-in
alias-expansion budget bound the decode against alias/billion-laughs bombs — the
same two-layer defense mdparser relies on for front matter — and an adversarial
alias-pyramid fixture pins it (ADR 0003's adversarial-fixture requirement).

The loader reads **exactly** `<scanRoot>/.matlatl.yml` — nothing outside the scan
root. The globs it carries are only *string-matched* against in-corpus document
IDs by `ResolveRootSet`; they are **never a filesystem read**, so a hostile
`roots: ["/etc/**"]` is inert.

### Layering (honors ADR 0004)

The loader lives in `internal/infrastructure/config` (infrastructure: it touches
the filesystem and parses YAML). It returns a plain `File{Version, Roots}` plus
application notices; the **CLI layer** (`cmd/matlatl`) unions its roots into
`application.Config.Roots` and maps a hard error to `ExitUsage`. The **domain
stays pure** — it never learns there is a config file, only that
`Config.Roots` has more entries.

## Consequences

- A repo can declare `.claude/agents/*.md` (or any path) as roots in a committed
  file; its edgeless agent docs stop being reported as isolated orphans, riding
  the ADR 0010 root→isolated exemption — with **zero** Claude-Code-specific
  knowledge in the tool. The repo's config carries the path convention, exactly
  as ADR 0010's boundary rule prescribed.
- The change can only **remove** findings (it adds roots; roots only exempt), so
  the `check` gate softens monotonically and ADR 0005 is unaffected — except for
  the new HARD-error rows, which are usage errors (exit 2) for a malformed or
  unsupported config, never a findings failure.
- The explicit `version` field plus the loud/tolerated split give a durable
  forward-compat story: old tools tolerate new additive keys; new tools refuse
  schemas from the future.
- The seam is proven minimal: `application.Config.Roots` and the domain's
  `ResolveRootSet` are unchanged. The config is purely a new source.
- Deferred (not adopted) in v1: ignore-in-config (subsumes `.matlatlignore`),
  convention-disabling, and run-behavior keys. Each is a future, additively
  versioned decision.

## See also

- [ADR 0003](0003-security-model.md) — security model (caps, scan scope, bounded decode).
- [ADR 0004](0004-ddd-layering-and-scope.md) — DDD layering (domain purity).
- [ADR 0005](0005-exit-code-contract.md) — the exit-code contract (ExitUsage = 2).
- [ADR 0007](0007-graph-node-semantics.md) — roots, orphans, unreachable.
- [ADR 0010](0010-agent-scaffolding-roots-and-default-ignores.md) — conventions, the root→isolated exemption, and the boundary rule this builds on.
- [docs/schemas/matlatl-config-v1.md](../schemas/matlatl-config-v1.md) — the human-readable schema reference.
