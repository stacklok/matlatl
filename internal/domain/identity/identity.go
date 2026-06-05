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
	"slices"
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

// MarkdownExts are the recognized markdown file extensions (lowercase, with
// leading dot). It is the single source of truth shared by the scanner
// (discovery) and the resolver (note vs. asset classification).
var MarkdownExts = []string{".md", ".markdown"}

// IsMarkdownPath reports whether p has a recognized markdown extension. The
// comparison is case-insensitive.
func IsMarkdownPath(p string) bool {
	lower := strings.ToLower(p)
	for _, ext := range MarkdownExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// directoryIndexBasenames are the conventional directory-index filenames
// (lowercase) recognized at any depth: README.md and index.md (ADR 0007). It is
// the single source of truth shared by root-set resolution and hierarchy
// building.
var directoryIndexBasenames = []string{"readme.md", "index.md"}

// IsDirectoryIndex reports whether base (a path basename) is a conventional
// directory-index file (README.md / index.md), matched case-insensitively. It
// is the ONE shared predicate used by both the root-set resolver and the
// hierarchy builder so the two never diverge on case handling (ADR 0007).
func IsDirectoryIndex(base string) bool {
	lower := strings.ToLower(base)
	for _, b := range directoryIndexBasenames {
		if lower == b {
			return true
		}
	}
	return false
}

// EscapesRoot reports whether a cleaned, slash-separated, root-relative path
// escapes its root — i.e. it is "..", begins with "../", or is empty/".". It is
// the single root-containment predicate shared across the codebase (ADR 0003);
// OS-separator callers convert with filepath.ToSlash first. Note: it expects an
// already-cleaned path (e.g. from path.Clean or filepath.Rel+ToSlash).
func EscapesRoot(slashRel string) bool {
	return slashRel == "" || slashRel == "." || slashRel == ".." || strings.HasPrefix(slashRel, "../")
}

// Contains joins relPath (a slash-or-OS-separated, root-relative path) under the
// absolute root absRoot, cleans it, and verifies the result stays within the
// root. It returns the cleaned OS-separator path and true when contained, or
// ("", false) when the joined path escapes the root (via "..", an absolute
// component, etc.). It is the single root-containment join used by every
// filesystem boundary in the codebase (the scanner's asset check, the llms-full
// body reader, the artifact writer) so they cannot diverge on the guard (ADR
// 0003). It is a pure path computation — it performs no I/O and (like the
// scanner's separate symlink resolution) does not itself resolve symlinks.
//
// An ABSOLUTE relPath is always rejected: filepath.Join(absRoot, "/etc/passwd")
// silently treats the absolute path as a relative component and would otherwise
// report it as contained. Callers pass root-relative paths; an absolute one is a
// containment violation by definition.
func Contains(absRoot, relPath string) (string, bool) {
	if filepath.IsAbs(relPath) {
		return "", false
	}
	full := filepath.Join(absRoot, filepath.FromSlash(relPath))
	rel, err := filepath.Rel(absRoot, full)
	if err != nil || EscapesRoot(filepath.ToSlash(rel)) {
		return "", false
	}
	return full, true
}

// IDStrings turns a slice of DocumentIDs into a non-nil, sorted slice of plain
// strings. It is the ONE shared helper used by every emitter that projects an ID
// list into JSON/MCP output (graph.json, the MCP server) so the two cannot drift
// on nil-vs-empty or sort order. The result is always non-nil (an empty input
// yields an empty, allocated slice) so JSON renders `[]` rather than `null`.
func IDStrings(ids []DocumentID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	slices.Sort(out)
	return out
}

// String returns the identifier as a plain string.
func (id DocumentID) String() string { return string(id) }

// Dir returns the slash-separated directory portion of the identity, or "." for
// a top-level document.
func (id DocumentID) Dir() string { return path.Dir(string(id)) }

// Base returns the final path element of the identity. It is a resolution hint
// only and is never used as identity (see ADR 0001).
func (id DocumentID) Base() string { return path.Base(string(id)) }
