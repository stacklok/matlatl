package nimbus

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
)

// DocRef identifies a required document section.
type DocRef struct {
	Path   string `json:"path"`
	Anchor string `json:"anchor"`
}

// Classification records the frozen task-stratum evidence.
type Classification struct {
	Rule                string   `json:"rule"`
	GrepLiteral         string   `json:"grepLiteral,omitempty"`
	GrepResultPaths     []string `json:"grepResultPaths,omitempty"`
	UnnamedTarget       string   `json:"unnamedTarget,omitempty"`
	FrozenAuthoredHops  int      `json:"frozenAuthoredHops,omitempty"`
	FrozenIntermediates []string `json:"frozenIntermediates,omitempty"`
}

// Task is one frozen Nimbus evaluation task.
type Task struct {
	SchemaVersion  int            `json:"schemaVersion"`
	ID             string         `json:"id"`
	Family         string         `json:"family"`
	PrimaryStratum string         `json:"primaryStratum"`
	Instruction    string         `json:"instruction"`
	Classification Classification `json:"classification"`
	RequiredDocs   []DocRef       `json:"requiredDocs"`
	NormalChecks   []string       `json:"normalChecks"`
	ReviewStatus   string         `json:"reviewStatus"`
	Disposable     bool           `json:"disposableQualificationOnly"`
}

// Mutation describes the deterministic corruption applied for a task.
type Mutation struct {
	SchemaVersion      int    `json:"schemaVersion"`
	TaskID             string `json:"taskId"`
	Path               string `json:"path"`
	Old                string `json:"old"`
	New                string `json:"new"`
	BaseCorpusTreeHash string `json:"baseCorpusTreeSha256"`
	BaseFileHash       string `json:"baseFileSha256"`
	MutatedCorpusHash  string `json:"mutatedCorpusTreeSha256"`
	MutatedFileHash    string `json:"mutatedFileSha256"`
	ExpectedLocalDelta int    `json:"expectedLocalDelta"`
}

// Replacement is one exact source replacement in a verifier case.
type Replacement struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

// PatchCase is a private verifier mutation and its expected verdict.
type PatchCase struct {
	Name         string        `json:"name"`
	ExpectPass   bool          `json:"expectPass"`
	Replacements []Replacement `json:"replacements"`
}

// PatchSet contains the private verifier cases for one task.
type PatchSet struct {
	SchemaVersion int         `json:"schemaVersion"`
	TaskID        string      `json:"taskId"`
	Cases         []PatchCase `json:"cases"`
}

// Suite is a validated Nimbus corpus, task, and verifier bundle.
type Suite struct {
	Root, Repository string
	Tasks            []Task
	Mutations        []Mutation
	Patches          []PatchSet
}

var strata = []string{"cross-document-synthesis", "grep-friendly-coding-control", "navigation-heavy-coding", "single-document-constraint"}

