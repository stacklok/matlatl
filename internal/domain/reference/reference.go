// Package reference models the directed edges of the corpus: links between
// documents (and sections), their type, their resolution target, and their
// health classification.
//
// This package is pure domain (standard library only, plus the leaf identity
// package). It is a lower layer than corpus: corpus depends on reference (a
// Document holds []RawReference). The authoritative DocumentID type lives in the
// even-lower identity package, which both reference and corpus import without
// any cycle. The resolver logic itself lands in a later phase; this file defines
// the type spine only.
package reference

import "github.com/stacklok/matlatl/internal/domain/identity"

// LinkType classifies the syntactic origin of a reference edge.
type LinkType int

const (
	// RelativeLink is a CommonMark relative link, e.g. [text](other.md).
	RelativeLink LinkType = iota
	// Wikilink is an Obsidian-style link, e.g. [[target]] or [[target|alias]].
	Wikilink
	// Anchor is a same- or cross-document fragment link, e.g. #heading.
	Anchor
	// ImageEmbed is an inline image, e.g. ![alt](img.png).
	ImageEmbed
	// Transclusion is an embedded note, e.g. ![[target]].
	Transclusion
	// FrontmatterRelated is an edge derived from front-matter fields such as
	// parent/related. Produced in P3 (graph/hierarchy build); the resolver
	// already routes it through relative-style resolution.
	FrontmatterRelated
	// External is a link to an off-corpus resource (http/https/mailto/etc.).
	External
)

// String returns the canonical name of the link type.
func (t LinkType) String() string {
	switch t {
	case RelativeLink:
		return "relative-link"
	case Wikilink:
		return "wikilink"
	case Anchor:
		return "anchor"
	case ImageEmbed:
		return "image-embed"
	case Transclusion:
		return "transclusion"
	case FrontmatterRelated:
		return "frontmatter-related"
	case External:
		return "external"
	default:
		return "unknown"
	}
}

// Valid reports whether t is a defined LinkType.
func (t LinkType) Valid() bool {
	return t >= RelativeLink && t <= External
}

// LinkHealth classifies the resolution outcome of a reference.
type LinkHealth int

const (
	// Unresolved is the zero value: the reference has not been resolved yet.
	Unresolved LinkHealth = iota
	// Valid means the target exists and (if applicable) the anchor exists.
	Valid
	// Broken means the target document does not exist in the corpus.
	Broken
	// BrokenAnchor means the target document exists but the fragment does not.
	BrokenAnchor
	// NonNote means the target resolved to a non-markdown asset (e.g. image).
	NonNote
	// Ambiguous means the raw target matched more than one candidate document.
	Ambiguous
	// HealthExternal means the reference points off-corpus (not checked unless
	// --check-external is enabled).
	HealthExternal
	// Ignored means the reference was deliberately excluded from analysis.
	Ignored
)

// String returns the canonical name of the health classification.
func (h LinkHealth) String() string {
	switch h {
	case Unresolved:
		return "unresolved"
	case Valid:
		return "valid"
	case Broken:
		return "broken"
	case BrokenAnchor:
		return "broken-anchor"
	case NonNote:
		return "non-note"
	case Ambiguous:
		return "ambiguous"
	case HealthExternal:
		return "external"
	case Ignored:
		return "ignored"
	default:
		return "unknown"
	}
}

// Valid reports whether h is a defined LinkHealth.
func (h LinkHealth) Valid() bool {
	return h >= Unresolved && h <= Ignored
}

// TargetKind tags the resolved target of a reference.
type TargetKind int

const (
	// TargetNone is the zero value: no target resolved.
	TargetNone TargetKind = iota
	// TargetDocument means the reference resolved to a document.
	TargetDocument
	// TargetSection means the reference resolved to a section within a document.
	TargetSection
	// TargetAsset means the reference resolved to a non-markdown asset.
	TargetAsset
	// TargetExternal means the reference points to an off-corpus resource.
	TargetExternal
	// TargetDirectory means the reference resolved to a directory in the corpus
	// (a folder containing markdown). See ADR 0008. The ResolvedTarget then
	// carries Directory (the folder path), DocumentID (the directory's index doc,
	// if any), and Children (the markdown docs directly in the folder).
	TargetDirectory
)

// String returns the canonical name of the target kind.
func (k TargetKind) String() string {
	switch k {
	case TargetNone:
		return "none"
	case TargetDocument:
		return "document"
	case TargetSection:
		return "section"
	case TargetAsset:
		return "asset"
	case TargetExternal:
		return "external"
	case TargetDirectory:
		return "directory"
	default:
		return "unknown"
	}
}

// Valid reports whether k is a defined TargetKind.
func (k TargetKind) Valid() bool {
	return k >= TargetNone && k <= TargetDirectory
}

// ResolutionPolicy selects how a raw target string is mapped to a DocumentID.
type ResolutionPolicy int

const (
	// Exact requires the raw target to equal a DocumentID after cleaning.
	Exact ResolutionPolicy = iota
	// LongestSuffix matches the candidate DocumentID with the longest matching
	// path suffix. This is the default (see ADR 0001).
	LongestSuffix
	// Basename matches on basename alone (least precise; opt-in).
	Basename
)

// DefaultResolutionPolicy is the policy used when none is configured.
const DefaultResolutionPolicy = LongestSuffix

// String returns the canonical name of the policy.
func (p ResolutionPolicy) String() string {
	switch p {
	case Exact:
		return "exact"
	case LongestSuffix:
		return "longest-suffix"
	case Basename:
		return "basename"
	default:
		return "unknown"
	}
}

// Valid reports whether p is a defined ResolutionPolicy.
func (p ResolutionPolicy) Valid() bool {
	return p >= Exact && p <= Basename
}

// RawReference is a reference edge as extracted from a document, before
// resolution. It captures everything the resolver needs and nothing it
// computes.
type RawReference struct {
	// Origin is the document the reference was found in.
	Origin identity.DocumentID
	// RawTarget is the link target text as written (e.g. "../other.md").
	RawTarget string
	// Fragment is the anchor portion without the leading '#', if any.
	Fragment string
	// Type is the syntactic classification of the link.
	Type LinkType
	// Line is the 1-based source line the reference appears on.
	Line int
}

// ResolvedTarget is the (tagged-union) outcome of resolving a RawReference.
// The valid fields depend on Kind.
type ResolvedTarget struct {
	// Kind tags which fields are meaningful.
	Kind TargetKind
	// DocumentID is set when Kind is TargetDocument, TargetSection or
	// TargetAsset. For TargetDirectory it is the directory's index document
	// (README.md / index.md) when one exists, else empty (ADR 0008).
	DocumentID identity.DocumentID
	// Anchor is the resolved section slug, set when Kind is TargetSection.
	Anchor string
	// Directory is the cleaned directory path, set only when Kind is
	// TargetDirectory (ADR 0008).
	Directory string
	// Children is the sorted set of markdown documents located DIRECTLY in the
	// directory (one level — no recursion), set only when Kind is
	// TargetDirectory. The index document, if any, is included. These are the
	// docs a directory link makes reachable under the default policy (ADR 0008).
	Children []identity.DocumentID
}

// Reference is a fully classified edge: the raw edge, its resolved target, and
// its health. Built once by the Resolver (see resolver.go) and treated as
// immutable.
type Reference struct {
	RawReference
	Target ResolvedTarget
	Health LinkHealth
	// Candidates is populated only when Health is Ambiguous: the set of
	// documents the target matched, sorted by DocumentID for deterministic
	// reporting.
	Candidates []identity.DocumentID
}
