// Package harness provides the minimal offline Agent and Scorer seams.
// Agent packages are cooperative allowlists, not hostile-process sandboxes.
package harness

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
	"github.com/stacklok/matlatl/eval/internal/manifest"
	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
)

// VisibleFile is one corpus file exposed to an agent.
type VisibleFile struct {
	Path    string
	Content []byte
}

// Package is the complete agent-visible package allowlist: instruction and
// corpus. It deliberately has no gold, scorer, oracle, or eval-root field.
type Package struct {
	Instruction string
	Corpus      []VisibleFile
}

// AgentOutcome is the terminal status, answer, and event stream reported by an
// agent. Errors are reserved for harness/evaluator failures that prevent an
// agent outcome from being recorded.
type AgentOutcome struct {
	Status manifest.Status
	Answer string
	Events []manifest.Event
}

// Agent executes one packaged task.
type Agent interface {
	ID() string
	Run(context.Context, Package) (AgentOutcome, error)
}

// Scorer privately scores an answer.
type Scorer interface {
	Score(*manifest.Task, manifest.Status, string) (int, error)
}

// MaxPackageBytes bounds the complete instruction-and-corpus package retained
// for one attempt. Individual files remain subject to evalfs.MaxFileBytes.
const MaxPackageBytes = 2 << 20

// BuildPackage constructs the agent-visible instruction-and-corpus package.
func BuildPackage(task *manifest.Task, fixturesRoot string) (Package, error) {
	if err := manifest.ValidateTask(task); err != nil {
		return Package{}, err
	}
	corpusPath, err := evalfs.Path(fixturesRoot, task.CorpusRef)
	if err != nil {
		return Package{}, err
	}
	corpusRoot, err := evalfs.Root(corpusPath)
	if err != nil {
		return Package{}, err
	}
	files, err := evalfs.Files(corpusRoot)
	if err != nil {
		return Package{}, err
	}
	pkg := Package{Instruction: task.Instruction, Corpus: make([]VisibleFile, 0, len(files))}
	totalBytes := len(task.Instruction)
	for _, rel := range files {
		if !identity.IsMarkdownPath(rel) {
			return Package{}, fmt.Errorf("harness: corpus contains non-markdown file %q", rel)
		}
		content, err := evalfs.Read(corpusRoot, rel)
		if err != nil {
			return Package{}, err
		}
		fileBytes := len(rel) + len(content)
		if fileBytes > MaxPackageBytes-totalBytes {
			return Package{}, fmt.Errorf("harness: agent package exceeds %d bytes", MaxPackageBytes)
		}
		totalBytes += fileBytes
		pkg.Corpus = append(pkg.Corpus, VisibleFile{Path: rel, Content: content})
	}
	return pkg, nil
}

// MockAgent deterministically searches only the visible corpus for the marker
// named as [[marker]] in the instruction.
type MockAgent struct{}

// ID identifies the deterministic v1 mock agent.
func (MockAgent) ID() string { return "mock-v1" }

// Run implements Agent.
func (MockAgent) Run(_ context.Context, pkg Package) (AgentOutcome, error) {
	marker := markerFromInstruction(pkg.Instruction)
	events := make([]manifest.Event, 1, 2)
	events[0] = manifest.Event{Kind: "search", Payload: marker}
	if marker == "" {
		return AgentOutcome{Status: manifest.StatusAgentProtocolFailure, Events: events}, nil
	}
	var answer string
	for _, file := range pkg.Corpus {
		if strings.Contains(string(file.Content), marker) {
			if answer != "" {
				return AgentOutcome{Status: manifest.StatusAgentProtocolFailure, Events: events}, nil
			}
			answer = file.Path
		}
	}
	events = append(events, manifest.Event{Kind: "answer", Payload: answer})
	return AgentOutcome{Status: manifest.StatusCompleted, Answer: answer, Events: events}, nil
}

func markerFromInstruction(instruction string) string {
	_, rest, ok := strings.Cut(instruction, "[[")
	if !ok {
		return ""
	}
	marker, _, ok := strings.Cut(rest, "]]")
	if !ok {
		return ""
	}
	return marker
}

// ExactPathScorer compares an answer with private gold/REF/answer.txt.
type ExactPathScorer struct{ GoldRoot string }

