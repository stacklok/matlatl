package nimbus

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
)

// VerificationResult is the score and bounded diagnostic for one patch case.
type VerificationResult struct {
	Task, Case, Status string
	Score              int
	Diagnostic         string
}

type hiddenCaseSet struct {
	SchemaVersion int          `json:"schemaVersion"`
	TaskID        string       `json:"taskId"`
	Cases         []hiddenCase `json:"cases"`
}

type hiddenCase struct {
	Operation string          `json:"operation"`
	Input     json.RawMessage `json:"input"`
	Expected  json.RawMessage `json:"expected"`
}

type adapterRequest struct {
	Sequence  uint64          `json:"sequence"`
	Challenge string          `json:"challenge"`
	Operation string          `json:"operation"`
	Input     json.RawMessage `json:"input"`
}

type adapterResponse struct {
	Sequence  uint64          `json:"sequence"`
	Challenge string          `json:"challenge"`
	Result    json.RawMessage `json:"result"`
}

// InspectVerifier returns the locally prepared immutable verifier identity.
func InspectVerifier(ctx context.Context, runtime string) (RuntimeImage, error) {
	exe, err := runtimeExecutable(runtime)
	if err != nil {
		return RuntimeImage{}, err
	}
	id, platform, err := inspectImage(ctx, exe, VerifierImage)
	if err != nil {
		return RuntimeImage{}, fmt.Errorf("%s verifier image not prepared; install/start runtime and run verify --prepare: %w", runtime, err)
	}
	return RuntimeImage{Runtime: runtime, ImageID: id, Platform: platform}, nil
}

// PrepareVerifier pulls and inspects the pinned verifier image.
func PrepareVerifier(ctx context.Context, runtime string) (RuntimeImage, error) {
	exe, err := runtimeExecutable(runtime)
	if err != nil {
		return RuntimeImage{}, err
	}
	// exe is resolved only after runtimeExecutable accepts docker or podman.
	cmd := exec.CommandContext(ctx, exe, "pull", VerifierImage) //nolint:gosec // Runtime executable and immutable image are allowlisted.
	out, _, err := boundedRun(cmd, 1<<20)
	if err != nil {
		return RuntimeImage{}, fmt.Errorf("%s verifier pull: %w: %s", runtime, err, out)
	}
	id, platform, err := inspectImage(ctx, exe, VerifierImage)
	if err != nil {
		return RuntimeImage{}, err
	}
	return RuntimeImage{Runtime: runtime, ImageID: id, Platform: platform}, nil
}

// Verify scores all private patch cases in isolated containers.
func Verify(ctx context.Context, s *Suite, runtime string) ([]VerificationResult, error) {
	if err := CheckFreeze(s); err != nil {
		return nil, fmt.Errorf("nimbus freeze gate failed; run nimbus freeze after the documented safe refreeze workflow: %w", err)
	}
	exe, err := runtimeExecutable(runtime)
	if err != nil {
		return nil, err
	}
	id, platform, err := inspectImage(ctx, exe, VerifierImage)
	if err != nil {
		return nil, fmt.Errorf("verifier image not prepared; rerun with --prepare: %w", err)
	}
	frozen, err := frozenRuntime(s, runtime)
	if err != nil {
		return nil, err
	}
	if id != frozen.ImageID || platform != frozen.Platform {
		return nil, fmt.Errorf("%s verifier image differs from freeze: got %s %s; prepare and audit before refreezing", runtime, id, platform)
	}
	verifyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	taskResults := make([][]VerificationResult, len(s.Tasks))
	taskErrors := make([]error, len(s.Tasks))
	var wg sync.WaitGroup
	concurrency := make(chan struct{}, 2)
	for i, task := range s.Tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			concurrency <- struct{}{}
			defer func() { <-concurrency }()
			taskResults[i], taskErrors[i] = verifyTask(verifyCtx, exe, s, task)
			if taskErrors[i] != nil {
				cancel()
			}
		}()
	}
	wg.Wait()
	var results []VerificationResult
	for i := range s.Tasks {
		results = append(results, taskResults[i]...)
		if taskErrors[i] != nil {
			return results, taskErrors[i]
		}
	}
	return results, nil
}

