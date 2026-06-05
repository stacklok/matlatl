package emit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/domain/identity"
)

// FSWriter implements application.ArtifactWriter by writing artifacts under an
// output directory. Every artifact name is sanitized and verified to stay under
// the directory (reverse zip-slip, ADR 0003): a name that escapes is rejected
// with an error, never written.
type FSWriter struct {
	outDir string
}

// NewFSWriter returns a writer rooted at outDir.
func NewFSWriter(outDir string) *FSWriter {
	return &FSWriter{outDir: outDir}
}

// compile-time assertion.
var _ application.ArtifactWriter = (*FSWriter)(nil)

// Write persists each artifact under the output directory, creating it if
// needed. It returns on the first error.
func (w *FSWriter) Write(_ context.Context, artifacts []application.Artifact) error {
	if w.outDir == "" {
		return fmt.Errorf("emit: empty output directory")
	}
	absOut, err := filepath.Abs(w.outDir)
	if err != nil {
		return fmt.Errorf("emit: resolve out dir %q: %w", w.outDir, err)
	}
	if mkErr := os.MkdirAll(absOut, 0o750); mkErr != nil {
		return fmt.Errorf("emit: create out dir %q: %w", absOut, mkErr)
	}

	for _, art := range artifacts {
		dest, perr := safeJoin(absOut, art.Name)
		if perr != nil {
			return perr
		}
		if mkErr := os.MkdirAll(filepath.Dir(dest), 0o750); mkErr != nil {
			return fmt.Errorf("emit: create dir for %q: %w", art.Name, mkErr)
		}
		if wErr := os.WriteFile(dest, art.Content, 0o644); wErr != nil { //nolint:gosec // 0644 artifacts are intended to be readable
			return fmt.Errorf("emit: write %q: %w", art.Name, wErr)
		}
	}
	return nil
}

// safeJoin cleans name and joins it under absOut, verifying the result stays
// within absOut (reverse zip-slip guard, ADR 0003).
func safeJoin(absOut, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("emit: artifact name %q must be relative", name)
	}
	dest := filepath.Join(absOut, clean)
	rel, err := filepath.Rel(absOut, dest)
	if err != nil {
		return "", fmt.Errorf("emit: bad artifact path %q: %w", name, err)
	}
	if identity.EscapesRoot(filepath.ToSlash(rel)) {
		return "", fmt.Errorf("emit: artifact name %q escapes the output directory", name)
	}
	return dest, nil
}
