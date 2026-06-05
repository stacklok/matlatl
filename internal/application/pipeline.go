package application

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/stacklok/doctopus/internal/domain/analysis"
	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/domain/reference"
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

// Compile-time check that the corpus satisfies the resolver's read-only
// Catalog interface (the resolver lives in the domain and must not import
// corpus; this assertion lives here in the application layer where both meet).
var _ reference.Catalog = (*corpus.Corpus)(nil)

// Result summarizes a pipeline run for the caller to present.
type Result struct {
	// DocumentCount is the number of documents successfully parsed.
	DocumentCount int
	// HeadingCount is the total number of heading slugs indexed.
	HeadingCount int
	// ReferenceCount is the total number of references resolved.
	ReferenceCount int
	// BrokenLinkCount / BrokenAnchorCount / AmbiguousCount are convenience
	// tallies for the human summary.
	BrokenLinkCount   int
	BrokenAnchorCount int
	AmbiguousCount    int
	// Report is the frozen analysis report (broken links/anchors, ambiguous).
	Report *analysis.AnalysisReport
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
	docParser := p.parserFac.New()
	c := corpus.NewCorpus()
	for _, file := range scan.Files {
		if cerr := ctx.Err(); cerr != nil {
			return platform.ExitRuntime, Result{}, fmt.Errorf("pipeline canceled during parse: %w", cerr)
		}
		doc, perr := docParser.Parse(ctx, file)
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

	// Stage 3: Resolve. Turn every raw reference into a health-classified edge
	// using the corpus as the catalog and a root-confined asset-existence lookup
	// (the resolver itself is pure: it only does path arithmetic + catalog
	// lookups, never filesystem access).
	resolver := reference.NewResolver(c, newAssetExistence(p.cfg.RootPath), p.cfg.ResolutionPolicy)
	var refs []reference.Reference
	for _, doc := range c.Documents() {
		refs = append(refs, resolver.ResolveAll(doc.RawReferences)...)
	}

	// Stage 5 (subset): turn unhealthy references into findings and freeze a
	// report. Orphan/unreachable analysis is P3.
	findings := findingsFromReferences(refs)
	report := analysis.NewAnalysisReport(findings)

	res := Result{
		DocumentCount:     c.Len(),
		HeadingCount:      c.HeadingCount(),
		ReferenceCount:    len(refs),
		BrokenLinkCount:   report.CountByKind(analysis.BrokenLink),
		BrokenAnchorCount: report.CountByKind(analysis.BrokenAnchor),
		AmbiguousCount:    report.CountByKind(analysis.Ambiguous),
		Report:            report,
		Notices:           scan.Notices,
	}
	return platform.ExitOK, res, nil
}

// fsAssetExistence answers reference.AssetExistence by stat-ing a cleaned,
// root-relative path under the scan root. The resolver only ever passes in-root,
// pre-cleaned paths (it rejects root escapes before calling this), so this never
// reads outside the root. A markdown path is reported as non-existent here since
// markdown is tracked via the corpus, not as an asset.
type fsAssetExistence struct {
	root string // absolute scan root; empty disables asset checks
}

func newAssetExistence(root string) *fsAssetExistence {
	abs, err := filepath.Abs(root)
	if err != nil {
		return &fsAssetExistence{}
	}
	return &fsAssetExistence{root: abs}
}

func (a *fsAssetExistence) AssetExists(relPath string) bool {
	if a.root == "" || relPath == "" {
		return false
	}
	full := filepath.Join(a.root, filepath.FromSlash(relPath))
	// Defense in depth: never stat outside the root even if a caller slipped.
	// Normalize to slashes and use the shared root-containment predicate.
	rel, err := filepath.Rel(a.root, full)
	if err != nil || identity.EscapesRoot(filepath.ToSlash(rel)) {
		return false
	}
	info, err := os.Lstat(full)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// reportNotices writes scan notices to the log sink (stderr in the CLI).
func (p *Pipeline) reportNotices(notices []Notice) {
	for _, n := range notices {
		_, _ = fmt.Fprintf(p.log, "doctopus: notice [%s] %s: %s\n", n.Kind, n.Path, n.Detail)
	}
}
