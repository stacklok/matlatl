// Package reference models the directed edges of the corpus: links between
// documents (and sections), their type, their resolution target, and their
// health classification.
//
// This package is pure domain (standard library only). It is the lower layer in
// the domain: corpus depends on reference (a Document holds []RawReference), so
// reference must NOT import corpus — that would be an import cycle. The single
// authoritative DocumentID type (with its validating constructor and
// root-containment check) therefore lives in the corpus package; reference
// carries document identities as plain strings (the underlying type of
// corpus.DocumentID). Callers convert with string(id) when building edges and
// corpus.DocumentID(s) when consuming them. The resolver logic itself lands in
// a later phase; this file defines the type spine only.
package reference

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
	// parent/related.
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
	// ExternalHealth means the reference points off-corpus (not checked unless
	// --check-external is enabled).
	ExternalHealth
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
	case ExternalHealth:
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
	default:
		return "unknown"
	}
}

// Valid reports whether k is a defined TargetKind.
func (k TargetKind) Valid() bool {
	return k >= TargetNone && k <= TargetExternal
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
	// Origin is the document the reference was found in, as a DocumentID string
	// (see the package doc for why this is not a corpus.DocumentID).
	Origin string
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
	// TargetAsset. Carried as a string for the no-import-cycle reason described
	// in the package doc.
	DocumentID string
	// Anchor is the resolved section slug, set when Kind is TargetSection.
	Anchor string
}

// Reference is a fully classified edge: the raw edge, its resolved target, and
// its health. Built once during resolution and treated as immutable.
//
// TODO(P2): the LinkResolver that populates Target and Health lives in this
// package in a later phase.
type Reference struct {
	RawReference
	Target ResolvedTarget
	Health LinkHealth
}
