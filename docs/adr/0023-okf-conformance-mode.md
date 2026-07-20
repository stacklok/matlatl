# 23. OKF v0.1 conformance mode

Date: 2026-07-20
Status: Accepted

## Context

Google's [Open Knowledge Format (OKF)](https://github.com/GoogleCloudPlatform/knowledge-catalog)
v0.1 is a small, markdown-native spec for distributing "knowledge bundles": a
directory tree of concept documents, each a markdown file with a YAML
frontmatter block. The spec is pinned, mapped, and quoted verbatim in
[the OKF research note](../research/okf-spec-pinned.md) (issue #18) — read it for
the normative text; this ADR records the implementation decision.

OKF ships a reference *consumption* agent but **no validator / linter /
conformance-checker**. matlatl already scans a repo's markdown, parses
frontmatter, and resolves OKF's recommended root-absolute links (ADR 0022) — it
is a near-drop-in conformance checker for that empty slot. This ADR adds an
opt-in **OKF conformance mode**.

The subtle risk is conflating **conformance** with **health**. OKF §9 is a tiny,
permissive normative surface, and it explicitly says consumers MUST NOT reject a
bundle for a broken cross-link, a missing `index.md`, or an unknown `type`.
matlatl's whole default posture — "broken links fail CI" — is *stricter* than OKF
conformance. The mode must keep the two distinct.

## Decision

### The verdict is exactly the three §9 rules

A bundle is **conformant** iff all three OKF v0.1 §9 rules hold. matlatl checks
exactly these and nothing else:

- **R1 — parseable frontmatter.** Every non-reserved `.md` concept document has a
  PARSEABLE YAML frontmatter block. Absent vs. present-but-unparseable is
  distinguished and surfaced (`details.frontmatterState` = `absent` |
  `unparseable`). **Known leniency:** matlatl's parser also accepts a TOML (`+++`)
  frontmatter block as "present and parsed", so a TOML block satisfies R1 even
  though OKF §4 specifies YAML. This is accepted leniency (matlatl treats `+++` as
  frontmatter everywhere); revisit if a real bundle depends on rejecting TOML.
- **R2 — non-empty `type`.** Every such frontmatter carries a non-empty string
  `type`. matlatl **NEVER** validates the type *value* against any list — OKF §4.1
  forbids a central registry and requires consumers to tolerate unknown types.
- **R3 — reserved-file structure.** A `log.md`'s `##` headings are ISO 8601
  `YYYY-MM-DD` dates (format only, not calendar validity — the spec's single
  MUST is the digit shape); a non-root `index.md` carries no frontmatter; and the
  bundle-root `index.md` may carry ONLY an `okf_version` key (a strict reading of
  §11, the accepted decision). The declared `okf_version` is detected and
  surfaced regardless.

**Reserved filenames are EXACTLY `index.md` and `log.md`** (case-insensitive).
`README.md` is a **concept document** — R1/R2 apply to it. This deliberately does
NOT reuse `identity.IsDirectoryIndex` (which also matches README.md for
reachability): the OKF reserved set is its own predicate in
`internal/domain/okf`.

### Verdict vs. health: a superset gate, never a relaxation

The verdict (§9) is reported **separately** from the health report (broken links,
orphans, navigability). Three consequences, all load-bearing:

- `--okf` **never** turns a broken link into a non-conformance. A CONFORMANT
  bundle with a broken link reports `OKF v0.1: CONFORMANT` and still exits 1 on
  the health finding.
- `--okf` **never** relaxes the health gate. It can only ever ADD failure
  conditions (a superset gate).
- The conformance findings gate the exit code **independent of `--strict`** (see
  the ADR 0005 amendment).

### Mode-scoped Error findings

Three new finding kinds, produced **only** when the mode is on:
`okf-missing-frontmatter` (R1), `okf-missing-type` (R2),
`okf-reserved-file-structure` (R3). All are **Error** severity. When the mode is
on, any of them gates `matlatl check` (exit 1) regardless of `--strict`.

### Activation: flag OR config

A persistent `--okf` flag on the root command, plus a `.matlatl.yml okf: true`
key (`File.OKF *bool`, absent → nil, bool → value, any other shape → hard error
per ADR 0011). The effective mode is `flag || config`. Off by default.

### Parser records two pure-data bools

The parser (the only place that decodes YAML) records `FrontMatterPresent` and
`FrontMatterParsed` on `corpus.Document`: `frontmatter.Get(pctx) == nil` ⇒ absent;
a decode error ⇒ present-but-unparsed; the oversized-frontmatter guard path
(ADR 0003) ⇒ present-but-unparsed. The domain `okf` package reads only these two
bools — it does no YAML parsing of its own, preserving domain purity (ADR 0004:
`internal/domain/okf` imports only the stdlib and the sibling corpus/identity
packages).

### Surfaces

- **Verdict line** in the `check` summary and the human report (terminal +
  markdown): `OKF v0.1: CONFORMANT` / `OKF v0.1: NOT CONFORMANT — N violation(s)
  (a missing-frontmatter, b missing-type, c reserved-file)`. Shown only in OKF
  mode.
- **findings.json** `schemaVersion` 7 → **8**: the three new kind values, three
  summary counts (`okfMissingFrontmatter`, `okfMissingType`,
  `okfReservedFileStructure`), and an **always-present** top-level
  `okfConformance` object `{checked, conformant, version, missingFrontmatter,
  missingType, reservedFileStructure}` — `checked:false` when the mode is off, so
  the shape is stable regardless of mode.
- **graph.json is UNCHANGED (v7)** and trails.json is unchanged: conformance is a
  findings concern, not a graph-shape concern. There are **no MCP changes** — the
  read-only graph-query tools are orthogonal to the verdict.

## Consequences

- matlatl fills OKF's empty validator slot behind one opt-in flag, without
  changing any default behavior: a non-`--okf` run is byte-for-byte what it was.
- The verdict-vs-health separation keeps the "conformance mode" claim honest —
  matlatl never rejects a bundle OKF says is fine, and its stricter health gate
  remains available as differentiated value on top.
- Because the new kinds are mode-scoped, existing corpora and CI are unaffected
  until a user opts in.
- **Dogfooding note:** matlatl's own repo is not an OKF bundle (its docs have no
  `type` frontmatter), so `task dogfood` does **not** run `--okf`; the dogfood
  gate stays a health gate. Running `matlatl check . --okf` on this repo would
  report NOT CONFORMANT, and that is expected and correct.
- One new pure-domain package (`internal/domain/okf`), two parser bools, one
  application finding mapper (`okffindings.go`), and the usual
  emit/config/CLI/report plumbing keep the addition isolated and mirror the
  existing analysis shapes.
