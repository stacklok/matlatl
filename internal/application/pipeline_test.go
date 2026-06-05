package application

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/platform"
)

// The fakes below double as a compile-time proof that the ports are real,
// satisfiable seams, and track call counts so tests can assert the pipeline
// drives them as expected.

type fakeScanner struct {
	result ScanResult
	err    error
	calls  int
}

func (f *fakeScanner) Scan(_ context.Context, _ string) (ScanResult, error) {
	f.calls++
	return f.result, f.err
}

type fakeParser struct {
	err     error
	calls   int
	onParse func(file ScannedFile) // optional hook (e.g. to cancel a context)
}

func (f *fakeParser) Parse(_ context.Context, file ScannedFile) (*corpus.Document, error) {
	f.calls++
	if f.onParse != nil {
		f.onParse(file)
	}
	if f.err != nil {
		return nil, f.err
	}
	return &corpus.Document{ID: file.ID}, nil
}

// fakeFactory hands out the shared fakeParser from New so tests can inspect its
// call count after the (single-threaded) run.
type fakeFactory struct {
	parser *fakeParser
	news   int
}

func (f *fakeFactory) New() DocumentParser { f.news++; return f.parser }

// Clone returns an INDEPENDENT fakeParser (its own call counter), honoring the
// per-worker contract: the real goldmark-backed parser is not concurrency-safe,
// so the factory must mint a fresh parser per worker rather than returning the
// same instance. Returning the shared parser here would let the fake silently
// pass a test that the real factory would fail under fan-out.
func (f *fakeFactory) Clone() DocumentParser { f.news++; return &fakeParser{} }

func newFakeFactory(p *fakeParser) *fakeFactory { return &fakeFactory{parser: p} }

type fakeWriter struct {
	written [][]Artifact
	calls   int
}

func (f *fakeWriter) Write(_ context.Context, artifacts []Artifact) error {
	f.calls++
	f.written = append(f.written, artifacts)
	return nil
}

var (
	_ FileScanner           = (*fakeScanner)(nil)
	_ DocumentParser        = (*fakeParser)(nil)
	_ DocumentParserFactory = (*fakeFactory)(nil)
	_ ArtifactWriter        = (*fakeWriter)(nil)
)

func sf(id string) ScannedFile {
	return ScannedFile{ID: identity.DocumentID(id), Path: "/abs/" + id}
}

func TestPipeline_Run_ScansAndParses(t *testing.T) {
	scanner := &fakeScanner{result: ScanResult{
		Files: []ScannedFile{sf("a.md"), sf("dir/b.md")},
	}}
	parser := &fakeParser{}
	// ParseWorkers:1 forces the sequential path, which drives the single shared
	// parser from Factory.New so this test can assert its per-file call count.
	// (The concurrent path Clones an independent parser per worker by contract;
	// determinism across worker counts is covered by TestPipeline_Determinism.)
	cfg := DefaultConfig()
	cfg.ParseWorkers = 1
	p := NewPipeline(cfg, scanner, newFakeFactory(parser), nil)

	code, res, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if code != platform.ExitOK {
		t.Errorf("Run() code = %v, want ExitOK", code)
	}
	if scanner.calls != 1 {
		t.Errorf("scanner called %d times, want 1", scanner.calls)
	}
	if parser.calls != 2 {
		t.Errorf("parser called %d times, want 2 (one per file)", parser.calls)
	}
	if res.DocumentCount != 2 {
		t.Errorf("DocumentCount = %d, want 2", res.DocumentCount)
	}
}

func TestPipeline_Run_EmptyCorpus(t *testing.T) {
	p := NewPipeline(DefaultConfig(), &fakeScanner{}, newFakeFactory(&fakeParser{}), nil)
	code, res, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if code != platform.ExitOK {
		t.Errorf("empty corpus code = %v, want ExitOK (ADR 0005: empty is not an error)", code)
	}
	if res.DocumentCount != 0 {
		t.Errorf("DocumentCount = %d, want 0", res.DocumentCount)
	}
}

func TestPipeline_Run_ParseErrorIsNonFatal(t *testing.T) {
	// A failing parser must not abort the whole run; the document is dropped.
	scanner := &fakeScanner{result: ScanResult{Files: []ScannedFile{sf("a.md")}}}
	parser := &fakeParser{err: errors.New("boom")}
	p := NewPipeline(DefaultConfig(), scanner, newFakeFactory(parser), nil)

	code, res, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() should not fail on a single parse error: %v", err)
	}
	if code != platform.ExitOK {
		t.Errorf("code = %v, want ExitOK", code)
	}
	if res.DocumentCount != 0 {
		t.Errorf("DocumentCount = %d, want 0 (the bad doc is dropped)", res.DocumentCount)
	}
}

