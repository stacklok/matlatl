package reference

import (
	"path"
	"slices"
	"strings"

	"github.com/stacklok/matlatl/internal/domain/identity"
)

// Catalog is the read-only view of the corpus that the resolver needs. It is a
// small domain interface (a real test seam) implemented by *corpus.Corpus and by
// in-test fakes. Defining it here — over identity.DocumentID only — keeps the
// resolver pure and avoids a reference→corpus import cycle (corpus already
// imports reference).
type Catalog interface {
	// HasDocument reports whether id is a known markdown document.
	HasDocument(id identity.DocumentID) bool
	// DocumentIDs returns all known markdown document identities. Order is not
	// relied upon; the resolver sorts candidates itself for determinism.
	DocumentIDs() []identity.DocumentID
	// HasHeading reports whether document id contains the given canonical slug
	// (ADR 0006). The slug must be in the exact dialect the parser produced.
	HasHeading(id identity.DocumentID, slug string) bool
	// LookupAlias returns the documents declaring the given front-matter alias,
	// sorted by DocumentID. Empty if unknown.
	LookupAlias(alias string) []identity.DocumentID
}

// AssetExistence answers whether a cleaned, root-relative path points at an
// existing NON-markdown asset — either a regular file (image/pdf/etc.) or a
// directory. It is injected so the domain stays free of filesystem access
// (ADR 0003/0004): the path is always in-root and already cleaned by the
// resolver before this is consulted. A nil AssetExistence is treated as "no
// assets exist".
type AssetExistence interface {
	// AssetExists reports whether the given root-relative slash path exists as a
	// non-markdown asset: an existing non-markdown file or directory. Markdown
	// is excluded (tracked via the corpus, not as an asset).
	AssetExists(relPath string) bool
}

// Resolver turns RawReferences into health-classified References. It is a
// stateless domain service: construct once with a Catalog, an optional
// AssetExistence, and a ResolutionPolicy, then call Resolve per reference.
type Resolver struct {
	catalog Catalog
	assets  AssetExistence
	policy  ResolutionPolicy
}

// NewResolver builds a Resolver. An invalid policy falls back to the default
// (LongestSuffix). A nil assets lookup means non-markdown targets that are not
// known documents resolve to Broken rather than NonNote.
func NewResolver(catalog Catalog, assets AssetExistence, policy ResolutionPolicy) *Resolver {
	if !policy.Valid() {
		policy = DefaultResolutionPolicy
	}
	return &Resolver{catalog: catalog, assets: assets, policy: policy}
}

// Resolve classifies a single RawReference. It performs only path arithmetic and
// catalog lookups — never any filesystem access (asset existence is delegated to
// the injected AssetExistence).
//
// Classification summary (see ADR 0001/0003/0006):
//
//   - External targets (http/https/mailto/autolink, or Type External) →
//     HealthExternal (not fetched in P2).
//   - Anchor-only targets (no path, has fragment) → resolved within the origin
//     document; Valid if the slug exists there, else BrokenAnchor.
//   - Relative links: resolved relative to the origin's directory and cleaned.
//     A target that escapes the corpus root → Broken (recorded as a finding,
//     never read). A target resolving to a known markdown doc → Valid (anchor
//     checked if present). A target resolving to an existing non-markdown asset
//     → NonNote. Otherwise → Broken.
//   - Wikilinks/transclusions: resolved by ResolutionPolicy against known docs;
//     exactly one candidate → Valid (anchor checked), more than one → Ambiguous
//     (candidates surfaced), zero → alias table, else Broken.
func (r *Resolver) Resolve(raw RawReference) Reference {
	switch raw.Type {
	case External:
		return ref(raw, ResolvedTarget{Kind: TargetExternal}, HealthExternal)
	case Anchor:
		return r.resolveAnchorOnly(raw)
	case Wikilink, Transclusion:
		return r.resolveWikilink(raw)
	case RelativeLink, ImageEmbed, FrontmatterRelated:
		return r.resolveRelative(raw)
	default:
		return ref(raw, ResolvedTarget{Kind: TargetNone}, Unresolved)
	}
}

// ResolveAll resolves every reference and returns the classified edges in input
// order (callers sort findings later for output determinism).
func (r *Resolver) ResolveAll(raws []RawReference) []Reference {
	out := make([]Reference, 0, len(raws))
	for _, raw := range raws {
		out = append(out, r.Resolve(raw))
	}
	return out
}

