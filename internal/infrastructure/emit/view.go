package emit

import (
	"path"
	"slices"
	"time"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/domain/analysis"
	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/graphmodel"
	"github.com/stacklok/doctopus/internal/domain/identity"
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

	// Orphans (isolated) and Unreachable are the distinct ADR-0007 classes,
	// sorted by DocumentID. Intentional orphans are already suppressed upstream.
	Orphans     []identity.DocumentID
	Unreachable []identity.DocumentID
	// ReachabilityIndeterminate mirrors the metrics flag (no root set found).
	ReachabilityIndeterminate bool

	// TopHubs / TopAuthorities are HITS rankings (descending, tie-break by ID).
	TopHubs        []graphmodel.RankedDocument
	TopAuthorities []graphmodel.RankedDocument

	// Gaps are experimental knowledge-gap bridge candidates (sorted upstream).
	Gaps          []graphmodel.Gap
	GapsTruncated bool

	// BrokenEdges are unresolved navigational references (origin → raw target),
	// for the diagram emitters' red placeholder target nodes. Sorted upstream.
	BrokenEdges []application.BrokenEdge

	// Metrics is the frozen graph-analysis carrier, for emitters that render the
	// graph itself (mermaid, dot). Read-only.
	Metrics *graphmodel.GraphMetrics
}

// Counts are the corpus-overview tallies surfaced at the top of every report.
type Counts struct {
	Documents    int
	Headings     int
	References   int
	BrokenLink   int
	BrokenAnchor int
	Ambiguous    int
	Orphan       int
	Unreachable  int
	KnowledgeGap int
	Components   int
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
}

// topN bounds how many hubs/authorities the human reports surface.
const topN = 5

// BuildView assembles the render-ready View from a frozen pipeline Result. The
// Result's Corpus is read-only. Panics are avoided: a nil Metrics or Corpus
// yields an empty-but-valid View.
func BuildView(res application.Result) View {
	v := View{docIndex: map[identity.DocumentID]int{}}
	c := res.Corpus
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
	v.Gaps = slices.Clone(m.Gaps)
	v.GapsTruncated = m.GapsTruncated
	v.TopHubs = m.HITS.TopHubs(topN)
	v.TopAuthorities = m.HITS.TopAuthorities(topN)
	v.BrokenEdges = slices.Clone(res.BrokenEdges)

	intentional := map[identity.DocumentID]struct{}{}
	for _, id := range graphmodel.IntentionalOrphans(c) {
		intentional[id] = struct{}{}
	}

	for _, doc := range c.Documents() { // sorted by ID
		title, desc := titleAndDescription(doc)
		deg := m.Degrees[doc.ID]
		dv := DocView{
			ID:          doc.ID,
			Title:       title,
			Description: desc,
			Category:    path.Dir(doc.ID.String()),
			ModTime:     doc.ModTime,
			InDegree:    deg.In,
			OutDegree:   deg.Out,
			Component:   m.ComponentOf(doc.ID),
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
			case analysis.Orphan, analysis.Unreachable, analysis.KnowledgeGap:
				// Carried via the dedicated View slices above, not the finding lists.
			}
		}
	}
	return v
}

// Doc returns the DocView for id and whether it exists.
func (v View) Doc(id identity.DocumentID) (DocView, bool) {
	i, ok := v.docIndex[id]
	if !ok {
		return DocView{}, false
	}
	return v.Docs[i], true
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
		Documents:    res.DocumentCount,
		Headings:     res.HeadingCount,
		References:   res.ReferenceCount,
		BrokenLink:   res.BrokenLinkCount,
		BrokenAnchor: res.BrokenAnchorCount,
		Ambiguous:    res.AmbiguousCount,
		Orphan:       res.OrphanCount,
		Unreachable:  res.UnreachableCount,
		KnowledgeGap: res.KnowledgeGapCount,
	}
	if m != nil {
		c.Components = len(m.WCC)
	}
	return c
}

// titleAndDescription derives a document's display title and description with
// the documented fallbacks: title := front-matter title → first heading →
// DocumentID; description := front-matter description → first heading text → "".
func titleAndDescription(doc *corpus.Document) (title, description string) {
	first := firstHeadingText(doc)
	title = doc.FrontMatter.Title
	if title == "" {
		title = first
	}
	if title == "" {
		title = doc.ID.String()
	}
	description = doc.FrontMatter.Description
	if description == "" {
		description = first
	}
	return title, description
}

// firstHeadingText returns the text of the first (document-order) heading with
// non-empty text, or "" if the document has no headings.
func firstHeadingText(doc *corpus.Document) string {
	if doc.Root == nil {
		return ""
	}
	var found string
	var walk func(s *corpus.Section) bool
	walk = func(s *corpus.Section) bool {
		for _, child := range s.Children {
			if child.Text != "" {
				found = child.Text
				return true
			}
			if walk(child) {
				return true
			}
		}
		return false
	}
	walk(doc.Root)
	return found
}
