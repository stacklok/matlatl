package llmstxt

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
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
//
// Two independent guards apply to every read (mirroring the scanner):
//   - Containment (above): the read can only touch a path under root.
//   - Size cap (below): a stat-before-read rejects a file larger than maxBytes.
//     The scanner caps file size at scan time, but emit re-opens the file later,
//     reopening a TOCTOU window (ADR 0003: an attacker who grows the file between
//     scan and emit could otherwise OOM us via an uncapped os.ReadFile). The
//     stat-before-read mirrors fsscanner's Lstat+size check so growth past the
//     cap is rejected here too.
type BodyReader struct {
	root     string // absolute scan root; empty disables reads
	maxBytes int64  // per-file size cap; <=0 means use the scanner default
}

// NewBodyReader returns a reader rooted at root using the scanner's default file
// size cap. A relative root is made absolute; if that fails the reader is inert
// (reads return an error).
func NewBodyReader(root string) *BodyReader {
	return NewBodyReaderWithCap(root, fsscanner.DefaultMaxFileSizeBytes)
}

// NewBodyReaderWithCap is NewBodyReader with an explicit per-file size cap (a
// non-positive cap falls back to the scanner default). It lets the emit pipeline
// reuse the same cap the scanner was configured with.
func NewBodyReaderWithCap(root string, maxBytes int64) *BodyReader {
	if maxBytes <= 0 {
		maxBytes = fsscanner.DefaultMaxFileSizeBytes
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return &BodyReader{maxBytes: maxBytes}
	}
	return &BodyReader{root: abs, maxBytes: maxBytes}
}

// Read returns the raw bytes of the document identified by id, confined under
// the root and capped at the reader's size limit. It returns an error if the
// reader is inert, the path escapes root, the file exceeds the cap, or the file
// cannot be read.
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
	// Size guard (ADR 0003): stat before read and refuse a file that has grown
	// past the cap since the scanner validated it (the accepted scan→emit TOCTOU
	// window), so an uncapped ReadFile cannot OOM us on hostile growth.
	fi, err := os.Stat(full) //nolint:gosec // path is root-confined above (ADR 0003)
	if err != nil {
		return nil, fmt.Errorf("llmstxt: stat %q: %w", id, err)
	}
	if fi.Size() > r.maxBytes {
		return nil, fmt.Errorf("llmstxt: document %q is %d bytes, exceeds cap %d", id, fi.Size(), r.maxBytes)
	}
	b, err := os.ReadFile(full) //nolint:gosec // path is root-confined and size-capped above (ADR 0003)
	if err != nil {
		return nil, fmt.Errorf("llmstxt: read %q: %w", id, err)
	}
	return b, nil
}
