package application

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/okf"
	"github.com/stacklok/matlatl/internal/domain/reference"
	"github.com/stacklok/matlatl/internal/platform"
)

// Pipeline orchestrates the six-stage matlatl flow (Scan → Parse → Resolve →
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
// Concurrency: parsing fans out across a bounded worker pool (each worker owns
// an independent parser via DocumentParserFactory.Clone, since goldmark parsers
// are not safe to share), and the parsed documents are merged into the Corpus on
// this single goroutine in DocumentID-sorted order. That sequential merge is
// what keeps the corpus, heading inventory and every downstream artifact
// byte-identical to the single-threaded path at any worker count.
type Pipeline struct {
	cfg       Config
	scanner   FileScanner
	parserFac DocumentParserFactory
	// log is where stage progress / notices are written; io.Discard if nil.
	log io.Writer
}

// NewPipeline constructs a Pipeline from a config, its ports, and a log sink.
// Parsers are obtained from a DocumentParserFactory: the single-worker fast path
// uses one parser, and the fan-out path mints one per worker via Clone. A nil
// log sink discards output.
//
// Emit (stage 6) is the command layer's job, so the pipeline takes no artifact
// writer: it returns a frozen Result the caller renders/writes (e.g. `check`
// writes findings.json/JUnit). The ArtifactWriter port stays on the command
// layer for that reason.
func NewPipeline(cfg Config, scanner FileScanner, parserFac DocumentParserFactory, log io.Writer) *Pipeline {
	if log == nil {
		log = io.Discard
	}
	return &Pipeline{
		cfg:       cfg,
		scanner:   scanner,
		parserFac: parserFac,
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
	// SuggestedLinkCount is the number of topology-based suggested-link findings
	// (ADR 0013). Info; never affects the exit code. SuggestedLinksTruncated
	// reports the suggestion list was capped or a hub neighbour was skipped.
	SuggestedLinkCount      int
	SuggestedLinksTruncated bool
	// UnderLinkedCount / DeadEndCount are the graduated structure-tier tallies
	// (ADR 0012). They affect the exit code only when StructureFindingsSeverity is
	// Warning (consulted by CheckExitCode).
	UnderLinkedCount int
	DeadEndCount     int
	// StructureFindingsSeverity is the resolved severity of under-linked/dead-end
	// findings for this run, carried so CheckExitCode can decide whether they gate
	// --strict.
	StructureFindingsSeverity StructureFindingsSeverity
	// DeadLinkCount is the number of failed external links (only non-zero when
	// --check-external is enabled). It does not affect the default run.
	DeadLinkCount int
	// ArticulationPointCount / BridgeCount are the critical-path structure tallies
	// (ADR 0015): cut vertices and cut edges of the undirected closure. Both are
	// Info findings that NEVER affect the exit code (consulted by no exit-code
	// path); they are carried for the human summary and machine artifacts.
	ArticulationPointCount int
	BridgeCount            int
	// LowScentAnchorCount is the number of low-scent-anchor findings (ADR 0016):
	// links whose anchor text barely previews the target. Info; NEVER affects the
	// exit code; carried for the human summary and machine artifacts.
	LowScentAnchorCount int
	// FarFromRootCount is the number of far-from-root findings (ADR 0021):
	// documents reachable but at or beyond the hop-distance threshold from every
	// root. Info; NEVER affects the exit code; carried for the human summary and
	// machine artifacts.
	FarFromRootCount int
	// OKF conformance (ADR 0023). OKFMode reports whether OKF conformance mode was
	// on for this run; the rest are meaningful only when it is. OKFConformant is
	// the §9 verdict (all three rule counts zero); OKFVersion is the bundle's
	// declared okf_version ("" when none). The three counts are the Error-severity
	// okf-* finding tallies. When OKFMode && !OKFConformant, CheckExitCode gates
	// exit 1 regardless of --strict.
	OKFMode                       bool
	OKFConformant                 bool
	OKFVersion                    string
	OKFMissingFrontmatterCount    int
	OKFMissingTypeCount           int
	OKFReservedFileStructureCount int
	// Report is the frozen analysis report (all finding kinds).
	Report *analysis.AnalysisReport
	// Metrics is the frozen P3 graph-analysis carrier (graph, components, HITS,
	// degrees, root set, reachability, gaps) for later emitters (P4/P5).
	Metrics *graphmodel.GraphMetrics
	// Corpus is the frozen corpus the run was computed over. Human emitters read
	// it for per-document presentation data (title, description, mod-date) that
	// the metrics/report do not carry. It is read-only; emitters must not mutate
	// it (ADR 0004). nil for an empty/failed run.
	Corpus *corpus.Corpus
	// BrokenEdges are the unresolved navigational references (origin → raw
	// target) extracted at resolution time. The frozen graph keeps only Valid
	// edges, so the P4 diagram emitters read this to render red placeholder
	// target nodes (ADR 0003 styling) without re-parsing finding messages.
	// Sorted (Origin, Target) for determinism.
	BrokenEdges []BrokenEdge
	// Notices are non-fatal observations from the scan stage.
	Notices []Notice
}

// BrokenEdge is an origin document and the raw target text of a reference that
// did not resolve to an in-corpus document (a broken link). It carries the
// presentation data the diagram emitters need without exposing the resolver.
type BrokenEdge struct {
	Origin identity.DocumentID
	Target string
}

// Run executes the pipeline: scan → parse → resolve → build → analyze, returning
// a frozen Result (report + GraphMetrics). Stage 6 (emit) is the caller's. It
// returns ExitOK on success and honors context cancellation.
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

	// Stage 2: Parse (fan-out worker pool) + merge into the Corpus
	// (single-threaded, DETERMINISTIC). Workers parse with their own cloned
	// parser; results are sorted by DocumentID and merged on this goroutine so
	// the Corpus, heading inventory and all downstream output are byte-identical
	// to the single-threaded path regardless of worker count (P6).
	c := corpus.NewCorpus()
	parsed, perr := p.parseFiles(ctx, scan.Files)
	if perr != nil {
		return platform.ExitRuntime, Result{}, perr
	}
	for _, pr := range parsed { // sorted by DocumentID
		if pr.err != nil {
			// A single unparseable file is a notice, not a fatal error: the scan
			// continues so one hostile/broken file cannot abort the whole run.
			_, _ = fmt.Fprintf(p.log, "matlatl: notice [parse-error] %s: %v\n", pr.id, pr.err)
			continue
		}
		if aerr := c.Add(pr.doc); aerr != nil {
			_, _ = fmt.Fprintf(p.log, "matlatl: notice [merge-error] %s: %v\n", pr.id, aerr)
			continue
		}
	}
	// Freeze the corpus at the parse/merge boundary: resolution and analysis run
	// over a read-only corpus (the freeze contract is now enforced, ADR 0004).
	c.Freeze()

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
	graph := graphmodel.BuildReferenceGraph(c, refs, graphmodel.BuildOptions{
		// Under --strict, a directory link validates but does not vouch for the
		// folder's contents (ADR 0008): non-index siblings still surface as
		// orphans/unreachable. The default (lenient) policy lets a directory link
		// confer reachability on the folder's direct children.
		StrictDirectoryLinks: p.cfg.Strict,
	})

	// Stage 5: Analyze. Run reachability/orphan/component/HITS/gap analysis over
	// the document projection, then turn reference + graph findings into a frozen
	// report. Gaps use MinComponentSize:2 so isolated singletons (already reported
	// as orphans) do not also generate an O(k^2) blow-up of singleton gaps
	// (ADR 0007).
	metrics := graphmodel.Analyze(graph, c, graphmodel.AnalyzeOptions{
		RootGlobs: p.cfg.Roots,
		Gaps:      graphmodel.GapOptions{MinComponentSize: 2},
		// Link prediction (ADR 0013) is an additive signal. The config-only
		// LinkSuggestionMinShared knob tunes the shared-neighbour floor; <=0 is
		// normalized to the domain default (2) inside PredictLinks.
		LinkPrediction:   graphmodel.LinkPredictionOptions{MinSharedNeighbours: p.cfg.LinkSuggestionMinShared},
		InboundThreshold: p.cfg.InboundThreshold, // <=0 normalized to default in Analyze
		// Far-from-root hop threshold (ADR 0021); <=0 normalized to default in Analyze.
		FarFromRootThreshold: p.cfg.FarFromRootThreshold,
	})
	if metrics.RootSet.Indeterminate && c.Len() > 0 {
		_, _ = fmt.Fprintln(p.log,
			"matlatl: notice [reachability-indeterminate] no root set found "+
				"(no README.md/index.md, no type:index, no --root); "+
				"reachability not computed (orphans still reported)")
	}
	for _, bad := range metrics.RootSet.BadGlobs {
		_, _ = fmt.Fprintf(p.log,
			"matlatl: notice [bad-root-glob] --root pattern %q is malformed and matched nothing\n", bad)
	}
	if metrics.GapsTruncated {
		_, _ = fmt.Fprintf(p.log,
			"matlatl: notice [gaps-truncated] knowledge-gap list capped at %d; "+
				"additional component pairs were not reported\n", graphmodel.MaxGaps)
	}
	if metrics.SuggestedLinksTruncated {
		_, _ = fmt.Fprintf(p.log,
			"matlatl: notice [suggested-links-truncated] suggested-link list capped at %d "+
				"(or a hub neighbour above the fan-out limit was skipped); "+
				"additional pairs were not reported\n", graphmodel.MaxSuggestedLinks)
	}
	// Navigability scalars are pure data (ADR 0014) and never gate the exit code.
	// A single non-gating notice flags a corpus that is large but very poorly
	// connected (compactness near 0), so an agent/maintainer notices the
	// navigability problem without it failing the build.
	if nav := metrics.Navigability; nav.Documents >= 10 && nav.Compactness < 0.1 {
		_, _ = fmt.Fprintf(p.log,
			"matlatl: notice [low-compactness] corpus compactness is %.3f across %d documents "+
				"(navigational reachability is very low; consider linking clusters together)\n",
			nav.Compactness, nav.Documents)
	}

	// Resolve the structure-finding severity (default Info when unset) and the
	// actual inbound threshold the domain used (it floors <=0 to the default), so
	// the finding messages/details and the exit-code decision all agree.
	structureSev := p.cfg.StructureFindingsSeverity
	if !structureSev.Valid() {
		structureSev = StructureFindingsInfo
	}
	threshold := p.cfg.InboundThreshold
	if threshold <= 0 {
		threshold = graphmodel.DefaultInboundThreshold
	}

	findings := findingsFromReferences(refs)
	findings = append(findings, findingsFromMetrics(metrics, threshold, structureSev)...)
	// Opt-in external link checking (--check-external). OFF by default so the
	// deterministic output is unchanged. DeadLink findings are appended only when
	// the checker is wired and enabled (ADR 0003); they never affect the default
	// run. The SSRF guard lives in the injected checker (infrastructure).
	if p.cfg.CheckExternal {
		findings = append(findings, p.checkExternalLinks(ctx, refs)...)
	}
	// OKF conformance mode (ADR 0023). Only when --okf is on: run the three §9
	// rules over the frozen corpus and append the mode-scoped okf-* Error findings.
	// The verdict is carried on the Result (below) and reported separately; the
	// findings gate `check` via CheckExitCode. Health findings are untouched — the
	// verdict never turns a broken link into non-conformance and never relaxes the
	// health gate.
	var okfReport okf.Report
	if p.cfg.OKF {
		okfReport = okf.Check(c)
		findings = append(findings, okfFindings(okfReport)...)
	}
	report := analysis.NewAnalysisReport(findings)
	brokenEdges := brokenEdgesFromReferences(refs)

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
		UnderLinkedCount:  report.CountByKind(analysis.UnderLinked),
		DeadEndCount:      report.CountByKind(analysis.DeadEnd),

		SuggestedLinkCount:      report.CountByKind(analysis.SuggestedLink),
		SuggestedLinksTruncated: metrics.SuggestedLinksTruncated,

		StructureFindingsSeverity: structureSev,

		DeadLinkCount: report.CountByKind(analysis.DeadLink),

		ArticulationPointCount: report.CountByKind(analysis.ArticulationPoint),
		BridgeCount:            report.CountByKind(analysis.Bridge),

		LowScentAnchorCount: report.CountByKind(analysis.LowScentAnchor),

		FarFromRootCount: report.CountByKind(analysis.FarFromRoot),

		OKFMode:                       p.cfg.OKF,
		OKFConformant:                 okfReport.Conformant,
		OKFVersion:                    okfReport.DetectedVersion,
		OKFMissingFrontmatterCount:    report.CountByKind(analysis.OKFMissingFrontmatter),
		OKFMissingTypeCount:           report.CountByKind(analysis.OKFMissingType),
		OKFReservedFileStructureCount: report.CountByKind(analysis.OKFReservedFileStructure),

		Report:      report,
		Metrics:     metrics,
		Corpus:      c,
		BrokenEdges: brokenEdges,
		Notices:     scan.Notices,
	}
	return platform.ExitOK, res, nil
}

