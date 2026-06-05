// Package identity defines DocumentID — a document's canonical, repository-
// relative identity (ADR 0001) — and its validating constructor. It is the
// lowest leaf of the domain: it depends only on the standard library so that
// every other domain package (corpus, reference, graphmodel, analysis) can
// import it without creating cycles. Keeping identity here lets the reference
// package carry typed DocumentIDs even though corpus depends on reference.
package identity

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// DocumentID is a document's identity: its canonical repository-relative path,
// cleaned and slash-separated, relative to the scan root (see ADR 0001). It is
// never a basename — duplicate basenames in different directories are distinct
// identities.
type DocumentID string

// NewDocumentID derives a canonical DocumentID for path p, interpreted relative
// to root. Both root and p may be absolute or relative; the result is always a
// cleaned, forward-slash, root-relative path. It returns an error if p escapes
// root (e.g. via ".." or an absolute path outside root), enforcing the
// root-containment boundary of ADR 0003 at the identity layer.
func NewDocumentID(root, p string) (DocumentID, error) {
	if p == "" {
		return "", fmt.Errorf("identity: empty document path")
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
		return "", fmt.Errorf("identity: cannot relativize %q against root %q: %w", p, root, err)
	}
	// filepath.Rel already returns a cleaned path; ToSlash only swaps
	// separators, so no further path.Clean is needed here.
	rel = filepath.ToSlash(rel)

	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("identity: path %q escapes root %q", p, root)
	}
	if rel == "." || rel == "" {
		return "", fmt.Errorf("identity: path %q resolves to the root itself", p)
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
