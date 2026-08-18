// Command eval is the stdlib-only offline evaluation scaffold.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/stacklok/matlatl/eval/internal/correctness"
	"github.com/stacklok/matlatl/eval/internal/evalfs"
	"github.com/stacklok/matlatl/eval/internal/harness"
	"github.com/stacklok/matlatl/eval/internal/manifest"
	"github.com/stacklok/matlatl/eval/internal/nimbus"
	"github.com/stacklok/matlatl/eval/internal/oracle"
	reportmd "github.com/stacklok/matlatl/eval/internal/report"
)

var canonicalCheck = (oracle.Canonical{}).Check

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: eval <validate|oracle|smoke|report|nimbus> [flags]")
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
	case "nimbus":
		return nimbusCommand(ctx, args[1:], stdout)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func nimbusCommand(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: eval nimbus <validate|freeze|inspect-images|verify|probe> [flags]")
	}
	const notice = "; calibration-only; second human review pending; not outcome evidence\n"
	subcommand := args[0]
	flags := flag.NewFlagSet("nimbus "+subcommand, flag.ContinueOnError)
	rootArg := flags.String("root", "eval/nimbus/v1", "Nimbus v1 root")
	var runtimeArg *string
	var prepare, write *bool
	var imageFile, outputFile *string
	switch subcommand {
	case "validate", "probe":
	case "inspect-images":
		outputFile = flags.String("out", "", "write audited Docker/Podman image identities to this new file")
	case "freeze":
		write = flags.Bool("write", false, "rewrite freeze.json after audited runtime image inspection")
		imageFile = flags.String("runtime-image-file", "", "audited JSON array of runtime image inspections")
	case "verify":
		runtimeArg = flags.String("runtime", "", "OCI runtime: docker or podman")
		prepare = flags.Bool("prepare", false, "pull the pinned verifier image before verification")
	default:
		return fmt.Errorf("unknown Nimbus subcommand %q", subcommand)
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("nimbus %s: unexpected arguments %v", subcommand, flags.Args())
	}
	suite, err := nimbus.Load(*rootArg)
	if err != nil {
		return err
	}
	switch subcommand {
	case "validate":
		_, err = fmt.Fprintf(stdout, "validated %d Nimbus v1 task(s)%s", len(suite.Tasks), notice)
	case "inspect-images":
		if *outputFile == "" {
			return errors.New("nimbus inspect-images requires --out; prepare both Docker and Podman verifier images first")
		}
		var images []nimbus.RuntimeImage
		for _, runtime := range []string{"docker", "podman"} {
			image, inspectErr := nimbus.InspectVerifier(ctx, runtime)
			if inspectErr != nil {
				return inspectErr
			}
			images = append(images, image)
		}
		data, marshalErr := json.MarshalIndent(images, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		data = append(data, '\n')
		file, openErr := openExclusive(*outputFile, 0o600)
		if openErr != nil {
			return openErr
		}
		if _, err = file.Write(data); err == nil {
			err = file.Close()
		} else {
			_ = file.Close()
		}
		if err == nil {
			_, err = fmt.Fprint(stdout, "wrote audited Nimbus runtime image file", notice)
		}
	case "freeze":
		if *write {
			if *imageFile == "" {
				return errors.New("nimbus freeze --write requires --runtime-image-file from audited `nimbus inspect-images`; never supply arbitrary IDs")
			}
			data, readErr := readFile(*imageFile)
			if readErr != nil {
				return readErr
			}
			var images []nimbus.RuntimeImage
			if jsonErr := json.Unmarshal(data, &images); jsonErr != nil {
				return jsonErr
			}
			if err = nimbus.WriteFreeze(suite, images); err == nil {
				_, err = fmt.Fprint(stdout, "wrote Nimbus freeze.json", notice)
			}
		} else if err = nimbus.CheckFreeze(suite); err == nil {
			_, err = fmt.Fprint(stdout, "Nimbus freeze.json matches", notice)
		}
	case "verify":
		if *runtimeArg == "" {
			return errors.New("nimbus verify requires --runtime docker|podman; install/start the runtime and use --prepare")
		}
		if *prepare {
			if _, err = nimbus.PrepareVerifier(ctx, *runtimeArg); err != nil {
				return err
			}
		}
		var results []nimbus.VerificationResult
		if results, err = nimbus.Verify(ctx, suite, *runtimeArg); err == nil {
			_, err = fmt.Fprintf(stdout, "verified %d Nimbus patch cases in isolated %s containers%s", len(results), *runtimeArg, notice)
		}
	case "probe":
		var result string
		if result, err = nimbus.Probe(ctx, suite); err == nil {
			_, err = fmt.Fprint(stdout, strings.TrimSuffix(result, "\n"), notice)
		}
	}
	return err
}

func readFile(path string) ([]byte, error) {
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) || !filepath.IsLocal(base) {
		return nil, fmt.Errorf("unsafe input path %q", path)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	data, readErr := root.ReadFile(base)
	return data, errors.Join(readErr, root.Close())
}

func openExclusive(path string, mode os.FileMode) (*os.File, error) {
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) || !filepath.IsLocal(base) {
		return nil, fmt.Errorf("unsafe output path %q", path)
	}
	dir := filepath.Dir(path)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	file, openErr := root.OpenFile(base, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	closeErr := root.Close()
	if openErr != nil || closeErr != nil {
		if file != nil {
			_ = file.Close()
		}
		return nil, errors.Join(openErr, closeErr)
	}
	return file, nil
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
	if err := canonicalCheck(graph); err != nil {
		return err
	}
	counts, err := correctness.Run(ctx, root)
	if err != nil {
		return err
	}
	counts.CanonicalNavigation = 1
	if _, err := io.WriteString(stdout, oracle.Summary()); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, counts.Summary())
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
	if err := canonicalCheck(graph); err != nil {
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