// Load reads and validates a Nimbus suite rooted at rootArg.
func Load(rootArg string) (*Suite, error) {
	root, err := evalfs.Root(rootArg)
	if err != nil {
		return nil, err
	}
	if err := securePrivateTree(root); err != nil {
		return nil, err
	}
	if err := validateSuiteBudget(root); err != nil {
		return nil, err
	}
	repo, err := evalfs.Path(root, "public/repository")
	if err != nil {
		return nil, err
	}
	tasksRoot, err := evalfs.Path(root, "tasks")
	if err != nil {
		return nil, err
	}
	files, err := evalfs.Files(tasksRoot)
	if err != nil {
		return nil, err
	}
	byDir := map[string]map[string][]byte{}
	for _, rel := range files {
		base := filepath.Base(rel)
		if base != "task.json" && base != "mutation.json" {
			return nil, fmt.Errorf("unexpected task file %q", rel)
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		if strings.Contains(dir, "/") || !validID(dir) {
			return nil, fmt.Errorf("unsafe task directory %q", dir)
		}
		b, err := evalfs.Read(tasksRoot, rel)
		if err != nil {
			return nil, err
		}
		if byDir[dir] == nil {
			byDir[dir] = map[string][]byte{}
		}
		byDir[dir][base] = b
	}
	ids := make([]string, 0, len(byDir))
	for id := range byDir {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if len(ids) != 4 {
		return nil, fmt.Errorf("require exactly four Nimbus tasks, got %d", len(ids))
	}
	s := &Suite{Root: root, Repository: repo}
	seenStrata := map[string]bool{}
	for _, id := range ids {
		pair := byDir[id]
		if len(pair) != 2 {
			return nil, fmt.Errorf("task %s requires task.json and mutation.json", id)
		}
		var task Task
		if err := decodeStrict(pair["task.json"], &task); err != nil {
			return nil, fmt.Errorf("%s/task.json: %w", id, err)
		}
		var mutation Mutation
		if err := decodeStrict(pair["mutation.json"], &mutation); err != nil {
			return nil, fmt.Errorf("%s/mutation.json: %w", id, err)
		}
		if err := validateTask(repo, id, task, mutation, seenStrata); err != nil {
			return nil, err
		}
		s.Tasks = append(s.Tasks, task)
		s.Mutations = append(s.Mutations, mutation)
		private, err := evalfs.Path(root, "private/"+id)
		if err != nil {
			return nil, err
		}
		b, err := evalfs.Read(private, "patches.json")
		if err != nil {
			return nil, err
		}
		var patches PatchSet
		if err := decodeStrict(b, &patches); err != nil {
			return nil, fmt.Errorf("%s patches: %w", id, err)
		}
		if err := validatePatches(id, patches); err != nil {
			return nil, err
		}
		if _, err := loadHiddenCases(s, id); err != nil {
			return nil, fmt.Errorf("%s hidden cases: %w", id, err)
		}
		s.Patches = append(s.Patches, patches)
	}
	if len(seenStrata) != len(strata) {
		return nil, errorsNew("primary strata are not mutually exhaustive")
	}
	return s, nil
}

func securePrivateTree(root string) (retErr error) {
	private, err := evalfs.Path(root, "private")
	if err != nil {
		return err
	}
	privateRoot, err := os.OpenRoot(private)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, privateRoot.Close()) }()
	return fs.WalkDir(privateRoot.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("private verifier tree contains symlink %q", name)
		}
		mode := os.FileMode(0o600)
		if entry.IsDir() {
			mode = 0o700
		} else if !entry.Type().IsRegular() {
			return fmt.Errorf("private verifier tree contains non-regular file %q", name)
		}
		if name == "." {
			return nil
		}
		return privateRoot.Chmod(name, mode)
	})
}

func validateSuiteBudget(root string) error {
	files, err := evalfs.Files(root)
	if err != nil {
		return err
	}
	if len(files) > 256 {
		return fmt.Errorf("nimbus suite exceeds 256 files")
	}
	total := 0
	for _, rel := range files {
		b, err := evalfs.Read(root, rel)
		if err != nil {
			return err
		}
		if len(b) > 4<<20-total {
			return fmt.Errorf("nimbus suite exceeds 4 MiB")
		}
		total += len(b)
	}
	return nil
}

