package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
	"github.com/stacklok/matlatl/internal/platform"
)

// brokenRef builds a broken (unresolved-to-corpus) reference at origin->target.
func brokenRef(origin, target string, line int) reference.Reference {
	return reference.Reference{
		RawReference: reference.RawReference{
			Origin:    identity.DocumentID(origin),
			RawTarget: target,
			Type:      reference.RelativeLink,
			Line:      line,
		},
		Health: reference.Broken,
	}
}

// TestBrokenEdgesFromReferences_DedupAndSort pins two properties of
// brokenEdgesFromReferences: (1) two IDENTICAL broken refs collapse to a single
// BrokenEdge (the diagram emitters must not draw duplicate placeholder edges),
// and (2) the output is deterministically sorted by (Origin, Target), so
// same-origin/different-target edges always appear in a stable order regardless
// of input order. Non-broken refs are ignored.
func TestBrokenEdgesFromReferences_DedupAndSort(t *testing.T) {
	refs := []reference.Reference{
		// Same origin, targets given out of order -> must sort to (zeta, then alpha? no: alpha < zeta).
		brokenRef("a.md", "zeta.md", 5),
		brokenRef("a.md", "alpha.md", 9),
		// Exact duplicate of the first edge (different line, same Origin+Target) -> deduped.
		brokenRef("a.md", "zeta.md", 5),
		// A non-broken ref must be excluded entirely.
		{RawReference: reference.RawReference{Origin: "a.md", RawTarget: "fine.md"}, Health: reference.Valid},
	}

	got := brokenEdgesFromReferences(refs)

	want := []BrokenEdge{
		{Origin: "a.md", Target: "alpha.md"},
		{Origin: "a.md", Target: "zeta.md"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d edges, want %d (dedup failed?): %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("edge[%d] = %+v, want %+v (sort order wrong)", i, got[i], want[i])
		}
	}
}

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
	// refsFor optionally supplies the raw outbound references for a parsed doc, so
	// a test can build a connected corpus (otherwise docs are edgeless). nil means
	// no references.
	refsFor func(file ScannedFile) []reference.RawReference
}

func (f *fakeParser) Parse(_ context.Context, file ScannedFile) (*corpus.Document, error) {
	f.calls++
	if f.onParse != nil {
		f.onParse(file)
	}
	if f.err != nil {
		return nil, f.err
	}
	doc := &corpus.Document{ID: file.ID}
	if f.refsFor != nil {
		doc.RawReferences = f.refsFor(file)
	}
	return doc, nil
}

// fakeFactory hands out the shared fakeParser from New so tests can inspect its
// call count after the (single-threaded) run. news is atomic because the fan-out
// path calls Clone from multiple worker goroutines concurrently.
type fakeFactory struct {
	parser *fakeParser
	news   atomic.Int64
}

func (f *fakeFactory) New() DocumentParser { f.news.Add(1); return f.parser }

// Clone returns an INDEPENDENT fakeParser (its own call counter), honoring the
// per-worker contract: the real goldmark-backed parser is not concurrency-safe,
// so the factory must mint a fresh parser per worker rather than returning the
// same instance. Returning the shared parser here would let the fake silently
// pass a test that the real factory would fail under fan-out.
//
// The cloned parser inherits the configured error and onParse hook from the
// template parser (but NOT its call counter — each worker counts its own). The
// previous version dropped these, so a fan-out test could never exercise a
// per-worker parse error or cancellation hook; TestPipeline_Run_*Concurrent*
// rely on this propagation.
func (f *fakeFactory) Clone() DocumentParser {
	f.news.Add(1)
	return &fakeParser{err: f.parser.err, onParse: f.parser.onParse, refsFor: f.parser.refsFor}
}

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

// TestPipeline_Run_CanceledMidParse_Concurrent covers cancellation on the
// CONCURRENT fan-out path (ParseWorkers>=2): when the context is canceled while
// workers are parsing, the feeder stops enqueuing and parseFiles returns the
// cancellation error (ExitRuntime / context.Canceled). Unlike the sequential
// test it cannot assert an exact parse count (workers race to observe Done), so
// it asserts the fatal outcome only. Run under -race to prove the fan-out has no
// data race on cancellation.
func TestPipeline_Run_CanceledMidParse_Concurrent(t *testing.T) {
	// Enough files that the feeder is still enqueuing when the cancel lands.
	files := make([]ScannedFile, 0, 200)
	for i := 0; i < 200; i++ {
		files = append(files, sf(fmt.Sprintf("d%03d.md", i)))
	}
	scanner := &fakeScanner{result: ScanResult{Files: files}}

	ctx, cancel := context.WithCancel(context.Background())
	// The hook cancels on the first parse; subsequent workers observe Done. cancel
	// is safe to call from multiple goroutines.
	parser := &fakeParser{onParse: func(_ ScannedFile) { cancel() }}
	cfg := DefaultConfig()
	cfg.ParseWorkers = 4 // force the multi-worker fan-out path
	p := NewPipeline(cfg, scanner, newFakeFactory(parser), nil)

	code, _, err := p.Run(ctx)
	if err == nil {
		t.Fatal("Run() canceled mid-parse (concurrent): want error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want wrapping context.Canceled", err)
	}
	if code != platform.ExitRuntime {
		t.Errorf("code = %v, want ExitRuntime", code)
	}
}

