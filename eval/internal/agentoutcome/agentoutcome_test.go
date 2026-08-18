package agentoutcome

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stacklok/matlatl/eval/internal/manifest"
	"github.com/stacklok/matlatl/internal/infrastructure/mcpserver"
)

const testImageID = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fakeRuntime struct {
	name               string
	args               []string
	preExposureFailure bool
	terminal           string
	captureSymlink     bool
	cleanupErr         error
	cleanupCalls       int
}

func (f *fakeRuntime) Name() string {
	if f.name == "" {
		return "docker"
	}
	return f.name
}
func (f *fakeRuntime) InspectImage(context.Context, string) (string, error)            { return testImageID, nil }
func (f *fakeRuntime) EnsureContainerAbsent(context.Context, ContainerOwnership) error { return nil }
func (f *fakeRuntime) PrepareWorkspace(context.Context, ContainerOwnership) error      { return nil }
func (f *fakeRuntime) Run(_ context.Context, args []string) ([]byte, error) {
	f.args = append([]string(nil), args...)
	if f.preExposureFailure {
		return nil, errors.New("runtime unavailable")
	}
	cidfile := flagValue(args, "--cidfile")
	if err := os.WriteFile(cidfile, []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"), 0o600); err != nil {
		return nil, err
	}
	capture := mountSource(args, "/capture")
	mode := envValue(args, "MATLATL_FAKE_MODE")
	answer := "docs/operate.md"
	if mode == "wrong" {
		answer = "README.md"
	}
	events := `{"type":"step_finish","part":{"tokens":{"input":12,"output":3,"cache":{"read":0,"write":0}}}}` + "\n" + `{"type":"tool_use","part":{"tool":"grep"}}` + "\n" + `{"type":"text","part":{"text":"` + answer + `"}}` + "\n"
	if err := os.WriteFile(filepath.Join(capture, "events.jsonl"), []byte(events), 0o600); err != nil {
		return nil, err
	}
	if f.captureSymlink {
		if err := os.Symlink(filepath.Join(mountSource(args, "/input"), "workspace", "README.md"), filepath.Join(capture, "isolation.json")); err != nil {
			return nil, err
		}
	}
	if err := fakeWorkspaceExport(filepath.Join(mountSource(args, "/input"), "workspace"), capture); err != nil {
		return nil, err
	}
	attempt := envValue(args, "MATLATL_ATTEMPT_ID")
	status := f.terminal
	if status == "" {
		status = "completed"
	}
	return []byte(`{"type":"exposure","attemptId":"` + attempt + `","time":"2026-08-17T00:00:00Z","exposed":true}` + "\n" + `{"type":"terminal","attemptId":"` + attempt + `","exposed":true,"status":"` + status + `"}` + "\n"), nil
}
func (f *fakeRuntime) Cleanup(_ context.Context, owner ContainerOwnership) error {
	f.cleanupCalls++
	if f.cleanupErr != nil {
		return f.cleanupErr
	}
	content, err := os.ReadFile(owner.CIDFile)
	if err != nil {
		return err
	}
	if !validContainerID(strings.TrimSpace(string(content))) {
		return errors.New("invalid fake cidfile")
	}
	return nil
}

func fakeWorkspaceExport(workspace, capture string) error {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	manifest := capturedWorkspaceManifest{SchemaVersion: 1}
	files, err := snapshotTree(workspace)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		state := files[path]
		sum := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, capturedWorkspaceFile{Path: path, Size: int64(len(data)), Mode: state.Mode, SHA256: hex.EncodeToString(sum[:])})
		if err := tw.WriteHeader(&tar.Header{Name: path, Mode: int64(state.Mode), Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(capture, "final-workspace.json"), encoded, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(capture, "final-workspace.tar.gz"), archive.Bytes(), 0o600)
}

