package application

import (
	"context"
	"errors"
	"testing"

	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/platform"
)

// The fakes below double as a compile-time proof that the ports are real,
// satisfiable seams.

// The fakes track call counts so the no-op test can prove the skeleton
// pipeline does not (yet) touch the ports — catching accidental wiring in P1.

type fakeScanner struct {
	files []ScannedFile
	err   error
	calls int
}

func (f *fakeScanner) Scan(_ context.Context, _ string) ([]ScannedFile, error) {
	f.calls++
	return f.files, f.err
}

type fakeParser struct {
	err   error
	calls int
}

func (f *fakeParser) Parse(_ context.Context, file ScannedFile) (*corpus.Document, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &corpus.Document{ID: file.ID}, nil
}

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
	_ FileScanner    = (*fakeScanner)(nil)
	_ DocumentParser = (*fakeParser)(nil)
	_ ArtifactWriter = (*fakeWriter)(nil)
)

func TestPipeline_Run_Noop(t *testing.T) {
	scanner := &fakeScanner{}
	parser := &fakeParser{}
	writer := &fakeWriter{}
	p := NewPipeline(DefaultConfig(), scanner, parser, writer, nil)

	code, res, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if code != platform.ExitOK {
		t.Errorf("Run() code = %v, want ExitOK", code)
	}
	if res.DocumentCount != 0 {
		t.Errorf("Run() DocumentCount = %d, want 0 (skeleton)", res.DocumentCount)
	}
	// The skeleton must not call any port yet.
	if scanner.calls != 0 || parser.calls != 0 || writer.calls != 0 {
		t.Errorf("ports were called (scanner=%d parser=%d writer=%d), want all 0",
			scanner.calls, parser.calls, writer.calls)
	}
}

func TestPipeline_Run_CanceledContext(t *testing.T) {
	p := NewPipeline(DefaultConfig(), &fakeScanner{}, &fakeParser{}, &fakeWriter{}, nil)
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

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.RootPath != "." {
		t.Errorf("RootPath = %q, want .", cfg.RootPath)
	}
	if !cfg.ResolutionPolicy.Valid() {
		t.Error("DefaultConfig ResolutionPolicy is invalid")
	}
}
