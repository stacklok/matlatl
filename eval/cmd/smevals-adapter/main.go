// Command smevals-adapter bridges smevals runner/checker contracts to OpenCode.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/stacklok/matlatl/eval/internal/agentoutcome"
	"github.com/stacklok/matlatl/eval/internal/manifest"
)

func main() {
	code, err := run(context.Background(), os.Args[1:])
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "smevals-adapter:", err)
	}
	os.Exit(code)
}

func run(ctx context.Context, args []string) (int, error) {
	if len(args) < 1 {
		return 2, errors.New("usage: smevals-adapter <run|check|image>")
	}
	switch args[0] {
	case "image":
		if len(args) != 2 {
			return 2, errors.New("usage: smevals-adapter image REPOSITORY_ROOT")
		}
		runtimeName, imageID, tag, err := agentoutcome.BuildImage(ctx, os.Getenv("MATLATL_OCI_RUNTIME"), args[1])
		if err != nil {
			return 1, err
		}
		_, err = fmt.Fprintf(os.Stdout, "%s %s %s\n", runtimeName, imageID, tag)
		return 0, err
	case "run", "check":
		if len(args) != 1 {
			return 2, errors.New("usage: smevals-adapter <run|check>")
		}
	}
	switch args[0] {
	case "run":
		env, err := agentoutcome.ReadEnvironment()
		if err != nil {
			return 1, err
		}
		outcome, _, gradeable, err := agentoutcome.Run(ctx, env)
		if err != nil {
			return 1, err
		}
		if !gradeable {
			return 1, errors.New("run did not produce a gradeable outcome")
		}
		if outcome.Answer != "" {
			_, err = fmt.Fprintln(os.Stdout, outcome.Answer)
		}
		return 0, err
	case "check":
		runDir, evalRoot, task := os.Getenv("SMEVALS_RUN_DIR"), os.Getenv("MATLATL_EVAL_ROOT"), os.Getenv("SMEVALS_TASK")
		missing := make([]string, 0, 3)
		for _, item := range []struct{ name, value string }{{"SMEVALS_RUN_DIR", runDir}, {"MATLATL_EVAL_ROOT", evalRoot}, {"SMEVALS_TASK", task}} {
			if item.value == "" {
				missing = append(missing, item.name)
			}
		}
		if len(missing) != 0 {
			return 1, fmt.Errorf("missing required checker environment variables: %s", strings.Join(missing, ", "))
		}
		result, passed, err := agentoutcome.Check(runDir, evalRoot, task)
		if err != nil {
			if recordErr := agentoutcome.RecordFailure(runDir, manifest.StatusEvaluatorFailure, err); recordErr != nil {
				return 1, errors.Join(err, recordErr)
			}
			return 1, err
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			return 1, err
		}
		if !passed {
			return 1, nil
		}
		return 0, nil
	default:
		return 2, fmt.Errorf("unknown command %q", args[0])
	}
}