func TestIsolatedRunPreparationAndSecurityArgv(t *testing.T) {
	root := repositoryRoot(t)
	prompt := canonicalInstruction(t, root)
	for _, arm := range []string{"baseline", "all"} {
		t.Run(arm, func(t *testing.T) {
			rt := &fakeRuntime{name: "docker"}
			runDir := t.TempDir()
			outcome, metrics, gradeable, err := RunWithRuntime(context.Background(), Environment{Model: "test/offline-fake", Prompt: prompt, Task: "canonical-navigation", RunDir: runDir, EvalRoot: filepath.Join(root, "eval"), Arm: arm, Image: "local", ScheduledRunID: "run-canonical-navigation", AttemptID: "attempt-0123456789abcdef01234567", Timeout: time.Second}, rt)
			if err != nil || !gradeable || outcome.Answer != "docs/operate.md" || metrics.ToolCalls != 1 {
				t.Fatalf("outcome=%+v metrics=%+v gradeable=%v err=%v", outcome, metrics, gradeable, err)
			}
			for _, flag := range []string{"--cidfile", "--label", "--pull=never", "--network=none", "--ipc=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--cpus=1.0", "--memory=512m", "--memory-swap=512m", "--ulimit=nofile=256:256", "--ulimit=core=0:0", "--ulimit=fsize=1048576:1048576", "--workdir", "/workspace"} {
				if !slices.Contains(rt.args, flag) {
					t.Errorf("missing security argument %s: %v", flag, rt.args)
				}
			}
			joinedArgs := strings.Join(rt.args, "\n")
			if !strings.Contains(joinedArgs, "type=volume,source=matlatl-eval-") || !strings.Contains(joinedArgs, "-tmp,destination=/tmp") {
				t.Fatalf("bounded /tmp volume mount missing: %v", rt.args)
			}
			if got := rt.args[len(rt.args)-1]; got != testImageID {
				t.Fatalf("run uses %q, not immutable ID", got)
			}
			if count := strings.Count(strings.Join(rt.args, "\n"), "--volume\n"); count != 2 {
				t.Fatalf("container has %d bind mounts, want exactly read-only input and supervisor capture: %v", count, rt.args)
			}
			joined := strings.Join(rt.args, " ")
			if !strings.Contains(joined, ":/input:ro") || !strings.Contains(joined, ":/capture:rw") || !strings.Contains(joined, "type=volume,source=matlatl-eval-") {
				t.Fatalf("missing hard filesystem isolation: %v", rt.args)
			}
			for _, forbidden := range []string{filepath.Join(root, "eval"), filepath.Join(root, "eval", "gold"), os.Getenv("HOME"), "/var/run/docker.sock"} {
				if forbidden != "" && strings.Contains(strings.Join(rt.args, "\n"), forbidden) {
					t.Fatalf("forbidden host path mounted: %s", forbidden)
				}
			}
			workspace := filepath.Join(runDir, "workspace")
			files, _ := snapshotTree(workspace)
			_, llms := files["llms.txt"]
			_, trails := files["trails.json"]
			if llms != (arm == "all") || trails != (arm == "all") {
				t.Fatalf("treatment files: %v", files)
			}
			var execution Execution
			decodeFile(t, filepath.Join(runDir, "execution.json"), &execution)
			if !execution.Exposed || !execution.CommonParity || execution.Billing.Method != "not-applicable" || execution.Billing.CorrelationID != execution.AttemptID {
				t.Fatalf("execution=%+v", execution)
			}
			var prep Preparation
			decodeFile(t, filepath.Join(runDir, "preparation.json"), &prep)
			if prep.MCP != (arm == "all") || prep.PointerPresent != (arm == "all") || (arm == "all" && prep.Notice != PilotPointer) || (arm == "baseline" && prep.Notice != "") {
				t.Fatalf("preparation=%+v", prep)
			}
		})
	}
}

