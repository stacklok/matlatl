//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/platform"
)

// mentionEdge is the graph.json edge shape the consumer reads (ADR 0026).
type mentionEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
	Kind string `json:"kind"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// TestIntegration_GraphJSONMentions runs the real CLI over testdata/mentions,
// whose .matlatl.yml declares a "/" invocation rule, and asserts the typed
// mention edges a consumer post-processes. Stale (docs/nope.md, ghost.md) and
// ambiguous (testing.md) mentions stay in graph.json as mention edges, and an
// HTML comment's mention (line 25) counts like prose.
func TestIntegration_GraphJSONMentions(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "mentions"))
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := runArgs(context.Background(), []string{"graph", root, "--format", "json"}, &out, &errOut); code != platform.ExitOK {
		t.Fatalf("graph code = %v, stderr=%q", code, errOut.String())
	}
	var graph struct {
		SchemaVersion int `json:"schemaVersion"`
		Summary       struct {
			Edges    int `json:"edges"`
			Mentions int `json:"mentions"`
		} `json:"summary"`
		Edges   []mentionEdge `json:"edges"`
		Orphans []string      `json:"orphans"`
	}
	if err := json.Unmarshal(out.Bytes(), &graph); err != nil {
		t.Fatalf("graph.json does not parse: %v", err)
	}
	if graph.SchemaVersion != 8 {
		t.Errorf("schemaVersion = %d, want 8", graph.SchemaVersion)
	}
	var mentions []mentionEdge
	for _, e := range graph.Edges {
		if e.Type == "mention" {
			mentions = append(mentions, e)
		}
	}
	want := []mentionEdge{
		{"docs/guide.md", ".claude/rules/metrics.md", "mention", "path", 3, ".claude/rules/metrics.md"},
		{"docs/guide.md", ".claude/skills/panel-review/SKILL.md", "mention", "invocation", 12, "/panel-review"},
		{"docs/guide.md", ".claude/skills/panel-review/SKILL.md", "mention", "path", 8, ".claude/skills/panel-review/"},
		{"docs/guide.md", ".claude/skills/triage-cve/SKILL.md", "mention", "invocation", 13, "/triage-cve"},
		{"docs/guide.md", "docs/design/frontdoor.md", "mention", "filename", 6, "frontdoor.md"},
		{"docs/guide.md", "docs/nope.md", "mention", "path", 15, "docs/nope.md"},
		{"docs/guide.md", "docs/reference.md", "mention", "path", 4, "docs/reference.md"},
		{"docs/guide.md", "docs/sibling.md", "mention", "path", 5, "./sibling.md"},
		{"docs/guide.md", "docs/sibling.md", "mention", "path", 25, "docs/sibling.md"},
		{"docs/guide.md", "ghost.md", "mention", "filename", 15, "ghost.md"},
		{"docs/guide.md", "testing.md", "mention", "filename", 7, "testing.md"},
	}
	if !slices.Equal(mentions, want) {
		t.Errorf("mention edges\n got: %+v\nwant: %+v", mentions, want)
	}
	if graph.Summary.Mentions != len(want) || graph.Summary.Edges+graph.Summary.Mentions != len(graph.Edges) {
		t.Errorf("summary edges=%d mentions=%d over %d edges", graph.Summary.Edges, graph.Summary.Mentions, len(graph.Edges))
	}
	if slices.Contains(graph.Orphans, "docs/sibling.md") {
		t.Error("a mentioned doc must not be an orphan")
	}
}

// TestIntegration_MentionsNeverFailCheck proves an unresolved, ambiguous or
// URL-shaped mention never produces a finding or changes the exit code: the
// fixture's dangling mentions (docs/nope.md, ghost.md, /no-such-skill,
// testing.md) leave `check --strict` at zero broken links.
func TestIntegration_MentionsNeverFailCheck(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "# Home\n\n[Guide](docs/guide.md)\n")
	write("docs/guide.md", "# Guide\n\n[Home](../README.md). See docs/nope.md, ghost.md, /no-such-skill and https://x.example/y.md.\n")
	write(".matlatl.yml", "version: 1\nmentions:\n  invocations:\n    - prefix: \"/\"\n      targets: [\"**/SKILL.md\"]\n")

	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", dir, "--strict"}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("check --strict code = %v, want 0; stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "0 broken link(s)") {
		t.Errorf("check output = %q, want 0 broken links", out.String())
	}
}
