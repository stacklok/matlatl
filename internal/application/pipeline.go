package application

import (
	"context"
	"fmt"
	"io"

	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/platform"
)

// Pipeline orchestrates the six-stage doctopus flow (Scan → Parse → Resolve →
// Build → Analyze → Emit; see architecture.md). It holds the configuration and
// the port implementations it drives.
//
// As of Phase 1 the Scan and Parse stages are real: the pipeline scans the
// root, parses each discovered file, and assembles a Corpus (with the heading
// inventory). Resolve/Build/Analyze/Emit remain no-ops for later phases.
//
// Concurrency (P6 note): parsing is single-threaded and documents are merged
// into the Corpus sequentially. This sequential merge is the seam where fan-out
// parsing will plug in later (per-worker parser, single-threaded merge).
type Pipeline struct {
	cfg       Config
	scanner   FileScanner
	parserFac DocumentParserFactory
	writer    ArtifactWriter
	// log is where stage progress / notices are written; io.Discard if nil.
	log io.Writer
}

// NewPipeline constructs a Pipeline from a config, its ports, and a log sink.
// The parser is obtained from a DocumentParserFactory (the P6 fan-out seam: one
// parser today, one-per-worker later). A nil log sink discards output.
func NewPipeline(cfg Config, scanner FileScanner, parserFac DocumentParserFactory, writer ArtifactWriter, log io.Writer) *Pipeline {
	if log == nil {
		log = io.Discard
	}
	return &Pipeline{
		cfg:       cfg,
		scanner:   scanner,
		parserFac: parserFac,
		writer:    writer,
		log:       log,
	}
}

// Result summarizes a pipeline run for the caller to present.
type Result struct {
	// DocumentCount is the number of documents successfully parsed.
	DocumentCount int
	// HeadingCount is the total number of heading slugs indexed.
	HeadingCount int
	// Notices are non-fatal observations from the scan stage.
	Notices []Notice
}

// Run executes the pipeline. As of Phase 1 it scans + parses; downstream stages
// are no-ops. It returns ExitOK on success and honors context cancellation.
func (p *Pipeline) Run(ctx context.Context) (platform.ExitCode, Result, error) {
	if err := ctx.Err(); err != nil {
		return platform.ExitRuntime, Result{}, fmt.Errorf("pipeline canceled: %w", err)
	}

	// Stage 1: Scan.
	scan, err := p.scanner.Scan(ctx, p.cfg.RootPath)
	if err != nil {
		return platform.ExitRuntime, Result{}, fmt.Errorf("scan: %w", err)
	}
	p.reportNotices(scan.Notices)

	// Stage 2: Parse + merge into the Corpus (single-threaded merge seam).
	// One parser is obtained from the factory for the whole sequential pass; P6
	// fan-out will instead Clone one per worker.
	parser := p.parserFac.New()
	c := corpus.NewCorpus()
	for _, file := range scan.Files {
		if cerr := ctx.Err(); cerr != nil {
			return platform.ExitRuntime, Result{}, fmt.Errorf("pipeline canceled during parse: %w", cerr)
		}
		doc, perr := parser.Parse(ctx, file)
		if perr != nil {
			// A single unparseable file is a notice, not a fatal error: the scan
			// continues so one hostile/broken file cannot abort the whole run.
			_, _ = fmt.Fprintf(p.log, "doctopus: notice [parse-error] %s: %v\n", file.ID, perr)
			continue
		}
		if aerr := c.Add(doc); aerr != nil {
			_, _ = fmt.Fprintf(p.log, "doctopus: notice [merge-error] %s: %v\n", file.ID, aerr)
			continue
		}
	}

	// Stages 3-6 (Resolve, Build, Analyze, Emit): TODO later phases.

	return platform.ExitOK, Result{
		DocumentCount: c.Len(),
		HeadingCount:  c.HeadingCount(),
		Notices:       scan.Notices,
	}, nil
}

// reportNotices writes scan notices to the log sink (stderr in the CLI).
func (p *Pipeline) reportNotices(notices []Notice) {
	for _, n := range notices {
		_, _ = fmt.Fprintf(p.log, "doctopus: notice [%s] %s: %s\n", n.Kind, n.Path, n.Detail)
	}
}
