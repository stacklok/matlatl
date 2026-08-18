//go:build ociintegration

package agentoutcome

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stacklok/matlatl/eval/internal/manifest"
)

type damagedCIDRuntime struct {
	*commandRuntime
	malformed bool
}

func (r *damagedCIDRuntime) Run(ctx context.Context, args []string) ([]byte, error) {
	out, err := r.commandRuntime.Run(ctx, args)
	cidfile := flagValue(args, "--cidfile")
	if r.malformed {
		_ = os.WriteFile(cidfile, []byte("not-a-container\n"), 0o600)
	} else {
		_ = os.Remove(cidfile)
	}
	return out, err
}

func TestOCIIsolationEndToEnd(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("OCI isolation acceptance is Linux-only")
	}
	root := repositoryRoot(t)
	choice := os.Getenv("MATLATL_OCI_RUNTIME")
	if choice == "" {
		choice = "auto"
	}
	runtimeName, imageID, imageTag, err := BuildImage(context.Background(), choice, root)
	if err != nil {
		t.Fatalf("OCI integration unavailable or unsupported: %v", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := RemoveImage(cleanupCtx, runtimeName, imageTag); err != nil {
			t.Errorf("remove generated image: %v", err)
			return
		}
		if _, err := rtImageInspect(runtimeName, imageTag); err == nil {
			t.Errorf("generated image tag %s remains inspectable", imageTag)
		}
		if _, err := rtImageInspect(runtimeName, imageID); err == nil {
			t.Errorf("generated image %s remains inspectable", imageID)
		}
	}()
	rt, err := SelectRuntime(runtimeName)
	if err != nil {
		t.Fatal(err)
	}
	gold := filepath.Join(root, "eval/gold/canonical-navigation/v1/answer.txt")
	tempSentinel := filepath.Join(t.TempDir(), "host-secret")
	if err := os.WriteFile(tempSentinel, []byte("HOST-TEMP-SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) })}
	go server.Serve(listener)
	defer server.Close()

	prompt := canonicalInstruction(t, root)
	scheduledRunID := os.Getenv("MATLATL_SCHEDULED_RUN_ID")
	if scheduledRunID == "" {
		scheduledRunID = "oci-canonical-navigation"
	}
	workspaces := map[string]map[string]fileState{}
	for _, arm := range []string{"baseline", "all"} {
		runDir := t.TempDir()
		attempt := "attempt-" + map[string]string{"baseline": "2123456789abcdef01234567", "all": "3123456789abcdef01234567"}[arm]
		outcome, _, gradeable, runErr := RunWithRuntime(context.Background(), Environment{Model: "test/offline-fake", Prompt: prompt, Task: "canonical-navigation", RunDir: runDir, EvalRoot: filepath.Join(root, "eval"), Arm: arm, Image: imageID, ScheduledRunID: scheduledRunID, AttemptID: attempt, FakeMode: "adversarial", HostGoldSentinel: gold, HostTempSentinel: tempSentinel, NetworkCanary: "http://" + listener.Addr().String() + "/canary", Timeout: 30 * time.Second}, rt)
		assertOwnershipAbsent(t, rt.(*commandRuntime), runDir)
		if runErr != nil || !gradeable || outcome.Status != "completed" {
			events, _ := os.ReadFile(filepath.Join(runDir, "events.jsonl"))
			t.Fatalf("arm=%s outcome=%+v gradeable=%v err=%v events=%s", arm, outcome, gradeable, runErr, events)
		}
		var isolation map[string]bool
		decodeFile(t, filepath.Join(runDir, "capture/isolation.json"), &isolation)
		wantIsolation := map[string]bool{"goldReadable": false, "tempReadable": false, "outsideWritable": false, "captureWritable": false, "networkReached": false, "workspaceWritable": true, "hasChownCapability": false}
		if len(isolation) != len(wantIsolation) {
			t.Fatalf("isolation keys=%v, want exact %v", isolation, wantIsolation)
		}
		for key, want := range wantIsolation {
			if value, ok := isolation[key]; !ok || value != want {
				t.Fatalf("isolation[%s]=%v,%v want %v in %s", key, value, ok, want, arm)
			}
		}
		config, err := os.ReadFile(filepath.Join(runDir, "capture/opencode.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(config), "stdio") || strings.Contains(string(config), "command") || (arm == "all" && !strings.Contains(string(config), `"type":"remote"`)) || (arm == "baseline" && string(config) != "{}") {
			t.Fatalf("unsafe MCP config for %s: %s", arm, config)
		}
		capturedPrompt, err := os.ReadFile(filepath.Join(runDir, "capture/stdin.txt"))
		if err != nil {
			t.Fatal(err)
		}
		wantPrompt := prompt
		if arm == "all" {
			wantPrompt += "\n\n" + PilotPointer
		}
		if string(capturedPrompt) != wantPrompt {
			t.Fatalf("captured prompt differs for %s", arm)
		}
		workspaces[arm], err = snapshotTree(filepath.Join(runDir, "workspace"))
		if err != nil {
			t.Fatal(err)
		}
		assertOwnershipAbsent(t, rt.(*commandRuntime), runDir)
	}
	if hits.Load() != 0 {
		t.Fatalf("host network canary received %d requests", hits.Load())
	}
	for name, state := range workspaces["baseline"] {
		if workspaces["all"][name] != state {
			t.Fatalf("common file differs: %s", name)
		}
	}
	for name := range workspaces["all"] {
		if _, common := workspaces["baseline"][name]; !common && name != "llms.txt" && name != "trails.json" {
			t.Fatalf("unexpected all-only file: %s", name)
		}
	}
	if _, ok := workspaces["all"]["llms.txt"]; !ok {
		t.Fatal("all lacks llms.txt")
	}
	if _, ok := workspaces["all"]["trails.json"]; !ok {
		t.Fatal("all lacks trails.json")
	}

	// A post-exposure provider failure is retained for grading.
	runDir := t.TempDir()
	outcome, _, gradeable, err := RunWithRuntime(context.Background(), Environment{Model: "test/offline-fake", Prompt: prompt, Task: "canonical-navigation", RunDir: runDir, EvalRoot: filepath.Join(root, "eval"), Arm: "all", Image: imageID, ScheduledRunID: scheduledRunID, AttemptID: "attempt-4123456789abcdef01234567", FakeMode: "provider", Timeout: 30 * time.Second}, rt)
	assertOwnershipAbsent(t, rt.(*commandRuntime), runDir)
	if err != nil || !gradeable || outcome.Status != "provider-failure" {
		t.Fatalf("provider outcome=%+v gradeable=%v err=%v", outcome, gradeable, err)
	}

	for _, malformed := range []bool{false, true} {
		name := "missing-cidfile"
		if malformed {
			name = "malformed-cidfile"
		}
		t.Run(name, func(t *testing.T) {
			runDir := t.TempDir()
			damaged := &damagedCIDRuntime{commandRuntime: rt.(*commandRuntime), malformed: malformed}
			outcome, _, gradeable, runErr := RunWithRuntime(context.Background(), Environment{Model: "test/offline-fake", Prompt: prompt, Task: "canonical-navigation", RunDir: runDir, EvalRoot: filepath.Join(root, "eval"), Arm: "baseline", Image: imageID, ScheduledRunID: scheduledRunID, AttemptID: "attempt-" + name, Timeout: 30 * time.Second}, damaged)
			var owner ContainerOwnership
			decodeFile(t, filepath.Join(runDir, "container-ownership.json"), &owner)
			if err := damaged.EnsureContainerAbsent(context.Background(), owner); err != nil {
				t.Fatalf("cleanup fallback: %v", err)
			}
			if runErr != nil || !gradeable || outcome.Status != manifest.StatusCompleted {
				t.Fatalf("outcome=%+v gradeable=%v err=%v", outcome, gradeable, runErr)
			}
		})
	}

	for _, tc := range []struct {
		mode      string
		gradeable bool
		status    manifest.Status
	}{{"pre-exposure", false, ""}, {"malformed-control", true, manifest.StatusEvaluatorFailure}, {"protocol", true, manifest.StatusAgentProtocolFailure}, {"capture-symlink", true, manifest.StatusEvaluatorFailure}} {
		t.Run(tc.mode, func(t *testing.T) {
			runDir := t.TempDir()
			outcome, _, gradeable, runErr := RunWithRuntime(context.Background(), Environment{Model: "test/offline-fake", Prompt: prompt, Task: "canonical-navigation", RunDir: runDir, EvalRoot: filepath.Join(root, "eval"), Arm: "baseline", Image: imageID, ScheduledRunID: scheduledRunID, AttemptID: "attempt-" + tc.mode, FakeMode: tc.mode, Timeout: 30 * time.Second}, rt)
			assertOwnershipAbsent(t, rt.(*commandRuntime), runDir)
			if gradeable != tc.gradeable || outcome.Status != tc.status || (tc.gradeable == (runErr != nil)) {
				t.Fatalf("outcome=%+v gradeable=%v err=%v", outcome, gradeable, runErr)
			}
			if tc.mode == "capture-symlink" {
				if _, err := os.Lstat(filepath.Join(runDir, "capture", "isolation.json")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("capture symlink or target bytes reached host capture")
				}
			}
		})
	}

	for _, mode := range []string{"output-overflow", "workspace-overflow", "inode-flood", "tmp-inode-flood"} {
		t.Run(mode, func(t *testing.T) {
			runDir := t.TempDir()
			outcome, _, gradeable, runErr := RunWithRuntime(context.Background(), Environment{Model: "test/offline-fake", Prompt: prompt, Task: "canonical-navigation", RunDir: runDir, EvalRoot: filepath.Join(root, "eval"), Arm: "baseline", Image: imageID, ScheduledRunID: scheduledRunID, AttemptID: "attempt-" + mode, FakeMode: mode, Timeout: 30 * time.Second}, rt)
			assertOwnershipAbsent(t, rt.(*commandRuntime), runDir)
			if runErr != nil || !gradeable || outcome.Status != "budget-exhausted" {
				t.Fatalf("outcome=%+v gradeable=%v err=%v", outcome, gradeable, runErr)
			}
			if mode == "inode-flood" {
				files, bytes, usageErr := hostTreeUsage(runDir)
				if usageErr != nil || files > 64 || bytes > 4<<20 {
					t.Fatalf("inode flood escaped to host: files=%d bytes=%d err=%v", files, bytes, usageErr)
				}
			}
		})
	}

	// Timeout and parent cancellation must both remove the runtime-owned container.
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		cancel  bool
	}{{"timeout", time.Second, false}, {"cancellation", 30 * time.Second, true}} {
		t.Run(tc.name, func(t *testing.T) {
			runDir := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				go func() {
					marker := filepath.Join(runDir, "capture", "exposure.marker")
					for {
						if _, err := os.Stat(marker); err == nil {
							cancel()
							return
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(10 * time.Millisecond):
						}
					}
				}()
			}
			outcome, _, gradeable, runErr := RunWithRuntime(ctx, Environment{Model: "test/offline-fake", Prompt: prompt, Task: "canonical-navigation", RunDir: runDir, EvalRoot: filepath.Join(root, "eval"), Arm: "baseline", Image: imageID, ScheduledRunID: scheduledRunID, AttemptID: "attempt-" + tc.name, FakeMode: "timeout", Timeout: tc.timeout}, rt)
			assertOwnershipAbsent(t, rt.(*commandRuntime), runDir)
			if runErr != nil || !gradeable || outcome.Status != "agent-timeout" {
				t.Fatalf("outcome=%+v gradeable=%v err=%v", outcome, gradeable, runErr)
			}
		})
	}

	assertScratchContents(t, rt.(*commandRuntime), imageID)
}

func hostTreeUsage(root string) (int, int64, error) {
	files := 0
	var bytes int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files++
		bytes += info.Size()
		return nil
	})
	return files, bytes, err
}