func validateTask(repo, id string, t Task, m Mutation, seen map[string]bool) error {
	if t.SchemaVersion != 1 || m.SchemaVersion != 1 || t.ID != id || m.TaskID != id || !validID(t.Family) {
		return fmt.Errorf("task %s identity/schema invalid", id)
	}
	if !slices.Contains(strata, t.PrimaryStratum) || seen[t.PrimaryStratum] {
		return fmt.Errorf("task %s primary stratum invalid or duplicate", id)
	}
	seen[t.PrimaryStratum] = true
	if t.ReviewStatus != "pending" || !t.Disposable || len(t.RequiredDocs) == 0 || !slices.Equal(t.NormalChecks, []string{"go test ./..."}) {
		return fmt.Errorf("task %s calibration/review contract invalid", id)
	}
	if t.Classification.Rule != t.PrimaryStratum {
		return fmt.Errorf("task %s classification rule does not match primary stratum", id)
	}
	for _, doc := range t.RequiredDocs {
		b, err := evalfs.Read(repo, doc.Path)
		if err != nil {
			return fmt.Errorf("task %s required doc: %w", id, err)
		}
		heading := strings.ReplaceAll(strings.TrimPrefix(doc.Anchor, "#"), "-", " ")
		if !strings.Contains(strings.ToLower(string(b)), strings.ToLower(heading)) {
			return fmt.Errorf("task %s missing anchor %q", id, doc.Anchor)
		}
	}
	if err := validateClassification(repo, t); err != nil {
		return fmt.Errorf("task %s classification: %w", id, err)
	}
	baseTree, err := evalfs.TreeHash(repo)
	if err != nil {
		return err
	}
	b, err := evalfs.Read(repo, m.Path)
	if err != nil {
		return err
	}
	if m.Old == m.New || strings.Count(string(b), m.Old) != 1 {
		return fmt.Errorf("task %s mutation must replace exactly once", id)
	}
	if m.ExpectedLocalDelta != len(m.New)-len(m.Old) || m.BaseCorpusTreeHash != baseTree || m.BaseFileHash != SHA256(b) {
		return fmt.Errorf("task %s mutation base hashes/delta invalid", id)
	}
	mutated := []byte(strings.Replace(string(b), m.Old, m.New, 1))
	if m.MutatedFileHash != SHA256(mutated) {
		return fmt.Errorf("task %s mutated file hash invalid", id)
	}
	return nil
}

func validateClassification(repo string, t Task) error {
	switch t.PrimaryStratum {
	case "grep-friendly-coding-control":
		if t.Classification.GrepLiteral == "" || len(t.Classification.GrepResultPaths) == 0 {
			return errorsNew("literal query and results required")
		}
		var got []string
		files, _ := evalfs.Files(repo)
		for _, rel := range files {
			b, err := evalfs.Read(repo, rel)
			if err == nil && strings.Contains(string(b), t.Classification.GrepLiteral) {
				got = append(got, rel)
			}
		}
		if !slices.Equal(got, t.Classification.GrepResultPaths) {
			return fmt.Errorf("literal query results %v do not equal frozen %v", got, t.Classification.GrepResultPaths)
		}
	case "cross-document-synthesis":
		if len(t.RequiredDocs) < 2 {
			return errorsNew("cross-document classification requires at least two documents")
		}
	case "navigation-heavy-coding":
		if t.Classification.UnnamedTarget == "" || strings.Contains(t.Instruction, t.Classification.UnnamedTarget) || t.Classification.FrozenAuthoredHops < 4 || len(t.Classification.FrozenIntermediates) < 3 {
			return errorsNew("unnamed target and frozen >=4-hop route with >=3 intermediates required")
		}
	case "single-document-constraint":
		if len(t.RequiredDocs) != 1 {
			return errorsNew("single-document classification requires exactly one document")
		}
	}
	return nil
}

func validatePatches(id string, p PatchSet) error {
	want := []string{"correct", "alternative", "plausible-wrong", "mutated", "init-exit", "testmain-bypass", "hidden-embed", "protocol-cheat", "timeout", "output-flood"}
	if id == "batch-ceiling" {
		want = slices.Insert(want, 2, "breaks-public-checks")
	}
	if p.SchemaVersion != 1 || p.TaskID != id || len(p.Cases) != len(want) {
		return fmt.Errorf("task %s patch set invalid", id)
	}
	for i, c := range p.Cases {
		if c.Name != want[i] || c.ExpectPass != (i < 2) {
			return fmt.Errorf("task %s patch case order/result invalid", id)
		}
		if len(c.Replacements) == 0 {
			return fmt.Errorf("task %s patch case %s requires an exact replacement", id, c.Name)
		}
		for _, r := range c.Replacements {
			if r.Old == r.New || r.Path == "" {
				return fmt.Errorf("task %s empty patch", id)
			}
		}
	}
	return nil
}

func errorsNew(s string) error { return fmt.Errorf("%s", s) }
