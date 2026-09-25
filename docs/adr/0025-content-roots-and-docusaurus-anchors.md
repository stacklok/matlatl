# 25. Content roots and Docusaurus heading anchors

Date: 2026-09-25

Status: Accepted

## Context

Docusaurus repositories commonly keep site content below a directory such as
`user-docs/`. Inside that directory `/api/widget.mdx` is site-root-relative, not
repository-root-relative. They also use MDX `<Heading id="…">` components and
explicit heading IDs, which are valid fragment targets without always mapping to
a conventional Markdown heading slug.

## Decision

### Config-only content roots

`.matlatl.yml` v1 gains optional `contentRoots`, a sorted set of canonical,
repository-relative directories. For an origin **strictly inside** one configured
root, a single-leading-slash target resolves from that content root. Origins
outside every content root retain ADR 0022 repository-root semantics; relative
links and wikilinks are unchanged. Multiple roots are allowed, but duplicates
and nested/overlapping roots are hard configuration errors.

`//host/path` remains external. Resolution strips the single slash, cleans, and
checks for root escape before adding a content-root prefix, so `/../x` remains
Broken and is never probed. A bare `/` remains Broken.

### Docusaurus anchors

The parser recognizes only block-level literal `<Heading … id="…">` or
single-quoted `id` attributes: the opening `<Heading` must begin a Markdown line
after at most three spaces. Attributes may be reordered or span lines, but the
tag must close within 16 KiB. This intentionally supports normal Docusaurus
generated MDX rather than arbitrary inline JSX. It does not evaluate
MDX/JavaScript: expression-valued IDs, malformed tags, other elements, comments,
strings, and code spans/fences are ignored. These IDs enter the heading
inventory for fragment validation only; they do not create sections or graph
vertices.

For headings, documented `## Title {#id}` and `## Title {/* #id */}` forms
replace the generated slug and are removed from displayed section text. The
automatic slug is deliberately not retained. Malformed forms stay ordinary
heading text. This does not add Docusaurus's legacy MDX classic-ID compatibility
beyond the standard Markdown `{#id}` form.

## Consequences

- Existing repositories have unchanged resolution because `contentRoots` is
  absent by default.
- Docusaurus `.mdx` files are scanned as Markdown documents.
- No artifact schema changes: literal anchors only affect validation, while
  explicit heading IDs reuse an existing section slug field.
