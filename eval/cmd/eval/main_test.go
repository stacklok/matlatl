package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/eval/internal/manifest"
)

func commandEvalRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCommandsOffline(t *testing.T) {
	root := commandEvalRoot(t)
	var output bytes.Buffer
	if err := run(context.Background(), []string{"validate", "--root", root}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "validated 1") {
		t.Fatalf("validate output = %q", output.String())
	}
	output.Reset()
	if err := run(context.Background(), []string{"oracle", "--root", root}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "documents=3 edges=3") {
		t.Fatalf("oracle output = %q", output.String())
	}

	records := filepath.Join(t.TempDir(), "records")
	output.Reset()
	if err := run(context.Background(), []string{"smoke", "--root", root, "--out", records}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "smoke passed") {
		t.Fatalf("smoke output = %q", output.String())
	}
	if err := run(context.Background(), []string{"smoke", "--root", root, "--out", records}, &bytes.Buffer{}); err == nil {
		t.Fatal("second smoke overwrote append-only records")
	}
	if err := run(context.Background(), []string{"validate", "--root", root, "--records", records}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	reportOut := filepath.Join(t.TempDir(), "report")
	if err := run(context.Background(), []string{"report", "--records", records, "--out", reportOut}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	report, err := os.ReadFile(filepath.Join(reportOut, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(report), "pass: 1") || !strings.Contains(string(report), "docs/operate.md") {
		t.Fatalf("report = %s", report)
	}
}

func TestReportCommandEmpty(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), []string{"report"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "No results recorded") {
		t.Fatalf("empty report = %q", output.String())
	}
}

func TestRecordCollectionBudgets(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		root := writeRecordCollection(t, manifest.MaxRecordCollectionBytes/manifest.MaxPayloadBytes+2, 1, strings.Repeat("x", manifest.MaxPayloadBytes))
		if _, _, err := loadRecords(root); err == nil || !strings.Contains(err.Error(), "bytes") {
			t.Fatalf("byte budget error = %v", err)
		}
	})
	t.Run("events", func(t *testing.T) {
		root := writeRecordCollection(t, manifest.MaxRecordCollectionEvents/manifest.MaxEventsPerTrajectory+1, manifest.MaxEventsPerTrajectory, "")
		if _, _, err := loadRecords(root); err == nil || !strings.Contains(err.Error(), "events") {
			t.Fatalf("event budget error = %v", err)
		}
	})
}

func writeRecordCollection(t *testing.T, count, eventCount int, payload string) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"results", "trajectories"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for i := range count {
		id := fmt.Sprintf("attempt-%04d", i)
		result := &manifest.Result{
			SchemaVersion: manifest.SchemaVersion, RunID: "run", AttemptID: id,
			TaskID: "task", TaskHash: "task-hash", Arm: "baseline", AgentID: "agent",
			Attempt: i + 1, Status: manifest.StatusCompleted, Answer: "doc.md", Score: 1,
		}
		events := make([]manifest.Event, eventCount)
		for j := range events {
			events[j] = manifest.Event{Sequence: j + 1, Kind: "event", Payload: payload}
		}
		trajectory := &manifest.Trajectory{
			SchemaVersion: manifest.SchemaVersion, RunID: "run", AttemptID: id,
			TaskHash: "task-hash", Arm: "baseline", AgentID: "agent", Events: events,
		}
		if _, err := manifest.SealResult(result); err != nil {
			t.Fatal(err)
		}
		if _, err := manifest.SealTrajectory(trajectory); err != nil {
			t.Fatal(err)
		}
		resultJSON, err := manifest.MarshalResult(result)
		if err != nil {
			t.Fatal(err)
		}
		trajectoryJSON, err := manifest.MarshalTrajectory(trajectory)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "results", id+".json"), resultJSON, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "trajectories", id+".json"), trajectoryJSON, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRecordErrorsNameArtifact(t *testing.T) {
	root := writeRecordCollection(t, 1, 1, "")
	if err := os.WriteFile(filepath.Join(root, "results", "attempt-0000.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadRecords(root); err == nil || !strings.Contains(err.Error(), "result attempt-0000.json") {
		t.Fatalf("path-aware error = %v", err)
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"smoke"}} {
		if err := run(context.Background(), args, &bytes.Buffer{}); err == nil {
			t.Errorf("run(%v) succeeded", args)
		}
	}
}
