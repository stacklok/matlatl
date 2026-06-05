package application

import (
	"context"

	"github.com/stacklok/doctopus/internal/domain/corpus"
)

// The interfaces in this file are the pipeline's real test seams (ADR 0004).
// They are the only interfaces introduced for collaborators: filesystem
// scanning, document parsing, and artifact writing all have a single production
// implementation in internal/infrastructure but are abstracted here so the
// pipeline can be driven with fakes in tests. We deliberately do NOT introduce
// interfaces for collaborators that lack a real seam.

// ScannedFile is a candidate file discovered by a FileScanner, carrying the
// information the parser needs without performing any parsing itself. Fuller
// metadata (size, modtime, symlink status) is added when the scanner lands.
type ScannedFile struct {
	// Path is the absolute filesystem path of the file.
	Path string
	// ID is the canonical document identity derived from the scan root.
	ID corpus.DocumentID
}

// FileScanner walks a root and returns the markdown files to parse, enforcing
// the security boundary and ignore rules (ADR 0003). Production implementation:
// internal/infrastructure/fsscanner.
type FileScanner interface {
	Scan(ctx context.Context, root string) ([]ScannedFile, error)
}

// DocumentParser turns a scanned file into a pure-domain Document (front
// matter, section tree, raw references). Production implementation:
// internal/infrastructure/mdparser (the only package allowed to import
// goldmark, ADR 0002).
type DocumentParser interface {
	Parse(ctx context.Context, file ScannedFile) (*corpus.Document, error)
}

// Artifact is a single named output to be written to the output directory.
type Artifact struct {
	// Name is the artifact filename relative to the output directory.
	Name string
	// Content is the rendered artifact bytes.
	Content []byte
}

// ArtifactWriter persists rendered artifacts, sanitizing every path to stay
// under the output directory (reverse zip-slip, ADR 0003). Production
// implementation: internal/infrastructure/emit.
type ArtifactWriter interface {
	Write(ctx context.Context, artifacts []Artifact) error
}
