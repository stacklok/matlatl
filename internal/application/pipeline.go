package application

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/stacklok/doctopus/internal/domain/analysis"
	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/graphmodel"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/domain/reference"
	"github.com/stacklok/doctopus/internal/platform"
)

// Pipeline orchestrates the six-stage doctopus flow (Scan → Parse → Resolve →
// Build → Analyze → Emit; see architecture.md). It holds the configuration and
// the port implementations it drives.
//
// Stages 1–5 (Scan, Parse, Resolve, Build, Analyze) are all wired here: the
// pipeline scans the root, parses each discovered file into a Corpus, resolves
// every reference, builds the reference graph (ADR 0007), and runs the full
// reachability/orphan/component/HITS/gap analysis into a frozen report +
// GraphMetrics. Emit (stage 6) is performed by the command layer after Run
// (e.g. check writes findings.json/JUnit), so the pipeline stays
// emitter-agnostic.
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
	// BrokenLinkCount / BrokenAnchorCount / AmbiguousCount / OrphanCount /
	// UnreachableCount / KnowledgeGapCount are convenience tallies for the human
	// summary and exit-code decision.
	BrokenLinkCount   int
	BrokenAnchorCount int
	AmbiguousCount    int
	OrphanCount       int
	UnreachableCount  int
	KnowledgeGapCount int
	// Report is the frozen analysis report (all finding kinds).
	Report *analysis.AnalysisReport
	// Metrics is the frozen P3 graph-analysis carrier (graph, components, HITS,
	// degrees, root set, reachability, gaps) for later emitters (P4/P5).
	Metrics *graphmodel.GraphMetrics
	// Notices are non-fatal observations from the scan stage.
	Notices []Notice
}

// Run executes the pipeline: scan → parse → resolve → build → analyze, returning
// a frozen Result (report + GraphMetrics). Stage 6 (emit) is the caller's. It
// returns ExitOK on success and honors context cancellation.
func (p *Pipeline) Run(ctx context.Context) (platform.ExitCode, Result, error) {
	if err := ctx.Err(); err != nil {
		return platform.ExitRuntime, Result{}, fmt.Errorf("pipeline canceled: %w", err)
	}

	// --check-external is accepted but not yet implemented (planned: P6). Surface
	// a notice when set so the flag is never silently a no-op, matching the
	// reachability-indeterminate notice pattern below (ADR 0003 external checks).
	if p.cfg.CheckExternal {
		_, _ = fmt.Fprintln(p.log,
			"doctopus: notice [check-external-noop] --check-external is not yet implemented (planned: P6)")
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

	// Stage 4: Build the reference graph (documents + sections, contains +
	// navigational reference edges; see ADR 0007).
	graph := graphmodel.BuildReferenceGraph(c, refs, graphmodel.BuildOptions{})

	// Stage 5: Analyze. Run reachability/orphan/component/HITS/gap analysis over
	// the document projection, then turn reference + graph findings into a frozen
	// report. Gaps use MinComponentSize:2 so isolated singletons (already reported
	// as orphans) do not also generate an O(k^2) blow-up of singleton gaps
	// (ADR 0007).
	metrics := graphmodel.Analyze(graph, c, graphmodel.AnalyzeOptions{
		RootGlobs: p.cfg.Roots,
		Gaps:      graphmodel.GapOptions{MinComponentSize: 2},
	})
	if metrics.RootSet.Indeterminate && c.Len() > 0 {
		_, _ = fmt.Fprintln(p.log,
			"doctopus: notice [reachability-indeterminate] no root set found "+
				"(no README.md/index.md, no type:index, no --root); "+
				"reachability not computed (orphans still reported)")
	}
	for _, bad := range metrics.RootSet.BadGlobs {
		_, _ = fmt.Fprintf(p.log,
			"doctopus: notice [bad-root-glob] --root pattern %q is malformed and matched nothing\n", bad)
	}
	if metrics.GapsTruncated {
		_, _ = fmt.Fprintf(p.log,
			"doctopus: notice [gaps-truncated] knowledge-gap list capped at %d; "+
				"additional component pairs were not reported\n", graphmodel.MaxGaps)
	}

	findings := findingsFromReferences(refs)
	findings = append(findings, findingsFromMetrics(metrics)...)
	report := analysis.NewAnalysisReport(findings)

	res := Result{
		DocumentCount:     c.Len(),
		HeadingCount:      c.HeadingCount(),
		ReferenceCount:    len(refs),
		BrokenLinkCount:   report.CountByKind(analysis.BrokenLink),
		BrokenAnchorCount: report.CountByKind(analysis.BrokenAnchor),
		AmbiguousCount:    report.CountByKind(analysis.Ambiguous),
		OrphanCount:       report.CountByKind(analysis.Orphan),
		UnreachableCount:  report.CountByKind(analysis.Unreachable),
		KnowledgeGapCount: report.CountByKind(analysis.KnowledgeGap),
		Report:            report,
		Metrics:           metrics,
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
