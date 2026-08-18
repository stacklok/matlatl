// Command eval-supervisor is PID 1 for the isolated fake evaluation container.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/stacklok/matlatl/internal/infrastructure/mcpserver"
)

const (
	maxControlMessage = 4096
	maxChildOutput    = 1 << 20
	maxWorkspaceBytes = 12 << 20
	childUID          = 65532
	childGID          = 65532
)

var diagnosticAllowlist = map[string]bool{
	"argv.txt": true, "stdin.txt": true, "opencode.json": true,
	"workspace-files.txt": true, "isolation.json": true,
}

type frame struct {
	Type      string `json:"type"`
	AttemptID string `json:"attemptId"`
	Time      string `json:"time,omitempty"`
	Exposed   bool   `json:"exposed"`
	Status    string `json:"status,omitempty"`
	Message   string `json:"message,omitempty"`
}

type workspaceFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256"`
}
type workspaceManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Files         []workspaceFile `json:"files"`
}

func main() {
	if err := run(); err != nil {
		emit(frame{Type: "terminal", AttemptID: attempt(), Status: "environment-failure", Message: bounded(err.Error())})
		os.Exit(1)
	}
}

func run() (retErr error) {
	arm, mode, id := os.Getenv("MATLATL_ARM"), os.Getenv("MATLATL_FAKE_MODE"), attempt()
	if id == "" || (arm != "baseline" && arm != "all") {
		return errors.New("invalid supervisor identity")
	}
	if err := validateTmpfsMount("/workspace", "workspace"); err != nil {
		return err
	}
	if err := validateTmpfsMount("/tmp", "temporary directory"); err != nil {
		return err
	}
	if err := copyInput(); err != nil {
		return fmt.Errorf("prepare workspace: %w", err)
	}
	if err := os.MkdirAll("/tmp/agent-capture", 0o700); err != nil {
		return err
	}
	if err := os.Chown("/tmp/agent-capture", childUID, childGID); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mcpDone chan error
	config := map[string]any{}
	if arm == "all" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("listen MCP: %w", err)
		}
		mcpDone = make(chan error, 1)
		go func() { mcpDone <- mcpserver.ServeListener(ctx, "/workspace", listener) }()
		endpoint := "http://" + listener.Addr().String() + mcpserver.EndpointPath
		if err := waitMCP(ctx, endpoint); err != nil {
			cancel()
			<-mcpDone
			return err
		}
		config["mcp"] = map[string]any{"matlatl": map[string]any{"type": "remote", "url": endpoint, "enabled": true, "oauth": false}}
	}
	configDir := "/tmp/config/opencode"
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	configBytes, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(configDir, "opencode.json"), configBytes, 0o600); err != nil {
		return err
	}
	prompt, err := os.ReadFile("/input/prompt.txt")
	if err != nil || len(prompt) > 65536 {
		return errors.New("invalid input prompt")
	}

	controlR, controlW, err := os.Pipe()
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, controlR.Close()) }()
	cmd := exec.Command("/fake-opencode", "--pure", "run", "--format", "json", "--model", "test/offline-fake", "--dir", "/workspace")
	cmd.Dir, cmd.Stdin, cmd.ExtraFiles = "/workspace", bytes.NewReader(prompt), []*os.File{controlW}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: childUID, Gid: childGID}}
	cmd.Env = []string{"HOME=/tmp/home", "XDG_CONFIG_HOME=/tmp/config", "XDG_DATA_HOME=/tmp/data", "XDG_CACHE_HOME=/tmp/cache", "MATLATL_CAPTURE_DIR=/tmp/agent-capture", "MATLATL_CONTROL_FD=3", "MATLATL_FAKE_MODE=" + mode}
	for _, key := range []string{"MATLATL_HOST_GOLD_SENTINEL", "MATLATL_HOST_TEMP_SENTINEL", "MATLATL_NETWORK_CANARY"} {
		if value := os.Getenv(key); value != "" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	stdout, stderr := &boundedBuffer{limit: maxChildOutput}, &boundedBuffer{limit: maxChildOutput}
	kill := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	stdout.overflow, stderr.overflow = kill, kill
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		_ = controlW.Close()
		return err
	}
	_ = controlW.Close()

	signalCh := make(chan error, 1)
	go func() {
		value, e := io.ReadAll(io.LimitReader(controlR, 65))
		if e == nil && string(value) != "first-model-request\n" {
			e = errors.New("invalid first-model-request control protocol")
		}
		signalCh <- e
	}()
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	exposed, budgetExceeded := false, false
	var runErr error
	for waitCh != nil {
		select {
		case signalErr := <-signalCh:
			signalCh = nil
			if signalErr != nil {
				kill()
				runErr = signalErr
				continue
			}
			exposed = true
			if err := writeCapture("exposure.marker", []byte(id+"\n")); err != nil {
				kill()
				runErr = err
				continue
			}
			emit(frame{Type: "exposure", AttemptID: id, Time: time.Now().UTC().Format(time.RFC3339Nano), Exposed: true})
		case childErr := <-waitCh:
			waitCh = nil
			if runErr == nil {
				runErr = childErr
			}
		case <-ticker.C:
			if mountedSize("/workspace") > maxWorkspaceBytes {
				budgetExceeded = true
				kill()
			}
		}
	}
	if !exposed && signalCh != nil {
		if signalErr := <-signalCh; signalErr == nil {
			exposed = true
			if err := writeCapture("exposure.marker", []byte(id+"\n")); err != nil {
				return err
			}
			emit(frame{Type: "exposure", AttemptID: id, Time: time.Now().UTC().Format(time.RFC3339Nano), Exposed: true})
		} else if runErr == nil {
			runErr = signalErr
		}
	}
	if !exposed {
		return fmt.Errorf("child failed before exposure: %w", runErr)
	}

	status := "completed"
	var exit *exec.ExitError
	if budgetExceeded || stdout.exceeded || stderr.exceeded || (errors.As(runErr, &exit) && exit.ExitCode() == 10) {
		status = "budget-exhausted"
	} else if runErr != nil {
		status = "evaluator-failure"
		if errors.As(runErr, &exit) && exit.ExitCode() == 7 {
			status = "provider-failure"
		}
		if errors.As(runErr, &exit) && exit.ExitCode() == 9 {
			status = "mcp-failure"
		}
	}
	if err := writeCapture("events.jsonl", stdout.bytes()); err != nil {
		status, runErr = "evaluator-failure", err
	}
	if err := exportDiagnostics(); err != nil {
		status, runErr = "evaluator-failure", err
	}
	if status != "budget-exhausted" {
		if err := exportWorkspace(); err != nil {
			status, runErr = "evaluator-failure", err
		}
	}
	cancel()
	if mcpDone != nil {
		if stopErr := <-mcpDone; stopErr != nil && runErr == nil {
			status, runErr = "mcp-failure", stopErr
		}
	}
	emit(frame{Type: "terminal", AttemptID: id, Exposed: true, Status: status, Message: bounded(errorString(runErr))})
	if mode == "malformed-control" {
		_, _ = fmt.Fprintln(os.Stdout, `{}`)
	}
	return nil
}

