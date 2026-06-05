package emit_test

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
	"github.com/stacklok/doctopus/internal/infrastructure/emit/diagram"
	idxemit "github.com/stacklok/doctopus/internal/infrastructure/emit/index"
	"github.com/stacklok/doctopus/internal/infrastructure/emit/report"
	"github.com/stacklok/doctopus/internal/infrastructure/fsscanner"
	"github.com/stacklok/doctopus/internal/infrastructure/mdparser"
)

// updateGolden, when set, rewrites the golden files instead of comparing.
// Regenerate with:
//
//	go test ./internal/infrastructure/emit/ -run TestGolden -update
//
// (or set DOCTOPUS_UPDATE_GOLDEN=1). The goldens live under
// internal/infrastructure/emit/testdata/golden/ and are checked into the repo.
var updateGolden = flag.Bool("update", false, "regenerate golden files")

// modTimeRE matches the RFC3339 mod-times the index emits. File mod-times are
// not reproducible across checkouts, so the index golden is compared with the
// Modified column normalized to a fixed placeholder. This is the ONE field that
// is intentionally not byte-asserted; everything else is exact.
var modTimeRE = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`)

func normalizeIndex(b []byte) []byte {
	return modTimeRE.ReplaceAll(b, []byte("<modtime>"))
}

// buildCorpusView runs the real pipeline (scan → parse → resolve → build →
// analyze) over testdata/corpus and returns the render-ready View.
func buildCorpusView(t *testing.T) emit.View {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := application.DefaultConfig()
	cfg.RootPath = root
	scanner := fsscanner.New(fsscanner.Config{})
	parserFac := mdparser.NewFactory(mdparser.Config{})
	pipe := application.NewPipeline(cfg, scanner, parserFac, nil, nil)
	_, res, err := pipe.Run(context.Background())
	if err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	return emit.BuildView(res)
}

func goldenPath(name string) string {
	return filepath.Join("testdata", "golden", name)
}

// assertGolden compares got against the committed golden file (or rewrites it
// under -update). normalize, if non-nil, is applied to both sides before
// comparison (used to mask non-reproducible mod-times in the index).
func assertGolden(t *testing.T, name string, got []byte, normalize func([]byte) []byte) {
	t.Helper()
	path := goldenPath(name)
	if *updateGolden || os.Getenv("DOCTOPUS_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", name, err)
	}
	g, w := got, want
	if normalize != nil {
		g, w = normalize(got), normalize(want)
	}
	if !bytes.Equal(g, w) {
		t.Errorf("golden mismatch for %s (regenerate with -update if intended).\n--- got ---\n%s\n--- want ---\n%s",
			name, got, want)
	}
}

func TestGolden_Artifacts(t *testing.T) {
	v := buildCorpusView(t)

	t.Run("report.md", func(t *testing.T) {
		assertGolden(t, "report.md", report.Markdown(v), nil)
	})
	t.Run("graph.mmd", func(t *testing.T) {
		assertGolden(t, "graph.mmd", diagram.Mermaid(v), nil)
	})
	t.Run("graph.dot", func(t *testing.T) {
		assertGolden(t, "graph.dot", diagram.DOT(v), nil)
	})
	t.Run("hierarchy.mmd", func(t *testing.T) {
		assertGolden(t, "hierarchy.mmd", diagram.MermaidHierarchy(v), nil)
	})
	t.Run("index.md", func(t *testing.T) {
		assertGolden(t, "index.md", idxemit.Markdown(v), normalizeIndex)
	})
	t.Run("report.txt", func(t *testing.T) {
		var buf bytes.Buffer
		if err := report.Terminal(&buf, v, report.TerminalOptions{Color: report.ColorNever}); err != nil {
			t.Fatal(err)
		}
		assertGolden(t, "report.txt", buf.Bytes(), nil)
	})
}

// TestGolden_ByteStable runs every emitter twice and asserts identical bytes,
// independent of the golden files (the determinism contract).
func TestGolden_ByteStable(t *testing.T) {
	v1 := buildCorpusView(t)
	v2 := buildCorpusView(t)

	cases := []struct {
		name string
		gen  func(emit.View) []byte
	}{
		{"report.md", report.Markdown},
		{"graph.mmd", diagram.Mermaid},
		{"graph.dot", diagram.DOT},
		{"hierarchy.mmd", diagram.MermaidHierarchy},
		{"index.md", idxemit.Markdown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !bytes.Equal(tc.gen(v1), tc.gen(v2)) {
				t.Errorf("%s is not byte-stable across two pipeline runs", tc.name)
			}
		})
	}
}