func verifyTask(ctx context.Context, exe string, s *Suite, task Task) ([]VerificationResult, error) {
	patches, _ := patchesFor(s, task.ID)
	var results []VerificationResult
	for _, pc := range patches.Cases {
		result := VerificationResult{Task: task.ID, Case: pc.Name, Status: "evaluator-failure"}
		base, err := os.MkdirTemp("", "nimbus-verify-*")
		if err != nil {
			return results, err
		}
		workspace := filepath.Join(base, "workspace")
		_, err = Materialize(s, task.ID, workspace)
		if err == nil {
			err = ApplyCase(workspace, pc)
		}
		if err == nil {
			err = makeCandidateReadable(workspace)
		}
		var output string
		if err == nil {
			output, err = verifyCase(ctx, exe, s, task.ID, workspace, base)
			switch {
			case err == nil:
				result.Status, result.Score = "completed", 1
			case exitError(err):
				result.Status, result.Score = "completed", 0
				result.Diagnostic = boundedDiagnostic(output)
			default:
				result.Diagnostic = boundedDiagnostic(err.Error() + ": " + output)
			}
		}
		removeErr := os.RemoveAll(base)
		if removeErr != nil {
			return results, fmt.Errorf("remove verifier host workspace: %w", removeErr)
		}
		results = append(results, result)
		if result.Status != "completed" {
			return results, fmt.Errorf("verifier evaluator failure for %s/%s: %s", task.ID, pc.Name, result.Diagnostic)
		}
		if (result.Score == 1) != pc.ExpectPass {
			return results, fmt.Errorf("verifier score mismatch for %s/%s: got %d: %s", task.ID, pc.Name, result.Score, result.Diagnostic)
		}
	}
	return results, nil
}

func makeCandidateReadable(workspace string) (retErr error) {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, root.Close()) }()
	return fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		mode := fs.FileMode(0o644)
		if entry.IsDir() {
			mode = 0o755
		}
		return root.Chmod(name, mode)
	})
}

