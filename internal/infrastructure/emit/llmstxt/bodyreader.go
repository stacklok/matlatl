package llmstxt

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/stacklok/doctopus/internal/domain/identity"
)

// BodyReader reads a document's raw markdown bytes from disk, confined to a scan
// root. The domain Document does not retain the raw body (it keeps only the
// parsed section tree + references), and llms-full.txt needs the cleaned source
// text, so it is read here in the infrastructure layer — never in the domain.
//
// Security (ADR 0003): a DocumentID is a cleaned, slash, root-relative path, but
// the reader independently re-verifies containment via identity.Contains (the
// same shared root-containment join the scanner/writer use) so a hostile ID can
// never read outside root. Contains rejects both "../"-style escapes and an
// absolute ID, so neither "../../etc/passwd" nor "/etc/passwd" can read off
// disk. A reader with an empty root reads nothing.
type BodyReader struct {
	root string // absolute scan root; empty disables reads
}

// NewBodyReader returns a reader rooted at root. A relative root is made
// absolute; if that fails the reader is inert (reads return an error).
func NewBodyReader(root string) *BodyReader {
	abs, err := filepath.Abs(root)
	if err != nil {
		return &BodyReader{}
	}
	return &BodyReader{root: abs}
}

// Read returns the raw bytes of the document identified by id, confined under
// the root. It returns an error if the reader is inert, the path escapes root,
// or the file cannot be read.
func (r *BodyReader) Read(id identity.DocumentID) ([]byte, error) {
	if r.root == "" {
		return nil, fmt.Errorf("llmstxt: body reader has no root")
	}
	// Defense in depth: re-verify the joined path stays within root via the
	// shared containment guard (the same one the scanner/writer use). It rejects
	// both "../" escapes and an absolute id.
	full, ok := identity.Contains(r.root, id.String())
	if !ok {
		return nil, fmt.Errorf("llmstxt: document %q escapes the scan root", id)
	}
	b, err := os.ReadFile(full) //nolint:gosec // path is root-confined above (ADR 0003)
	if err != nil {
		return nil, fmt.Errorf("llmstxt: read %q: %w", id, err)
	}
	return b, nil
}
