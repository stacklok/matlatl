package nimbus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
)

func testSuite(t *testing.T) *Suite {
	t.Helper()
	s, err := Load(filepath.Join("..", "..", "nimbus", "v1"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLoadFreezeAndReverseEnumeration(t *testing.T) {
	s := testSuite(t)
	if err := CheckFreeze(s); err != nil {
		t.Fatal(err)
	}
	before, err := evalfs.TreeHash(s.Repository)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(s.Tasks) - 1; i >= 0; i-- {
		dir := filepath.Join(t.TempDir(), "workspace")
		root, err := Materialize(s, s.Tasks[i].ID, dir)
		if err != nil {
			t.Fatal(err)
		}
		mutated, _ := evalfs.TreeHash(root)
		if mutated == before {
			t.Fatalf("%s mutation was inert", s.Tasks[i].ID)
		}
		if err := Reset(s, s.Tasks[i].ID, root); err != nil {
			t.Fatal(err)
		}
		restored, _ := evalfs.TreeHash(root)
		if restored != before {
			t.Fatalf("%s reset did not restore base", s.Tasks[i].ID)
		}
		cmd := exec.Command("go", "test", "./...")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s public checks after reset: %v: %s", s.Tasks[i].ID, err, output)
		}
	}
	after, err := evalfs.TreeHash(s.Repository)
	if err != nil || after != before {
		t.Fatal("immutable Nimbus base changed during mutation/reset tests")
	}
	imagesA := []RuntimeImage{{Runtime: "docker", ImageID: "sha256:" + strings.Repeat("a", 64), Platform: "linux/amd64"}, {Runtime: "podman", ImageID: "sha256:" + strings.Repeat("b", 64), Platform: "linux/amd64"}}
	imagesB := []RuntimeImage{{Runtime: "podman", ImageID: "sha256:" + strings.Repeat("b", 64), Platform: "linux/amd64"}, {Runtime: "docker", ImageID: "sha256:" + strings.Repeat("a", 64), Platform: "linux/amd64"}}
	a, err := BuildFreeze(s, imagesA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildFreeze(s, imagesB)
	if err != nil {
		t.Fatal(err)
	}
	canonicalA, _ := json.MarshalIndent(a, "", "  ")
	canonicalB, _ := json.MarshalIndent(b, "", "  ")
	if !bytes.Equal(canonicalA, canonicalB) {
		t.Fatal("full canonical freeze is not deterministic")
	}

	copyRoot := filepath.Join(t.TempDir(), "nimbus")
	copyTree(t, s.Root, copyRoot)
	copySuite, err := Load(copyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFreeze(copySuite, imagesA); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(copyRoot, "freeze.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckFreeze(copySuite); err != nil {
		t.Fatal(err)
	}
	if err := WriteFreeze(copySuite, imagesB); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(copyRoot, "freeze.json"))
	if !bytes.Equal(first, second) {
		t.Fatal("copied suite freeze write is not repeatable")
	}
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.Mkdir(destination, 0o750); err != nil {
		t.Fatal(err)
	}
	files, err := evalfs.Files(source)
	if err != nil {
		t.Fatal(err)
	}
	root, err := evalfs.Root(destination)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range files {
		data, err := evalfs.Read(source, rel)
		if err != nil {
			t.Fatal(err)
		}
		if err := evalfs.WriteExclusive(root, rel, data); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStrictContracts(t *testing.T) {
	for _, data := range [][]byte{[]byte(`{"schemaVersion":1,"schemaVersion":1}`), []byte(`{"schemaVersion":1,"unknown":true}`)} {
		var task Task
		if decodeStrict(data, &task) == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	valid := []byte(`{"schemaVersion":1,"calibrationOnly":true,"cacheRead":0,"cacheWrite":0,"inputTokens":12,"outputTokens":3,"turns":2,"toolCalls":7,"billedMicros":456,"wallMillis":900}`)
	tm, err := ValidateTelemetry(valid)
	if err != nil {
		t.Fatal(err)
	}
	if tm.InputTokens != 12 || tm.OutputTokens != 3 || tm.Turns != 2 || tm.ToolCalls != 7 || tm.BilledMicros != 456 || tm.WallMillis != 900 {
		t.Fatal("telemetry mapping changed")
	}
	for _, invalid := range []string{
		`{"schemaVersion":1,"calibrationOnly":true,"cacheWrite":0,"inputTokens":0,"outputTokens":0,"turns":0,"toolCalls":0,"billedMicros":0,"wallMillis":0}`,
		`{"schemaVersion":1,"calibrationOnly":true,"cacheRead":1,"cacheWrite":0,"inputTokens":0,"outputTokens":0,"turns":0,"toolCalls":0,"billedMicros":0,"wallMillis":0}`,
		`{"schemaVersion":1,"calibrationOnly":true,"cacheRead":0,"cacheWrite":0,"inputTokens":-1,"outputTokens":0,"turns":0,"toolCalls":0,"billedMicros":0,"wallMillis":0}`,
	} {
		if _, err := ValidateTelemetry([]byte(invalid)); err == nil {
			t.Fatalf("accepted invalid telemetry %s", invalid)
		}
	}

	candidate := map[string]any{
		"name": "synthetic", "baselineOnly": true, "competencePassed": true, "protocolReliabilityPpm": 1_000_000,
		"telemetryComplete": true, "projectedCostMicros": 1, "budgetMicros": 2, "deterministicTieBreak": "synthetic",
		"calibrationOnly": true, "treatmentGenerated": false,
		"telemetry": map[string]any{"schemaVersion": 1, "calibrationOnly": true, "cacheRead": 0, "cacheWrite": 0, "inputTokens": 1, "outputTokens": 1, "turns": 1, "toolCalls": 1, "billedMicros": 1, "wallMillis": 1},
	}
	for name := range candidate {
		copy := cloneJSONMap(t, candidate)
		delete(copy, name)
		data, _ := json.Marshal(copy)
		if _, err := ValidateQualificationCandidate(data); err == nil {
			t.Fatalf("qualification accepted missing top-level field %s", name)
		}
	}
	telemetry := candidate["telemetry"].(map[string]any)
	for name := range telemetry {
		copy := cloneJSONMap(t, candidate)
		delete(copy["telemetry"].(map[string]any), name)
		data, _ := json.Marshal(copy)
		if _, err := ValidateQualificationCandidate(data); err == nil {
			t.Fatalf("qualification accepted missing telemetry field %s", name)
		}
	}
}

func cloneJSONMap(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var copy map[string]any
	if err := json.Unmarshal(data, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}

func TestAccessTransportAndProbe(t *testing.T) {
	s := testSuite(t)
	b, err := evalfs.Read(s.Root, "private/probes.json")
	if err != nil {
		t.Fatal(err)
	}
	var p ProbeManifest
	if err := decodeStrict(b, &p); err != nil {
		t.Fatal(err)
	}
	summary, err := SummarizeAccess(p)
	if err != nil {
		t.Fatal(err)
	}
	if summary.LLMS != 2 || summary.Trails != 1 || summary.TotalTools != 7 || len(summary.Calls) != 7 {
		t.Fatal("access reconciliation")
	}
	for _, c := range summary.PerTool {
		if c.Count != 1 || len(summary.Calls[0].ArgumentsSHA256) != 64 || len(summary.Calls[0].ResponseSHA256) != 64 {
			t.Fatal("per-tool hashes/counts")
		}
	}
	if ValidateMCPTransport("remote", "http://127.0.0.1:8080/mcp") != nil {
		t.Fatal("remote transport rejected")
	}
	for _, kind := range []string{"stdio", "local"} {
		if ValidateMCPTransport(kind, "http://127.0.0.1:8080/mcp") == nil {
			t.Fatalf("accepted %s", kind)
		}
	}
	report, err := Probe(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "no directional arm statistic") {
		t.Fatal("calibration report missing prohibition")
	}
}

func TestAccessCountCalibration(t *testing.T) {
	if err := calibrateFilesystemAccessCounts(); err != nil {
		t.Fatal(err)
	}
	for _, surface := range []string{"llms.txt", "trails.json"} {
		for _, want := range []int{0, 1, 4} {
			p := ProbeManifest{SchemaVersion: 1, ClaimBoundary: "unit events; not kernel-level evidence"}
			for range want {
				p.FilesystemEvents = append(p.FilesystemEvents, FilesystemEvent{Operation: "open", Path: surface})
			}
			summary, err := SummarizeAccess(p)
			if err != nil {
				t.Fatal(err)
			}
			got := summary.LLMS
			if surface == "trails.json" {
				got = summary.Trails
			}
			if got != want {
				t.Fatalf("%s count=%d, want %d", surface, got, want)
			}
		}
	}
}

func TestNormalChecksRejectPublicRegression(t *testing.T) {
	s := testSuite(t)
	patches, err := patchesFor(s, "batch-ceiling")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"correct", "alternative", "breaks-public-checks"} {
		t.Run(name, func(t *testing.T) {
			workspace := filepath.Join(t.TempDir(), "workspace")
			if _, err := Materialize(s, "batch-ceiling", workspace); err != nil {
				t.Fatal(err)
			}
			var patch PatchCase
			for _, candidate := range patches.Cases {
				if candidate.Name == name {
					patch = candidate
					break
				}
			}
			if err := ApplyCase(workspace, patch); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "test", "./...")
			cmd.Dir = workspace
			cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off")
			output, runErr := cmd.CombinedOutput()
			if (runErr == nil) != (name != "breaks-public-checks") {
				t.Fatalf("normal checks err=%v output=%s", runErr, output)
			}
		})
	}
}

func TestOwnedContainerCancellationRemovesContainerBeforeWait(t *testing.T) {
	state := t.TempDir()
	t.Setenv("NIMBUS_SHIM_STATE", state)
	shim := filepath.Join(t.TempDir(), "runtime-shim")
	script := `#!/bin/sh
set -eu
state=$NIMBUS_SHIM_STATE
case "$1" in
run)
  shift
  cidfile=
  label=
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --cidfile) cidfile=$2; shift 2 ;;
      --label) label=${2#*=}; shift 2 ;;
      *) shift ;;
    esac
  done
  printf 'shim-id\n' > "$cidfile"
  printf '%s\n' "$label" > "$state/label"
  : > "$state/container"
  sleep 30 &
  printf '%s\n' "$!" > "$state/pid"
  wait
  ;;
inspect)
  if [ -f "$state/container" ]; then cat "$state/label"; exit 0; fi
  echo 'no such container' >&2
  exit 1
  ;;
rm)
  if [ -f "$state/pid" ]; then kill "$(cat "$state/pid")" 2>/dev/null || true; fi
  rm -f "$state/container" "$state/pid"
  ;;
*) exit 2 ;;
esac
`
	if err := os.WriteFile(shim, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	container := ownedContainer{
		name:    "matlatl-nimbus-compile-shim",
		cidfile: filepath.Join(state, "container.cid"),
		label:   "io.stacklok.matlatl.nimbus-verifier=shim-owner",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, err := runOwnedContainer(ctx, shim, container, []string{"run", "--cidfile", container.cidfile, "--label", container.label}, nil, 1024)
	if err == nil || !evaluatorFailure(err) {
		t.Fatalf("canceled runtime got %v, want evaluator failure", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("canceled runtime returned after %s", elapsed)
	}
	if _, err := os.Stat(filepath.Join(state, "container")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned container remained after cancellation: %v", err)
	}
}

func TestNormalChecksAndAdapterBuildAreSeparate(t *testing.T) {
	script, err := compileScript([]string{"go test ./..."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(script, "set -eu\n") || !strings.Contains(script, "go test ./...") || strings.Contains(script, "go build ") || !strings.Contains(script, "kill -KILL 1") {
		t.Fatalf("normal-check script changed: %q", script)
	}
	if !strings.Contains(adapterBuildCommand, "/trusted-adapter/main.go") || strings.Contains(adapterBuildCommand, "go test") || strings.Contains(adapterBuildCommand, "./cmd/nimbus-adapter") {
		t.Fatalf("adapter build is not isolated: %q", adapterBuildCommand)
	}
	if _, err := compileScript([]string{"go test ./...; echo unsafe"}); err == nil {
		t.Fatal("unfrozen normal check accepted")
	}
}

func TestReservedAdapterNamespaceRejectedBeforeLaunch(t *testing.T) {
	s := testSuite(t)
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	if _, err := Materialize(s, "batch-ceiling", workspace); err != nil {
		t.Fatal(err)
	}
	adapterDir := filepath.Join(workspace, "cmd", "nimbus-adapter")
	if err := os.MkdirAll(adapterDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adapterDir, "sibling.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := verifyCase(context.Background(), "runtime-must-not-launch", s, "batch-ceiling", workspace, base)
	if err == nil || !exitError(err) || !strings.Contains(err.Error(), "reserved path") {
		t.Fatalf("reserved adapter collision got %v", err)
	}
}

func TestAppendOnly(t *testing.T) {
	root, err := evalfs.Root(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := evalfs.WriteExclusive(root, "attempt/result.json", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := evalfs.WriteExclusive(root, "attempt/result.json", []byte("b")); err == nil {
		t.Fatal("append-only overwrite accepted")
	}
}

func TestFreshAttemptsHaveNoMarkerCrossover(t *testing.T) {
	s := testSuite(t)
	parent := t.TempDir()
	first, err := Materialize(s, "batch-ceiling", filepath.Join(parent, "attempt-a"))
	if err != nil {
		t.Fatal(err)
	}
	marker, _ := evalfs.Path(first, ".attempt-marker")
	if err := os.WriteFile(marker, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Materialize(s, "batch-ceiling", filepath.Join(parent, "attempt-b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := evalfs.Read(second, ".attempt-marker"); err == nil {
		t.Fatal("marker crossed attempts")
	}
	if err := Reset(s, "batch-ceiling", first); err == nil {
		t.Fatal("reset accepted dirty workspace")
	}
	if err := Reset(s, "batch-ceiling", second); err != nil {
		t.Fatal(err)
	}
	third, err := Materialize(s, "batch-ceiling", filepath.Join(parent, "attempt-c"))
	if err != nil {
		t.Fatal(err)
	}
	batch, _ := evalfs.Path(third, "relay/batch.go")
	data, err := os.ReadFile(batch)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(batch, append(data, []byte("\n// dirty\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Reset(s, "batch-ceiling", third); err == nil {
		t.Fatal("reset accepted altered mutation target")
	}
}