// Score implements Scorer.
func (scorer ExactPathScorer) Score(task *manifest.Task, status manifest.Status, answer string) (int, error) {
	if status != manifest.StatusCompleted {
		return -1, nil
	}
	gold, err := evalfs.Read(scorer.GoldRoot, filepath.ToSlash(filepath.Join(task.GoldRef, "answer.txt")))
	if err != nil {
		return 0, err
	}
	want := cleanAnswer(string(gold))
	got := cleanAnswer(answer)
	if want != "" && got == want {
		return 1, nil
	}
	return 0, nil
}

func cleanAnswer(answer string) string {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(answer)))
	if clean == "." || !filepath.IsLocal(clean) {
		return ""
	}
	return filepath.ToSlash(clean)
}

// EmitGraph runs the real scan, parse, pipeline, and graph emitter path.
func EmitGraph(ctx context.Context, corpusRoot string) ([]byte, error) {
	cfg := application.DefaultConfig()
	cfg.RootPath = corpusRoot
	pipeline := application.NewPipeline(
		cfg,
		fsscanner.New(fsscanner.Config{}),
		mdparser.NewFactory(mdparser.Config{}),
		nil,
	)
	_, result, err := pipeline.Run(ctx)
	if err != nil {
		return nil, err
	}
	return graphjson.JSON(emit.BuildView(result))
}

// RunOptions describes one offline attempt.
type RunOptions struct {
	RunID        string
	Task         *manifest.Task
	FixturesRoot string
	Arm          string
	Attempt      int
	Agent        Agent
	Scorer       Scorer
}

// RunRecord is one matching result and trajectory pair.
type RunRecord struct {
	Result     *manifest.Result
	Trajectory *manifest.Trajectory
}

// Run executes and validates one attempt in memory.
func Run(ctx context.Context, options RunOptions) (*RunRecord, error) {
	if options.Agent == nil || options.Scorer == nil {
		return nil, errors.New("harness: agent and scorer are required")
	}
	pkg, err := BuildPackage(options.Task, options.FixturesRoot)
	if err != nil {
		return nil, err
	}
	taskHash, err := manifest.TaskHash(options.Task)
	if err != nil {
		return nil, err
	}
	attemptID := manifest.AttemptID(options.RunID, taskHash, options.Arm, options.Agent.ID(), options.Attempt)
	outcome, err := options.Agent.Run(ctx, pkg)
	if err != nil {
		return nil, fmt.Errorf("harness: agent execution: %w", err)
	}
	if !manifest.IsAgentOutcomeStatus(outcome.Status) {
		return nil, fmt.Errorf("harness: agent reported invalid status %q", outcome.Status)
	}
	if outcome.Status != manifest.StatusCompleted {
		outcome.Answer = ""
	}
	for i := range outcome.Events {
		outcome.Events[i].Sequence = i + 1
	}
	score, err := options.Scorer.Score(options.Task, outcome.Status, outcome.Answer)
	if err != nil {
		return nil, fmt.Errorf("harness: scorer: %w", err)
	}
	if outcome.Status != manifest.StatusCompleted {
		score = -1
	}
	trajectory := &manifest.Trajectory{
		SchemaVersion: manifest.SchemaVersion, RunID: options.RunID, AttemptID: attemptID,
		TaskHash: taskHash, Arm: options.Arm, AgentID: options.Agent.ID(), Events: outcome.Events,
	}
	result := &manifest.Result{
		SchemaVersion: manifest.SchemaVersion, RunID: options.RunID, AttemptID: attemptID,
		TaskID: options.Task.ID, TaskHash: taskHash, Arm: options.Arm, AgentID: options.Agent.ID(),
		Attempt: options.Attempt, Status: outcome.Status, Answer: outcome.Answer, Score: score,
	}
	if _, err := manifest.SealTrajectory(trajectory); err != nil {
		return nil, err
	}
	if _, err := manifest.SealResult(result); err != nil {
		return nil, err
	}
	if err := manifest.ValidateRecords([]*manifest.Result{result}, []*manifest.Trajectory{trajectory}); err != nil {
		return nil, err
	}
	return &RunRecord{Result: result, Trajectory: trajectory}, nil
}