func verifyCase(ctx context.Context, exe string, s *Suite, taskID, workspace, base string) (output string, retErr error) {
	cases, err := loadHiddenCases(s, taskID)
	if err != nil {
		return "", err
	}
	var normalChecks []string
	for _, task := range s.Tasks {
		if task.ID == taskID {
			normalChecks = task.NormalChecks
			break
		}
	}
	compileCommand, err := compileScript(normalChecks)
	if err != nil {
		return "", err
	}
	adapter, err := evalfs.Read(s.Root, "private/adapter.go.txt")
	if err != nil {
		return "", err
	}
	adapterDir := filepath.Join(workspace, "cmd", "nimbus-adapter")
	if _, err := os.Lstat(adapterDir); err == nil {
		return "", verificationFailure{errors.New("candidate uses reserved path cmd/nimbus-adapter")}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	trustedDir := filepath.Join(base, "trusted-adapter")
	// The adapter contains no hidden expectations; it is made readable solely so
	// rootful Docker can bind-mount it after all capabilities are dropped.
	if err := os.Mkdir(trustedDir, 0o755); err != nil { //nolint:gosec // Public protocol adapter, not private verifier data.
		return "", err
	}
	trustedAdapter := filepath.Join(trustedDir, "main.go")
	if err := os.WriteFile(trustedAdapter, adapter, 0o644); err != nil { //nolint:gosec // Public protocol adapter, not private verifier data.
		return "", err
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(nonceBytes)
	label := verifierLabelKey + "=" + nonce
	containers := []ownedContainer{
		{name: "matlatl-nimbus-check-" + nonce, cidfile: filepath.Join(base, "check.cid"), label: label},
		{name: "matlatl-nimbus-build-" + nonce, cidfile: filepath.Join(base, "build.cid"), label: label},
		{name: "matlatl-nimbus-run-" + nonce, cidfile: filepath.Join(base, "run.cid"), label: label},
	}
	volume := ownedVolume{name: "matlatl-nimbus-output-" + nonce, label: label}
	defer func() {
		retErr = errors.Join(retErr, cleanupVerifierResources(exe, containers, volume))
	}()
	for _, container := range containers {
		if err := ensureContainerAbsent(exe, container.name); err != nil {
			return "", err
		}
	}
	if err := createOwnedVolume(exe, volume); err != nil {
		return "", err
	}

	checkCtx, cancelCheck := context.WithTimeout(ctx, 180*time.Second)
	defer cancelCheck()
	checkArgs := append([]string{"run", "--name", containers[0].name, "--cidfile", containers[0].cidfile}, lockedContainerArgs(label, "1g", "2", "128", true)...)
	checkArgs = append(checkArgs,
		"--volume", workspace+":/candidate:ro,Z", "--workdir", "/candidate",
		"--env", "HOME=/tmp", "--env", "GOCACHE=/tmp/go-cache", "--env", "GOTMPDIR=/tmp", "--env", "CGO_ENABLED=0",
		VerifierImage, "/bin/sh", "-c", compileCommand)
	stdout, stderr, runErr := runOwnedContainer(checkCtx, exe, containers[0], checkArgs, nil, 256<<10)
	output = stdout + stderr
	if runErr != nil {
		if evaluatorFailure(runErr) || containerSetupFailure(runErr) {
			return output, runErr
		}
		return output, verificationFailure{runErr}
	}

	buildCtx, cancelBuild := context.WithTimeout(ctx, 180*time.Second)
	defer cancelBuild()
	buildArgs := append([]string{"run", "--detach", "--name", containers[1].name, "--cidfile", containers[1].cidfile}, lockedContainerArgs(label, "1g", "2", "128", true)...)
	buildArgs = append(buildArgs,
		"--volume", workspace+":/candidate:ro,Z", "--volume", trustedAdapter+":/trusted-adapter/main.go:ro,Z", "--volume", volume.name+":/output:rw",
		"--workdir", "/candidate", "--env", "HOME=/tmp", "--env", "GOCACHE=/tmp/go-cache", "--env", "GOTMPDIR=/tmp", "--env", "CGO_ENABLED=0",
		VerifierImage, "/bin/sh", "-c", adapterBuildCommand)
	stdout, stderr, runErr = runDetachedBuild(buildCtx, exe, containers[1], buildArgs)
	output = stdout + stderr
	if runErr != nil {
		if evaluatorFailure(runErr) || containerSetupFailure(runErr) {
			return output, runErr
		}
		return output, verificationFailure{runErr}
	}

	stdin, challenge, err := encodeRequests(cases)
	if err != nil {
		return output, err
	}
	runCtx, cancelRun := context.WithTimeout(ctx, 60*time.Second)
	defer cancelRun()
	runArgs := append([]string{"run", "--interactive", "--name", containers[2].name, "--cidfile", containers[2].cidfile, "--user=65532:65532"}, lockedContainerArgs(label, "256m", "1", "64", false)...)
	runArgs = append(runArgs, "--volume", volume.name+":/output:ro", "--workdir", "/tmp", VerifierImage, "/output/candidate-adapter")
	stdout, stderr, runErr = runOwnedContainer(runCtx, exe, containers[2], runArgs, stdin, 64<<10)
	output = stderr
	if runErr != nil {
		if evaluatorFailure(runErr) || containerSetupFailure(runErr) {
			return output, runErr
		}
		return output, verificationFailure{runErr}
	}
	if err := verifyResponses([]byte(stdout), cases, challenge); err != nil {
		return boundedDiagnostic(err.Error()), verificationFailure{err}
	}
	return output, nil
}

func compileScript(normalChecks []string) (string, error) {
	if len(normalChecks) == 0 {
		return "", errors.New("task has no normal checks")
	}
	commands := []string{"set -eu"}
	for _, check := range normalChecks {
		if check != "go test ./..." {
			return "", fmt.Errorf("unsupported normal check %q", check)
		}
		commands = append(commands,
			`(sleep 60; kill -KILL 1) & watchdog=$!`,
			"set +e", check, `status=$?`, "set -e",
			`kill "$watchdog" 2>/dev/null || true`, `wait "$watchdog" 2>/dev/null || true`,
			`[ "$status" -eq 0 ] || exit "$status"`)
	}
	return strings.Join(commands, "\n"), nil
}

const adapterBuildCommand = `set -eu
go build -buildvcs=false -mod=readonly -trimpath -ldflags='-s -w -buildid=' -o /output/candidate-adapter /trusted-adapter/main.go
: > /output/.ready
exec sleep 300`

func containerSetupFailure(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() >= 125 && exit.ExitCode() <= 127
}

func lockedContainerArgs(label, memory, cpus, pids string, tmpExec bool) []string {
	tmpOptions := "rw,nosuid,nodev,noexec,size=536870912,mode=1777"
	if tmpExec {
		tmpOptions = "rw,nosuid,nodev,exec,size=536870912,mode=1777"
	}
	return []string{"--pull=never", "--network=none", "--read-only", "--pids-limit=" + pids, "--cpus=" + cpus, "--memory=" + memory, "--memory-swap=" + memory,
		"--cap-drop=ALL", "--security-opt=no-new-privileges", "--ulimit", "nofile=256:256", "--ulimit", "core=0:0", "--ulimit", "fsize=67108864:67108864",
		"--tmpfs", "/tmp:" + tmpOptions, "--label", label}
}

func loadHiddenCases(s *Suite, taskID string) (hiddenCaseSet, error) {
	b, err := evalfs.Read(s.Root, "private/"+taskID+"/cases.json")
	if err != nil {
		return hiddenCaseSet{}, err
	}
	var set hiddenCaseSet
	if err := decodeStrict(b, &set); err != nil {
		return set, err
	}
	if set.SchemaVersion != 1 || set.TaskID != taskID || len(set.Cases) == 0 || len(set.Cases) > 64 {
		return set, errors.New("invalid hidden case set")
	}
	for _, c := range set.Cases {
		if c.Operation == "" || len(c.Input) == 0 || len(c.Expected) == 0 || !json.Valid(c.Input) || !json.Valid(c.Expected) {
			return set, errors.New("invalid hidden case")
		}
	}
	return set, nil
}

func encodeRequests(set hiddenCaseSet) ([]byte, string, error) {
	challengeBytes := make([]byte, 32)
	if _, err := rand.Read(challengeBytes); err != nil {
		return nil, "", err
	}
	challenge := hex.EncodeToString(challengeBytes)
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	for i, c := range set.Cases {
		req := adapterRequest{Sequence: uint64(i + 1), Challenge: challenge, Operation: c.Operation, Input: c.Input}
		if err := encoder.Encode(req); err != nil {
			return nil, "", err
		}
	}
	return data.Bytes(), challenge, nil
}

func verifyResponses(data []byte, set hiddenCaseSet, challenge string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	for i, c := range set.Cases {
		var response adapterResponse
		if err := decoder.Decode(&response); err != nil {
			return fmt.Errorf("required response %d missing or malformed", i+1)
		}
		if response.Sequence != uint64(i+1) || response.Challenge != challenge || !jsonEqual(response.Result, c.Expected) {
			return fmt.Errorf("required response %d did not match", i+1)
		}
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("extra or malformed adapter response")
	}
	return nil
}

func jsonEqual(a, b []byte) bool {
	var av, bv any
	if decodeStrict(a, &av) != nil || decodeStrict(b, &bv) != nil {
		return false
	}
	ac, _ := json.Marshal(av)
	bc, _ := json.Marshal(bv)
	return bytes.Equal(ac, bc)
}

type ownedContainer struct{ name, cidfile, label string }
type ownedVolume struct{ name, label string }

const (
	verifierLabelKey   = "io.stacklok.matlatl.nimbus-verifier"
	outputVolumeBytes  = "67108864"
	outputVolumeInodes = "64"
)

const processWaitDelay = 5 * time.Second

func runDetachedBuild(ctx context.Context, exe string, container ownedContainer, args []string) (string, string, error) {
	// exe is resolved only from the docker/podman allowlist; args are assembled
	// from fixed flags plus nonce-derived identifiers in verifyCase.
	cmd := exec.CommandContext(ctx, exe, args...) //nolint:gosec // Runtime and argument vocabulary are constrained above.
	stdout, stderr, err := boundedRun(cmd, 256<<10)
	if err != nil {
		if ctx.Err() != nil {
			return stdout, stderr, evaluatorRuntimeFailure{ctx.Err()}
		}
		return stdout, stderr, err
	}
	id, err := verifyContainerOwnership(ctx, exe, container)
	if err != nil {
		return stdout, stderr, evaluatorRuntimeFailure{err}
	}
	for {
		// exe is docker or podman, id is a verified 64-byte hex container ID, and
		// the shell fragment is a fixed readiness check.
		ready := exec.CommandContext(ctx, exe, "exec", id, "/bin/sh", "-c", "test -f /output/.ready") //nolint:gosec // Strict runtime/ID allowlists and fixed command.
		_, _, readyErr := boundedRun(ready, 4096)
		if readyErr == nil {
			return stdout, stderr, nil
		}
		if ctx.Err() != nil {
			return stdout, stderr, evaluatorRuntimeFailure{ctx.Err()}
		}
		inspect := exec.CommandContext(ctx, exe, "inspect", "--format", "{{.State.Running}} {{.State.ExitCode}}", id) //nolint:gosec // Allowlisted runtime and verified container ID.
		state, inspectStderr, inspectErr := boundedRun(inspect, 4096)
		if inspectErr != nil {
			return stdout, stderr + inspectStderr, evaluatorRuntimeFailure{inspectErr}
		}
		if !strings.HasPrefix(strings.TrimSpace(state), "true ") {
			logs := exec.CommandContext(ctx, exe, "logs", id) //nolint:gosec // Allowlisted runtime and verified container ID.
			logOut, logErr, _ := boundedRun(logs, 256<<10)
			return stdout, stderr + logOut + logErr, verificationFailure{fmt.Errorf("adapter build container stopped: %s", strings.TrimSpace(state))}
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return stdout, stderr, evaluatorRuntimeFailure{ctx.Err()}
		case <-timer.C:
		}
	}
}

func verifyContainerOwnership(ctx context.Context, exe string, container ownedContainer) (string, error) {
	idBytes, err := os.ReadFile(container.cidfile)
	if err != nil {
		return "", fmt.Errorf("read verifier cidfile: %w", err)
	}
	id := strings.TrimSpace(string(idBytes))
	decoded, decodeErr := hex.DecodeString(id)
	if decodeErr != nil || len(decoded) != 32 {
		return "", errors.New("runtime wrote malformed verifier cidfile")
	}
	inspect := exec.CommandContext(ctx, exe, "inspect", "--format", "{{index .Config.Labels \""+verifierLabelKey+"\"}}", id) //nolint:gosec // Allowlisted runtime and verified container ID.
	stdout, stderr, err := boundedRun(inspect, 4096)
	want := strings.TrimPrefix(container.label, verifierLabelKey+"=")
	if err != nil || strings.TrimSpace(stdout) != want {
		return "", fmt.Errorf("verify verifier container ownership: %w: %s", err, boundedDiagnostic(stdout+stderr))
	}
	return id, nil
}

// runOwnedContainer couples the runtime client process to the container it owns.
// A runtime client can outlive context cancellation while container pipes remain
// open, so cancellation must remove the container independently of Cmd.Wait.
func runOwnedContainer(ctx context.Context, exe string, container ownedContainer, args []string, stdin []byte, limit int) (string, string, error) {
	// exe is allowlisted and args are assembled from fixed OCI flags, immutable
	// image identity, validated suite paths, and nonce-generated resource names.
	cmd := exec.Command(exe, args...) //nolint:gosec // Runtime and complete container argv are constrained by verifyCase.
	var stdout, stderr killWriter
	stdout.limit, stderr.limit = limit, limit
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = processWaitDelay
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	overflow := make(chan struct{}, 1)
	signalOverflow := func() {
		select {
		case overflow <- struct{}{}:
		default:
		}
	}
	stdout.kill, stderr.kill = signalOverflow, signalOverflow
	if err := cmd.Start(); err != nil {
		return stdout.String(), stderr.String(), evaluatorRuntimeFailure{err}
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	var runErr error
	aborted := false
	select {
	case runErr = <-waited:
		if stdout.Exceeded() || stderr.Exceeded() {
			runErr, aborted = errOutputLimit, true
		}
	case <-ctx.Done():
		runErr, aborted = ctx.Err(), true
	case <-overflow:
		runErr, aborted = errOutputLimit, true
	}
	if !aborted {
		ownershipCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, ownershipErr := verifyContainerOwnership(ownershipCtx, exe, container)
		cancel()
		cleanupErr := cleanupOneContainer(exe, container)
		if ownershipErr != nil || cleanupErr != nil {
			return stdout.String(), stderr.String(), evaluatorRuntimeFailure{errors.Join(ownershipErr, cleanupErr)}
		}
		return stdout.String(), stderr.String(), runErr
	}

	// Do not wait for a stuck client before asking the runtime to remove the
	// container: removal closes the conmon/container pipe ends that block Wait.
	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- cleanupOneContainer(exe, container) }()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	select {
	case <-waited:
	case <-time.After(processWaitDelay + time.Second):
		runErr = errors.Join(runErr, errors.New("runtime client did not exit after kill"))
	}
	firstCleanupErr := <-cleanupDone
	// Repeat after the client exits to close the create/remove race, and verify
	// the named container is absent before returning.
	finalCleanupErr := cleanupOneContainer(exe, container)
	if finalCleanupErr != nil {
		runErr = errors.Join(runErr, firstCleanupErr, finalCleanupErr)
	}
	return stdout.String(), stderr.String(), evaluatorRuntimeFailure{runErr}
}

var errOutputLimit = errors.New("command output exceeded limit")

type evaluatorRuntimeFailure struct{ error }

func evaluatorFailure(err error) bool {
	var failure evaluatorRuntimeFailure
	return errors.As(err, &failure) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func cleanupVerifierResources(exe string, containers []ownedContainer, volume ownedVolume) error {
	var errs []error
	for _, container := range containers {
		if err := cleanupOneContainer(exe, container); err != nil {
			errs = append(errs, err)
		}
	}
	if err := cleanupOwnedVolume(exe, volume); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func ensureContainerAbsent(exe, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	inspect := exec.CommandContext(ctx, exe, "inspect", name) //nolint:gosec // Allowlisted runtime and nonce-generated container name.
	stdout, stderr, err := boundedRun(inspect, 4096)
	if err == nil || !resourceAbsent([]byte(stdout+stderr)) {
		return fmt.Errorf("verifier container name %s is not absent", name)
	}
	return nil
}

func createOwnedVolume(exe string, volume ownedVolume) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	options := "size=" + outputVolumeBytes + ",nr_inodes=" + outputVolumeInodes + ",mode=0755,exec"
	create := exec.CommandContext(ctx, exe, "volume", "create", "--label", volume.label, "--opt", "type=tmpfs", "--opt", "device=tmpfs", "--opt", "o="+options, volume.name) //nolint:gosec // Runtime allowlist and nonce-generated label/name.
	stdout, stderr, err := boundedRun(create, 4096)
	if err != nil {
		return fmt.Errorf("create verifier output volume: %w: %s", err, boundedDiagnostic(stdout+stderr))
	}
	inspect := exec.CommandContext(ctx, exe, "volume", "inspect", "--format", "{{index .Labels \""+verifierLabelKey+"\"}}", volume.name) //nolint:gosec // Runtime allowlist and nonce-generated volume name.
	stdout, stderr, err = boundedRun(inspect, 4096)
	want := strings.TrimPrefix(volume.label, verifierLabelKey+"=")
	if err != nil || strings.TrimSpace(stdout) != want {
		return fmt.Errorf("verify verifier output volume ownership: %w: %s", err, boundedDiagnostic(stdout+stderr))
	}
	return nil
}

func cleanupOwnedVolume(exe string, volume ownedVolume) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	inspect := exec.CommandContext(ctx, exe, "volume", "inspect", "--format", "{{index .Labels \""+verifierLabelKey+"\"}}", volume.name) //nolint:gosec // Runtime allowlist and nonce-generated volume name.
	stdout, stderr, inspectErr := boundedRun(inspect, 4096)
	if inspectErr == nil {
		want := strings.TrimPrefix(volume.label, verifierLabelKey+"=")
		if strings.TrimSpace(stdout) != want {
			return fmt.Errorf("refusing cleanup of unowned verifier volume %s", volume.name)
		}
		remove := exec.CommandContext(ctx, exe, "volume", "rm", "--force", volume.name) //nolint:gosec // Ownership-checked nonce-generated volume name.
		removeOut, removeStderr, err := boundedRun(remove, 4096)
		if err != nil {
			return fmt.Errorf("remove verifier output volume: %w: %s", err, boundedDiagnostic(removeOut+removeStderr))
		}
	} else if !resourceAbsent([]byte(stdout + stderr)) {
		return fmt.Errorf("inspect verifier output volume: %w: %s", inspectErr, boundedDiagnostic(stdout+stderr))
	}
	inspect = exec.CommandContext(ctx, exe, "volume", "inspect", volume.name) //nolint:gosec // Runtime allowlist and nonce-generated volume name.
	stdout, stderr, err := boundedRun(inspect, 4096)
	if err == nil || !resourceAbsent([]byte(stdout+stderr)) {
		return fmt.Errorf("verifier output volume %s remained after cleanup", volume.name)
	}
	return nil
}

func cleanupOneContainer(exe string, container ownedContainer) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	idBytes, readErr := os.ReadFile(container.cidfile)
	id := strings.TrimSpace(string(idBytes))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read verifier cidfile: %w", readErr)
	}
	identity := id
	decoded, decodeErr := hex.DecodeString(identity)
	if identity == "" || decodeErr != nil || len(decoded) != 32 {
		identity = container.name
	}
	// identity is either a validated 64-byte hex ID or a nonce-generated name.
	inspect := exec.CommandContext(cleanupCtx, exe, "inspect", "--format", "{{index .Config.Labels \""+verifierLabelKey+"\"}}", identity) //nolint:gosec // Allowlisted runtime and validated/generated identity.
	labelOut, inspectStderr, inspectErr := boundedRun(inspect, 4096)
	labelValue := []byte(labelOut + inspectStderr)
	if inspectErr == nil {
		want := strings.TrimPrefix(container.label, "io.stacklok.matlatl.nimbus-verifier=")
		if strings.TrimSpace(labelOut) != want {
			return fmt.Errorf("refusing cleanup of unowned verifier container %s", identity)
		}
		remove := exec.CommandContext(cleanupCtx, exe, "rm", "--force", identity) //nolint:gosec // Ownership-checked validated/generated identity.
		removeOut, removeStderr, err := boundedRun(remove, 4096)
		if err != nil {
			return fmt.Errorf("remove verifier container: %w: %s", err, boundedDiagnostic(removeOut+removeStderr))
		}
	} else if !resourceAbsent(labelValue) {
		return fmt.Errorf("inspect verifier container: %w: %s", inspectErr, boundedDiagnostic(string(labelValue)))
	}
	if err := os.Remove(container.cidfile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove verifier cidfile: %w", err)
	}
	inspect = exec.CommandContext(cleanupCtx, exe, "inspect", container.name) //nolint:gosec // Runtime allowlist and nonce-generated container name.
	remainingOut, remainingStderr, err := boundedRun(inspect, 4096)
	remaining := []byte(remainingOut + remainingStderr)
	if err == nil || !resourceAbsent(remaining) {
		return fmt.Errorf("verifier container %s remained after cleanup", container.name)
	}
	return nil
}

