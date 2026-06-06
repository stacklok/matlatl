package emit_test

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/diagram"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	idxemit "github.com/stacklok/matlatl/internal/infrastructure/emit/index"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/llmstxt"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/report"
	trailsemit "github.com/stacklok/matlatl/internal/infrastructure/emit/trails"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
)

// corpusRootPath is the absolute path of the corpus fixture, needed by the
// llms-full/small emitters' root-confined body reader.
func corpusRootPath(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// updateGolden, when set, rewrites the golden files instead of comparing.
// Regenerate with:
//
//	go test ./internal/infrastructure/emit/ -run TestGolden -update
//
// (or set MATLATL_UPDATE_GOLDEN=1). The goldens live under
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
	pipe := application.NewPipeline(cfg, scanner, parserFac, nil)
	_, res, err := pipe.Run(context.Background())
	if err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	return emit.BuildView(res)
}

// buildCorpusReport runs the same real pipeline as buildCorpusView but returns
// the frozen AnalysisReport (which carries the per-finding Details the View
// drops), needed by the fix-prompt emitter.
func buildCorpusReport(t *testing.T) *analysis.AnalysisReport {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := application.DefaultConfig()
	cfg.RootPath = root
	scanner := fsscanner.New(fsscanner.Config{})
	parserFac := mdparser.NewFactory(mdparser.Config{})
	pipe := application.NewPipeline(cfg, scanner, parserFac, nil)
	_, res, err := pipe.Run(context.Background())
	if err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	return res.Report
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
	if *updateGolden || os.Getenv("MATLATL_UPDATE_GOLDEN") == "1" {
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

	// --- P5 LLM artifacts ---
	root := corpusRootPath(t)
	reader := llmstxt.NewBodyReader(root)
	opts := llmstxt.Options{} // derive the title from the root doc (deterministic)
	t.Run("graph.json", func(t *testing.T) {
		b, err := graphjson.JSON(v)
		if err != nil {
			t.Fatal(err)
		}
		assertGolden(t, "graph.json", b, nil)
	})
	t.Run("trails.json", func(t *testing.T) {
		b, err := trailsemit.JSON(v)
		if err != nil {
			t.Fatal(err)
		}
		assertGolden(t, "trails.json", b, nil)
	})
	t.Run("llms.txt", func(t *testing.T) {
		assertGolden(t, "llms.txt", llmstxt.LLMSTxt(v, opts), nil)
	})
	t.Run("llms-full.txt", func(t *testing.T) {
		assertGolden(t, "llms-full.txt", llmstxt.LLMSFull(v, reader, opts), nil)
	})
	t.Run("llms-small.txt", func(t *testing.T) {
		assertGolden(t, "llms-small.txt", llmstxt.LLMSSmall(v, reader, opts), nil)
	})

	// fix-prompt consumes the full report (not the View), so build it separately.
	t.Run("fix-prompt.md", func(t *testing.T) {
		rep := buildCorpusReport(t)
		assertGolden(t, "fix-prompt.md", emit.FixPrompt(rep, emit.FixPromptOptions{}), nil)
	})
}

// TestGolden_ByteStable runs every emitter twice and asserts identical bytes,
// independent of the golden files (the determinism contract).
func TestGolden_ByteStable(t *testing.T) {
	v1 := buildCorpusView(t)
	v2 := buildCorpusView(t)

	reader := llmstxt.NewBodyReader(corpusRootPath(t))
	cases := []struct {
		name string
		gen  func(emit.View) []byte
	}{
		{"report.md", report.Markdown},
		{"graph.mmd", diagram.Mermaid},
		{"graph.dot", diagram.DOT},
		{"hierarchy.mmd", diagram.MermaidHierarchy},
		{"index.md", idxemit.Markdown},
		{"graph.json", func(v emit.View) []byte {
			b, err := graphjson.JSON(v)
			if err != nil {
				t.Fatalf("graph.json emit: %v", err)
			}
			return b
		}},
		{"trails.json", func(v emit.View) []byte {
			b, err := trailsemit.JSON(v)
			if err != nil {
				t.Fatalf("trails.json emit: %v", err)
			}
			return b
		}},
		{"llms.txt", func(v emit.View) []byte { return llmstxt.LLMSTxt(v, llmstxt.Options{}) }},
		{"llms-full.txt", func(v emit.View) []byte { return llmstxt.LLMSFull(v, reader, llmstxt.Options{}) }},
		{"llms-small.txt", func(v emit.View) []byte { return llmstxt.LLMSSmall(v, reader, llmstxt.Options{}) }},
		// report.txt: the terminal emitter writes to an io.Writer, so adapt it to
		// the []byte generator shape (color forced off for a stable comparison).
		{"report.txt", func(v emit.View) []byte {
			var buf bytes.Buffer
			if err := report.Terminal(&buf, v, report.TerminalOptions{Color: report.ColorNever}); err != nil {
				t.Fatalf("terminal emit: %v", err)
			}
			return buf.Bytes()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !bytes.Equal(tc.gen(v1), tc.gen(v2)) {
				t.Errorf("%s is not byte-stable across two pipeline runs", tc.name)
			}
		})
	}

	// fix-prompt is report-driven (not View-driven), so check it on its own.
	t.Run("fix-prompt.md", func(t *testing.T) {
		r1 := buildCorpusReport(t)
		r2 := buildCorpusReport(t)
		if !bytes.Equal(
			emit.FixPrompt(r1, emit.FixPromptOptions{}),
			emit.FixPrompt(r2, emit.FixPromptOptions{}),
		) {
			t.Error("fix-prompt.md is not byte-stable across two pipeline runs")
		}
	})
}
