package nimbus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
)

// AttemptRecord records retry identity and exact workspace state.
type AttemptRecord struct {
	SchemaVersion int    `json:"schemaVersion"`
	AttemptID     string `json:"attemptId"`
	RetryParent   string `json:"retryParent,omitempty"`
	WorkspaceHash string `json:"workspaceTreeSha256"`
	Exposed       bool   `json:"exposed"`
	Status        string `json:"status"`
}

// WriteAttempt validates and exclusively persists an attempt record.
func WriteAttempt(root string, record AttemptRecord) error {
	if record.SchemaVersion != 1 || !validID(record.AttemptID) || record.WorkspaceHash == "" {
		return errorsNew("invalid Nimbus attempt record")
	}
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return evalfs.WriteExclusive(root, "attempts/"+record.AttemptID+".json", b)
}

// ValidateRetry enforces the pre-exposure retry contract.
func ValidateRetry(parent, child AttemptRecord) error {
	if child.RetryParent != parent.AttemptID {
		return errorsNew("retry parent is not registered")
	}
	if parent.Exposed {
		return errorsNew("post-exposure outcomes cannot retry")
	}
	if parent.Status != "environment-failure" {
		return errorsNew("only pre-exposure environment failure may retry")
	}
	return nil
}

// Materialize creates the exact mutated workspace for a task.
func Materialize(s *Suite, taskID, destination string) (string, error) {
	m, err := mutationFor(s, taskID)
	if err != nil {
		return "", err
	}
	baseTree, err := evalfs.TreeHash(s.Repository)
	if err != nil {
		return "", err
	}
	baseFile, err := evalfs.FileHash(s.Repository, m.Path)
	if err != nil {
		return "", err
	}
	if baseTree != m.BaseCorpusTreeHash || baseFile != m.BaseFileHash {
		return "", fmt.Errorf("task %s base does not match mutation manifest", taskID)
	}
	if err := os.Mkdir(destination, 0o750); err != nil {
		return "", err
	}
	dst, err := evalfs.Root(destination)
	if err != nil {
		return "", err
	}
	files, err := evalfs.Files(s.Repository)
	if err != nil {
		return "", err
	}
	for _, rel := range files {
		b, err := evalfs.Read(s.Repository, rel)
		if err != nil {
			return "", err
		}
		if err := evalfs.WriteExclusive(dst, rel, b); err != nil {
			return "", err
		}
		src, _ := evalfs.Path(s.Repository, rel)
		target, _ := evalfs.Path(dst, rel)
		info, _ := os.Stat(src)
		if err := os.Chmod(target, info.Mode().Perm()); err != nil {
			return "", err
		}
	}
	if err := replaceOne(dst, m.Path, m.Old, m.New); err != nil {
		return "", err
	}
	mutatedTree, err := evalfs.TreeHash(dst)
	if err != nil {
		return "", err
	}
	mutatedFile, err := evalfs.FileHash(dst, m.Path)
	if err != nil {
		return "", err
	}
	if mutatedTree != m.MutatedCorpusHash || mutatedFile != m.MutatedFileHash {
		return "", fmt.Errorf("task %s materialized state does not match mutation manifest", taskID)
	}
	return dst, nil
}

// Reset restores an untouched materialized workspace to its exact base state.
func Reset(s *Suite, taskID, workspace string) error {
	m, err := mutationFor(s, taskID)
	if err != nil {
		return err
	}
	root, err := evalfs.Root(workspace)
	if err != nil {
		return err
	}
	before, err := evalfs.TreeHash(root)
	if err != nil {
		return err
	}
	fileBefore, err := evalfs.FileHash(root, m.Path)
	if err != nil {
		return err
	}
	if before != m.MutatedCorpusHash || fileBefore != m.MutatedFileHash {
		return fmt.Errorf("task %s reset refused: workspace is dirty or has extra files", taskID)
	}
	if err := replaceOne(root, m.Path, m.New, m.Old); err != nil {
		return err
	}
	after, err := evalfs.TreeHash(root)
	if err != nil {
		return err
	}
	fileAfter, err := evalfs.FileHash(root, m.Path)
	if err != nil {
		return err
	}
	if after != m.BaseCorpusTreeHash || fileAfter != m.BaseFileHash {
		return fmt.Errorf("task %s reset failed to restore exact base", taskID)
	}
	return nil
}

// ApplyCase applies a private verifier patch case to a workspace.
func ApplyCase(workspace string, c PatchCase) error {
	root, err := evalfs.Root(workspace)
	if err != nil {
		return err
	}
	for _, r := range c.Replacements {
		if err := replaceOne(root, r.Path, r.Old, r.New); err != nil {
			return fmt.Errorf("case %s: %w", c.Name, err)
		}
	}
	return nil
}

func replaceOne(root, rel, old, new string) error {
	b, err := evalfs.Read(root, rel)
	if err != nil {
		return err
	}
	if strings.Count(string(b), old) != 1 {
		return fmt.Errorf("%s: replacement occurrence count is not one", rel)
	}
	path, err := evalfs.Path(root, rel)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	next := strings.Replace(string(b), old, new, 1)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nimbus-replace-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := tmp.WriteString(next); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func mutationFor(s *Suite, id string) (Mutation, error) {
	for _, m := range s.Mutations {
		if m.TaskID == id {
			return m, nil
		}
	}
	return Mutation{}, fmt.Errorf("unknown Nimbus task %q", id)
}
func patchesFor(s *Suite, id string) (PatchSet, error) {
	for _, p := range s.Patches {
		if p.TaskID == id {
			return p, nil
		}
	}
	return PatchSet{}, fmt.Errorf("unknown Nimbus task %q", id)
}
