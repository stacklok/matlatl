package harness

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
	"github.com/stacklok/matlatl/eval/internal/manifest"
	"github.com/stacklok/matlatl/eval/internal/oracle"
)

func evalRoot(t *testing.T) string {
	t.Helper()
	root, err := evalfs.Root(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func loadCanonicalTask(t *testing.T, root string) *manifest.Task {
	t.Helper()
	b, err := evalfs.Read(root, "tasks/canonical-navigation/v1/task.json")
	if err != nil {
		t.Fatal(err)
	}
	task, err := manifest.DecodeTask(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestCanonicalSmokeUsesRealPipeline(t *testing.T) {
	root := evalRoot(t)
	task := loadCanonicalTask(t, root)
	fixtures, _ := evalfs.Path(root, "fixtures")
	gold, _ := evalfs.Path(root, "gold")
	corpus, _ := evalfs.Path(fixtures, task.CorpusRef)
	graph, err := EmitGraph(context.Background(), corpus)
	if err != nil {
		t.Fatal(err)
	}
	secondGraph, err := EmitGraph(context.Background(), corpus)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(graph, secondGraph) {
		t.Fatal("EmitGraph output changed between identical runs")
	}
	if err := (oracle.Canonical{}).Check(graph); err != nil {
		t.Fatal(err)
	}
	record, err := Run(context.Background(), RunOptions{
		RunID: "test", Task: task, FixturesRoot: fixtures,
		Arm: "baseline", Attempt: 1, Agent: MockAgent{},
		Scorer: ExactPathScorer{GoldRoot: gold},
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Result.Answer != "docs/operate.md" || record.Result.Score != 1 {
		t.Fatalf("answer=%q score=%d", record.Result.Answer, record.Result.Score)
	}
}

func TestAgentPackageDoesNotLeakGoldSentinel(t *testing.T) {
	root := t.TempDir()
	fixtures := filepath.Join(root, "fixtures")
	gold := filepath.Join(root, "gold")
	if err := os.MkdirAll(filepath.Join(fixtures, "corpus", "v1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gold, "task", "v1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "corpus", "v1", "README.md"), []byte("public marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	const sentinel = "PRIVATE-GOLD-SENTINEL-9f2a"
	if err := os.WriteFile(filepath.Join(gold, "task", "v1", "answer.txt"), []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	task := &manifest.Task{
		SchemaVersion: 1, ID: "task", Version: "v1", Kind: "navigation",
		Instruction: "find [[marker]]", CorpusRef: "corpus/v1", GoldRef: "task/v1", AnswerFormat: "path",
	}
	fixturesRoot, _ := evalfs.Root(fixtures)
	pkg, err := BuildPackage(task, fixturesRoot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pkg.Instruction, sentinel) {
		t.Fatal("sentinel leaked in instruction")
	}
	for _, file := range pkg.Corpus {
		if strings.Contains(file.Path, "gold") || strings.Contains(string(file.Content), sentinel) {
			t.Fatalf("private gold leaked through %+v", file)
		}
	}
}

func TestPackageRejectsAggregateCorpusBytes(t *testing.T) {
	fixtures := t.TempDir()
	corpus := filepath.Join(fixtures, "corpus", "v1")
	if err := os.MkdirAll(corpus, 0o700); err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("x"), MaxPackageBytes/4+1)
	for i := range 5 {
		name := filepath.Join(corpus, fmt.Sprintf("doc-%d.md", i))
		if err := os.WriteFile(name, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	task := &manifest.Task{
		SchemaVersion: 1, ID: "task", Version: "v1", Kind: "navigation",
		Instruction: "find [[marker]]", CorpusRef: "corpus/v1", GoldRef: "task/v1", AnswerFormat: "path",
	}
	if _, err := BuildPackage(task, fixtures); err == nil || !strings.Contains(err.Error(), "package exceeds") {
		t.Fatalf("aggregate corpus error = %v", err)
	}
}

type fixedAgent struct {
	outcome AgentOutcome
	err     error
}

func (fixedAgent) ID() string { return "fixed" }
func (agent fixedAgent) Run(context.Context, Package) (AgentOutcome, error) {
	return agent.outcome, agent.err
}

func TestMockAgentOutcomeSemantics(t *testing.T) {
	mock := MockAgent{}
	missing, err := mock.Run(context.Background(), Package{Instruction: "find the marker"})
	if err != nil || missing.Status != manifest.StatusAgentProtocolFailure || missing.Answer != "" {
		t.Fatalf("missing marker outcome = %+v, %v", missing, err)
	}
	duplicate, err := mock.Run(context.Background(), Package{
		Instruction: "find [[marker]]",
		Corpus:      []VisibleFile{{Path: "a.md", Content: []byte("marker")}, {Path: "b.md", Content: []byte("marker")}},
	})
	if err != nil || duplicate.Status != manifest.StatusAgentProtocolFailure || duplicate.Answer != "" {
		t.Fatalf("duplicate marker outcome = %+v, %v", duplicate, err)
	}
}

func TestRunOutcomeValidationAndNormalization(t *testing.T) {
	fixtures, gold, task := runnerFixture(t, "public text", "wanted.md")
	t.Run("marker not found is completed failure", func(t *testing.T) {
		record, err := Run(context.Background(), RunOptions{
			RunID: "run", Task: task, FixturesRoot: fixtures, Arm: "baseline", Attempt: 1,
			Agent: MockAgent{}, Scorer: ExactPathScorer{GoldRoot: gold},
		})
		if err != nil {
			t.Fatal(err)
		}
		if record.Result.Status != manifest.StatusCompleted || record.Result.Answer != "" || record.Result.Score != 0 {
			t.Fatalf("result = %+v", record.Result)
		}
	})
	t.Run("non-completed answer is cleared", func(t *testing.T) {
		record, err := Run(context.Background(), RunOptions{
			RunID: "run", Task: task, FixturesRoot: fixtures, Arm: "baseline", Attempt: 2,
			Agent:  fixedAgent{outcome: AgentOutcome{Status: manifest.StatusAgentTimeout, Answer: "must not persist"}},
			Scorer: ExactPathScorer{GoldRoot: gold},
		})
		if err != nil {
			t.Fatal(err)
		}
		if record.Result.Answer != "" || record.Result.Score != -1 {
			t.Fatalf("result = %+v", record.Result)
		}
	})
	t.Run("invalid agent status", func(t *testing.T) {
		_, err := Run(context.Background(), RunOptions{
			RunID: "run", Task: task, FixturesRoot: fixtures, Arm: "baseline", Attempt: 3,
			Agent:  fixedAgent{outcome: AgentOutcome{Status: manifest.StatusProviderFailure}},
			Scorer: ExactPathScorer{GoldRoot: gold},
		})
		if err == nil || !strings.Contains(err.Error(), "invalid status") {
			t.Fatalf("error = %v", err)
		}
	})
}

func runnerFixture(t *testing.T, corpusContent, goldAnswer string) (string, string, *manifest.Task) {
	t.Helper()
	root := t.TempDir()
	fixtures := filepath.Join(root, "fixtures")
	gold := filepath.Join(root, "gold")
	if err := os.MkdirAll(filepath.Join(fixtures, "corpus", "v1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gold, "task", "v1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtures, "corpus", "v1", "README.md"), []byte(corpusContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gold, "task", "v1", "answer.txt"), []byte(goldAnswer), 0o600); err != nil {
		t.Fatal(err)
	}
	return fixtures, gold, &manifest.Task{
		SchemaVersion: 1, ID: "task", Version: "v1", Kind: "navigation",
		Instruction: "find [[marker]]", CorpusRef: "corpus/v1", GoldRef: "task/v1", AnswerFormat: "path",
	}
}

func TestMaliciousGoldAsCorpusRejected(t *testing.T) {
	task := &manifest.Task{
		SchemaVersion: 1, ID: "task", Version: "v1", Kind: "navigation",
		Instruction: "find [[marker]]", CorpusRef: "../gold/task/v1", GoldRef: "task/v1", AnswerFormat: "path",
	}
	if _, err := BuildPackage(task, t.TempDir()); err == nil {
		t.Fatal("gold-as-corpus task accepted")
	}
}