// brokenEdgesFromReferences extracts the origin→target pairs of references that
// did not resolve to an in-corpus document (Health==Broken), sorted (Origin,
// Target) and de-duplicated, for the diagram emitters' red placeholder nodes.
func brokenEdgesFromReferences(refs []reference.Reference) []BrokenEdge {
	seen := make(map[BrokenEdge]struct{})
	var out []BrokenEdge
	for _, r := range refs {
		if r.Health != reference.Broken {
			continue
		}
		e := BrokenEdge{Origin: r.Origin, Target: rawTargetText(r)}
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b BrokenEdge) int {
		if c := cmp.Compare(a.Origin, b.Origin); c != 0 {
			return c
		}
		return cmp.Compare(a.Target, b.Target)
	})
	return out
}

// fsAssetExistence answers reference.AssetExistence by stat-ing a cleaned,
// root-relative path under the scan root. The resolver only ever passes in-root,
// pre-cleaned paths (it rejects root escapes before calling this), so this never
// reads outside the root. An existing non-markdown FILE OR DIRECTORY counts as
// an asset; markdown is reported as non-existent here since markdown is tracked
// via the corpus, not as an asset. Symlinks are deliberately excluded: this uses
// Lstat, so a symlink is neither a regular file nor a directory and stays
// non-asset — the load-bearing safety property that keeps scanning from
// following links out of (or around) the root (ADR 0003).
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
	// Defense in depth: never stat outside the root even if a caller slipped.
	// Use the shared root-containment join (the same guard the writer/body
	// reader use).
	full, ok := identity.Contains(a.root, relPath)
	if !ok {
		return false
	}
	info, err := os.Lstat(full)
	if err != nil {
		return false
	}
	// An existing non-markdown regular file or directory counts as an asset.
	// Symlinks are excluded by construction: Lstat does not follow them, so a
	// symlink is neither regular nor a directory and stays a non-asset (ADR 0003
	// containment — never follow links out of/around the root).
	return info.Mode().IsRegular() || info.IsDir()
}

// reportNotices writes scan notices to the log sink (stderr in the CLI).
func (p *Pipeline) reportNotices(notices []Notice) {
	for _, n := range notices {
		_, _ = fmt.Fprintf(p.log, "matlatl: notice [%s] %s: %s\n", n.Kind, n.Path, n.Detail)
	}
}
