package correctness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"slices"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
)

type fixtureEntry struct {
	path    string
	content []byte
}

type fixtureSnapshot struct {
	entries []fixtureEntry
	hash    string
}

func snapshotFixture(ctx context.Context, evalRoot, rel string, maxFiles, maxBytes int) (*fixtureSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := evalfs.Path(evalRoot, rel)
	if err != nil {
		return nil, err
	}
	files, err := evalfs.Files(root)
	if err != nil {
		return nil, err
	}
	if len(files) > maxFiles {
		return nil, fmt.Errorf("cumulative fixture file count exceeds %d", maxSuiteFixtureFiles)
	}
	snapshot := &fixtureSnapshot{entries: make([]fixtureEntry, 0, len(files))}
	hash := sha256.New()
	bytesRead := 0
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		content, err := evalfs.Read(root, path)
		if err != nil {
			return nil, err
		}
		bytesRead += len(content)
		if bytesRead > maxBytes {
			return nil, fmt.Errorf("cumulative fixture bytes exceed %d", maxSuiteFixtureBytes)
		}
		content = slices.Clone(content)
		snapshot.entries = append(snapshot.entries, fixtureEntry{path: path, content: content})
		_, _ = fmt.Fprintf(hash, "%d:%s:%d:", len(path), path, len(content))
		_, _ = hash.Write(content)
	}
	snapshot.hash = hex.EncodeToString(hash.Sum(nil))
	return snapshot, nil
}

func (s *fixtureSnapshot) materialize(ctx context.Context, prefix string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", err
	}
	for _, entry := range s.entries {
		if err := ctx.Err(); err != nil {
			_ = os.RemoveAll(root)
			return "", err
		}
		if err := evalfs.WriteExclusive(root, entry.path, entry.content); err != nil {
			_ = os.RemoveAll(root)
			return "", err
		}
	}
	return root, nil
}

func (s *fixtureSnapshot) fileHash(rel string) (string, error) {
	for _, entry := range s.entries {
		if entry.path == rel {
			digest := sha256.Sum256(entry.content)
			return hex.EncodeToString(digest[:]), nil
		}
	}
	return "", fmt.Errorf("snapshot file not found: %q", rel)
}