func resourceAbsent(out []byte) bool {
	message := strings.ToLower(strings.TrimSpace(string(out)))
	return message == "[]" || strings.Contains(message, "no such") || strings.Contains(message, "not found") || strings.Contains(message, "does not exist")
}

func runtimeExecutable(runtime string) (string, error) {
	if runtime != "docker" && runtime != "podman" {
		return "", fmt.Errorf("unsupported OCI runtime %q", runtime)
	}
	p, err := exec.LookPath(runtime)
	if err != nil {
		return "", fmt.Errorf("%s unavailable", runtime)
	}
	return p, nil
}

func inspectImage(ctx context.Context, exe, image string) (string, string, error) {
	cmd := exec.CommandContext(ctx, exe, "image", "inspect", "--format", "{{.Id}} {{.Os}}/{{.Architecture}}", image) //nolint:gosec // Allowlisted runtime and pinned verifier image.
	out, _, err := boundedRun(cmd, 4096)
	if err != nil {
		return "", "", err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return "", "", fmt.Errorf("runtime returned malformed image inspection %q", strings.TrimSpace(out))
	}
	id, platform := fields[0], fields[1]
	if len(id) == 64 {
		id = "sha256:" + id
	}
	if !strings.HasPrefix(id, "sha256:") || len(id) != 71 || (platform != "linux/amd64" && platform != "linux/arm64") {
		return "", "", fmt.Errorf("runtime returned invalid image identity %q %q", id, platform)
	}
	return id, platform, nil
}

