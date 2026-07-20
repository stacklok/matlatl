// Package corpus is the pure-domain model of a scanned markdown repository: the
// documents, their front matter and section trees, and the in-memory Corpus
// that holds them along with the indices (HeadingInventory, AliasTable) that
// downstream resolution and analysis read from.
//
// This package depends only on the standard library and the sibling identity
// and reference packages (it imports nothing from application, infrastructure,
// cobra, or goldmark). See ADR 0004.
package corpus

import (
	"time"

	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// DocumentID is the canonical document identity (ADR 0001). It is re-exported
// from the identity package as a convenience alias so existing corpus call
// sites keep working; the validating constructor lives in identity.
type DocumentID = identity.DocumentID

// FrontMatter holds the typed YAML/TOML front-matter fields matlatl
// understands, plus any unrecognized keys in Extra. A zero FrontMatter is a
// valid "no front matter" value.
type FrontMatter struct {
	Title       string
	Description string
	Tags        []string
	Aliases     []string
	// Name is an alternate single-name alias for the document (common in note
	// systems). It is indexed into the AliasTable alongside Aliases so a wikilink
	// `[[name]]` resolves to this document (ADR 0001).
	Name    string
	Parent  string
	Related []string
	Status  string
	// Date is kept as a string in the skeleton; typed-date parsing is deferred.
	Date string
	// Extra holds front-matter keys not modeled above, preserved verbatim.
	Extra map[string]any
}

// Section is a heading-scoped node of a document. Sections form a tree rooted
// at the synthetic document root (Level 0). It is a graph vertex in later
// phases (ADR 0004). This is a pure data type; slug computation lives in the
// infrastructure parser (ADR 0006).
//
// Concurrency: the section tree (including the Parent back-pointers) is built by
// a single goroutine in the parser and is never mutated after the Document is
// handed to the corpus. It is therefore safe to hand off and read concurrently;
// callers must not mutate it post-construction.
type Section struct {
	// Level is the heading level (1-6); the synthetic root uses 0.
	Level int
	// Text is the rendered heading text.
	Text string
	// Slug is the canonical anchor slug for the heading (ADR 0006).
	Slug string
	// Parent is the enclosing section, or nil for the root. It is a back-pointer
	// set once during single-goroutine construction (see the type note above).
	Parent *Section
	// Children are the directly nested sections, in document order.
	Children []*Section
	// Start and End are the byte offsets of the section's span in the source.
	Start int
	End   int
	// StartLine and EndLine are the 1-based source line span of the section
	// (heading line through the last line it encloses). Used to attribute a
	// reference (which carries a line number) to its containing section when
	// building the graph (ADR 0007). 0 means unset.
	StartLine int
	EndLine   int
}

// Document is one parsed markdown file as a pure-domain value: its identity,
// front matter, section tree, the raw reference edges extracted from it, and
// the file's modification time. Built once by the parser and treated as
// immutable thereafter.
type Document struct {
	// ID is the canonical identity (ADR 0001).
	ID identity.DocumentID
	// FrontMatter holds the document's parsed front matter.
	FrontMatter FrontMatter
	// Root is the synthetic root of the section tree (Level 0). May be nil for
	// a document with no headings.
	Root *Section
	// RawReferences are the outbound link edges extracted from the document,
	// before resolution.
	RawReferences []reference.RawReference
	// ModTime is the file's last-modified time.
	ModTime time.Time
	// FrontMatterPresent reports whether the source began with a frontmatter fence
	// block (`---`/`+++`) at all — independent of whether it parsed. It is pure
	// data set by the parser, used by the OKF conformance mode (ADR 0023) to tell
	// an ABSENT frontmatter block from a present-but-unparseable one. A document
	// whose oversized frontmatter was stripped by the size guard (ADR 0003) is
	// FrontMatterPresent=true, FrontMatterParsed=false.
	FrontMatterPresent bool
	// FrontMatterParsed reports whether a present frontmatter block decoded
	// successfully into FrontMatter. It is false when there was no block at all
	// (FrontMatterPresent=false), when the YAML/TOML failed to decode, or when the
	// oversized-frontmatter guard stripped the block before decoding. Pure data
	// set by the parser (ADR 0023).
	FrontMatterParsed bool
}

// Title returns the document's display title with the documented fallbacks
// (ADR 0016): front-matter title → first heading text → the DocumentID string.
// This is the single source of truth for a document's title; the emit-layer
// presentation helper and the information-scent analysis both go through it so
// they cannot drift.
func (d *Document) Title() string {
	if d == nil {
		return ""
	}
	if t := d.FrontMatter.Title; t != "" {
		return t
	}
	if h := d.FirstHeadingText(); h != "" {
		return h
	}
	return d.ID.String()
}

// FirstHeadingText returns the text of the first (document-order) heading with
// non-empty text, or "" if the document has no headings.
func (d *Document) FirstHeadingText() string {
	if d == nil || d.Root == nil {
		return ""
	}
	var found string
	var walk func(s *Section) bool
	walk = func(s *Section) bool {
		for _, child := range s.Children {
			if child.Text != "" {
				found = child.Text
				return true
			}
			if walk(child) {
				return true
			}
		}
		return false
	}
	walk(d.Root)
	return found
}

// HeadingTextBySlug returns the heading text of the section whose canonical
// anchor slug (ADR 0006) equals slug, walking the section tree in document
// order. Slugs are unique per document (duplicates are suffixed per ADR 0006),
// so the first match wins. The information-scent analysis (ADR 0016) uses it to
// score an anchored link against the heading it actually points at.
func (d *Document) HeadingTextBySlug(slug string) (string, bool) {
	if d == nil || d.Root == nil || slug == "" {
		return "", false
	}
	var found string
	var ok bool
	var walk func(s *Section) bool
	walk = func(s *Section) bool {
		for _, child := range s.Children {
			if child.Slug == slug {
				found, ok = child.Text, true
				return true
			}
			if walk(child) {
				return true
			}
		}
		return false
	}
	walk(d.Root)
	return found, ok
}

// HeadingTexts returns every non-empty heading text in the document, in
// document order (ADR 0016): the information-scent analysis scores a
// document-targeted anchor against each of these (plus the title) and takes
// the best match.
func (d *Document) HeadingTexts() []string {
	if d == nil || d.Root == nil {
		return nil
	}
	var out []string
	var walk func(s *Section)
	walk = func(s *Section) {
		for _, child := range s.Children {
			if child.Text != "" {
				out = append(out, child.Text)
			}
			walk(child)
		}
	}
	walk(d.Root)
	return out
}
