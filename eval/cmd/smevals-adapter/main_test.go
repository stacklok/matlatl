package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckCommandPreservesSmevalsContract(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root, _ := filepath.Abs(filepath.Join(filepath.Dir(file), "../../.."))
	runDir := t.TempDir()
	content, _ := json.Marshal(map[string]any{"schemaVersion": 1, "status": "completed", "answer": "docs/operate.md"})
	if err := os.WriteFile(filepath.Join(runDir, "outcome.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMEVALS_RUN_DIR", runDir)
	t.Setenv("MATLATL_EVAL_ROOT", filepath.Join(root, "eval"))
	t.Setenv("SMEVALS_TASK", "canonical-navigation")
	code, err := run(context.Background(), []string{"check"})
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestUsageFailure(t *testing.T) {
	for _, args := range [][]string{nil, {"image"}, {"run", "extra"}, {"unknown"}} {
		if code, err := run(context.Background(), args); code != 2 || err == nil {
			t.Fatalf("args=%v code=%d err=%v", args, code, err)
		}
	}
}
