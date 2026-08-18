// Package fakeopencode implements the deterministic in-container OpenCode stand-in.
package fakeopencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is the pinned OpenCode version emulated by the fake CLI.
const Version = "1.18.18"

// Main runs the fake CLI and returns its exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		if _, err := fmt.Fprintln(stdout, Version); err != nil {
			return 8
		}
		return 0
	}
	prompt, err := io.ReadAll(io.LimitReader(stdin, 65537))
	if err != nil || len(prompt) > 65536 {
		if _, writeErr := fmt.Fprintln(stderr, "invalid prompt"); writeErr != nil {
			return 8
		}
		return 8
	}
	capture := os.Getenv("MATLATL_CAPTURE_DIR")
	if err := captureInvocation(capture, args, prompt); err != nil {
		if _, writeErr := fmt.Fprintln(stderr, err); writeErr != nil {
			return 8
		}
		return 8
	}
	if os.Getenv("MATLATL_FAKE_MODE") == "pre-exposure" {
		if _, err := fmt.Fprintln(stderr, "simulated pre-exposure failure"); err != nil {
			return 8
		}
		return 8
	}
	if err := signalExposure(); err != nil {
		if _, writeErr := fmt.Fprintln(stderr, err); writeErr != nil {
			return 8
		}
		return 8
	}
	configRoot := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	var mcpTools []string
	if content, readErr := readRootFile(configRoot, "opencode.json"); readErr == nil && bytes.Contains(content, []byte(`"mcp"`)) {
		var probeErr error
		mcpTools, probeErr = probeMCP(content)
		if probeErr != nil {
			if _, writeErr := fmt.Fprintln(stderr, probeErr); writeErr != nil {
				return 8
			}
			return 9
		}
	}
	switch os.Getenv("MATLATL_FAKE_MODE") {
	case "provider":
		if _, err := fmt.Fprintln(stdout, `{"type":"error","error":{"name":"ProviderError","message":"unavailable"}}`); err != nil {
			return 8
		}
		return 7
	case "evaluator":
		if _, err := fmt.Fprintln(stderr, "simulated evaluator failure"); err != nil {
			return 8
		}
		return 8
	case "protocol":
		if _, err := fmt.Fprintln(stdout, "not-json"); err != nil {
			return 8
		}
		return 0
	case "timeout":
		time.Sleep(10 * time.Minute)
		return 0
	case "output-overflow":
		_, _ = io.CopyN(stdout, zeroReader{}, 2<<20)
		return 0
	case "capture-symlink":
		_ = os.Symlink("/workspace/README.md", filepath.Join(capture, "isolation.json"))
		return 0
	case "workspace-overflow":
		block := make([]byte, 1<<20)
		for i := 0; ; i++ {
			if err := os.WriteFile(filepath.Join("/workspace", fmt.Sprintf("overflow-%02d", i)), block, 0o600); err != nil {
				return 10
			}
		}
	case "tmp-inode-flood":
		for i := 0; ; i++ {
			path := filepath.Join("/tmp", fmt.Sprintf("inode-%06d", i))
			if i%2 == 0 {
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					return 10
				}
			} else if err := os.Mkdir(path, 0o700); err != nil {
				return 10
			}
		}
	case "inode-flood":
		for i := 0; ; i++ {
			path := filepath.Join("/workspace", fmt.Sprintf("inode-%06d", i))
			if i%2 == 0 {
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					return 10
				}
			} else if err := os.Mkdir(path, 0o700); err != nil {
				return 10
			}
		}
	case "adversarial":
		result := map[string]bool{}
		for key, env := range map[string]string{"goldReadable": "MATLATL_HOST_GOLD_SENTINEL", "tempReadable": "MATLATL_HOST_TEMP_SENTINEL"} {
			// This adversarial mode intentionally attempts the host path supplied by
			// the isolation test and records whether the sandbox blocks it.
			_, readErr := os.ReadFile(os.Getenv(env)) //nolint:gosec // Deliberate adversarial host-filesystem probe.
			result[key] = readErr == nil
		}
		writeErr := os.WriteFile("/outside-allowed-mounts", []byte("escape"), 0o600)
		result["outsideWritable"] = writeErr == nil
		result["captureWritable"] = os.WriteFile("/capture/child-escape", []byte("escape"), 0o600) == nil
		if writeErr == nil {
			_ = os.Remove("/outside-allowed-mounts")
		}
		workspaceProbe := "/workspace/.writable-probe"
		workspaceErr := os.WriteFile(workspaceProbe, []byte("create"), 0o600)
		result["hasChownCapability"] = workspaceErr == nil && os.Chown(workspaceProbe, 0, 0) == nil
		if workspaceErr == nil {
			workspaceErr = os.WriteFile(workspaceProbe, []byte("modify"), 0o600)
		}
		if workspaceErr == nil {
			workspaceErr = os.Remove(workspaceProbe)
		}
		result["workspaceWritable"] = workspaceErr == nil
		client := &http.Client{Timeout: 500 * time.Millisecond}
		// This mode intentionally probes the caller-supplied network canary to prove
		// that the locked test container cannot reach it.
		resp, requestErr := client.Get(os.Getenv("MATLATL_NETWORK_CANARY")) //nolint:gosec // Deliberate adversarial isolation probe, not a production fetch.
		result["networkReached"] = requestErr == nil
		if resp != nil {
			_ = resp.Body.Close()
		}
		encoded, _ := json.Marshal(result)
		if err := writeRootFile(capture, "isolation.json", encoded); err != nil {
			if _, writeErr := fmt.Fprintln(stderr, err); writeErr != nil {
				return 8
			}
			return 8
		}
	}
	answer := "docs/operate.md"
	if os.Getenv("MATLATL_FAKE_MODE") == "wrong" {
		answer = "README.md"
	}
	if _, err := fmt.Fprintln(stdout, `{"type":"step_finish","part":{"tokens":{"input":12,"output":3,"cache":{"read":0,"write":0}}}}`); err != nil {
		return 8
	}
	for _, tool := range mcpTools {
		if _, err := fmt.Fprintf(stdout, "{\"type\":\"tool_use\",\"part\":{\"tool\":%q}}\n", tool); err != nil {
			return 8
		}
	}
	if _, err := fmt.Fprintln(stdout, `{"type":"tool_use","part":{"tool":"grep"}}`); err != nil {
		return 8
	}
	if _, err := fmt.Fprintf(stdout, "{\"type\":\"text\",\"part\":{\"text\":%q}}\n", answer); err != nil {
		return 8
	}
	return 0
}

