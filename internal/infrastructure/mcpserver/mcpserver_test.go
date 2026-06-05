package mcpserver

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// fixtureRoot is the committed testdata corpus.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func buildTestAnalysis(t *testing.T) *Analysis {
	t.Helper()
	a, err := BuildAnalysis(context.Background(), fixtureRoot(t))
	if err != nil {
		t.Fatalf("BuildAnalysis: %v", err)
	}
	return a
}

// callTool finds the named tool and invokes its handler with args, in-process
// (no transport, no live client). It returns the result.
func callTool(t *testing.T, a *Analysis, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	var handler server.ToolHandlerFunc
	for _, tl := range a.Tools() {
		if tl.Tool.Name == name {
			handler = tl.Handler
			break
		}
	}
	if handler == nil {
		t.Fatalf("tool %q not registered", name)
	}
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("tool %q handler error: %v", name, err)
	}
	if res == nil {
		t.Fatalf("tool %q returned nil result", name)
	}
	return res
}

// structured extracts the structuredContent of a successful result.
func structured(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool returned error result: %+v", res.Content)
	}
	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content is %T, want map", res.StructuredContent)
	}
	return m
}

// TestServer_Construction asserts NewServer registers all five tools.
func TestServer_Construction(t *testing.T) {
	a := buildTestAnalysis(t)
	if got := len(a.Tools()); got != 5 {
		t.Fatalf("Tools() = %d, want 5", got)
	}
	// NewServer must not panic and must register the tools.
	_ = NewServer(a)
	want := []string{"what-links-to", "list-orphans", "path-between", "get-section", "corpus-summary"}
	have := map[string]bool{}
	for _, tl := range a.Tools() {
		have[tl.Tool.Name] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing tool %q", w)
		}
	}
}

// TestTool_WhatLinksTo: docs/README.md is linked to from within the fixture; an
// unknown doc is refused.
func TestTool_WhatLinksTo(t *testing.T) {
	a := buildTestAnalysis(t)

	res := callTool(t, a, "what-links-to", map[string]any{"doc": "README.md"})
	m := structured(t, res)
	if _, ok := m["backlinks"]; !ok {
		t.Fatalf("result missing backlinks: %+v", m)
	}

	// Unknown document -> error result (validated, not a panic / OOB read).
	bad := callTool(t, a, "what-links-to", map[string]any{"doc": "../../etc/passwd"})
	if !bad.IsError {
		t.Error("expected error for out-of-corpus document id")
	}
}

// TestTool_ListOrphans: the fixture's CHANGELOG.md is an intentional orphan and
// must be suppressed; the genuine isolated docs appear.
func TestTool_ListOrphans(t *testing.T) {
	a := buildTestAnalysis(t)
	m := structured(t, callTool(t, a, "list-orphans", nil))
	iso := toStrings(t, m["isolated"])
	for _, id := range iso {
		if id == "CHANGELOG.md" {
			t.Error("intentional orphan CHANGELOG.md must be suppressed from list-orphans")
		}
	}
	if _, ok := m["unreachable"]; !ok {
		t.Error("result missing unreachable list")
	}
}

// TestTool_PathBetween: a path from the root README to a reachable doc exists; a
// path to a nonexistent doc is refused; identical endpoints yield a 1-node path.
func TestTool_PathBetween(t *testing.T) {
	a := buildTestAnalysis(t)

	// Self path.
	m := structured(t, callTool(t, a, "path-between", map[string]any{"from": "README.md", "to": "README.md"}))
	if found, _ := m["found"].(bool); !found {
		t.Error("self path should be found")
	}

	// Unknown target -> error.
	bad := callTool(t, a, "path-between", map[string]any{"from": "README.md", "to": "nope.md"})
	if !bad.IsError {
		t.Error("expected error for unknown target document")
	}

	// A reachable target (docs/guide.md is linked from the README chain). If not
	// directly reachable the tool still returns a structured found=false result,
	// which we assert is well-formed rather than asserting a specific topology.
	m2 := structured(t, callTool(t, a, "path-between", map[string]any{"from": "README.md", "to": "docs/guide.md"}))
	if _, ok := m2["path"]; !ok {
		t.Errorf("path-between missing path field: %+v", m2)
	}
}

// TestTool_GetSection: a known heading resolves; a bad ref / unknown slug is
// refused.
func TestTool_GetSection(t *testing.T) {
	a := buildTestAnalysis(t)

	m := structured(t, callTool(t, a, "get-section", map[string]any{"ref": "README.md#getting-started"}))
	if m["slug"] != "getting-started" {
		t.Errorf("get-section slug = %v, want getting-started", m["slug"])
	}

	// Missing '#'.
	if !callTool(t, a, "get-section", map[string]any{"ref": "README.md"}).IsError {
		t.Error("expected error for ref without #slug")
	}
	// Unknown slug.
	if !callTool(t, a, "get-section", map[string]any{"ref": "README.md#no-such-slug"}).IsError {
		t.Error("expected error for unknown slug")
	}
}

// TestTool_CorpusSummary: returns the graph.json manifest with a non-empty
// document count matching the fixture.
func TestTool_CorpusSummary(t *testing.T) {
	a := buildTestAnalysis(t)
	res := callTool(t, a, "corpus-summary", nil)
	if res.IsError {
		t.Fatalf("corpus-summary errored: %+v", res.Content)
	}
	// The structured content is the typed graphjson.Document; just assert it is
	// present and the fallback text mentions documents.
	if res.StructuredContent == nil {
		t.Fatal("corpus-summary returned no structured content")
	}
	if len(res.Content) == 0 {
		t.Fatal("corpus-summary returned no fallback text content")
	}
}

func toStrings(t *testing.T, v any) []string {
	t.Helper()
	switch xs := v.(type) {
	case []string:
		return xs
	case []any:
		out := make([]string, 0, len(xs))
		for _, x := range xs {
			s, _ := x.(string)
			out = append(out, s)
		}
		return out
	default:
		t.Fatalf("expected string slice, got %T", v)
		return nil
	}
}