func TestPipeline_Run_ScanErrorIsFatal(t *testing.T) {
	scanner := &fakeScanner{err: errors.New("io failure")}
	p := NewPipeline(DefaultConfig(), scanner, newFakeFactory(&fakeParser{}), nil)

	code, _, err := p.Run(context.Background())
	if err == nil {
		t.Fatal("Run() with scan error: want error")
	}
	if code != platform.ExitRuntime {
		t.Errorf("code = %v, want ExitRuntime", code)
	}
}

func TestPipeline_Run_PropagatesNotices(t *testing.T) {
	scanner := &fakeScanner{result: ScanResult{
		Notices: []Notice{{Kind: NoticeSkippedSymlink, Path: "/x", Detail: "skipped"}},
	}}
	p := NewPipeline(DefaultConfig(), scanner, newFakeFactory(&fakeParser{}), nil)

	_, res, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notices) != 1 || res.Notices[0].Kind != NoticeSkippedSymlink {
		t.Errorf("notices = %+v, want one skipped-symlink notice", res.Notices)
	}
}

func TestPipeline_Run_CanceledContextBeforeScan(t *testing.T) {
	p := NewPipeline(DefaultConfig(), &fakeScanner{}, newFakeFactory(&fakeParser{}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	code, _, err := p.Run(ctx)
	if err == nil {
		t.Fatal("Run() with canceled context: want error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want wrapping context.Canceled", err)
	}
	if code != platform.ExitRuntime {
		t.Errorf("Run() code = %v, want ExitRuntime", code)
	}
}

// TestPipeline_Run_CanceledMidParse covers the per-iteration ctx check inside
// the parse loop: cancellation triggered while parsing the first file must
// abort before the second, returning ExitRuntime / context.Canceled.
func TestPipeline_Run_CanceledMidParse(t *testing.T) {
	scanner := &fakeScanner{result: ScanResult{
		Files: []ScannedFile{sf("a.md"), sf("b.md"), sf("c.md")},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	parser := &fakeParser{onParse: func(_ ScannedFile) { cancel() }}
	// Force the sequential path so the per-iteration ctx check is the one under
	// test (the concurrent path's cancellation is covered separately and cannot
	// guarantee "exactly one parse" before workers observe Done).
	cfg := DefaultConfig()
	cfg.ParseWorkers = 1
	p := NewPipeline(cfg, scanner, newFakeFactory(parser), nil)

	code, _, err := p.Run(ctx)
	if err == nil {
		t.Fatal("Run() canceled mid-parse: want error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want wrapping context.Canceled", err)
	}
	if code != platform.ExitRuntime {
		t.Errorf("code = %v, want ExitRuntime", code)
	}
	// Only the first file should have been parsed before the cancel was observed.
	if parser.calls != 1 {
		t.Errorf("parser called %d times, want 1 (aborted after cancel)", parser.calls)
	}
}

// TestPipeline_Run_BadRootGlobNotice pins the bad-root-glob notice the analyze
// stage emits when a configured --root pattern is malformed (ResolveRootSet
// collects it in BadGlobs). The notice must name the offending pattern so it is
// never silently ignored.
func TestPipeline_Run_BadRootGlobNotice(t *testing.T) {
	scanner := &fakeScanner{result: ScanResult{Files: []ScannedFile{sf("a.md")}}}
	cfg := DefaultConfig()
	cfg.Roots = []string{"["} // unterminated character class -> ErrBadPattern
	var log bytes.Buffer
	p := NewPipeline(cfg, scanner, newFakeFactory(&fakeParser{}), &log)

	if _, _, err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	out := log.String()
	if !strings.Contains(out, "[bad-root-glob]") || !strings.Contains(out, `"["`) {
		t.Errorf("expected a bad-root-glob notice naming the pattern, got:\n%s", out)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.RootPath != "." {
		t.Errorf("RootPath = %q, want .", cfg.RootPath)
	}
	if !cfg.ResolutionPolicy.Valid() {
		t.Error("DefaultConfig ResolutionPolicy is invalid")
	}
}

func TestNoticeKind_String(t *testing.T) {
	all := []NoticeKind{
		NoticeSkippedSymlink, NoticeEscapesRoot, NoticeOversized,
		NoticeTruncated, NoticeWalkError, NoticeIOError,
	}
	seen := make(map[string]bool)
	for _, k := range all {
		s := k.String()
		if s == "" || s == "unknown" {
			t.Errorf("NoticeKind(%d).String() = %q, bad", int(k), s)
		}
		if seen[s] {
			t.Errorf("duplicate NoticeKind String() %q", s)
		}
		seen[s] = true
	}
	if NoticeKind(99).String() != "unknown" {
		t.Error("out-of-range NoticeKind should String() to unknown")
	}
}