func readRootFile(rootPath, name string) (data []byte, retErr error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, root.Close()) }()
	return root.ReadFile(name)
}

func writeRootFile(rootPath, name string, data []byte) (retErr error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, root.Close()) }()
	return root.WriteFile(name, data, 0o600)
}

func captureInvocation(dir string, args []string, prompt []byte) error {
	if dir == "" {
		return errors.New("capture directory is required")
	}
	if err := writeRootFile(dir, "stdin.txt", prompt); err != nil {
		return err
	}
	if err := writeRootFile(dir, "argv.txt", []byte(strings.Join(args, "\n")+"\n")); err != nil {
		return err
	}
	configRoot := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	if config, err := readRootFile(configRoot, "opencode.json"); err == nil {
		if err := writeRootFile(dir, "opencode.json", config); err != nil {
			return err
		}
	}
	entries := make([]string, 0)
	err := filepath.WalkDir("/workspace", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			entries = append(entries, strings.TrimPrefix(path, "/workspace/"))
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(entries)
	return writeRootFile(dir, "workspace-files.txt", []byte(strings.Join(entries, "\n")+"\n"))
}

func signalExposure() error {
	if os.Getenv("MATLATL_CONTROL_FD") != "3" {
		return errors.New("inherited control FD is required")
	}
	control := os.NewFile(3, "model-request-control")
	if control == nil {
		return errors.New("open inherited control FD")
	}
	if _, err := io.WriteString(control, "first-model-request\n"); err != nil {
		return err
	}
	return control.Close()
}

func probeMCP(content []byte) (names []string, retErr error) {
	var config struct {
		MCP map[string]struct{ Type, URL string } `json:"mcp"`
	}
	if err := json.Unmarshal(content, &config); err != nil {
		return nil, err
	}
	entry := config.MCP["matlatl"]
	if entry.Type != "remote" || !strings.HasPrefix(entry.URL, "http://127.0.0.1:") || !strings.HasSuffix(entry.URL, "/mcp") {
		return nil, errors.New("matlatl MCP is not remote loopback HTTP")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "fake-opencode", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: entry.URL, HTTPClient: &http.Client{Timeout: 5 * time.Second}, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return nil, fmt.Errorf("MCP initialize: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, session.Close()) }()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("MCP list tools: %w", err)
	}
	names = make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{"corpus-summary", "critical-docs", "get-section", "list-orphans", "path-between", "suggest-links", "what-links-to"}
	if strings.Join(names, "\x00") != strings.Join(want, "\x00") {
		return nil, fmt.Errorf("MCP tools = %v, want %v", names, want)
	}
	calls := []struct {
		name string
		args map[string]any
	}{
		{"corpus-summary", map[string]any{}},
		{"critical-docs", map[string]any{}},
		{"get-section", map[string]any{"ref": "docs/operate.md#operation-guide"}},
		{"list-orphans", map[string]any{}},
		{"path-between", map[string]any{"from": "README.md", "to": "docs/operate.md"}},
		{"suggest-links", map[string]any{}},
		{"what-links-to", map[string]any{"doc": "docs/operate.md"}},
	}
	for _, call := range calls {
		result, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		if callErr != nil || result.IsError {
			return nil, fmt.Errorf("MCP %s failed: %w", call.name, callErr)
		}
		if call.name == "corpus-summary" {
			encoded, marshalErr := json.Marshal(result.StructuredContent)
			if marshalErr != nil || !bytes.Contains(encoded, []byte(`"docs/operate.md"`)) || !bytes.Contains(encoded, []byte(`"schemaVersion":7`)) {
				return nil, errors.New("MCP corpus-summary omitted canonical fixture content")
			}
		}
	}
	return names, nil
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}
