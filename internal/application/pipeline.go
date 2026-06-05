package application

import (
	"context"
	"fmt"
	"io"

	"github.com/stacklok/doctopus/internal/platform"
)

// Pipeline orchestrates the six-stage doctopus flow (Scan → Parse → Resolve →
// Build → Analyze → Emit; see architecture.md). It holds the configuration and
// the port implementations it drives. In Phase 0 Run is a no-op spine: it walks
// the stage sequence, logs what it would do, and returns ExitOK. The real stage
// logic lands in later phases.
type Pipeline struct {
	cfg     Config
	scanner FileScanner
	parser  DocumentParser
	writer  ArtifactWriter
	// log is where stage progress is written; defaults to io.Discard if nil.
	log io.Writer
}

// NewPipeline constructs a Pipeline from a config, its ports, and a log sink.
// A nil log sink discards output.
func NewPipeline(cfg Config, scanner FileScanner, parser DocumentParser, writer ArtifactWriter, log io.Writer) *Pipeline {
	if log == nil {
		log = io.Discard
	}
	return &Pipeline{
		cfg:     cfg,
		scanner: scanner,
		parser:  parser,
		writer:  writer,
		log:     log,
	}
}

// Result summarizes a pipeline run for the caller to present.
type Result struct {
	// DocumentCount is the number of documents processed (0 in the skeleton).
	DocumentCount int
}

// Run executes the pipeline. In Phase 0 it performs the no-op stage sequence
// and always returns ExitOK with a zeroed Result. It honors context
// cancellation between stages.
func (p *Pipeline) Run(ctx context.Context) (platform.ExitCode, Result, error) {
	stages := []string{"scan", "parse", "resolve", "build", "analyze", "emit"}
	for _, stage := range stages {
		if err := ctx.Err(); err != nil {
			return platform.ExitRuntime, Result{}, fmt.Errorf("pipeline canceled before %s: %w", stage, err)
		}
		// TODO(P1+): replace the no-op with the real stage implementation.
		// Log writes are best-effort progress output; a failing sink does not
		// abort the run.
		_, _ = fmt.Fprintf(p.log, "doctopus: stage %q (skeleton no-op)\n", stage)
	}
	return platform.ExitOK, Result{DocumentCount: 0}, nil
}
