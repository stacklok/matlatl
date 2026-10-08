package graphjson_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/infrastructure/config"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
)

// buildMentionsView runs the real pipeline over testdata/mentions with its
// .matlatl.yml invocation rules applied (ADR 0026).
func buildMentionsView(t *testing.T) emit.View {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "testdata", "mentions"))
	if err != nil {
		t.Fatal(err)
	}
	file, _, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := application.DefaultConfig()
	cfg.RootPath = root
	cfg.MentionInvocations = file.MentionInvocations
	prefixes := make([]string, 0, len(file.MentionInvocations))
	for _, r := range file.MentionInvocations {
		prefixes = append(prefixes, r.Prefix)
	}
	pipe := application.NewPipeline(cfg,
		fsscanner.New(fsscanner.Config{}),
		mdparser.NewFactory(mdparser.Config{InvocationPrefixes: prefixes}),
		nil)
	_, res, err := pipe.Run(context.Background())
	if err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	return emit.BuildView(res)
}

func TestJSON_MentionEdges(t *testing.T) {
	doc := graphjson.Build(buildMentionsView(t))

	type key struct {
		From, To, Type, Kind, Health string
		Line                         int
	}
	got := map[key]string{}
	mentions := 0
	for _, e := range doc.Edges {
		got[key{e.From, e.To, e.Type, e.Kind, e.Health, e.Line}] = e.Text
		if e.Type == "mention" {
			mentions++
		} else if e.Kind != "" || e.Line != 0 || e.Text != "" {
			t.Errorf("reference edge %+v must not carry mention fields", e)
		}
	}
	want := map[key]string{
		{"docs/guide.md", ".claude/rules/metrics.md", "mention", "path", "valid", 3}:                    ".claude/rules/metrics.md",
		{"docs/guide.md", "docs/reference.md", "mention", "path", "valid", 4}:                           "docs/reference.md",
		{"docs/guide.md", "docs/reference.md", "reference", "", "valid", 0}:                             "",
		{"docs/guide.md", "docs/sibling.md", "mention", "path", "valid", 5}:                             "./sibling.md",
		{"docs/guide.md", "docs/design/frontdoor.md", "mention", "filename", "valid", 6}:                "frontdoor.md",
		{"docs/guide.md", "docs/a/testing.md", "mention", "filename", "ambiguous", 7}:                   "testing.md",
		{"docs/guide.md", "docs/b/testing.md", "mention", "filename", "ambiguous", 7}:                   "testing.md",
		{"docs/guide.md", ".claude/skills/panel-review/SKILL.md", "mention", "path", "valid", 8}:        ".claude/skills/panel-review/",
		{"docs/guide.md", ".claude/skills/panel-review/SKILL.md", "mention", "invocation", "valid", 12}: "/panel-review",
		{"docs/guide.md", ".claude/skills/triage-cve/SKILL.md", "mention", "invocation", "valid", 13}:   "/triage-cve",
		{"docs/guide.md", "docs/nope.md", "mention", "path", "broken", 15}:                              "docs/nope.md",
		{"docs/guide.md", "ghost.md", "mention", "filename", "broken", 15}:                              "ghost.md",
		{"docs/guide.md", "docs/sibling.md", "mention", "path", "valid", 25}:                            "docs/sibling.md",
	}
	for k, text := range want {
		gotText, ok := got[k]
		if !ok {
			t.Errorf("missing edge %+v", k)
			continue
		}
		if gotText != text {
			t.Errorf("edge %+v text = %q, want %q", k, gotText, text)
		}
	}
	// Fenced blocks, URL/path-prefixed invocations and out-of-glob
	// front-matter names never become edges.
	for _, e := range doc.Edges {
		switch e.To {
		case "docs/fenced.md", "docs/design/panel-review.md":
			t.Errorf("unexpected edge to %s: %+v", e.To, e)
		}
	}
	if doc.Summary.Mentions != mentions || doc.Summary.Edges+doc.Summary.Mentions != len(doc.Edges) {
		t.Errorf("summary edges=%d mentions=%d, want mentions=%d and a sum of %d",
			doc.Summary.Edges, doc.Summary.Mentions, mentions, len(doc.Edges))
	}
	if !slices.IsSortedFunc(doc.Edges, func(a, b graphjson.Edge) int {
		if a.From != b.From {
			return strings.Compare(a.From, b.From)
		}
		return strings.Compare(a.To, b.To)
	}) {
		t.Error("edges are not sorted by (from, to)")
	}
}

// TestJSON_MentionsCountInMetrics proves a mention confers reachability and
// lifts orphan status (ADR 0026): docs/sibling.md is only ever mentioned, while
// docs/fenced.md is only named inside a fenced block.
func TestJSON_MentionsCountInMetrics(t *testing.T) {
	doc := graphjson.Build(buildMentionsView(t))
	if slices.Contains(doc.Orphans, "docs/sibling.md") {
		t.Error("docs/sibling.md is mentioned, so it must not be an orphan")
	}
	if !slices.Contains(doc.Orphans, "docs/fenced.md") {
		t.Error("docs/fenced.md is only named in a fenced block, so it must stay an orphan")
	}
	for _, n := range doc.Nodes {
		if n.ID == "docs/sibling.md" && (n.InDegree != 1 || !n.Reachable) {
			t.Errorf("docs/sibling.md inDegree=%d reachable=%v, want 1/true", n.InDegree, n.Reachable)
		}
	}
	if len(doc.BrokenLinks) != 0 || len(doc.Ambiguous) != 0 {
		t.Errorf("unresolved/ambiguous mentions must not produce findings: broken=%v ambiguous=%v",
			doc.BrokenLinks, doc.Ambiguous)
	}
}

func TestJSON_MentionsValidateAgainstSchema(t *testing.T) {
	b, err := graphjson.JSON(buildMentionsView(t))
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	schema := loadGraphSchema(t)
	if errs := validateNode(data, schema, schema, "$"); len(errs) > 0 {
		sort.Strings(errs)
		t.Fatalf("graph.json does not satisfy graph.schema.json:\n  %v", errs)
	}

	// The schema requires kind/line/text on mention edges: drop line from one and
	// the validator must reject it (proves the if/then clause is enforced).
	for _, e := range data["edges"].([]any) {
		edge := e.(map[string]any)
		if edge["type"] == "mention" {
			delete(edge, "line")
			break
		}
	}
	if errs := validateNode(data, schema, schema, "$"); len(errs) == 0 {
		t.Fatal("a mention edge without line must fail schema validation")
	}
}

func loadGraphSchema(t *testing.T) map[string]any {
	t.Helper()
	sb, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "schemas", "graph.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(sb, &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}