// TestPipeline_Run_ParseError_Concurrent exercises a per-worker parse error on
// the CONCURRENT fan-out path. fakeFactory.Clone propagates the template
// parser's configured error to every cloned (per-worker) parser, so each file
// fails to parse. A parse error is non-fatal (ADR 0003): the run completes with
// an empty corpus and exit OK, with the failures surfaced as parse-error notices.
// This guards the test seam that previously dropped the injected error in Clone.
func TestPipeline_Run_ParseError_Concurrent(t *testing.T) {
	files := []ScannedFile{sf("a.md"), sf("b.md"), sf("c.md"), sf("d.md")}
	scanner := &fakeScanner{result: ScanResult{Files: files}}
	parser := &fakeParser{err: errors.New("boom")}
	cfg := DefaultConfig()
	cfg.ParseWorkers = 4
	var log bytes.Buffer
	p := NewPipeline(cfg, scanner, newFakeFactory(parser), &log)

	code, res, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected fatal error: %v", err)
	}
	if code != platform.ExitOK {
		t.Errorf("code = %v, want ExitOK (parse errors are non-fatal)", code)
	}
	if res.DocumentCount != 0 {
		t.Errorf("DocumentCount = %d, want 0 (every file failed to parse)", res.DocumentCount)
	}
	if got := strings.Count(log.String(), "[parse-error]"); got != len(files) {
		t.Errorf("parse-error notices = %d, want %d:\n%s", got, len(files), log.String())
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

// TestPipeline_Run_LowCompactnessNotice pins the [low-compactness] notice rule
// (ADR 0014): it fires only when Documents>=10 AND compactness<0.1, and it never
// affects the exit code (navigability is pure data, not a finding). The fake
// parser yields edgeless documents, so any corpus of >=10 docs has compactness 0
// (no ordered pair reaches another) and trips the notice; a 9-doc corpus does
// not (the threshold is N>=10).
func TestPipeline_Run_LowCompactnessNotice(t *testing.T) {
	run := func(t *testing.T, n int) (string, platform.ExitCode) {
		t.Helper()
		files := make([]ScannedFile, n)
		for i := range files {
			files[i] = sf(fmt.Sprintf("doc%02d.md", i))
		}
		scanner := &fakeScanner{result: ScanResult{Files: files}}
		var log bytes.Buffer
		p := NewPipeline(DefaultConfig(), scanner, newFakeFactory(&fakeParser{}), &log)
		code, res, err := p.Run(context.Background())
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
		if code != platform.ExitOK {
			t.Fatalf("pipeline code = %v, want ExitOK", code)
		}
		// Navigability is pure data and must NEVER gate the exit code: the default
		// (non-strict) run over these edgeless docs has no broken links/anchors, so
		// despite the very low compactness it stays ExitOK. (Under --strict the
		// orphan/unreachable structure findings gate — that is ADR 0005, NOT
		// navigability — so we assert the non-strict contract here.)
		if ec := res.CheckExitCode(false); ec != platform.ExitOK {
			t.Errorf("CheckExitCode(false) = %v, want ExitOK (navigability never gates)", ec)
		}
		return log.String(), code
	}

	t.Run("fires at N>=10 and low compactness", func(t *testing.T) {
		out, _ := run(t, 10)
		if !strings.Contains(out, "[low-compactness]") {
			t.Errorf("expected a [low-compactness] notice for 10 disconnected docs, got:\n%s", out)
		}
	})
	t.Run("silent below the document threshold", func(t *testing.T) {
		out, _ := run(t, 9)
		if strings.Contains(out, "[low-compactness]") {
			t.Errorf("did not expect a [low-compactness] notice for 9 docs (N<10), got:\n%s", out)
		}
	})

	// The Compactness<0.1 comparator itself must be exercised, not just the
	// N-boundary: a well-connected N>=10 corpus has compactness well ABOVE 0.1, so
	// the notice must STAY SILENT. This catches a comparator flip (>= vs <) or a
	// threshold-widening regression. We pin the BEHAVIOR (silent), not the float.
	t.Run("silent when compactness is healthy at N>=10", func(t *testing.T) {
		const n = 10
		const hub = "doc00.md"
		files := make([]ScannedFile, n)
		for i := range files {
			files[i] = sf(fmt.Sprintf("doc%02d.md", i))
		}
		// Hub<->spoke: the hub links to all 9 leaves and every leaf links back to the
		// hub. Every doc reaches every other in <=2 hops, so compactness ~0.91
		// (>> 0.1); verified by the navigability fixture tests. (18 resolved refs.)
		parser := &fakeParser{refsFor: func(file ScannedFile) []reference.RawReference {
			if file.ID.String() == hub {
				refs := make([]reference.RawReference, 0, n-1)
				for i := 1; i < n; i++ {
					refs = append(refs, reference.RawReference{
						Origin: file.ID, RawTarget: fmt.Sprintf("doc%02d.md", i), Type: reference.RelativeLink, Line: 1,
					})
				}
				return refs
			}
			return []reference.RawReference{
				{Origin: file.ID, RawTarget: hub, Type: reference.RelativeLink, Line: 1},
			}
		}}
		scanner := &fakeScanner{result: ScanResult{Files: files}}
		var log bytes.Buffer
		cfg := DefaultConfig()
		cfg.ParseWorkers = 1
		p := NewPipeline(cfg, scanner, newFakeFactory(parser), &log)
		_, res, err := p.Run(context.Background())
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
		// Guard the fixture: the corpus really is well-connected (>0.1), so a SILENT
		// result means the comparator held, not that compactness happened to be low.
		if got := res.Metrics.Navigability.Compactness; got <= 0.1 {
			t.Fatalf("fixture invalid: expected compactness >0.1, got %.4f", got)
		}
		if strings.Contains(log.String(), "[low-compactness]") {
			t.Errorf("did not expect a [low-compactness] notice for a well-connected 10-doc corpus, got:\n%s", log.String())
		}
	})
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
		NoticeTruncated, NoticeWalkError, NoticeIOError, NoticeConfig,
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