func TestFailureExposureSemantics(t *testing.T) {
	root := repositoryRoot(t)
	base := Environment{Model: "test/offline-fake", Prompt: canonicalInstruction(t, root), Task: "canonical-navigation", EvalRoot: filepath.Join(root, "eval"), Arm: "baseline", Image: "local", ScheduledRunID: "run-canonical-navigation", AttemptID: "attempt-1123456789abcdef01234567", Timeout: time.Second}
	t.Run("post-exposure-provider-is-gradeable", func(t *testing.T) {
		env := base
		env.RunDir = t.TempDir()
		rt := &fakeRuntime{terminal: "provider-failure"}
		outcome, _, gradeable, err := RunWithRuntime(context.Background(), env, rt)
		if err != nil || !gradeable || outcome.Status != "provider-failure" {
			t.Fatalf("outcome=%+v gradeable=%v err=%v", outcome, gradeable, err)
		}
	})
	t.Run("cleanup-failure-after-exposure-is-gradeable", func(t *testing.T) {
		env := base
		env.RunDir = t.TempDir()
		rt := &fakeRuntime{cleanupErr: errors.New("simulated cleanup failure")}
		outcome, _, gradeable, err := RunWithRuntime(context.Background(), env, rt)
		if err != nil || !gradeable || outcome.Status != manifest.StatusEnvironmentFailure || rt.cleanupCalls != 1 {
			t.Fatalf("outcome=%+v gradeable=%v cleanupCalls=%d err=%v", outcome, gradeable, rt.cleanupCalls, err)
		}
		var execution Execution
		decodeFile(t, filepath.Join(env.RunDir, "execution.json"), &execution)
		if !execution.Exposed || execution.TerminalStatus != manifest.StatusEnvironmentFailure || !strings.Contains(execution.Diagnostic, "simulated cleanup failure") {
			t.Fatalf("execution=%+v", execution)
		}
	})
	t.Run("cleanup-failure-before-exposure-is-non-gradeable", func(t *testing.T) {
		env := base
		env.RunDir = t.TempDir()
		rt := &fakeRuntime{preExposureFailure: true, cleanupErr: errors.New("simulated cleanup failure")}
		_, _, gradeable, err := RunWithRuntime(context.Background(), env, rt)
		if err == nil || gradeable || rt.cleanupCalls != 1 {
			t.Fatalf("gradeable=%v cleanupCalls=%d err=%v", gradeable, rt.cleanupCalls, err)
		}
		var execution Execution
		decodeFile(t, filepath.Join(env.RunDir, "execution.json"), &execution)
		if execution.Exposed || execution.TerminalStatus != manifest.StatusEnvironmentFailure || !strings.Contains(execution.Diagnostic, "simulated cleanup failure") {
			t.Fatalf("execution=%+v", execution)
		}
	})
	t.Run("pre-exposure-runtime-is-nonzero", func(t *testing.T) {
		env := base
		env.RunDir = t.TempDir()
		_, _, gradeable, err := RunWithRuntime(context.Background(), env, &fakeRuntime{preExposureFailure: true})
		if err == nil || gradeable {
			t.Fatalf("gradeable=%v err=%v", gradeable, err)
		}
	})
	t.Run("malicious-capture-symlink-is-evaluator-failure", func(t *testing.T) {
		env := base
		env.RunDir = t.TempDir()
		outcome, _, gradeable, err := RunWithRuntime(context.Background(), env, &fakeRuntime{captureSymlink: true})
		if err != nil || !gradeable || outcome.Status != "evaluator-failure" {
			t.Fatalf("outcome=%+v gradeable=%v err=%v", outcome, gradeable, err)
		}
		if _, err := os.Stat(filepath.Join(env.RunDir, "isolation.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("capture symlink target bytes were copied to host output")
		}
	})
}

func TestRuntimeSelectionAndImageValidation(t *testing.T) {
	if _, err := SelectRuntime("sh"); err == nil {
		t.Fatal("arbitrary executable accepted")
	}
	if !validImageID(testImageID) || validImageID("local:latest") {
		t.Fatal("image ID validation failed")
	}
	wantTmpfsVolume := []string{"volume", "create", "--driver", "local", "--label", runLabelKey + "=run", "--opt", "type=tmpfs", "--opt", "device=tmpfs", "--opt", "o=size=16m,nr_inodes=2048,mode=1777,uid=0,gid=0,noexec,nosuid,nodev", "tmp"}
	if got := tempVolumeCreateArgs("tmp", "run"); !slices.Equal(got, wantTmpfsVolume) {
		t.Fatalf("tmpfs volume argv=%v want %v", got, wantTmpfsVolume)
	}
	args := containerArgs(testImageID, "/input", "/capture", "/r/container.cid", "matlatl-eval-nonce", "matlatl-eval-nonce-workspace", "matlatl-eval-nonce-tmp", "all", "attempt-a", "run-a", "correct")
	if runtime.GOOS == "linux" && (!strings.Contains(strings.Join(args, " "), ":/input:ro,Z") || !strings.Contains(strings.Join(args, " "), ":/capture:rw,Z")) {
		t.Fatal("SELinux relabel missing")
	}
	if got := imageBuildArgs("docker", true, "/ctx", "tag", "run"); strings.Join(got[:3], " ") != "buildx build --load" {
		t.Fatalf("buildx argv=%v", got)
	}
	if got := imageBuildArgs("docker", false, "/ctx", "tag", "run"); got[0] != "build" || slices.Contains(got, "--load") {
		t.Fatalf("portable docker argv=%v", got)
	}
	if got := imageBuildArgs("podman", false, "/ctx", "tag", "run"); got[0] != "build" || slices.Contains(got, "--load") {
		t.Fatalf("podman argv=%v", got)
	}
}

func TestBuildContextContainsOnlyThreeRequiredFiles(t *testing.T) {
	contextDir, err := buildContext(context.Background(), repositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(contextDir); err != nil {
			t.Error(err)
		}
	})
	entries, err := os.ReadDir(contextDir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	if strings.Join(names, ",") != "Containerfile,fake-opencode,supervisor" {
		t.Fatalf("build context=%v", names)
	}
	if _, err := os.Stat(filepath.Join(contextDir, "go.mod")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("repository sentinel leaked into restricted build context")
	}
}

func TestControlProtocolValidation(t *testing.T) {
	valid := []byte("{\"type\":\"exposure\",\"attemptId\":\"attempt-a\",\"time\":\"2026-08-17T00:00:00Z\",\"exposed\":true}\n{\"type\":\"terminal\",\"attemptId\":\"attempt-a\",\"exposed\":true,\"status\":\"completed\"}\n")
	if frames, err := parseControl(valid, "attempt-a"); err != nil || len(frames) != 2 {
		t.Fatalf("frames=%v err=%v", frames, err)
	}
	if _, err := parseControl([]byte("{\"type\":\"exposure\",\"attemptId\":\"other\"}\n"), "attempt-a"); err == nil {
		t.Fatal("mismatched attempt accepted")
	}
}

func TestCheckerPreservesExactPathContract(t *testing.T) {
	root := repositoryRoot(t)
	for _, tc := range []struct {
		answer string
		pass   bool
	}{{"docs/operate.md", true}, {"README.md", false}} {
		dir := t.TempDir()
		content, _ := json.Marshal(Outcome{SchemaVersion: 1, Status: "completed", Answer: tc.answer})
		if err := os.WriteFile(filepath.Join(dir, "outcome.json"), content, 0o600); err != nil {
			t.Fatal(err)
		}
		_, pass, err := Check(dir, filepath.Join(root, "eval"), "canonical-navigation")
		if err != nil || pass != tc.pass {
			t.Fatalf("answer=%s pass=%v err=%v", tc.answer, pass, err)
		}
	}
}

func TestCheckerAcceptsExposedHostFailuresAndPreservesClassification(t *testing.T) {
	root := repositoryRoot(t)
	for _, status := range []string{"agent-timeout", "budget-exhausted", "agent-protocol-failure", "environment-failure", "mcp-failure", "provider-failure", "evaluator-failure"} {
		dir := t.TempDir()
		content, _ := json.Marshal(Outcome{SchemaVersion: 1, Status: manifest.Status(status)})
		if err := os.WriteFile(filepath.Join(dir, "outcome.json"), content, 0o600); err != nil {
			t.Fatal(err)
		}
		result, passed, err := Check(dir, filepath.Join(root, "eval"), "canonical-navigation")
		if err != nil || passed || result["score"] != float64(0) {
			t.Fatalf("status=%s result=%v passed=%v err=%v", status, result, passed, err)
		}
		tags := result["tags"].([]string)
		if len(tags) != 1 || tags[0] != status {
			t.Fatalf("status=%s tags=%v", status, tags)
		}
	}
	for _, status := range []string{"invalid-task", "infra-exhausted"} {
		dir := t.TempDir()
		content, _ := json.Marshal(Outcome{SchemaVersion: 1, Status: manifest.Status(status)})
		_ = os.WriteFile(filepath.Join(dir, "outcome.json"), content, 0o600)
		if _, _, err := Check(dir, filepath.Join(root, "eval"), "canonical-navigation"); err == nil {
			t.Fatalf("accepted non-gradeable status %s", status)
		}
	}
}

func TestSmevalsWrapperWiringHermetic(t *testing.T) {
	root := repositoryRoot(t)
	bin := t.TempDir()
	fakeGo := filepath.Join(bin, "go")
	script := "#!/bin/sh\nprintf '%s|%s|%s|%s|%s\\n' \"$MATLATL_AGENT_ARM\" \"$MATLATL_SCHEDULED_RUN_ID\" \"$MATLATL_ATTEMPT_ID\" \"$MATLATL_EVAL_ROOT\" \"$*\"\n"
	if err := os.WriteFile(fakeGo, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, arm := range []string{"baseline", "all"} {
		cmd := exec.Command(filepath.Join(root, "eval/agent-outcome/runners", arm))
		cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s wrapper: %v: %s", arm, err, out)
		}
		want := arm + "|smevals-canonical-navigation|smevals-canonical-navigation-" + arm + "-1|" + filepath.Join(root, "eval") + "|run ./eval/cmd/smevals-adapter run"
		if strings.TrimSpace(string(out)) != want {
			t.Fatalf("%s wrapper=%q want %q", arm, out, want)
		}
	}
	cmd := exec.Command(filepath.Join(root, "eval/agent-outcome/checkers/exact-path"))
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("checker wrapper: %v: %s", err, out)
	}
	want := "|||" + filepath.Join(root, "eval") + "|run ./eval/cmd/smevals-adapter check"
	if strings.TrimSpace(string(out)) != want {
		t.Fatalf("checker wrapper=%q want %q", out, want)
	}
}

func flagValue(args []string, key string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}
func mountSource(args []string, dst string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] != "--volume" {
			continue
		}
		parts := strings.Split(args[i+1], ":")
		if len(parts) >= 2 && parts[1] == dst {
			return parts[0]
		}
	}
	return ""
}
func envValue(args []string, key string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--env" && strings.HasPrefix(args[i+1], key+"=") {
			return strings.TrimPrefix(args[i+1], key+"=")
		}
	}
	return ""
}
func decodeFile(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
func canonicalInstruction(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "eval/tasks/canonical-navigation/v1/task.json"))
	if err != nil {
		t.Fatal(err)
	}
	var task struct {
		Instruction string `json:"instruction"`
	}
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatal(err)
	}
	return task.Instruction
}
func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "../../.."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

var _ = mcpserver.EndpointPath