// resolveAnchorOnly handles [](#frag) / [[#frag]]: the fragment must exist in
// the origin document itself.
func (r *Resolver) resolveAnchorOnly(raw RawReference) Reference {
	if raw.Fragment == "" {
		return ref(raw, ResolvedTarget{Kind: TargetNone}, Broken)
	}
	if r.catalog.HasHeading(raw.Origin, raw.Fragment) {
		return ref(raw, ResolvedTarget{Kind: TargetSection, DocumentID: raw.Origin, Anchor: raw.Fragment}, Valid)
	}
	return ref(raw, ResolvedTarget{Kind: TargetSection, DocumentID: raw.Origin}, BrokenAnchor)
}

// resolveRelative resolves a relative link/image against the origin directory.
func (r *Resolver) resolveRelative(raw RawReference) Reference {
	target := raw.RawTarget
	// A relative link with no path but a fragment is an in-document anchor.
	if target == "" {
		if raw.Fragment != "" {
			return r.resolveAnchorOnly(raw)
		}
		return ref(raw, ResolvedTarget{Kind: TargetNone}, Broken)
	}

	cleaned, ok := resolveInRoot(raw.Origin, target)
	if !ok {
		// Escapes the corpus root (ADR 0003): recorded as a finding, never read.
		return ref(raw, ResolvedTarget{Kind: TargetNone}, Broken)
	}
	id := identity.DocumentID(cleaned)

	if identity.IsMarkdownPath(cleaned) {
		if r.catalog.HasDocument(id) {
			return r.withAnchor(raw, id)
		}
		// A markdown path that is not a known document may still name a directory
		// in the corpus (rare, e.g. a folder literally named "x.md"), so attempt a
		// directory resolution before falling back to Broken (ADR 0008).
		if dir, ok := r.resolveDirectory(raw, cleaned); ok {
			return dir
		}
		return ref(raw, ResolvedTarget{Kind: TargetNone}, Broken)
	}

	// Non-markdown path: a known doc (rare) wins, else a directory in the corpus
	// (ADR 0008), else an existing asset → NonNote, else Broken.
	if r.catalog.HasDocument(id) {
		return r.withAnchor(raw, id)
	}
	if dir, ok := r.resolveDirectory(raw, cleaned); ok {
		return dir
	}
	if r.assets != nil && r.assets.AssetExists(cleaned) {
		return ref(raw, ResolvedTarget{Kind: TargetAsset, DocumentID: id}, NonNote)
	}
	return ref(raw, ResolvedTarget{Kind: TargetNone}, Broken)
}

// resolveWikilink resolves a wikilink/transclusion by the active policy.
func (r *Resolver) resolveWikilink(raw RawReference) Reference {
	if raw.RawTarget == "" {
		// [[#frag]] handled as Anchor by the parser, but guard anyway.
		if raw.Fragment != "" {
			return r.resolveAnchorOnly(raw)
		}
		return ref(raw, ResolvedTarget{Kind: TargetNone}, Broken)
	}

	candidates := r.matchCandidates(raw.RawTarget)
	switch len(candidates) {
	case 1:
		return r.withAnchor(raw, candidates[0])
	case 0:
		// Fall back to the alias table.
		if aliases := r.catalog.LookupAlias(raw.RawTarget); len(aliases) == 1 {
			return r.withAnchor(raw, aliases[0])
		} else if len(aliases) > 1 {
			return refAmbiguous(raw, aliases)
		}
		// A wikilink may name a directory in the corpus (ADR 0008): clean the
		// target as a root-relative path (rejecting root escapes) and attempt a
		// directory resolution before declaring it Broken.
		if cleaned := cleanWikilinkPath(raw.RawTarget); cleaned != "" {
			if dir, ok := r.resolveDirectory(raw, cleaned); ok {
				return dir
			}
		}
		return ref(raw, ResolvedTarget{Kind: TargetNone}, Broken)
	default:
		return refAmbiguous(raw, candidates)
	}
}

// withAnchor finalizes a target document, validating the fragment (if any)
// against the heading inventory (ADR 0006).
func (r *Resolver) withAnchor(raw RawReference, id identity.DocumentID) Reference {
	if raw.Fragment == "" {
		return ref(raw, ResolvedTarget{Kind: TargetDocument, DocumentID: id}, Valid)
	}
	if r.catalog.HasHeading(id, raw.Fragment) {
		return ref(raw, ResolvedTarget{Kind: TargetSection, DocumentID: id, Anchor: raw.Fragment}, Valid)
	}
	return ref(raw, ResolvedTarget{Kind: TargetSection, DocumentID: id}, BrokenAnchor)
}

