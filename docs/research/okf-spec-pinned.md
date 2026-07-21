---
title: "Open Knowledge Format (OKF) v0.1 — pinned spec + matlatl mapping"
matlatl: orphan-intentional
---

# OKF v0.1: pinned specification and mapping to matlatl

> Research note for [issue #18](https://github.com/stacklok/matlatl/issues/18)
> ("OKF conformance mode"). Its job is to **pin the real spec text** so the
> conformance mode is implemented against the actual normative document, not a
> blog paraphrase — and to map each conformance rule onto matlatl's existing
> machinery.
>
> Marked `orphan-intentional` so matlatl doesn't flag its own research note.
>
> **Status (2026-07-20): IMPLEMENTED.** The conformance mode this note designs
> shipped in issue #18 as [ADR 0023](../adr/0023-okf-conformance-mode.md) — the
> `--okf` flag / `.matlatl.yml okf: true` key, the three §9 rules, the
> verdict-vs-health separation, and `findings.json` schema v8. This note is
> retained as the pinned normative source; when a rule's semantics are in doubt,
> the verbatim spec text below is authoritative.

## Provenance (what was pinned, and how confident)

| Field | Value |
|-------|-------|
| Canonical spec | `okf/SPEC.md` in **GoogleCloudPlatform/knowledge-catalog** |
| Spec URL | <https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md> |
| Pinned commit | `d44368c15e38e7c92481c5992e4f9b5b421a801d` |
| `SPEC.md` blob SHA | `55d0a46cc988e99aa35cd027964d6278a4f93f35` |
| Version | **0.1 — Draft** (per the document's own header) |
| License | **Apache License 2.0** (`okf/LICENSE.md`, blob `6b0b1270ff0ca8f03867efcd09ba6ddb6392b1e1`) |
| Retrieved | 2026-07-19 (via GitHub API) |
| Announcement blog | <https://cloud.google.com/blog/products/data-analytics/how-the-open-knowledge-format-can-improve-data-sharing/> |

**Confidence this is the real spec: high.** It is a normative document titled
"Open Knowledge Format (OKF) — Version 0.1 — Draft" living in Google's own
`GoogleCloudPlatform` GitHub org, in the `knowledge-catalog` repo the blog post
points to, under Apache-2.0, alongside a reference agent (`okf/src/reference_agent`),
sample bundles (`okf/samples`, `okf/bundles`), and tests (`okf/tests`). It is a
primary source, not secondary reporting. The blog's specific claims ("fits on a
single page", "conformance criteria, cross-linking rules, and reserved
filenames", "only `type` required") all check out against the text below.

**One caveat worth flagging for issue #18:** the repo ships a reference
*consumption* agent but **no validator / linter / conformance-checker** in
`src/`. Issue #18's premise — "that slot is empty and matlatl is a near-drop-in"
— holds. (Third-party validators exist, e.g. `openknowledge-sh/openknowledge`,
`Sudhakaran88/okf-conformance`, `gsemet/okf-schema`, but none is Google-blessed.)

The status is **"0.1 — Draft"**, so treat field lists and reserved filenames as
stable-but-not-frozen; §11 (Versioning) tells us how it will evolve (below).

---

## Verbatim spec text

Everything in this section is quoted verbatim from the pinned `SPEC.md` (Apache-2.0,
© Google LLC). Reproduced under that license for the purpose of implementation.
The **§ numbering is the spec's own.** Blockquotes = spec text; anything outside a
blockquote is my annotation.

### §1 Motivation, Goals, Non-goals

> OKF takes the position that knowledge is best represented in commonly
> accessible, established formats that are:
> - **Readable** by humans without tooling.
> - **Parseable** by agents without bespoke SDKs.
> - **Diffable** in version control.
> - **Portable** across tools, organizations, and time.

Non-goals (verbatim):

> - Defining a fixed taxonomy of concept types.
> - Prescribing storage, serving, or query infrastructure.
> - Replacing domain-specific schemas (Avro, Protobuf, OpenAPI, etc.) — OKF
>   *references* them; it does not subsume them.

### §2 Terminology (verbatim)

> - **Knowledge Bundle** — A self-contained, hierarchical collection of
>   knowledge documents. The unit of distribution.
> - **Concept** — A single unit of knowledge within a bundle. Represented as one
>   markdown document. May describe a tangible asset (a table, an API), an
>   abstract idea (a metric, a business process), or anything in between.
> - **Concept ID** — The path of the concept's file within the bundle, with the
>   `.md` suffix removed. For example, `tables/users.md` has concept ID
>   `tables/users`.
> - **Frontmatter** — YAML metadata block delimited by `---` at the top of a
>   markdown file.
> - **Body** — Everything in the file after the frontmatter.
> - **Link** — A standard markdown link from one concept to another, used to
>   express relationships beyond the implicit parent/child hierarchy.
> - **Citation** — A link from a concept to an external source that supports a
>   claim in the body.

### §3 Bundle Structure (verbatim)

> A bundle is a directory tree of markdown files. The directory structure is
> independent of the domain — producers organize concepts however makes sense for
> the knowledge being captured.

> A bundle MAY be distributed as:
> - A git repository (recommended — provides history, attribution, diffs).
> - A tarball or zip archive of the directory.
> - A subdirectory within a larger repository.

#### §3.1 Reserved filenames (verbatim)

> The following filenames have defined meaning at any level of the hierarchy and
> MUST NOT be used for concept documents:
>
> | Filename     | Purpose                                                |
> |--------------|--------------------------------------------------------|
> | `index.md`   | Directory listing. See §6.                             |
> | `log.md`     | Update history. See §7.                                |
>
> All other `.md` files are concept documents.

> Tags themselves remain a first-class concept — see the `tags` frontmatter field
> in §4.1. OKF does not specify a separate file format for aggregating documents
> by tag; producers that want a tag-browsing view can synthesize one at
> consumption time by scanning frontmatter.

There are exactly **two** reserved filenames: `index.md` and `log.md`.

### §4 Concept Documents

> Every concept is a UTF-8 markdown file. It has two parts:
> 1. A **YAML frontmatter block**, delimited by `---` on its own line at the
>    start of the file and a closing `---` on its own line.
> 2. A **markdown body**, containing free-form content.

#### §4.1 Frontmatter (verbatim)

Frontmatter template from the spec:

```yaml
---
type: <Type name>                  # REQUIRED
title: <Optional display name>
description: <Optional one-line summary>
resource: <Optional canonical URI for the underlying asset>
tags: [<tag>, <tag>, …]            # Optional
timestamp: <ISO 8601 datetime>     # Optional last-modified time
# … other producer-defined key/value pairs
---
```

> **Required:**
> - `type` — A short string identifying the kind of concept. Consumers use this
>   for routing, filtering, and presentation. Example values: `BigQuery Table`,
>   `BigQuery Dataset`, `API Endpoint`, `Metric`, `Playbook`, `Reference`.
>
>   Type values are **not** registered centrally. Producers SHOULD pick values
>   that are descriptive and self-explanatory; consumers MUST tolerate unknown
>   types gracefully (typically by treating them as generic concepts).

> **Recommended (in priority order):**
> - `title` — Human-readable display name. If omitted, consumers MAY derive a
>   title from the filename.
> - `description` — A single sentence summarizing the concept. Used by `index.md`
>   generators, search snippets, and previews.
> - `resource` — A URI that uniquely identifies the underlying asset the concept
>   describes. Absent for concepts that describe abstract ideas rather than
>   physical resources.
> - `tags` — A YAML list of short strings for cross-cutting categorization.
> - `timestamp` — ISO 8601 datetime of last meaningful change.
>
> **Extensions:** Producers MAY include any additional keys. Consumers SHOULD
> preserve unknown keys when round-tripping and SHOULD NOT reject documents with
> unrecognized fields.

So: **exactly one required field (`type`), non-empty string, no central registry.**
Everything else is optional. Unknown keys must be tolerated (this is compatible
with matlatl's own `matlatl:` frontmatter key and with `title`/aliases).

#### §4.2 Body — conventional (not required) section headings (verbatim)

> There are no required body sections. The following section headings have
> **conventional** meaning and SHOULD be used when applicable:
>
> | Heading        | Purpose                                                |
> |----------------|--------------------------------------------------------|
> | `# Schema`     | Structured description of an asset's columns/fields.   |
> | `# Examples`   | Concrete usage examples, often as fenced code blocks.  |
> | `# Citations`  | External sources backing claims in the body. See §8.   |

### §5 Cross-linking (verbatim — the load-bearing section for matlatl)

> Concepts MAY link to other concepts using standard markdown links. Two forms
> are supported:

**§5.1 Absolute (bundle-relative) links:**

> Begin with `/`, interpreted relative to the bundle root.
> ```markdown
> See the [customers table](/tables/customers.md) for the join key.
> ```
> This is the **recommended** form because it is stable when documents are moved
> within their subdirectory.

**§5.2 Relative links:**

> Standard markdown relative paths.
> ```markdown
> See the [neighboring concept](./other.md).
> ```

**§5.3 Link semantics:**

> A link from concept A to concept B asserts a *relationship*. The specific kind
> of relationship (parent/child, references, joins-with, depends-on, etc.) is
> conveyed by the surrounding prose, not by the link itself. Consumers that build
> a graph view typically treat all links as directed edges of an untyped
> relationship.
>
> Consumers MUST tolerate broken links — a link whose target does not exist in
> the bundle is not malformed; it may simply represent not-yet-written knowledge.

Two things matter enormously here for matlatl:
1. The **recommended** link form is `/`-prefixed, **resolved from the bundle
   root** — a resolution rule matlatl does *not* currently implement (see
   Conflicts below).
2. **Broken links are explicitly NOT non-conformance.** A consumer "MUST
   tolerate" them. matlatl's default treatment of broken links as a gating
   finding is *stricter* than OKF conformance — which is arguably the product
   value (a CI gate on link rot), but it means "broken link" ≠ "non-conformant
   bundle" and the conformance mode must not conflate them.

### §6 Index Files (verbatim)

> An `index.md` file MAY appear in any directory, including the bundle root. It
> enumerates the directory's contents to support **progressive disclosure** [...]
>
> Index files contain no frontmatter. The body uses one or more sections, each
> grouping concepts under a heading:
>
> ```markdown
> # Section / Group Heading
>
> * [Title 1](relative-url-1) - short description of item 1
> * [Title 2](relative-url-2) - short description of item 2
>
> # Another Section
>
> * [Subdirectory](subdir/) - short description of the subdirectory
> ```
>
> Entries SHOULD include the description from the linked concept's frontmatter.
> Producers MAY generate `index.md` automatically; consumers MAY synthesize one
> on the fly when none is present.

Note the tension with §11: index files "contain no frontmatter" **except** the
bundle-root `index.md` may carry a single `okf_version` key (see §11).

### §7 Log Files (verbatim)

> A `log.md` file MAY appear at any level of the hierarchy to record the history
> of changes to that scope. The format is a flat list of date-grouped entries,
> newest first:
>
> ```markdown
> # Directory Update Log
>
> ## 2026-05-22
> * **Update**: Added new BigQuery table reference for [Customer Metrics](/tables/customer-metrics.md).
> * **Creation**: Established the [Dataplex Playbook](/playbooks/dataplex.md).
> ```
>
> Date headings MUST use ISO 8601 `YYYY-MM-DD` form. Log entries are prose; the
> leading bold word (`**Update**`, `**Creation**`, `**Deprecation**`, etc.) is a
> convention, not a requirement.

The only **MUST** here is the `YYYY-MM-DD` date-heading form.

### §8 Citations (verbatim)

> When a concept's body makes claims sourced from external material, those sources
> SHOULD be listed under a `# Citations` heading at the bottom of the document,
> numbered [...]
>
> Citation links MAY be absolute URLs, bundle-relative paths, or paths into a
> `references/` subdirectory that mirrors external material as first-class OKF
> concepts.

`references/` is a **convention**, not a reserved filename.

### §9 Conformance (verbatim — the normative core)

> A bundle is **conformant** with OKF v0.1 if:
>
> 1. Every non-reserved `.md` file in the tree contains a parseable YAML
>    frontmatter block.
> 2. Every frontmatter block contains a non-empty `type` field.
> 3. Every reserved filename (`index.md`, `log.md`) follows the structure
>    described in §6 and §7 respectively when present.
>
> Consumers SHOULD treat all other constraints as soft guidance. In particular,
> consumers MUST NOT reject a bundle because of:
> - Missing optional frontmatter fields.
> - Unknown `type` values.
> - Unknown additional frontmatter keys.
> - Broken cross-links.
> - Missing `index.md` files.
>
> This permissive consumption model is intentional: OKF is meant to remain useful
> as bundles grow, get refactored, and are partially generated by agents.

**This is the entire normative surface. There are exactly three conformance
rules.** Everything else in the spec is SHOULD/MAY. Note in particular that
**broken links and missing index files are explicitly carved out** as things a
consumer MUST NOT reject on.

### §11 Versioning (verbatim)

> This document specifies OKF version **0.1**. Future revisions will be versioned
> in the form `<major>.<minor>`:
> - A **minor** version bump introduces backward-compatible additions (new
>   optional fields, new conventional section headings).
> - A **major** version bump may make breaking changes (renaming required fields,
>   changing reserved filenames).
>
> Bundles MAY declare the OKF version they target by including
> `okf_version: "0.1"` in a bundle-root `index.md` frontmatter block (the only
> place frontmatter is permitted in an `index.md`). Consumers that do not
> understand the declared version SHOULD attempt best-effort consumption rather
> than refusing the bundle.

Bundle-root marker: **`okf_version` in the root `index.md` frontmatter** is the
closest thing OKF has to a manifest / bundle-root marker. It is **optional** and
its presence is the *only* case where an `index.md` carries frontmatter.

---

## Mapping OKF conformance → matlatl machinery

The three normative rules from §9, mapped to what matlatl does today.

### Rule 1 — every non-reserved `.md` has a parseable YAML frontmatter block

- **matlatl today:** parses frontmatter already (`internal/infrastructure/mdparser`
  handles `---`/`+++` fenced frontmatter; the domain reads `title`, aliases, the
  `matlatl:` control key, front-matter parent). It does **not** currently *require*
  frontmatter, nor emit a finding for a document that has none or has malformed
  YAML.
- **Gap:** a new validation that flags concept docs (i.e. non-`index.md`/`log.md`)
  with **absent or unparseable** frontmatter. The parser already distinguishes
  "has frontmatter fence" from "doesn't"; this is a new finding type gated behind
  the OKF mode, not a change to default behavior.

### Rule 2 — every frontmatter block has a non-empty `type`

- **matlatl today:** does not read or care about a `type` key.
- **Gap:** new validation — read `type`, flag empty/missing. Mechanically trivial
  (the frontmatter is already parsed to a map); the work is the finding type +
  wiring it to be OKF-mode-only. This is the single most OKF-specific new check.

### Rule 3 — reserved files (`index.md`, `log.md`) follow §6/§7 structure when present

- **matlatl today:** treats `index.md`/`README.md` as directory-index docs for
  reachability (ADR 0007/0008) but does **not** validate their internal structure;
  `log.md` has no special handling.
- **Gap (graduated):**
  - **`index.md`:** §6 says no frontmatter (except root `index.md` may carry
    `okf_version`). A cheap check: flag frontmatter in a non-root `index.md`.
    Validating "is a list of `[Title](url) - desc` entries" is looser (SHOULD-ish
    prose) — probably out of scope for a first cut, or Info-level only.
  - **`log.md`:** the only **MUST** is `## YYYY-MM-DD` date headings. A check that
    every second-level heading in a `log.md` matches `\d{4}-\d{2}-\d{2}` is
    well-defined and cheap.

### Reused as-is (matlatl's existing gates map onto OKF's *soft* guidance)

These are **not** OKF conformance requirements (§9 explicitly says consumers MUST
NOT reject on them), but they are exactly matlatl's value-add — "format integrity"
in the issue's framing. In `--okf` mode they should be reframed as **quality
signals, not conformance failures**:

- **Broken links / anchors** — matlatl's core. Per §5.3/§9, a broken cross-link
  is *conformant*. So: keep reporting, but in OKF mode they are Info/warn about
  bundle *health*, not a conformance verdict. (This is the subtle bit — do not let
  `--okf` turn broken links into a conformance failure.)
- **Orphans / under-linked / dead-end** (ADR 0012) — an orphan concept is
  un-integrated knowledge; strong signal, still not a §9 requirement.
- **Navigability / hops-from-root / critical-path** (ADR 0014/0015/0021) — bonus
  insight over an OKF bundle, all out of the conformance verdict.

### Out of scope for conformance

- `type` **value** taxonomy — §4.1 forbids central registration and requires
  consumers to tolerate unknown types. matlatl MUST NOT validate `type` against
  any allow-list.
- Conventional body headings (`# Schema` / `# Examples` / `# Citations`) — SHOULD.
- Citations format, `references/` convention — SHOULD/MAY.
- `index.md` entry-line format, `log.md` bold-word convention — explicitly "a
  convention, not a requirement."

---

## Conflicts / mismatches with matlatl's model

### 🔴 Conflict 1 (the big one): OKF absolute `/`-links vs matlatl's origin-relative resolution

> **Resolved in #27 / [ADR 0022](../adr/0022-root-absolute-links.md).** A single
> leading `/` now resolves from the scan root (default-on, all modes); `//` stays
> external. The analysis below is retained as the original problem statement.

OKF §5.1 makes `/`-prefixed, **bundle-root-relative** links the **recommended**
form. matlatl does **not** resolve these correctly today.

`internal/domain/reference/resolver.go` → `resolveInRoot()` joins every relative
target onto the **origin document's directory**:

```go
dir := path.Dir(string(origin)) // e.g. "datasets" for datasets/sales.md
joined := path.Join(dir, target)
```

For an OKF link `[orders](/tables/orders.md)` written in `datasets/sales.md`,
`path.Join("datasets", "/tables/orders.md")` yields **`datasets/tables/orders.md`**
(verified empirically), not the intended bundle-root `tables/orders.md`. The
leading `/` is swallowed by `path.Join`, not treated as "reset to root."

Consequence: **matlatl would report false "broken link" findings on a perfectly
conformant OKF bundle that uses the recommended link style** — from every
non-root document. (It happens to work by accident only when the origin is at the
bundle root, where `path.Dir` is `.`.)

Also note `isExternal()` treats a `//`-prefixed target as external (protocol-
relative URL), so the guard is specifically the *single* leading slash.

**Fix direction:** teach the resolver that a target beginning with a single `/`
(and not `//`) resolves relative to the corpus/bundle root — i.e. strip the
leading slash and clean from root instead of from `path.Dir(origin)`. This is a
small, well-contained change in `resolveRelative`/`resolveInRoot` (or at parse
time), but it is a **prerequisite** for OKF mode to be correct, and it has
implications for the default (non-OKF) mode too — decide whether root-absolute
links become a first-class supported form everywhere or only under `--okf`. The
security invariant (reject root escapes, ADR 0003) is unaffected: a root-relative
path still cannot escape the root.

### 🟡 Conflict 2: "broken link" is a matlatl gate but is conformant per OKF §5.3/§9

matlatl's whole default posture is "broken links fail CI." OKF explicitly says a
broken cross-link is **not** a conformance violation. These aren't contradictory
— matlatl offers a *stricter, opt-in* health gate — but `--okf` mode must keep
the **conformance verdict** (§9's three rules) distinct from the **health
report** (link rot, orphans). Conflating them would make matlatl reject bundles
OKF says are fine, undermining the "conformance mode" claim.

### 🟢 Conflict 3 (minor): wikilinks are non-OKF

OKF cross-linking is **standard markdown links only** (§5). matlatl also parses
`[[wikilinks]]` and `![[transclusions]]` (a superset). No conflict — wikilinks
simply aren't part of OKF; a bundle using them is using a non-OKF extension.
matlatl need not do anything special, but should not *require* or *reward*
wikilinks in OKF mode. (Prior dogfood note already flagged the wikilink gap;
irrelevant to OKF conformance.)

### 🟢 Alignment: concept identity

OKF §2 "Concept ID = file path minus `.md`" is essentially matlatl's ADR 0001
(identity = repo-relative path). matlatl keeps the `.md`; OKF strips it — a
cosmetic difference in the ID string, same underlying model. No conflict.

### 🟢 Alignment: frontmatter extensibility

OKF §4.1 requires consumers to tolerate unknown frontmatter keys. matlatl's own
`matlatl:` control key and `title`/alias handling are compatible — matlatl already
ignores keys it doesn't recognize. matlatl's `orphan-intentional` marker is just
an "additional producer-defined key" from OKF's point of view.

---

## Bottom line for issue #18

- The spec is **real, primary, Apache-2.0, and pinnable** (commit + blob SHAs
  above). Plan against the spec text, not the blog. Status is "0.1 — Draft."
- **Conformance is exactly three rules** (§9): parseable frontmatter on every
  concept doc; non-empty `type`; reserved-file structure. Two of the three
  (`type` presence, frontmatter presence) are **new, small, well-defined** checks.
  The third (reserved-file structure) reduces to a couple of cheap checks
  (`log.md` date headings; non-root `index.md` should have no frontmatter).
- **One real prerequisite bug/feature:** matlatl mis-resolves OKF's *recommended*
  `/`-absolute links (Conflict 1). This must be fixed for OKF mode to be
  trustworthy and is the highest-value implementation item.
- **Keep conformance separate from health.** Broken links/orphans are matlatl's
  differentiated value but are *not* OKF non-conformance — surface them as bundle
  health, not as a conformance failure.

## Sources

- OKF v0.1 spec (pinned): <https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/d44368c15e38e7c92481c5992e4f9b5b421a801d/okf/SPEC.md>
- OKF license (Apache-2.0): <https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/LICENSE.md>
- OKF README: <https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/README.md>
- Announcement: <https://cloud.google.com/blog/products/data-analytics/how-the-open-knowledge-format-can-improve-data-sharing/>
- matlatl resolver (origin-relative resolution): `internal/domain/reference/resolver.go`
- Retrieved 2026-07-19.
</content>
</invoke>