func validateTmpfsMount(path, description string) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return err
	}
	const tmpfsMagic int64 = 0x01021994
	if stat.Bsize <= 0 {
		return fmt.Errorf("%s has invalid tmpfs block size: %d", description, stat.Bsize)
	}
	blockSize := uint64(stat.Bsize)
	if stat.Blocks > (16<<20)/blockSize {
		return fmt.Errorf("%s has excessive tmpfs capacity: blocks=%d blockSize=%d", description, stat.Blocks, stat.Bsize)
	}
	bytes := stat.Blocks * blockSize
	if statfsType(stat.Type) != tmpfsMagic || bytes > 16<<20 || stat.Files > 2048 {
		return fmt.Errorf("%s is not a byte-and-inode-bounded tmpfs: type=%x bytes=%d inodes=%d", description, stat.Type, bytes, stat.Files)
	}
	return nil
}

func copyInput() (retErr error) {
	source, err := os.OpenRoot("/input/workspace")
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, source.Close()) }()
	destination, err := os.OpenRoot("/workspace")
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, destination.Close()) }()

	err = fs.WalkDir(source.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink in input")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return destination.Mkdir(name, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return errors.New("invalid input file")
		}
		data, err := source.ReadFile(name)
		if err != nil {
			return err
		}
		return destination.WriteFile(name, data, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	return fs.WalkDir(destination.FS(), ".", func(name string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		return destination.Chown(name, childUID, childGID)
	})
}

func exportDiagnostics() error {
	entries, err := os.ReadDir("/tmp/agent-capture")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !diagnosticAllowlist[entry.Name()] {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return fmt.Errorf("invalid diagnostic %s", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join("/tmp/agent-capture", entry.Name()))
		if err != nil {
			return err
		}
		if err = writeCapture(entry.Name(), data); err != nil {
			return err
		}
	}
	return nil
}
func writeCapture(name string, data []byte) error {
	if len(data) > 1<<20 {
		return errors.New("capture file exceeds bound")
	}
	f, err := os.OpenFile(filepath.Join("/capture", name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func exportWorkspace() (retErr error) {
	workspace, err := os.OpenRoot("/workspace")
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, workspace.Close()) }()
	var manifest workspaceManifest
	manifest.SchemaVersion = 1
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	err = fs.WalkDir(workspace.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return errors.New("invalid final workspace entry")
		}
		data, err := workspace.ReadFile(name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		rel := filepath.ToSlash(name)
		manifest.Files = append(manifest.Files, workspaceFile{Path: rel, Size: info.Size(), Mode: uint32(info.Mode().Perm()), SHA256: hex.EncodeToString(sum[:])})
		h := &tar.Header{Name: rel, Mode: int64(info.Mode().Perm()), Size: int64(len(data)), Typeflag: tar.TypeReg}
		if err = tw.WriteHeader(h); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		return err
	}
	if err = tw.Close(); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	if archive.Len() > maxWorkspaceBytes {
		return errors.New("final workspace archive exceeds bound")
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err = writeCapture("final-workspace.json", encoded); err != nil {
		return err
	}
	return writeCapture("final-workspace.tar.gz", archive.Bytes())
}

func mountedSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			if i, x := e.Info(); x == nil {
				total += i.Size()
			}
		}
		return nil
	})
	return total
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	overflow func()
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(p) < remaining {
			remaining = len(p)
		}
		_, _ = b.buffer.Write(p[:remaining])
	}
	if len(p) > remaining {
		b.exceeded = true
		if b.overflow != nil {
			b.overflow()
		}
	}
	return len(p), nil
}
func (b *boundedBuffer) bytes() []byte { return append([]byte(nil), b.buffer.Bytes()...) }
func waitMCP(ctx context.Context, endpoint string) error {
	addr := strings.TrimSuffix(strings.TrimPrefix(endpoint, "http://"), mcpserver.EndpointPath)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return errors.New("MCP did not become ready")
}
func attempt() string { return os.Getenv("MATLATL_ATTEMPT_ID") }
func emit(v frame)    { _ = json.NewEncoder(os.Stdout).Encode(v) }
func bounded(v string) string {
	v = strings.ReplaceAll(v, "\n", " ")
	if len(v) > maxControlMessage {
		return v[:maxControlMessage]
	}
	return v
}
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
