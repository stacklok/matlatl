# 8. Directory links resolve and confer navigational reachability

Date: 2026-06-05
Status: Accepted

## Context

A markdown link whose target is a *directory* — `[ADRs](docs/adr/)`,
`[ADRs](adr/)`, `[notes](sub)` — is a universal documentation convention.
GitHub, MkDocs and Docusaurus all render it: GitHub shows the folder's file
listing (or its `README.md` if present), MkDocs/Docusaurus route to the folder's
`index.md`. matlatl did not model this. The resolver only matched markdown-file
`DocumentID`s, so a directory link was classified **Broken**, and — because no
edge was produced — the markdown files inside that directory showed up as
**orphans / unreachable** even though something explicitly linked their folder.

This was verified by dogfooding matlatl on its own repo: `docs/adr/` and `adr/`
(linked from `README.md`, `docs/architecture.md`, `docs/dev-guide.md`,
`docs/user-guide.md`) were reported as broken links, and
`docs/adr/0007-graph-node-semantics.md` (which nothing links to by file path) was
a false orphan.

Both classifications are wrong. Linking a folder is a real, resolvable
navigational act, and "pointing at the folder" should reach the folder's
contents.

The hazard this ADR must avoid is re-introducing blanket structural
reachability. ADR 0007 deliberately **drops CONTAINS edges** from the document
projection so the folder tree does not make every document trivially reachable
(the "nothing is ever unreachable" failure mode). A naive fix — "a directory link
reaches everything under the directory, recursively" — would smuggle the folder
tree back in through the navigational door. This ADR is scoped precisely to
avoid that.

## Decision

### What is a directory link

A `RelativeLink` (or `Wikilink`/`Transclusion`/`ImageEmbed`/`FrontmatterRelated`,
i.e. the path-bearing types the resolver already routes through relative
resolution) whose **cleaned, in-root target denotes a directory in the corpus**
is a **directory link**. A target denotes a directory when it is *not itself a
known markdown `DocumentID`* but **at least one known `DocumentID` has it as a
path-segment prefix** (i.e. `Dir()` of some document equals the target, at the
direct-child level — see "one level" below). A trailing slash is optional:
`docs/adr` and `docs/adr/` are equivalent (both clean to `docs/adr`).

Ordering of guards (unchanged precedence):

1. **External** (`http`, `mailto`, `file:`, `data:`, …) → `HealthExternal`,
   never inspected.
2. **Out-of-root guard (ADR 0003)** runs *first*. A target escaping the corpus
   root is `Broken` and is **never** inspected as a directory.
3. **Markdown file** target that is a known `DocumentID` → existing
   `TargetDocument` behavior (unchanged).
4. **Directory** target (this ADR) → `TargetDirectory`.
5. Existing **asset** / **broken** fallbacks (unchanged).

### Resolution outcome

A directory link is **`Valid`** with `TargetKind = TargetDirectory`. Its
`ResolvedTarget` carries:

- `Directory` — the cleaned directory path (e.g. `docs/adr`).
- `DocumentID` — the directory's **index document** if one exists
  (`README.md` / `index.md`, matched case-insensitively via
  `identity.IsDirectoryIndex`, located **directly** in that directory), else
  empty. This is the canonical/primary target shown to users and used as the
  primary "what-links-here" edge.
- `Children` — the sorted list of markdown `DocumentID`s located **directly** in
  the directory (the index, if any, is included).

Cases:

- **Directory with an index doc** → `Valid`; primary target = the index;
  children enumerated.
- **Directory containing markdown but NO index** → still `Valid` (GitHub shows a
  file listing); primary target = the directory itself (no `DocumentID`);
  children enumerated.
- **Target that names a non-corpus path that does not exist on disk** →
  `Broken` (genuinely dangling).
- **Target that is an existing non-markdown directory (no markdown directly
  inside it, e.g. an `examples/` of code or assets)** → `NonNote` (asset), not
  `Broken`: it exists on disk, so it is not link rot, but it is not a
  documentation destination and confers no reachability (no child edges). This
  mirrors how an existing non-markdown *file* asset resolves.