// matchCandidates returns the known documents that match a wikilink target under
// the active policy, sorted by DocumentID for determinism.
//
// Semantics (documented contract):
//
//   - Exact: the cleaned target (with .md appended if extensionless) must equal a
//     DocumentID exactly. 0 or 1 result.
//   - LongestSuffix (default): a candidate matches if the DocumentID equals the
//     target or ends with "/"+target, comparing on path segments (so "guide.md"
//     matches "docs/guide.md" but not "myguide.md"). Among matches, only those
//     sharing the longest segment-suffix length are kept; ties at that maximal
//     length are genuine ambiguity (>1 ⇒ Ambiguous). The target is tried both
//     as-written and with ".md" appended when extensionless.
//   - Basename: a candidate matches if its basename equals the target's basename
//     (with/without .md). Least precise; multiple matches ⇒ Ambiguous.
func (r *Resolver) matchCandidates(target string) []identity.DocumentID {
	forms := targetForms(target)
	ids := r.catalog.DocumentIDs()

	switch r.policy {
	case Exact:
		return exactMatches(ids, forms)
	case Basename:
		return basenameMatches(ids, forms)
	case LongestSuffix:
		fallthrough
	default:
		return longestSuffixMatches(ids, forms)
	}
}

// targetForms returns the candidate path forms for a target: the cleaned target
// as written, plus a ".md"-appended form when it has no markdown extension. Both
// are slash-cleaned. Empty/escaping forms are dropped.
func targetForms(target string) []string {
	target = path.Clean(strings.TrimSpace(strings.ReplaceAll(target, "\\", "/")))
	if identity.EscapesRoot(target) {
		return nil
	}
	forms := []string{target}
	if !identity.IsMarkdownPath(target) {
		forms = append(forms, target+".md")
	}
	return forms
}

// exactMatches returns DocumentIDs that equal any target form exactly.
func exactMatches(ids []identity.DocumentID, forms []string) []identity.DocumentID {
	var out []identity.DocumentID
	for _, id := range ids {
		for _, f := range forms {
			if string(id) == f {
				out = append(out, id)
				break
			}
		}
	}
	return sortedUnique(out)
}

// basenameMatches returns DocumentIDs whose basename equals any target form's
// basename.
func basenameMatches(ids []identity.DocumentID, forms []string) []identity.DocumentID {
	wantBases := make(map[string]struct{}, len(forms))
	for _, f := range forms {
		wantBases[path.Base(f)] = struct{}{}
	}
	var out []identity.DocumentID
	for _, id := range ids {
		if _, ok := wantBases[path.Base(string(id))]; ok {
			out = append(out, id)
		}
	}
	return sortedUnique(out)
}

// longestSuffixMatches implements the default policy: keep candidates matching a
// target form by path-segment suffix, retaining only those at the maximal
// suffix-segment length.
func longestSuffixMatches(ids []identity.DocumentID, forms []string) []identity.DocumentID {
	// First pass: record each candidate's best suffix-segment match length.
	type scored struct {
		id identity.DocumentID
		n  int
	}
	scoredIDs := make([]scored, 0, len(ids))
	bestLen := -1
	for _, id := range ids {
		idStr := string(id)
		for _, f := range forms {
			if n, ok := suffixSegmentLen(idStr, f); ok {
				scoredIDs = append(scoredIDs, scored{id: id, n: n})
				if n > bestLen {
					bestLen = n
				}
				break
			}
		}
	}
	// Second pass: keep only candidates at the maximal length.
	best := make([]identity.DocumentID, 0, len(scoredIDs))
	for _, s := range scoredIDs {
		if s.n == bestLen {
			best = append(best, s.id)
		}
	}
	return sortedUnique(best)
}

// suffixSegmentLen reports whether candidate ends with target on a path-segment
// boundary, returning the number of matched trailing segments. "guide.md"
// matches "docs/guide.md" (1 segment) but not "myguide.md".
func suffixSegmentLen(candidate, target string) (int, bool) {
	if candidate == target {
		return segmentCount(target), true
	}
	if strings.HasSuffix(candidate, "/"+target) {
		return segmentCount(target), true
	}
	return 0, false
}

