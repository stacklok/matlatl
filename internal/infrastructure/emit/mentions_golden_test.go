package emit_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/infrastructure/config"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
)

// buildMentionsView runs the real pipeline over testdata/mentions with its
// .matlatl.yml applied the way the CLI applies it: the invocation rules feed the
// resolver, and their prefixes feed the parser (ADR 0026).
func buildMentionsView(t *testing.T) emit.View {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "mentions"))
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

// TestGolden_MentionsGraphJSON pins graph.json for a corpus exercising every
// mention form: path (code span, plain text, doc-relative, repo-root-relative,
// directory), unique and ambiguous file names, invocations (resolved, filtered
// by target glob, URL/path-prefixed non-matches), fenced blocks, and
// reference-link definitions.
func TestGolden_MentionsGraphJSON(t *testing.T) {
	b, err := graphjson.JSON(buildMentionsView(t))
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "graph-mentions.json", b, nil)
}