func frozenRuntime(s *Suite, runtime string) (RuntimeImage, error) {
	b, err := evalfs.Read(s.Root, "freeze.json")
	if err != nil {
		return RuntimeImage{}, err
	}
	var freeze Freeze
	if err := decodeStrict(b, &freeze); err != nil {
		return RuntimeImage{}, err
	}
	for _, image := range freeze.Toolchain.RuntimeImages {
		if image.Runtime == runtime {
			return image, nil
		}
	}
	return RuntimeImage{}, fmt.Errorf("runtime %s has no frozen verifier image; inspect and safely refreeze", runtime)
}

func boundedRun(cmd *exec.Cmd, limit int) (string, string, error) {
	var stdout, stderr killWriter
	stdout.limit, stderr.limit = limit, limit
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = processWaitDelay
	if err := cmd.Start(); err != nil {
		return stdout.String(), stderr.String(), err
	}
	kill := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	stdout.kill, stderr.kill = kill, kill
	err := cmd.Wait()
	if stdout.Exceeded() || stderr.Exceeded() {
		return stdout.String(), stderr.String(), errOutputLimit
	}
	return stdout.String(), stderr.String(), err
}

type killWriter struct {
	mu sync.Mutex
	bytes.Buffer
	limit    int
	exceeded bool
	kill     func()
}

func (w *killWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	remain := w.limit - w.Len()
	if remain > 0 {
		if len(p) > remain {
			_, _ = w.Buffer.Write(p[:remain])
		} else {
			_, _ = w.Buffer.Write(p)
		}
	}
	if n > remain {
		w.exceeded = true
		if w.kill != nil {
			w.kill()
		}
	}
	return n, nil
}

func (w *killWriter) Exceeded() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.exceeded
}

func (w *killWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Buffer.String()
}

type verificationFailure struct{ error }

func exitError(err error) bool {
	var failure verificationFailure
	return errors.As(err, &failure)
}

func boundedDiagnostic(s string) string {
	if len(s) > 4096 {
		return s[:4096]
	}
	return s
}