func segmentCount(p string) int {
	if p == "" {
		return 0
	}
	return strings.Count(p, "/") + 1
}

// sortedUnique sorts DocumentIDs and removes duplicates for deterministic output.
func sortedUnique(ids []identity.DocumentID) []identity.DocumentID {
	if len(ids) == 0 {
		return nil
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// resolveDirectory attempts to classify cleaned (a cleaned, in-root,
// slash-separated path) as a DIRECTORY in the corpus (ADR 0008). It is pure: it
// answers "is this a directory?" purely from the Catalog of known DocumentIDs —
// a path is a directory iff at least one known markdown document lives DIRECTLY
// in it (one level; the document's Dir() equals the path). The out-of-root guard
// has already run before this is called, so cleaned is always in-root.
//
// On success it returns a Valid TargetDirectory reference carrying the directory
// path, its index document (README.md / index.md, if present) and the sorted set
// of direct-child markdown documents, and ok=true. If cleaned names no such
// directory (no markdown lives directly under it), it returns ok=false and the
// caller falls through to its existing Broken/asset handling.
func (r *Resolver) resolveDirectory(raw RawReference, cleaned string) (Reference, bool) {
	children, index, ok := r.directoryContents(cleaned)
	if !ok {
		return Reference{}, false
	}
	return ref(raw, ResolvedTarget{
		Kind:       TargetDirectory,
		Directory:  cleaned,
		DocumentID: index, // empty when the directory has no index doc
		Children:   children,
	}, Valid), true
}

// directoryContents enumerates the markdown documents located DIRECTLY in dir
// (one level — no recursion), and identifies the directory index document
// (README.md / index.md, case-insensitive, ADR 0007/0008) if one exists. It
// returns ok=false when no markdown document lives directly under dir (dir is
// not a documentation directory in the corpus). Children are sorted (the catalog
// already returns sorted DocumentIDs; we preserve that order). This is a pure set
// computation over the Catalog — no filesystem access.
func (r *Resolver) directoryContents(dir string) (children []identity.DocumentID, index identity.DocumentID, ok bool) {
	for _, id := range r.catalog.DocumentIDs() {
		if id.Dir() != dir {
			continue
		}
		children = append(children, id)
	}
	if len(children) == 0 {
		return nil, "", false
	}
	// Sort defensively so the result is deterministic regardless of the catalog's
	// iteration order, and pick the lowest-sorted index doc as the canonical one
	// (README.md sorts before index.md) for a stable primary target.
	slices.Sort(children)
	for _, id := range children {
		if identity.IsDirectoryIndex(id.Base()) {
			index = id
			break
		}
	}
	return children, index, true
}

// cleanWikilinkPath cleans a wikilink target into a root-relative slash path
// suitable for directory lookup, returning "" if it is empty or escapes the root
// (ADR 0003 — such a target is never inspected as a directory).
func cleanWikilinkPath(target string) string {
	cleaned := path.Clean(strings.TrimSpace(strings.ReplaceAll(target, "\\", "/")))
	if identity.EscapesRoot(cleaned) {
		return ""
	}
	return cleaned
}

// resolveInRoot joins a relative target onto the origin document's directory and
// cleans it to a root-relative slash path. It returns ok=false when the result
// escapes the corpus root (ADR 0003) — the target is then never read.
func resolveInRoot(origin identity.DocumentID, target string) (string, bool) {
	target = strings.ReplaceAll(target, "\\", "/")
	dir := path.Dir(string(origin)) // "." for a top-level origin
	joined := path.Join(dir, target)
	cleaned := path.Clean(joined)
	if identity.EscapesRoot(cleaned) {
		return "", false
	}
	return cleaned, true
}

// ref builds a resolved Reference.
func ref(raw RawReference, target ResolvedTarget, health LinkHealth) Reference {
	return Reference{RawReference: raw, Target: target, Health: health}
}

// refAmbiguous builds an Ambiguous reference, surfacing the candidate documents
// (sorted) so a finding can list them.
func refAmbiguous(raw RawReference, candidates []identity.DocumentID) Reference {
	r := ref(raw, ResolvedTarget{Kind: TargetNone}, Ambiguous)
	r.Candidates = append([]identity.DocumentID(nil), candidates...)
	return r
}
