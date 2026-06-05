package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/platform"
)

func runCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRootCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

// runExit exercises runArgs (the testable core of run()) and returns the mapped
// ExitCode along with captured stdout/stderr.
func runExit(t *testing.T, args ...string) (platform.ExitCode, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRoot_NoopOnTempDir(t *testing.T) {
	dir := t.TempDir()
	out, _, err := runCmd(t, dir, "--quiet")
	if err != nil {
		t.Fatalf("root command error: %v", err)
	}
	if !strings.Contains(out, "no markdown documents found") {
		t.Errorf("output missing empty-corpus notice, got: %q", out)
	}
}

func TestVersionCommand(t *testing.T) {
	out, _, err := runCmd(t, "version")
	if err != nil {
		t.Fatalf("version command error: %v", err)
	}
	if !strings.Contains(out, "matlatl") {
		t.Errorf("version output = %q, want it to contain 'matlatl'", out)
	}
}

func TestCheckEmptyDirExitsOK(t *testing.T) {
	// check on an empty dir: no markdown found → exit 0 with a notice (ADR 0005).
	dir := t.TempDir()
	out, _, err := runCmd(t, "check", dir)
	if err != nil {
		t.Fatalf("check empty dir error: %v", err)
	}
	if !strings.Contains(out, "no markdown documents found") {
		t.Errorf("check output = %q, want no-markdown notice", out)
	}
}

// TestServeCommandRegistered checks that `serve` is a real, registered command
// with usable help (it now runs the MCP server over streamable HTTP; the serve
// loop itself is exercised in the mcpserver package's tests, not here, to avoid
// binding a port).
func TestServeCommandRegistered(t *testing.T) {
	out, _, err := runCmd(t, "serve", "--help")
	if err != nil {
		t.Fatalf("serve --help error: %v", err)
	}
	if !strings.Contains(out, "MCP") {
		t.Errorf("serve --help = %q, want mention of MCP", out)
	}
	if strings.Contains(out, "not yet implemented") {
		t.Errorf("serve is still a stub: %q", out)
	}
}

// TestFixPromptCommandRegistered checks that `fix-prompt` is a real, registered
// command whose help explains the agent-agnostic pipe usage.
func TestFixPromptCommandRegistered(t *testing.T) {
	out, _, err := runCmd(t, "fix-prompt", "--help")
	if err != nil {
		t.Fatalf("fix-prompt --help error: %v", err)
	}
	if !strings.Contains(out, "claude -p") {
		t.Errorf("fix-prompt --help = %q, want the pipe-usage example", out)
	}
	if !strings.Contains(out, "errors-only") {
		t.Errorf("fix-prompt --help = %q, want mention of --errors-only", out)
	}
}

// TestGraphMermaidStdout: graph now renders a Mermaid block on stdout.
func TestGraphMermaidStdout(t *testing.T) {
	dir := t.TempDir()
	out, _, err := runCmd(t, "graph", dir)
	if err != nil {
		t.Fatalf("graph error: %v", err)
	}
	if !strings.Contains(out, "```mermaid") || !strings.Contains(out, "flowchart") {
		t.Errorf("graph output missing mermaid fence/flowchart, got: %q", out)
	}
}

func TestTooManyArgsErrors(t *testing.T) {
	// The root takes at most one positional path; a second positional is a
	// usage error.
	_, _, err := runCmd(t, "pathA", "pathB")
	if err == nil {
		t.Fatal("two positional args: want error")
	}
}

// The following tests assert the ADR 0005 exit-code mapping at the run() level,
// not merely that an error was returned.

func TestExitCode_OK(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := runExit(t, dir, "--quiet")
	if code != platform.ExitOK {
		t.Fatalf("clean run code = %v, want ExitOK", code)
	}
	if !strings.Contains(out, "no markdown documents found") {
		t.Errorf("missing empty-corpus notice, got: %q", out)
	}
}

func TestExitCode_UsageTooManyArgsRoot(t *testing.T) {
	code, _, errOut := runExit(t, "pathA", "pathB")
	if code != platform.ExitUsage {
		t.Fatalf("too-many-args root code = %v, want ExitUsage", code)
	}
	if !strings.Contains(errOut, "matlatl:") {
		t.Errorf("usage error printed nothing useful to stderr, got: %q", errOut)
	}
}

func TestExitCode_UsageTooManyArgsStub(t *testing.T) {
	// Regression for the gate finding: `matlatl check a b` must exit 2, not 3.
	code, _, errOut := runExit(t, "check", "a", "b")
	if code != platform.ExitUsage {
		t.Fatalf("`check a b` code = %v, want ExitUsage", code)
	}
	if !strings.Contains(errOut, "matlatl:") {
		t.Errorf("stub usage error printed nothing to stderr, got: %q", errOut)
	}
}

func TestExitCode_UsageBadFlag(t *testing.T) {
	code, _, errOut := runExit(t, "--bad-flag")
	if code != platform.ExitUsage {
		t.Fatalf("bad-flag code = %v, want ExitUsage", code)
	}
	if !strings.Contains(errOut, "matlatl:") {
		t.Errorf("bad-flag printed nothing to stderr, got: %q", errOut)
	}
}

func TestExitCode_VersionOK(t *testing.T) {
	code, out, _ := runExit(t, "version")
	if code != platform.ExitOK {
		t.Fatalf("version code = %v, want ExitOK", code)
	}
	if !strings.Contains(out, "matlatl") {
		t.Errorf("version output = %q, want 'matlatl'", out)
	}
}