Anchors on a directory link (`docs/adr/#x`) are not meaningful (a directory has
no headings); the fragment is ignored and the link resolves as a plain directory
link.

### Reachability (the scoped part)

A directory link makes the markdown documents located **directly in that
directory — one level, direct children only, NOT recursive into subdirectories**
— reachable from the origin in the document projection. Concretely, the graph
build adds navigational `Origin → child` edges for each direct child, plus the
primary `Origin → index` edge (the index is itself a direct child, so it is
covered).

**Why one level, not recursive.** This is the crux of preserving ADR 0007. A
directory link is a *navigational* act scoped to exactly the folder that was
linked. Making it recursive would mean a single link to a top-level folder (e.g.
`docs/`) transitively reaches every document in every subtree, which is
indistinguishable from re-adding the CONTAINS tree as reachability edges — the
exact thing ADR 0007 dropped to avoid "nothing is ever unreachable". One level
keeps reachability tied to an *explicit* human decision to link a *specific*
folder: linking `docs/adr/` vouches for the ADRs in that folder, but does not
silently vouch for `docs/adr/archive/old/` two levels down. A subfolder earns
reachability only when something links *it* (directly, or via its parent's index
that itself links it). This is navigational reachability for an
explicitly-linked directory — **not** blanket structural reachability. ADR 0007's
invariant (CONTAINS edges are never reachability edges) stands; we add a bounded,
opt-in-by-authoring navigational edge set.

### Policy knob (strictness)

Two behaviors, gated on the existing `--strict` flag (`Config.Strict`), threaded
into graph construction via `BuildOptions.StrictDirectoryLinks`:

- **Default (lenient / "vouch")**: a directory link confers reachability on its
  direct children (edges `Origin → each direct child`). This is the behavior
  documented above and the sensible default — authors expect linking a folder to
  surface its contents.
- **Strict (`--strict`)**: a directory link still **resolves and validates**
  (primary edge `Origin → index` only, when an index exists), but does **not
  vouch for the directory's contents**. Non-index siblings that are not otherwise
  linked then correctly surface as orphans/unreachable. This is the
  "documentation hygiene" hardline: an index that does not explicitly link its
  siblings is a real gap the author should close. (When the directory has no
  index, strict adds no edge at all — there is nothing to vouch and nothing to
  link to.)

`--strict` already promotes orphan/unreachable/ambiguous findings to build
failures (ADR 0005); coupling the hardline reachability semantics to the same
flag keeps the mental model single-axis ("strict = hold me to a higher
standard") and avoids a new flag.

### Purity

The resolver remains pure (ADR 0004): it detects a directory target, finds its
index, and enumerates direct children **entirely from the `Catalog` of known
`DocumentID`s** — no filesystem access, no `os`, no goldmark. "Is this path a
directory?" is answered as "does some known document live directly under it?",
which is a pure set computation over `Catalog.DocumentIDs()`. The `Catalog`
interface gains no methods; the resolver computes children by scanning the sorted
`DocumentIDs()` it already has access to.

## Consequences

- Directory links (`docs/adr/`, `adr/`) are `Valid`, not broken.
- Files in an explicitly-linked folder (e.g. `docs/adr/0007-…md`) are reachable
  and no longer false orphans — under the default policy.
- `--strict` turns the lenient vouch off, restoring the documentation-hygiene
  hardline (an index must explicitly link its siblings).
- ADR 0007's no-blanket-structural-reachability invariant is preserved: the
  one-level scoping means a directory link never transitively reaches a subtree,
  so the folder tree is not re-introduced as reachability.
- Determinism is preserved: children are enumerated by scanning the already-sorted
  `DocumentIDs()` and the resulting child list / edges are sorted.
- The out-of-root guard (ADR 0003) is unchanged and runs before any directory
  inspection: an escaping target is `Broken` and never enumerated.
