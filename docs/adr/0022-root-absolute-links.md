# 22. Root-absolute links: a single leading `/` resolves from the scan root

Date: 2026-07-20
Status: Accepted

## Context

matlatl resolves a relative link against the linking document's directory
(ADR 0001): from `datasets/sales.md`, `../tables/orders.md` reaches
`tables/orders.md`. That is the correct default for most markdown, but it leaves
one common authoring form mishandled: the **root-absolute** link `/tables/orders.md`
— a single leading slash meaning "from the repository root", the form static-site
generators (Hugo, Jekyll, Docusaurus, MkDocs) and many docs sites render as a
site-root link.

Before this decision the resolver did no special-casing: `path.Join(dir, "/tables/orders.md")`
folds the leading `/` into the origin directory, so `/tables/orders.md` from
`datasets/sales.md` resolved to the in-root `datasets/tables/orders.md` — almost
never what the author meant, and reported as a broken link. Authors who write
root-absolute links (a legitimate, widely-used form) saw false broken-link
findings, and the doc graph missed real edges.

## Decision

### Single leading `/` resolves from the scan root

A link target with a **single** leading slash is **root-absolute**: it resolves
from the scan root, independent of the linking document's directory. So
`/tables/orders.md` resolves to `tables/orders.md` from **any** origin —
`datasets/sales.md`, `README.md`, or a doc six levels deep all reach the same
target. This is **default-on**: no mode gate, no flag, no migration notice, no
corpus scan. A relative link (no leading slash) is unchanged.

### It lives at resolve time, in one place

The change is confined to `resolveInRoot` in
`internal/domain/reference/resolver.go`, the single helper every path-bearing
relative reference (`RelativeLink`, `ImageEmbed`, `FrontmatterRelated`) already
funnels through. If the target is root-absolute, the base is the scan root
(skip `path.Dir(origin)`); otherwise the base is the origin directory, exactly as
before. Everything downstream — markdown-doc / directory / asset / anchor
classification — is untouched, so root-absolute links pick up **directory links**
(`/adr/` → `TargetDirectory`, ADR 0008), **fragments** (`/guide.md#overview`,
validated against the heading inventory, ADR 0006), and **image assets**
(`![](/assets/logo.png)`, probed as the root-relative `assets/logo.png`) for free.

### `//` stays external

A **double** leading slash (`//host/path`) is a protocol-relative URL and remains
**External** (ADR 0003). The parser's `isExternal` already classifies `//` as
external, but `FrontmatterRelated` edges bypass that classification and reach the
resolver directly, so the resolver carries its own explicit guard
(`reference.IsRootAbsolute`): a target is root-absolute only when it starts with
`/` and not `//`. A `FrontmatterRelated` `//`-target that reaches the resolver is
therefore **not** re-classified External there — it is simply refused
root-absolute treatment and falls through relative resolution, which for an
off-corpus host is a normal **Broken** link.

### Security: strip, THEN clean, THEN check (ADR 0003)

The order is security-critical. For a root-absolute target the resolver:

1. **strips the single leading slash** first (`target[1:]`; `IsRootAbsolute`
   guarantees exactly one leading slash) — `/../etc/passwd` → `../etc/passwd`;
2. **`path.Clean`s** the result — `../etc/passwd` stays `../etc/passwd`;
3. runs the existing **`EscapesRoot`** guard — `../etc/passwd` escapes → **Broken**,
   recorded as a finding and **never read**.

Cleaning **before** stripping would be a vulnerability: `path.Clean("/../etc/passwd")`
returns `/etc/passwd`, folding away the `..` and hiding the traversal. Stripping
first preserves the `..` so the guard can reject it. The slash is **not**
URL-decoded, so a percent-encoded traversal like `/..%2F..` stays a literal
in-root filename (`..%2F..`) — Broken because no such document/asset exists, not
an escape. The asset-existence probe is never consulted for an escaping target.

A bare `/` names the scan root itself, which is not a linkable document or
directory: it strips to `""`, cleans to `"."`, and resolves to **Broken** — the
same posture as a relative `./`. Root-directory linking is intentionally not a
supported form.

### No schema, version, or diagnostic changes

This is purely a resolution-semantics change. No artifact shape changes, so
**no schema bumps** (graph v7 / findings v7 / trails v1 stand) and **no golden
changes** — no existing fixture corpus contains real root-absolute links. **No
new finding kind**: a root-absolute link that resolves is a normal Valid edge, and
one that escapes or dangles is a normal Broken link, indistinguishable in the
findings from any other relative link.

### Wikilinks are out of scope

`[[/foo]]` wikilinks and `![[...]]` transclusions are **not** affected: their
behavior is unchanged. Wikilinks resolve by name-matching policy (ADR 0007), not
directory-relative path arithmetic, so the "resolve from root" concept does not
map onto them. If a root-absolute wikilink form is ever wanted it is a separate
decision.

## Consequences

- Authors can use the root-absolute `/path` form (as static-site generators do)
  without false broken-link findings, and the doc graph captures those edges.
- Root-absolute links are **origin-independent**: the same `/path` means the same
  target from every document, which is easier to author and refactor than deep
  `../../` chains.
- Directory, fragment, and image interactions come for free because the change is
  a single pre-classification base swap — no new code paths to keep in sync.
- The containment guarantee (ADR 0003) is preserved by the strip-then-clean-then-check
  ordering; the security tests pin that a `/`-form traversal is Broken and never
  probed.
- This is a **prerequisite for #18** (root-relative link rewriting / emit-time
  link normalization): once `/`-links resolve, they can be emitted and rewritten
  as a first-class link form.
