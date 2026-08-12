// Command eval is the stdlib-only offline evaluation scaffold.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
	"github.com/stacklok/matlatl/eval/internal/harness"
	"github.com/stacklok/matlatl/eval/internal/manifest"
	"github.com/stacklok/matlatl/eval/internal/oracle"
	reportmd "github.com/stacklok/matlatl/eval/internal/report"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: eval <validate|oracle|smoke|report> [flags]")
	}
	switch args[0] {
	case "validate":
		return validateCommand(args[1:], stdout)
	case "oracle":
		return oracleCommand(ctx, args[1:], stdout)
	case "smoke":
		return smokeCommand(ctx, args[1:], stdout)
	case "report":
		return reportCommand(args[1:], stdout)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func validateCommand(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	rootArg := flags.String("root", "eval", "eval directory")
	recordsArg := flags.String("records", "", "optional result root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	root, tasks, err := loadTasks(*rootArg)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		fixtures, _ := evalfs.Path(root, "fixtures")
		if _, err := harness.BuildPackage(task, fixtures); err != nil {
			return fmt.Errorf("task %s corpus: %w", task.ID, err)
		}
		gold, _ := evalfs.Path(root, "gold")
		if _, err := evalfs.Read(gold, filepath.ToSlash(filepath.Join(task.GoldRef, "answer.txt"))); err != nil {
			return fmt.Errorf("task %s gold: %w", task.ID, err)
		}
	}
	if *recordsArg != "" {
		results, trajectories, err := loadRecords(*recordsArg)
		if err != nil {
			return err
		}
		if err := manifest.ValidateRecords(results, trajectories); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(stdout, "validated %d v1 task(s)\n", len(tasks))
	return err
}

func oracleCommand(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("oracle", flag.ContinueOnError)
	rootArg := flags.String("root", "eval", "eval directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	root, tasks, err := loadTasks(*rootArg)
	if err != nil {
		return err
	}
	task, err := canonicalTask(tasks)
	if err != nil {
		return err
	}
	fixtures, _ := evalfs.Path(root, "fixtures")
	corpus, err := evalfs.Path(fixtures, task.CorpusRef)
	if err != nil {
		return err
	}
	graph, err := harness.EmitGraph(ctx, corpus)
	if err != nil {
		return err
	}
	if err := (oracle.Canonical{}).Check(graph); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, oracle.Summary())
	return err
}

func smokeCommand(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("smoke", flag.ContinueOnError)
	rootArg := flags.String("root", "eval", "eval directory")
	outArg := flags.String("out", "", "new append-only output directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *outArg == "" {
		return errors.New("smoke requires --out")
	}
	root, tasks, err := loadTasks(*rootArg)
	if err != nil {
		return err
	}
	task, err := canonicalTask(tasks)
	if err != nil {
		return err
	}
	fixtures, _ := evalfs.Path(root, "fixtures")
	gold, _ := evalfs.Path(root, "gold")
	corpus, err := evalfs.Path(fixtures, task.CorpusRef)
	if err != nil {
		return err
	}
	graph, err := harness.EmitGraph(ctx, corpus)
	if err != nil {
		return err
	}
	if err := (oracle.Canonical{}).Check(graph); err != nil {
		return err
	}
	record, err := harness.Run(ctx, harness.RunOptions{
		RunID: "offline-smoke-v1", Task: task, FixturesRoot: fixtures,
		Arm: "baseline", Attempt: 1, Agent: harness.MockAgent{},
		Scorer: harness.ExactPathScorer{GoldRoot: gold},
	})
	if err != nil {
		return err
	}
	if record.Result.Score != 1 {
		return fmt.Errorf("mock answer did not score: %q", record.Result.Answer)
	}
	out, err := outputRoot(*outArg)
	if err != nil {
		return err
	}
	trajectoryJSON, err := manifest.MarshalTrajectory(record.Trajectory)
	if err != nil {
		return err
	}
	resultJSON, err := manifest.MarshalResult(record.Result)
	if err != nil {
		return err
	}
	if err := evalfs.WriteExclusive(out, "trajectories/"+record.Result.AttemptID+".json", trajectoryJSON); err != nil {
		return err
	}
	if err := evalfs.WriteExclusive(out, "results/"+record.Result.AttemptID+".json", resultJSON); err != nil {
		return err
	}
	results, trajectories, err := loadRecords(out)
	if err != nil {
		return err
	}
	if err := manifest.ValidateRecords(results, trajectories); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "smoke passed: %s\n", record.Result.AttemptID)
	return err
}

func reportCommand(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	recordsArg := flags.String("records", "", "result root (empty renders an empty report)")
	outArg := flags.String("out", "", "optional new output directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	var results []*manifest.Result
	if *recordsArg != "" {
		loaded, trajectories, err := loadRecords(*recordsArg)
		if err != nil {
			return err
		}
		if err := manifest.ValidateRecords(loaded, trajectories); err != nil {
			return err
		}
		results = loaded
	}
	markdown := []byte(reportmd.Markdown(results))
	if *outArg == "" {
		_, err := stdout.Write(markdown)
		return err
	}
	out, err := outputRoot(*outArg)
	if err != nil {
		return err
	}
	reportPath, err := evalfs.Path(out, "report.md")
	if err != nil {
		return err
	}
	if err := os.WriteFile(reportPath, markdown, 0o600); err != nil {
		return fmt.Errorf("write report.md: %w", err)
	}
	_, err = fmt.Fprintln(stdout, "wrote report.md")
	return err
}

func loadTasks(rootArg string) (string, []*manifest.Task, error) {
	root, err := evalfs.Root(rootArg)
	if err != nil {
		return "", nil, err
	}
	tasksRoot, err := evalfs.Path(root, "tasks")
	if err != nil {
		return "", nil, err
	}
	files, err := evalfs.Files(tasksRoot)
	if err != nil {
		return "", nil, err
	}
	var tasks []*manifest.Task
	for _, rel := range files {
		if filepath.Base(rel) != "task.json" {
			return "", nil, fmt.Errorf("unexpected task file %q", rel)
		}
		b, err := evalfs.Read(tasksRoot, rel)
		if err != nil {
			return "", nil, err
		}
		task, err := manifest.DecodeTask(bytes.NewReader(b))
		if err != nil {
			return "", nil, fmt.Errorf("%s: %w", rel, err)
		}
		tasks = append(tasks, task)
	}
	if len(tasks) == 0 {
		return "", nil, errors.New("no tasks found")
	}
	return root, tasks, nil
}

func loadRecords(rootArg string) ([]*manifest.Result, []*manifest.Trajectory, error) {
	root, err := evalfs.Root(rootArg)
	if err != nil {
		return nil, nil, err
	}
	budget := recordLoadBudget{}
	results, err := loadResultDir(root, "results", &budget)
	if err != nil {
		return nil, nil, err
	}
	trajectories, err := loadTrajectoryDir(root, "trajectories", &budget)
	if err != nil {
		return nil, nil, err
	}
	return results, trajectories, nil
}

type recordLoadBudget struct {
	bytes  int
	events int
}

func (budget *recordLoadBudget) addBytes(size int) error {
	if size > manifest.MaxRecordCollectionBytes-budget.bytes {
		return fmt.Errorf("record collection exceeds %d bytes", manifest.MaxRecordCollectionBytes)
	}
	budget.bytes += size
	return nil
}

func (budget *recordLoadBudget) addEvents(count int) error {
	if count > manifest.MaxRecordCollectionEvents-budget.events {
		return fmt.Errorf("record collection exceeds %d events", manifest.MaxRecordCollectionEvents)
	}
	budget.events += count
	return nil
}

func loadResultDir(root string, relDir string, budget *recordLoadBudget) ([]*manifest.Result, error) {
	dir, err := evalfs.Path(root, relDir)
	if err != nil {
		return nil, err
	}
	files, err := evalfs.Files(dir)
	if err != nil {
		return nil, err
	}
	results := make([]*manifest.Result, 0, len(files))
	for _, rel := range files {
		if !strings.HasSuffix(rel, ".json") {
			return nil, fmt.Errorf("unexpected result file %q", rel)
		}
		b, err := evalfs.Read(dir, rel)
		if err != nil {
			return nil, err
		}
		if err := budget.addBytes(len(b)); err != nil {
			return nil, fmt.Errorf("result %s: %w", rel, err)
		}
		result, err := manifest.DecodeResult(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("result %s: %w", rel, err)
		}
		results = append(results, result)
	}
	return results, nil
}

func loadTrajectoryDir(root string, relDir string, budget *recordLoadBudget) ([]*manifest.Trajectory, error) {
	dir, err := evalfs.Path(root, relDir)
	if err != nil {
		return nil, err
	}
	files, err := evalfs.Files(dir)
	if err != nil {
		return nil, err
	}
	trajectories := make([]*manifest.Trajectory, 0, len(files))
	for _, rel := range files {
		b, err := evalfs.Read(dir, rel)
		if err != nil {
			return nil, err
		}
		if err := budget.addBytes(len(b)); err != nil {
			return nil, fmt.Errorf("trajectory %s: %w", rel, err)
		}
		trajectory, err := manifest.DecodeTrajectory(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("trajectory %s: %w", rel, err)
		}
		if err := budget.addEvents(len(trajectory.Events)); err != nil {
			return nil, fmt.Errorf("trajectory %s: %w", rel, err)
		}
		trajectories = append(trajectories, trajectory)
	}
	return trajectories, nil
}

func canonicalTask(tasks []*manifest.Task) (*manifest.Task, error) {
	for _, task := range tasks {
		if task.ID == "canonical-navigation" && task.Version == "v1" {
			return task, nil
		}
	}
	return nil, errors.New("canonical-navigation v1 task not found")
}

func outputRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := evalfs.Root(filepath.Dir(abs)); err != nil {
		return "", fmt.Errorf("output parent: %w", err)
	}
	if err := os.Mkdir(abs, 0o750); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	return evalfs.Root(abs)
}
