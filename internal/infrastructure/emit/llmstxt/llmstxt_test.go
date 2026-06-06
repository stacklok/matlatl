package llmstxt_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/llmstxt"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
)

func corpusRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func buildCorpusView(t *testing.T) (emit.View, string) {
	t.Helper()
	root := corpusRoot(t)
	cfg := application.DefaultConfig()
	cfg.RootPath = root
	scanner := fsscanner.New(fsscanner.Config{})
	parserFac := mdparser.NewFactory(mdparser.Config{})
	pipe := application.NewPipeline(cfg, scanner, parserFac, nil)
	_, res, err := pipe.Run(context.Background())
	if err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	return emit.BuildView(res), root
}

// TestLLMSTxt_Backlinks: a curated entry carries a "(linked from: …)" clause
// naming the documents that link TO it (ADR 0016, Nelson/Xanadu two-way links).
// In the fixture corpus, docs/guide.md is linked from README.md (among others).
func TestLLMSTxt_Backlinks(t *testing.T) {
	v, _ := buildCorpusView(t)
	out := string(llmstxt.LLMSTxt(v, llmstxt.Options{}))

	// Find the curated line for docs/guide.md and assert its backlinks clause.
	var guideLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "(docs/guide.md)") {
			guideLine = line
			break
		}
	}
	if guideLine == "" {
		t.Fatalf("no curated entry for docs/guide.md:\n%s", out)
	}
	if !strings.Contains(guideLine, "(linked from:") {
		t.Errorf("docs/guide.md entry missing a backlinks clause: %q", guideLine)
	}
	if !strings.Contains(guideLine, "README.md") {
		t.Errorf("docs/guide.md backlinks clause should name README.md as a source: %q", guideLine)
	}
}

// TestLLMSTxt_SpecShape asserts the llms.txt spec shape: exactly one H1, a
// blockquote summary, H2 sections, an Optional section position (if present)
// LAST among content sections, and a Known-gaps note.
func TestLLMSTxt_SpecShape(t *testing.T) {
	v, _ := buildCorpusView(t)
	out := string(llmstxt.LLMSTxt(v, llmstxt.Options{Title: "My Docs"}))

	lines := strings.Split(out, "\n")
	h1 := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "# ") {
			h1++
		}
	}
	if h1 != 1 {
		t.Errorf("want exactly one H1, got %d\n%s", h1, out)
	}
	if !strings.HasPrefix(out, "# My Docs\n") {
		t.Errorf("H1 should be the configured title:\n%s", out)
	}
	if !strings.Contains(out, "\n> A markdown documentation corpus") {
		t.Errorf("missing blockquote summary:\n%s", out)
	}
	if !strings.Contains(out, "## Documentation") {
		t.Errorf("missing Documentation H2:\n%s", out)
	}
	if !strings.Contains(out, "## Known gaps") {
		t.Errorf("missing Known gaps note:\n%s", out)
	}
	// Known gaps must be the final section (it is the incompleteness flag).
	if idx := strings.LastIndex(out, "## "); idx >= 0 {
		if !strings.HasPrefix(out[idx:], "## Known gaps") {
			t.Errorf("Known gaps must be the last section:\n%s", out)
		}
	}
}

// TestLLMSTxt_ImportanceOrdering asserts a hub/high-authority doc appears before
// a leaf doc (most-connected first — the "lost in the middle" mitigation).
func TestLLMSTxt_ImportanceOrdering(t *testing.T) {
	v, _ := buildCorpusView(t)
	out := string(llmstxt.LLMSTxt(v, llmstxt.Options{}))

	// docs/guide.md is a hub (high authority/in-degree) in the fixture; it must
	// be listed before docs/sub/overview.md (a leaf one hop further out).
	guide := strings.Index(out, "(docs/guide.md)")
	overview := strings.Index(out, "(docs/sub/overview.md)")
	if guide < 0 || overview < 0 {
		t.Fatalf("expected both guide and overview in output:\n%s", out)
	}
	if guide > overview {
		t.Errorf("importance ordering broken: guide (%d) should precede overview (%d)\n%s", guide, overview, out)
	}
}

// TestLLMSTxt_OnlyReachable: an unreachable doc must not appear in the curated
// index (llms.txt curates reachable docs; gaps are flagged separately).
func TestLLMSTxt_OnlyReachable(t *testing.T) {
	v, _ := buildCorpusView(t)
	out := string(llmstxt.LLMSTxt(v, llmstxt.Options{}))
	// Scope the assertion to the curated index (the Documentation/Optional
	// sections), which is where reachability matters. The "Suggested reading
	// order" block (ADR 0016) intentionally covers every connected cluster,
	// including unreachable ones, so we only check the curated prefix here.
	curated := out
	if i := strings.Index(out, "## Suggested reading order"); i >= 0 {
		curated = out[:i]
	} else if i := strings.Index(out, "## Known gaps"); i >= 0 {
		curated = out[:i]
	}
	if strings.Contains(curated, "(docs/stray.md)") {
		t.Errorf("unreachable doc docs/stray.md should not be curated:\n%s", out)
	}
}