func rtImageInspect(runtimeName, image string) (string, error) {
	rt, err := SelectRuntime(runtimeName)
	if err != nil {
		return "", err
	}
	return rt.InspectImage(context.Background(), image)
}

func assertOwnershipAbsent(t *testing.T, rt *commandRuntime, runDir string) {
	t.Helper()
	var owner ContainerOwnership
	decodeFile(t, filepath.Join(runDir, "container-ownership.json"), &owner)
	if err := rt.EnsureContainerAbsent(context.Background(), owner); err != nil {
		t.Fatalf("container cleanup not verified: %v", err)
	}
}

func assertScratchContents(t *testing.T, rt *commandRuntime, imageID string) {
	t.Helper()
	name := "matlatl-eval-inspect-contents"
	_ = exec.Command(rt.executable, "rm", "-f", name).Run()
	if out, err := exec.Command(rt.executable, "create", "--name", name, imageID).CombinedOutput(); err != nil {
		t.Fatalf("create image inspection container: %v: %s", err, out)
	}
	defer exec.Command(rt.executable, "rm", "-f", name).Run()
	cmd := exec.Command(rt.executable, "export", name)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(stdout)
	files := []string{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			files = append(files, strings.TrimPrefix(header.Name, "/"))
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	if strings.Join(files, ",") != "fake-opencode,supervisor" {
		t.Fatalf("scratch image files=%v", files)
	}
}
