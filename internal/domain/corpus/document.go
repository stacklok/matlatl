// Package corpus is the pure-domain model of a scanned markdown repository: the
// documents, their front matter and section trees, and the in-memory Corpus
// that holds them along with the indices (HeadingInventory, AliasTable) that
// downstream resolution and analysis read from.
//
// This package depends only on the standard library and the sibling reference
// package (it imports nothing from application, infrastructure, cobra, or
// goldmark). See ADR 0004.
package corpus

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/stacklok/doctopus/internal/domain/reference"
)

// DocumentID is a document's identity: its canonical repository-relative path,
// cleaned and slash-separated, relative to the scan root (see ADR 0001). It is
// never a basename — duplicate basenames in different directories are distinct
// identities. The underlying type is string and matches the string identities
// carried in the reference package.
type DocumentID string

// NewDocumentID derives a canonical DocumentID for path, interpreted relative
// to root. Both root and path may be absolute or relative; the result is always
// a cleaned, forward-slash, root-relative path. It returns an error if path
// escapes root (e.g. via ".." or an absolute path outside root), enforcing the
// root-containment boundary of ADR 0003 at the identity layer.
func NewDocumentID(root, p string) (DocumentID, error) {
	if p == "" {
		return "", fmt.Errorf("corpus: empty document path")
	}
	// Normalize to the OS separator for the relative computation, then convert
	// the result to slashes for the canonical identity.
	cleanRoot := filepath.Clean(root)
	if cleanRoot == "" {
		cleanRoot = "."
	}

	var abs string
	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		abs = filepath.Join(cleanRoot, p)
	}

	rel, err := filepath.Rel(cleanRoot, abs)
	if err != nil {
		return "", fmt.Errorf("corpus: cannot relativize %q against root %q: %w", p, root, err)
	}
	// filepath.Rel already returns a cleaned path; ToSlash only swaps
	// separators, so no further path.Clean is needed here.
	rel = filepath.ToSlash(rel)

	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("corpus: path %q escapes root %q", p, root)
	}
	if rel == "." || rel == "" {
		return "", fmt.Errorf("corpus: path %q resolves to the root itself", p)
	}

	return DocumentID(rel), nil
}

// String returns the identifier as a plain string.
func (id DocumentID) String() string { return string(id) }

// Dir returns the slash-separated directory portion of the identity, or "." for
// a top-level document.
func (id DocumentID) Dir() string { return path.Dir(string(id)) }

// Base returns the final path element of the identity. It is a resolution hint
// only and is never used as identity (see ADR 0001).
func (id DocumentID) Base() string { return path.Base(string(id)) }

// FrontMatter holds the typed YAML/TOML front-matter fields doctopus
// understands, plus any unrecognized keys in Extra. A zero FrontMatter is a
// valid "no front matter" value.
type FrontMatter struct {
	Title       string
	Description string
	Tags        []string
	Aliases     []string
	Parent      string
	Related     []string
	Status      string
	// Date is kept as a string in the skeleton; typed-date parsing is deferred.
	Date string
	// Extra holds front-matter keys not modeled above, preserved verbatim.
	Extra map[string]any
}

// Section is a heading-scoped node of a document. Sections form a tree rooted
// at the synthetic document root (Level 0). It is a graph vertex in later
// phases (ADR 0004). This is a pure data type; slug computation lives in the
// infrastructure parser (ADR 0006).
type Section struct {
	// Level is the heading level (1-6); the synthetic root uses 0.
	Level int
	// Text is the rendered heading text.
	Text string
	// Slug is the canonical anchor slug for the heading (ADR 0006).
	Slug string
	// Parent is the enclosing section, or nil for the root.
	Parent *Section
	// Children are the directly nested sections, in document order.
	Children []*Section
	// Start and End are the byte offsets of the section's span in the source.
	Start int
	End   int
}

// Document is one parsed markdown file as a pure-domain value: its identity,
// front matter, section tree, the raw reference edges extracted from it, and
// the file's modification time. Built once by the parser and treated as
// immutable thereafter.
type Document struct {
	// ID is the canonical identity (ADR 0001).
	ID DocumentID
	// FrontMatter holds the document's parsed front matter.
	FrontMatter FrontMatter
	// Root is the synthetic root of the section tree (Level 0). May be nil for
	// a document with no headings in the skeleton.
	Root *Section
	// RawReferences are the outbound link edges extracted from the document,
	// before resolution.
	RawReferences []reference.RawReference
	// ModTime is the file's last-modified time.
	ModTime time.Time
}