// TestLLMSTxt_HostileTitleSanitized: a hostile title cannot inject markdown or a
// new line into the H1.
func TestLLMSTxt_HostileTitleSanitized(t *testing.T) {
	v, _ := buildCorpusView(t)
	out := string(llmstxt.LLMSTxt(v, llmstxt.Options{Title: "Evil\nTitle # injected"}))
	first := strings.SplitN(out, "\n", 2)[0]
	if strings.Contains(first, "\n") || !strings.HasPrefix(first, "# ") {
		t.Errorf("hostile title broke the H1 line: %q", first)
	}
	// The newline must have collapsed to a space, keeping it one line.
	if !strings.HasPrefix(out, "# Evil Title # injected\n") {
		t.Errorf("hostile title not single-lined:\n%s", out)
	}
}

// TestLLMSFull_ContextHeadersAndOrdering: each doc body is preceded by a context
// header (path + situating line) and bodies are importance-ordered.
func TestLLMSFull_ContextHeadersAndOrdering(t *testing.T) {
	v, root := buildCorpusView(t)
	out := string(llmstxt.LLMSFull(v, llmstxt.NewBodyReader(root), llmstxt.Options{Title: "Corp"}))

	if !strings.Contains(out, "Path: `docs/guide.md`") {
		t.Errorf("missing context header for guide:\n%s", out)
	}
	if !strings.Contains(out, "This document is part of Corp, section docs.") {
		t.Errorf("missing situating line:\n%s", out)
	}
	// Front matter must be stripped: guide.md has a `---` block; its keys (title:)
	// must not leak into the concatenated body.
	if strings.Contains(out, "title: User Guide") {
		t.Errorf("front matter leaked into llms-full body:\n%s", out)
	}
	// Importance order: guide body precedes overview body.
	guide := strings.Index(out, "Path: `docs/guide.md`")
	overview := strings.Index(out, "Path: `docs/sub/overview.md`")
	if guide < 0 || overview < 0 || guide > overview {
		t.Errorf("llms-full not importance-ordered (guide=%d overview=%d)", guide, overview)
	}
}

// TestLLMS_ByteStable: all three artifacts are byte-stable across two runs.
func TestLLMS_ByteStable(t *testing.T) {
	v1, root := buildCorpusView(t)
	v2, _ := buildCorpusView(t)
	r := llmstxt.NewBodyReader(root)
	cases := []struct {
		name string
		gen  func(emit.View) []byte
	}{
		{"llms.txt", func(v emit.View) []byte { return llmstxt.LLMSTxt(v, llmstxt.Options{}) }},
		{"llms-full.txt", func(v emit.View) []byte { return llmstxt.LLMSFull(v, r, llmstxt.Options{}) }},
		{"llms-small.txt", func(v emit.View) []byte { return llmstxt.LLMSSmall(v, r, llmstxt.Options{}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !bytes.Equal(tc.gen(v1), tc.gen(v2)) {
				t.Errorf("%s not byte-stable across runs", tc.name)
			}
		})
	}
}

// TestBodyReader_RootConfined: a reader cannot read outside its root.
func TestBodyReader_RootConfined(t *testing.T) {
	r := llmstxt.NewBodyReader(corpusRoot(t))
	if _, err := r.Read("README.md"); err != nil {
		t.Errorf("legit read failed: %v", err)
	}
	// An empty-root reader is inert.
	if _, err := llmstxt.NewBodyReader("").Read("README.md"); err == nil {
		t.Error("empty-root reader should not read")
	}
}

// TestBodyReader_RejectsTraversal is the ADR-0003 adversarial path-traversal
// test (mirrors emit's TestFSWriter_RejectsZipSlip): a DocumentID that climbs
// out of the root or names an absolute path must be rejected with a containment
// error, never read off disk.
func TestBodyReader_RejectsTraversal(t *testing.T) {
	r := llmstxt.NewBodyReader(corpusRoot(t))
	for _, id := range []identity.DocumentID{
		"../../../etc/passwd",
		"/etc/passwd",
	} {
		_, err := r.Read(id)
		if err == nil {
			t.Errorf("Read(%q) should be rejected as escaping the root", id)
			continue
		}
		// The rejection must come from the containment guard, not an incidental
		// "file does not exist" off disk: assert the containment message. This
		// catches the absolute-path bug where filepath.Join silently treated
		// "/etc/passwd" as relative and the read only failed because the path
		// happened not to exist.
		if !strings.Contains(err.Error(), "escapes") {
			t.Errorf("Read(%q) error = %q, want a containment ('escapes') error", id, err)
		}
	}
}

// TestBodyReader_RejectsOversizeFile is the ADR-0003 size-guard test for the
// scan→emit TOCTOU window: a file that has grown past the cap since scan time
// must be rejected by the stat-before-read guard rather than read uncapped (an
// OOM-from-growth vector). A small file under the cap still reads fine.
func TestBodyReader_RejectsOversizeFile(t *testing.T) {
	root := t.TempDir()
	const cap = 1024

	small := filepath.Join(root, "small.md")
	if err := os.WriteFile(small, []byte("# ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(root, "big.md")
	if err := os.WriteFile(big, bytes.Repeat([]byte("A"), cap+1), 0o600); err != nil {
		t.Fatal(err)
	}

	r := llmstxt.NewBodyReaderWithCap(root, cap)

	if _, err := r.Read("small.md"); err != nil {
		t.Errorf("under-cap read failed: %v", err)
	}
	_, err := r.Read("big.md")
	if err == nil {
		t.Fatal("over-cap read should be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "exceeds cap") {
		t.Errorf("error = %q, want a size-cap ('exceeds cap') error", err)
	}
}
