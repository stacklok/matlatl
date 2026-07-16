package emit

import (
	"path"
	"slices"
	"time"

	ignore "github.com/sabhiram/go-gitignore"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// View is the render-ready, emitter-agnostic snapshot every human emitter
// (terminal, markdown, mermaid, dot, index) renders from. It is derived ONCE
// from the frozen application.Result + corpus.Corpus and is itself treated as
// immutable: emitters read it, never mutate the domain model (ADR 0004). All
// slices are sorted so every emitter produces byte-stable output.
//
// The corpus is required because the GraphMetrics/AnalysisReport do not carry
// per-document presentation data (title, description, mod-date); those come
// from the parsed Document. The corpus is read-only here.
type View struct {
	// Counts are the corpus-overview tallies.
	Counts Counts
	// Docs is every document, sorted by DocumentID, with presentation metadata.
	Docs []DocView
	// docIndex maps a DocumentID to its index in Docs for O(1) lookup.
	docIndex map[identity.DocumentID]int

	// BrokenLinks / BrokenAnchors / Ambiguous are findings split by kind, each
	// already sorted (the AnalysisReport sorts findings).
	BrokenLinks   []analysis.Finding
	BrokenAnchors []analysis.Finding
	Ambiguous     []analysis.Finding
	// LowScent are the low-scent-anchor findings (ADR 0016), already sorted. Info;
	// they never gate the exit code.
	LowScent []analysis.Finding

	// Orphans (isolated) and Unreachable are the distinct ADR-0007 classes,
	// sorted by DocumentID. Intentional orphans are already suppressed upstream.
	Orphans     []identity.DocumentID
	Unreachable []identity.DocumentID
	// UnderLinked and DeadEnd are the graduated structure tiers (ADR 0012), sorted
	// by DocumentID. Mutually exclusive with Orphans and each other.
	UnderLinked []identity.DocumentID
	DeadEnd     []identity.DocumentID
	// FarFromRoot are the documents reachable but at or beyond the hop-distance
	// threshold from every root (ADR 0021), sorted by DocumentID. Their per-doc
	// hop distance is on the DocView (Hops). Empty when reachability is
	// indeterminate. Root members and intentional orphans are suppressed upstream.
	FarFromRoot []identity.DocumentID
	// ReachabilityIndeterminate mirrors the metrics flag (no root set found).
	ReachabilityIndeterminate bool

	// TopHubs / TopAuthorities are HITS rankings (descending, tie-break by ID).
	TopHubs        []graphmodel.RankedDocument
	TopAuthorities []graphmodel.RankedDocument

	// TopPageRank are the documents ranked by PageRank (ADR 0016): global
	// importance via the random-surfer stationary distribution, descending,
	// tie-broken by ID. Pure data, surfaced in graph.json and the human report.
	TopPageRank []graphmodel.RankedDocument

	// Trails are the per-weak-component suggested reading orders (ADR 0016),
	// sorted by Root. Surfaced in trails.json and the llms.txt reading-order block.
	Trails []graphmodel.Trail

	// TopBetweenness are the load-bearing docs ranked by betweenness centrality
	// (ADR 0015), descending, tie-broken by ID. ArticulationPoints (cut vertices)
	// and Bridges (cut edges) are the corpus' critical structure: single points of
	// failure in the link graph. All pure data, surfaced in the reports/graph.json.
	TopBetweenness     []graphmodel.RankedDocument
	ArticulationPoints []identity.DocumentID
	Bridges            []graphmodel.Bridge

	// Gaps are experimental knowledge-gap bridge candidates (sorted upstream).
	Gaps          []graphmodel.Gap
	GapsTruncated bool

	// SuggestedLinks are topology-based link-prediction suggestions (ADR 0013),
	// ranked by Adamic/Adar upstream. An additive signal alongside Gaps.
	SuggestedLinks          []graphmodel.LinkSuggestion
	SuggestedLinksTruncated bool

	// Navigability holds the corpus-level navigability scalars (ADR 0014):
	// compactness, stratum, characteristic/median path length, clustering,
	// diameter. Pure data, surfaced in graph.json and the human reports.
	Navigability graphmodel.Navigability

	// BrokenEdges are unresolved navigational references (origin → raw target),
	// for the diagram emitters' red placeholder target nodes. Sorted upstream.
	BrokenEdges []application.BrokenEdge

	// Metrics is the frozen graph-analysis carrier, for emitters that render the
	// graph itself (mermaid, dot). Read-only.
	Metrics *graphmodel.GraphMetrics

	// corpus is the frozen corpus the run was computed over, retained so the
	// machine emitters (graph.json sections, llms-full bodies) can reach a
	// document's section tree and front matter. It is READ-ONLY: emitters must
	// never mutate it (ADR 0004). Reached only through the Document accessor.
	corpus *corpus.Corpus

	// emitExclude is the compiled `.matlatl.yml emitExclude` matcher (ADR 0019),
	// set by WithEmitExclude and nil otherwise. Consulted ONLY by the consumption
	// emitters (llmstxt, index, trails) via EmitExcluded / RenderedBacklinks /
	// RenderedTrails; the diagnostic and machine surfaces ignore it. See
	// exclude.go.
	emitExclude *ignore.GitIgnore
}

// Counts are the corpus-overview tallies surfaced at the top of every report.
type Counts struct {
	Documents     int
	Headings      int
	References    int
	BrokenLink    int
	BrokenAnchor  int
	Ambiguous     int
	Orphan        int
	Unreachable   int
	KnowledgeGap  int
	UnderLinked   int
	DeadEnd       int
	SuggestedLink int
	FarFromRoot   int
	Components    int
}

// DocView is a single document's presentation metadata, derived from its parsed
// Document plus the graph metrics (degree / component).
type DocView struct {
	ID identity.DocumentID
	// Title is the front-matter title, falling back to the first H1/heading,
	// falling back to the DocumentID.
	Title string
	// Description is the front-matter description, falling back to the first
	// heading text (ADR: the index.md description fallback to H1).
	Description string
	// Category is the document's directory ("." for top level).
	Category string
	// ModTime is the file's last-modified time.
	ModTime time.Time
	// InDegree / OutDegree are the projection navigational degrees.
	InDegree  int
	OutDegree int
	// Component is the WCC ID (sorted-min member) of the document.
	Component identity.DocumentID
	// Intentional reports the document opted out of orphan/unreachable findings.
	Intentional bool
	// Bowtie is the document's bow-tie bucket
	// (core/in/out/tendril/disconnected), pure structure data (ADR 0012).
	Bowtie string
	// Betweenness is the document's betweenness-centrality score (ADR 0015): how
	// load-bearing it is as a connector. IsArticulation marks it a cut vertex.
	Betweenness    float64
	IsArticulation bool
	// PageRank is the document's PageRank score (ADR 0016): global importance via
	// the random-surfer stationary distribution.
	PageRank float64
	// Hops is the document's shortest hop distance from the nearest root
	// (ADR 0021), or -1 when it is unreachable or the root set is indeterminate.
	Hops int
}

// topN bounds how many hubs/authorities the human reports surface.
const topN = 5

// BuildView assembles the render-ready View from a frozen pipeline Result. The
// Result's Corpus is read-only. Panics are avoided: a nil Metrics or Corpus
// yields an empty-but-valid View.
func BuildView(res application.Result) View {
	v := View{docIndex: map[identity.DocumentID]int{}}
	c := res.Corpus
	v.corpus = c
	if res.Metrics == nil || c == nil {
		v.Counts = countsFromResult(res, nil)
		return v
	}
	m := res.Metrics
	v.Metrics = m
	v.Counts = countsFromResult(res, m)
	v.ReachabilityIndeterminate = m.Orphans.Indeterminate
	v.Orphans = slices.Clone(m.Orphans.Isolated)
	v.Unreachable = slices.Clone(m.Orphans.Unreachable)
	v.UnderLinked = slices.Clone(m.Orphans.UnderLinked)
	v.DeadEnd = slices.Clone(m.Orphans.DeadEnd)
	v.FarFromRoot = slices.Clone(m.Hops.FarFromRoot)
	v.Gaps = slices.Clone(m.Gaps)
	v.GapsTruncated = m.GapsTruncated
	v.SuggestedLinks = slices.Clone(m.SuggestedLinks)
	v.SuggestedLinksTruncated = m.SuggestedLinksTruncated
	v.Navigability = m.Navigability
	v.TopHubs = m.HITS.TopHubs(topN)
	v.TopAuthorities = m.HITS.TopAuthorities(topN)
	v.TopPageRank = m.PageRank.Top(topN)
	v.Trails = slices.Clone(m.Trails)
	v.TopBetweenness = m.Betweenness.TopBetweenness(topN)
	v.ArticulationPoints = slices.Clone(m.Critical.ArticulationPoints)
	v.Bridges = slices.Clone(m.Critical.Bridges)
	v.BrokenEdges = slices.Clone(res.BrokenEdges)

	intentional := identity.IDSet(graphmodel.IntentionalOrphans(c))

	for _, doc := range c.Documents() { // sorted by ID
		title, desc := titleAndDescription(doc)
		deg := m.Degrees.Degree(doc.ID)
		// Hops-from-root (ADR 0021): the nearest-root distance, or -1 when the doc
		// is unreachable (absent from the distance map) OR the root set is
		// indeterminate (the map is empty), so both render as hopsFromRoot: -1.
		hops := -1
		if d, ok := m.Hops.Distance(doc.ID); ok {
			hops = d
		}
		dv := DocView{
			ID:             doc.ID,
			Title:          title,
			Description:    desc,
			Category:       path.Dir(doc.ID.String()),
			ModTime:        doc.ModTime,
			InDegree:       deg.In,
			OutDegree:      deg.Out,
			Component:      m.ComponentOf(doc.ID),
			Bowtie:         m.Bowtie.BucketOf(doc.ID).String(),
			Betweenness:    m.Betweenness.Score(doc.ID),
			IsArticulation: m.Critical.IsArticulation(doc.ID),
			PageRank:       m.PageRank.Score(doc.ID),
			Hops:           hops,
		}
		if _, ok := intentional[doc.ID]; ok {
			dv.Intentional = true
		}
		v.docIndex[doc.ID] = len(v.Docs)
		v.Docs = append(v.Docs, dv)
	}

	if res.Report != nil {
		for _, f := range res.Report.Findings() { // already sorted
			switch f.Kind {
			case analysis.BrokenLink:
				v.BrokenLinks = append(v.BrokenLinks, f)
			case analysis.BrokenAnchor:
				v.BrokenAnchors = append(v.BrokenAnchors, f)
			case analysis.Ambiguous:
				v.Ambiguous = append(v.Ambiguous, f)
			case analysis.LowScentAnchor:
				// Low-scent links (ADR 0016) are carried as findings for the machine
				// artifacts (findings.json) and surfaced via the metrics Scent slice for
				// the human report; not a dedicated finding-list slice here.
				v.LowScent = append(v.LowScent, f)
			case analysis.Orphan, analysis.Unreachable, analysis.KnowledgeGap,
				analysis.UnderLinked, analysis.DeadEnd, analysis.SuggestedLink,
				analysis.ArticulationPoint, analysis.Bridge, analysis.FarFromRoot:
				// Carried via the dedicated View slices above, not the finding lists.
			case analysis.DeadLink:
				// Opt-in (--check-external) only; surfaced via findings.json, not
				// the deterministic human View slices.
			}
		}
	}
	return v
}

// ReachableSet returns the set of reachable document IDs and whether
// reachability is INDETERMINATE (no root set was found). It is the single shared
// reachability helper for the machine emitters (graph.json, llms.txt) so they
// cannot diverge on the indeterminate case: per ADR 0007, indeterminate is NOT
// the same as "everything unreachable" — callers must consult the returned flag
// and treat every document as reachable (do not mark anything unreachable) when
// it is true. A nil metrics carrier is treated as indeterminate with an empty
// set.
func ReachableSet(m *graphmodel.GraphMetrics) (set map[identity.DocumentID]struct{}, indeterminate bool) {
	if m == nil || m.Orphans.Indeterminate {
		return map[identity.DocumentID]struct{}{}, true
	}
	return identity.IDSet(m.Reachability.Reached), false
}

// Doc returns the DocView for id and whether it exists.
func (v View) Doc(id identity.DocumentID) (DocView, bool) {
	i, ok := v.docIndex[id]
	if !ok {
		return DocView{}, false
	}
	return v.Docs[i], true
}

// Document returns the frozen *corpus.Document for id and whether it exists. The
// returned document is READ-ONLY (ADR 0004): the machine emitters read its
// section tree / front matter but must not mutate it.
func (v View) Document(id identity.DocumentID) (*corpus.Document, bool) {
	if v.corpus == nil {
		return nil, false
	}
	return v.corpus.Get(id)
}

// Backlinks returns the documents that navigationally link TO id (ADR 0016),
// sorted by DocumentID (= path) and self-excluded — exactly the document
// projection's in-neighbours. Empty when nothing links to id or metrics are
// absent. This realizes Nelson's Xanadu two-way links: every page can show what
// points at it, not just where it points. Derived from the existing projection
// (no redundant graph.json array).
func (v View) Backlinks(id identity.DocumentID) []identity.DocumentID {
	if v.Metrics == nil || v.Metrics.Graph == nil {
		return nil
	}
	return v.Metrics.Graph.ProjectionIn(id)
}

// TitleOf returns the best-effort display title for id (or the id itself).
func (v View) TitleOf(id identity.DocumentID) string {
	if d, ok := v.Doc(id); ok && d.Title != "" {
		return d.Title
	}
	return id.String()
}

func countsFromResult(res application.Result, m *graphmodel.GraphMetrics) Counts {
	c := Counts{
		Documents:     res.DocumentCount,
		Headings:      res.HeadingCount,
		References:    res.ReferenceCount,
		BrokenLink:    res.BrokenLinkCount,
		BrokenAnchor:  res.BrokenAnchorCount,
		Ambiguous:     res.AmbiguousCount,
		Orphan:        res.OrphanCount,
		Unreachable:   res.UnreachableCount,
		KnowledgeGap:  res.KnowledgeGapCount,
		UnderLinked:   res.UnderLinkedCount,
		DeadEnd:       res.DeadEndCount,
		SuggestedLink: res.SuggestedLinkCount,
		FarFromRoot:   res.FarFromRootCount,
	}
	if m != nil {
		c.Components = len(m.WCC)
	}
	return c
}

// titleAndDescription derives a document's display title and description with
// the documented fallbacks: title := front-matter title → first heading →
// DocumentID; description := front-matter description → first heading text → "".
// Title resolution goes through the domain corpus.Document.Title so the emit
// presentation and the information-scent analysis cannot drift (ADR 0016).
func titleAndDescription(doc *corpus.Document) (title, description string) {
	first := doc.FirstHeadingText()
	title = doc.Title()
	description = doc.FrontMatter.Description
	if description == "" {
		description = first
	}
	return title, description
}
