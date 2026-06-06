// Package graphjson renders graph.json — the PRIMARY machine-/LLM-queryable
// artifact: a compact, schemaVersion-stamped, fully parseable manifest of the
// analyzed corpus (nodes, edges, sections, components, HITS, and every gap/
// orphan/broken-link signal). It is infrastructure: it reads the frozen
// emit.View + graphmodel + corpus and never mutates the domain (ADR 0004).
//
// Determinism contract: the schema is struct-defined (stable field order) and
// every slice is sorted. HITS hub/authority scores are floats — Go map iteration
// is randomized and float text can vary — so they are formatted at a FIXED
// precision (HITSFloatPrecision decimals) into a typed Float so output is
// byte-stable across runs. See the package test for the round-trip + stability
// proofs and docs/schemas/graph.schema.json for the published contract.
package graphjson

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
)

// Structured-detail keys are imported directly from the application layer
// (the authoritative source of the findings.json schema-v2 key names) so the
// read side here cannot silently drift from the write side. The emit-deep
// subpackages already depend on application (emit/view.go, emit/writer.go), so
// this introduces no new layering edge (ADR 0004).
const (
	detailTarget       = application.DetailTarget
	detailExpectedSlug = application.DetailExpectedSlug
	detailCandidates   = application.DetailCandidates
)

// splitCandidates splits the newline-joined candidate list (the Details encoding
// of a multi-valued field) into a sorted, non-nil slice.
func splitCandidates(s string) []string {
	if s == "" {
		return []string{}
	}
	out := strings.Split(s, "\n")
	slices.Sort(out)
	return out
}

// GraphJSONName is the conventional graph.json filename.
const GraphJSONName = "graph.json"

// SchemaVersion is the graph.json schema version. Additive fields are
// backward-compatible; renaming/removing a field bumps this. It is mirrored by
// docs/schemas/graph.schema.json (kept in lockstep; a test validates against it).
// v2 (ADR 0012) adds per-node bowtie/underLinked/deadEnd, top-level underLinked/
// deadEnd arrays, a bowtie summary, and underLinked/deadEnd summary counts.
// v3 (ADR 0013) adds the top-level suggestedLinks array (topology-based
// link-prediction suggestions) and a suggestedLinks summary count.
// v4 (ADR 0014) adds a summary.navigability object (compactness, stratum,
// characteristic/median path length, clustering coefficient, diameter,
// reachablePairs) — corpus-level navigability scalars, pure data.
// v5 (ADR 0015) adds critical-path analysis: per-node betweenness (number) and
// isArticulation (bool); a top-level betweenness object {topDocs:[{id,score}]};
// top-level articulationPoints ([]string) and bridges ([]{from,to}); and
// summary.articulationPoints / summary.bridges counts — all pure data.
// v6 (ADR 0016) adds PageRank: a per-node pageRank (number) and a top-level
// pageRank object {topDocs:[{id,score}]} parallel to betweenness — pure data.
const SchemaVersion = 6

// HITSFloatPrecision is the FIXED number of decimal places HITS hub/authority
// scores are rounded to in graph.json. HITS scores are L2-normalized into [0,1]
// and the power iteration converges to ~1e-8 (hits.go), so 6 decimals preserves
// all signal while guaranteeing byte-stable text: 'f' format with a fixed
// precision never emits exponent notation or run-dependent trailing digits.
// The P3 panel flagged float formatting as the determinism risk; this is the fix.
const HITSFloatPrecision = 6

// Float is a float64 that marshals as a JSON number with FIXED precision so
// graph.json is byte-stable. It round-trips: it unmarshals from a JSON number
// back into the float. Stored pre-rounded (newFloat) so equal inputs are equal.
type Float float64

func newFloat(f float64) Float {
	// Round to the fixed precision up front so the stored value and the rendered
	// text agree and two runs with the same input compare equal.
	r, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'f', HITSFloatPrecision, 64), 64)
	return Float(r)
}

// MarshalJSON renders the float at the fixed precision as a bare JSON number.
func (f Float) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatFloat(float64(f), 'f', HITSFloatPrecision, 64)), nil
}

// UnmarshalJSON parses a JSON number back into the fixed-precision float, so the
// typed struct round-trips from emitted bytes.
func (f *Float) UnmarshalJSON(b []byte) error {
	v, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		return fmt.Errorf("graphjson: parse float %q: %w", b, err)
	}
	*f = newFloat(v)
	return nil
}

// Document is the top-level graph.json shape. Field order is the wire order
// (encoding/json preserves struct field order); every slice field is sorted.
type Document struct {
	SchemaVersion  int             `json:"schemaVersion"`
	Tool           string          `json:"tool"`
	GeneratedNote  string          `json:"generatedNote"`
	Summary        Summary         `json:"summary"`
	Nodes          []Node          `json:"nodes"`
	Edges          []Edge          `json:"edges"`
	Sections       []Section       `json:"sections"`
	Orphans        []string        `json:"orphans"`
	Unreachable    []string        `json:"unreachable"`
	UnderLinked    []string        `json:"underLinked"`
	DeadEnd        []string        `json:"deadEnd"`
	Bowtie         BowtieSummary   `json:"bowtie"`
	BrokenLinks    []BrokenLink    `json:"brokenLinks"`
	BrokenAnchors  []BrokenAnchor  `json:"brokenAnchors"`
	Ambiguous      []Ambiguous     `json:"ambiguous"`
	Components     Components      `json:"components"`
	HITS           HITS            `json:"hits"`
	Betweenness    Betweenness     `json:"betweenness"`
	PageRank       PageRank        `json:"pageRank"`
	Gaps           []Gap           `json:"gaps"`
	SuggestedLinks []SuggestedLink `json:"suggestedLinks"`
	// ArticulationPoints are cut vertices; Bridges are cut edges of the undirected
	// closure (ADR 0015) — the corpus' single points of failure, pure data.
	ArticulationPoints []string     `json:"articulationPoints"`
	Bridges            []Bridge     `json:"bridges"`
	RootSet            []string     `json:"rootSet"`
	Reachability       Reachability `json:"reachability"`
}

// Summary holds the corpus-overview counts.
type Summary struct {
	Documents      int `json:"documents"`
	Sections       int `json:"sections"`
	Edges          int `json:"edges"`
	References     int `json:"references"`
	Components     int `json:"components"`
	Orphans        int `json:"orphans"`
	Unreachable    int `json:"unreachable"`
	UnderLinked    int `json:"underLinked"`
	DeadEnd        int `json:"deadEnd"`
	BrokenLinks    int `json:"brokenLinks"`
	BrokenAnchors  int `json:"brokenAnchors"`
	Ambiguous      int `json:"ambiguous"`
	KnowledgeGaps  int `json:"knowledgeGaps"`
	SuggestedLinks int `json:"suggestedLinks"`
	// ArticulationPoints / Bridges are the critical-path structure counts
	// (ADR 0015): cut vertices and cut edges of the undirected closure.
	ArticulationPoints int `json:"articulationPoints"`
	Bridges            int `json:"bridges"`
	// Navigability holds the corpus-level navigability scalars (ADR 0014). Floats
	// use the fixed-precision Float type so graph.json stays byte-stable.
	Navigability Navigability `json:"navigability"`
}

// Navigability is the wire shape of the corpus navigability scalars (ADR 0014).
// The float fields reuse the fixed-precision Float type (the HITS determinism
// mechanism) so output is byte-stable; diameter and reachablePairs are integers.
type Navigability struct {
	Compactness              Float `json:"compactness"`
	Stratum                  Float `json:"stratum"`
	CharacteristicPathLength Float `json:"characteristicPathLength"`
	MedianPathLength         Float `json:"medianPathLength"`
	ClusteringCoefficient    Float `json:"clusteringCoefficient"`
	Diameter                 int   `json:"diameter"`
	ReachablePairs           int   `json:"reachablePairs"`
}

// BowtieSummary is the corpus-level bow-tie tally relative to the giant SCC
// (the "core"): the per-bucket document counts plus the giant SCC's ID and size.
// A giantSCCSize of 1 means the corpus has no cyclic core (every SCC is a
// singleton); the buckets are still populated deterministically (ADR 0012).
type BowtieSummary struct {
	Core         int    `json:"core"`
	In           int    `json:"in"`
	Out          int    `json:"out"`
	Tendril      int    `json:"tendril"`
	Disconnected int    `json:"disconnected"`
	GiantSCC     string `json:"giantScc"`
	GiantSCCSize int    `json:"giantSccSize"`
}

// Node is a document (or section) vertex with its presentation + analysis data.
type Node struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Title             string `json:"title"`
	Path              string `json:"path"`
	Description       string `json:"description"`
	Category          string `json:"category"`
	InDegree          int    `json:"inDegree"`
	OutDegree         int    `json:"outDegree"`
	Component         string `json:"component"`
	HubScore          Float  `json:"hubScore"`
	AuthorityScore    Float  `json:"authorityScore"`
	Reachable         bool   `json:"reachable"`
	Orphan            bool   `json:"orphan"`
	IntentionalOrphan bool   `json:"intentionalOrphan"`
	// UnderLinked / DeadEnd are the graduated structure tiers (ADR 0012);
	// mutually exclusive with Orphan and each other.
	UnderLinked bool `json:"underLinked"`
	DeadEnd     bool `json:"deadEnd"`
	// Bowtie is the node's bow-tie bucket: core/in/out/tendril/disconnected.
	Bowtie string `json:"bowtie"`
	// Betweenness is the node's directed betweenness-centrality score in [0,1]
	// (ADR 0015): how load-bearing it is as a shortest-path connector. Fixed
	// precision (Float) so graph.json is byte-stable. IsArticulation marks it a
	// cut vertex of the undirected closure.
	Betweenness    Float `json:"betweenness"`
	IsArticulation bool  `json:"isArticulation"`
	// PageRank is the node's PageRank score (ADR 0016): global importance via the
	// random-surfer stationary distribution. Fixed precision (Float) so graph.json
	// is byte-stable.
	PageRank Float `json:"pageRank"`
}

// Edge is a directed document-projection navigational edge. Health is always
// "valid" here: the projection retains only resolved in-corpus edges (ADR 0007);
// unresolved targets are reported in brokenLinks/brokenAnchors instead.
type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Type   string `json:"type"`
	Health string `json:"health"`
}

// Section is a heading-scoped vertex: its node id, owning document, slug, level,
// and title.
type Section struct {
	ID    string `json:"id"`
	Doc   string `json:"doc"`
	Slug  string `json:"slug"`
	Level int    `json:"level"`
	Title string `json:"title"`
}

// BrokenLink is a reference whose target does not resolve to a corpus document.
type BrokenLink struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Target string `json:"target"`
	Detail string `json:"detail"`
}

// BrokenAnchor is a reference whose document resolved but the anchor did not.
type BrokenAnchor struct {
	File         string `json:"file"`
	Line         int    `json:"line"`
	Target       string `json:"target"`
	ExpectedSlug string `json:"expectedSlug"`
	Detail       string `json:"detail"`
}

// Ambiguous is a reference that matched more than one candidate document.
type Ambiguous struct {
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Target     string   `json:"target"`
	Candidates []string `json:"candidates"`
	Detail     string   `json:"detail"`
}

// Components groups documents by weak (WCC) and strong (SCC) component.
type Components struct {
	WCC []Component `json:"wcc"`
	SCC []Component `json:"scc"`
}

// Component is one component: its ID (sorted-min member) and sorted members.
type Component struct {
	ID      string   `json:"id"`
	Members []string `json:"members"`
}

// HITS holds the top hubs and authorities (importance-ranked, fixed precision).
type HITS struct {
	TopHubs        []Ranked `json:"topHubs"`
	TopAuthorities []Ranked `json:"topAuthorities"`
}

// Ranked pairs a document with a fixed-precision HITS score.
type Ranked struct {
	ID    string `json:"id"`
	Score Float  `json:"score"`
}

// Betweenness holds the top load-bearing documents by betweenness centrality
// (ADR 0015), parallel to the HITS block. Scores use the fixed-precision Float
// type so graph.json is byte-stable.
type Betweenness struct {
	TopDocs []Ranked `json:"topDocs"`
}

// PageRank holds the top documents by PageRank (ADR 0016), parallel to the HITS
// and Betweenness blocks. Scores use the fixed-precision Float type so graph.json
// is byte-stable.
type PageRank struct {
	TopDocs []Ranked `json:"topDocs"`
}

// Bridge is a cut edge of the undirected closure (ADR 0015): the only link
// between two parts of the corpus. from < to canonically.
type Bridge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Gap is a candidate bridge between two distinct weak components.
type Gap struct {
	ComponentA      string `json:"componentA"`
	ComponentB      string `json:"componentB"`
	RepresentativeA string `json:"representativeA"`
	RepresentativeB string `json:"representativeB"`
}

// SuggestedLink is a topology-based suggestion that two UNLINKED but
// structurally-close documents may warrant a navigational link (ADR 0013). DocA
// < DocB. The Adamic/Adar score reuses the fixed-precision Float type (the HITS
// determinism mechanism) so output is byte-stable.
type SuggestedLink struct {
	DocA             string `json:"docA"`
	DocB             string `json:"docB"`
	SharedNeighbours int    `json:"sharedNeighbours"`
	Coupling         int    `json:"coupling"`
	CoCitation       int    `json:"coCitation"`
	AdamicAdar       Float  `json:"adamicAdar"`
}

// Reachability mirrors the analysis reachability state. Indeterminate is true
// when no root set was found (reachability was not computed); consumers must not
// treat every non-reached doc as unreachable in that case (ADR 0007).
type Reachability struct {
	Indeterminate bool `json:"indeterminate"`
}

const generatedNote = "Generated by matlatl. Machine-readable analysis of a markdown corpus: " +
	"nodes are documents (importance via hubScore/authorityScore/inDegree), edges are resolved " +
	"navigational links, and orphans/unreachable/brokenLinks/gaps flag what is INCOMPLETE."

// Build assembles the typed graph.json Document from the frozen View. A View
// with no metrics/corpus yields a valid empty-but-stamped document.
func Build(v emit.View) Document {
	doc := Document{
		SchemaVersion: SchemaVersion,
		Tool:          "matlatl",
		GeneratedNote: generatedNote,
		// Initialize every slice to non-nil so the JSON shape is stable ([] not null).
		Nodes:              []Node{},
		Edges:              []Edge{},
		Sections:           []Section{},
		Orphans:            []string{},
		Unreachable:        []string{},
		UnderLinked:        []string{},
		DeadEnd:            []string{},
		BrokenLinks:        []BrokenLink{},
		BrokenAnchors:      []BrokenAnchor{},
		Ambiguous:          []Ambiguous{},
		Components:         Components{WCC: []Component{}, SCC: []Component{}},
		HITS:               HITS{TopHubs: []Ranked{}, TopAuthorities: []Ranked{}},
		Betweenness:        Betweenness{TopDocs: []Ranked{}},
		PageRank:           PageRank{TopDocs: []Ranked{}},
		Gaps:               []Gap{},
		SuggestedLinks:     []SuggestedLink{},
		ArticulationPoints: []string{},
		Bridges:            []Bridge{},
		RootSet:            []string{},
	}

	m := v.Metrics
	if m == nil {
		return doc
	}

	reachable, indeterminate := emit.ReachableSet(m)
	orphanSet := identity.IDSet(v.Orphans)
	underLinkedSet := identity.IDSet(v.UnderLinked)
	deadEndSet := identity.IDSet(v.DeadEnd)

	for _, d := range v.Docs { // sorted by ID
		hub, auth := m.HITS.Score(d.ID)
		// Per ADR 0007 indeterminate reachability (no root set) is NOT
		// unreachability: do not mark any node unreachable in that case.
		_, isReachable := reachable[d.ID]
		if indeterminate {
			isReachable = true
		}
		_, isOrphan := orphanSet[d.ID]
		_, isUnderLinked := underLinkedSet[d.ID]
		_, isDeadEnd := deadEndSet[d.ID]
		doc.Nodes = append(doc.Nodes, Node{
			ID:                d.ID.String(),
			Kind:              "doc",
			Title:             d.Title,
			Path:              d.ID.String(),
			Description:       d.Description,
			Category:          emit.CategoryLabel(d.Category),
			InDegree:          d.InDegree,
			OutDegree:         d.OutDegree,
			Component:         d.Component.String(),
			HubScore:          newFloat(hub),
			AuthorityScore:    newFloat(auth),
			Reachable:         isReachable,
			Orphan:            isOrphan,
			IntentionalOrphan: d.Intentional,
			UnderLinked:       isUnderLinked,
			DeadEnd:           isDeadEnd,
			Bowtie:            d.Bowtie,
			Betweenness:       newFloat(d.Betweenness),
			IsArticulation:    d.IsArticulation,
			PageRank:          newFloat(d.PageRank),
		})
	}

	doc.Edges = edgesFrom(m.Graph)
	doc.Sections = sectionsFrom(v)
	doc.Orphans = identity.IDStrings(v.Orphans)
	doc.Unreachable = identity.IDStrings(v.Unreachable)
	doc.UnderLinked = identity.IDStrings(v.UnderLinked)
	doc.DeadEnd = identity.IDStrings(v.DeadEnd)
	doc.Bowtie = bowtieSummary(m.Bowtie)
	doc.BrokenLinks = brokenLinks(v.BrokenLinks)
	doc.BrokenAnchors = brokenAnchors(v.BrokenAnchors)
	doc.Ambiguous = ambiguous(v.Ambiguous)
	doc.Components = Components{WCC: components(m.WCC), SCC: components(m.SCC)}
	doc.HITS = HITS{TopHubs: ranked(v.TopHubs), TopAuthorities: ranked(v.TopAuthorities)}
	doc.Betweenness = Betweenness{TopDocs: ranked(v.TopBetweenness)}
	doc.PageRank = PageRank{TopDocs: ranked(v.TopPageRank)}
	doc.Gaps = gaps(v.Gaps)
	doc.SuggestedLinks = suggestedLinks(v.SuggestedLinks)
	doc.ArticulationPoints = identity.IDStrings(v.ArticulationPoints)
	doc.Bridges = bridges(v.Bridges)
	doc.RootSet = identity.IDStrings(m.RootSet.Roots)
	doc.Reachability = Reachability{Indeterminate: m.Orphans.Indeterminate}

	doc.Summary = Summary{
		Documents:          v.Counts.Documents,
		Sections:           len(doc.Sections),
		Edges:              len(doc.Edges),
		References:         v.Counts.References,
		Components:         len(doc.Components.WCC),
		Orphans:            len(doc.Orphans),
		Unreachable:        len(doc.Unreachable),
		UnderLinked:        len(doc.UnderLinked),
		DeadEnd:            len(doc.DeadEnd),
		BrokenLinks:        len(doc.BrokenLinks),
		BrokenAnchors:      len(doc.BrokenAnchors),
		Ambiguous:          len(doc.Ambiguous),
		KnowledgeGaps:      v.Counts.KnowledgeGap,
		SuggestedLinks:     len(doc.SuggestedLinks),
		ArticulationPoints: len(doc.ArticulationPoints),
		Bridges:            len(doc.Bridges),
		Navigability:       navigability(m.Navigability),
	}
	return doc
}

// bridges projects the domain bridges (cut edges) into the wire shape. The slice
// is already sorted (A<B canonical, then by (A,B)) upstream.
func bridges(bs []graphmodel.Bridge) []Bridge {
	out := make([]Bridge, 0, len(bs))
	for _, b := range bs { // already sorted
		out = append(out, Bridge{From: b.A.String(), To: b.B.String()})
	}
	return out
}

// navigability projects the domain navigability scalars into the wire shape,
// rounding the floats to the fixed precision (newFloat) so graph.json is
// byte-stable.
func navigability(n graphmodel.Navigability) Navigability {
	return Navigability{
		Compactness:              newFloat(n.Compactness),
		Stratum:                  newFloat(n.Stratum),
		CharacteristicPathLength: newFloat(n.CharacteristicPathLength),
		MedianPathLength:         newFloat(n.MedianPathLength),
		ClusteringCoefficient:    newFloat(n.ClusteringCoefficient),
		Diameter:                 n.Diameter,
		ReachablePairs:           n.ReachablePairs,
	}
}

// JSON renders the View as the canonical graph.json bytes (pretty-printed,
// trailing newline). Deterministic: struct field order + sorted slices + fixed
// float precision. The hostile-title fixture test asserts encoding/json escapes
// node titles/paths (they are JSON string values, never interpolated).
func JSON(v emit.View) ([]byte, error) {
	b, err := json.MarshalIndent(Build(v), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("graphjson: marshal graph.json: %w", err)
	}
	return append(b, '\n'), nil
}

// edgesFrom returns the document-projection navigational edges in sorted (From,
// To) order. The projection retains only resolved, navigational, in-corpus edges
// (ADR 0007), so health is always "valid"; the edge Type is collapsed by the
// projection, so we report the stable navigational marker "reference".
func edgesFrom(g *graphmodel.ReferenceGraph) []Edge {
	out := []Edge{}
	if g == nil {
		return out
	}
	for _, from := range g.Documents() { // sorted
		for _, to := range g.ProjectionOut(from) { // sorted
			out = append(out, Edge{
				From:   from.String(),
				To:     to.String(),
				Type:   "reference",
				Health: "valid",
			})
		}
	}
	return out
}

// sectionsFrom walks every document's section tree (sorted document order, then
// document-order sections) and emits the slugged section vertices.
func sectionsFrom(v emit.View) []Section {
	out := []Section{}
	if v.Metrics == nil {
		return out
	}
	// Reach the corpus via the View's per-doc list + the graph's section nodes
	// would lose level/title, so walk the corpus document trees directly. The
	// corpus is carried on the metrics graph indirectly; the View exposes Docs
	// (sorted), and we resolve each Document through the View's corpus accessor.
	for _, d := range v.Docs { // sorted by ID
		doc, ok := v.Document(d.ID)
		if !ok || doc.Root == nil {
			continue
		}
		appendSections(&out, doc)
	}
	return out
}

func appendSections(out *[]Section, doc *corpus.Document) {
	var walk func(s *corpus.Section)
	walk = func(s *corpus.Section) {
		for _, child := range s.Children {
			if child.Slug != "" {
				*out = append(*out, Section{
					ID:    graphmodel.NodeIDForSection(doc.ID, child.Slug).String(),
					Doc:   doc.ID.String(),
					Slug:  child.Slug,
					Level: child.Level,
					Title: child.Text,
				})
			}
			walk(child)
		}
	}
	walk(doc.Root)
}

func brokenLinks(findings []analysis.Finding) []BrokenLink {
	out := make([]BrokenLink, 0, len(findings))
	for _, f := range findings {
		out = append(out, BrokenLink{
			File:   f.Location.Document.String(),
			Line:   f.Location.Line,
			Target: f.Details[detailTarget],
			Detail: f.Message,
		})
	}
	return out
}

func brokenAnchors(findings []analysis.Finding) []BrokenAnchor {
	out := make([]BrokenAnchor, 0, len(findings))
	for _, f := range findings {
		out = append(out, BrokenAnchor{
			File:         f.Location.Document.String(),
			Line:         f.Location.Line,
			Target:       f.Details[detailTarget],
			ExpectedSlug: f.Details[detailExpectedSlug],
			Detail:       f.Message,
		})
	}
	return out
}

func ambiguous(findings []analysis.Finding) []Ambiguous {
	out := make([]Ambiguous, 0, len(findings))
	for _, f := range findings {
		out = append(out, Ambiguous{
			File:       f.Location.Document.String(),
			Line:       f.Location.Line,
			Target:     f.Details[detailTarget],
			Candidates: splitCandidates(f.Details[detailCandidates]),
			Detail:     f.Message,
		})
	}
	return out
}

func components(comps graphmodel.Components) []Component {
	out := make([]Component, 0, len(comps))
	for _, c := range comps { // already sorted by ID
		out = append(out, Component{ID: c.ID.String(), Members: identity.IDStrings(c.Members)})
	}
	return out
}

func ranked(rs []graphmodel.RankedDocument) []Ranked {
	out := make([]Ranked, 0, len(rs))
	for _, r := range rs { // already importance-ordered (score desc, ID asc)
		out = append(out, Ranked{ID: r.ID.String(), Score: newFloat(r.Score)})
	}
	return out
}

// bowtieSummary projects the domain bow-tie report into the wire summary.
func bowtieSummary(r graphmodel.BowtieReport) BowtieSummary {
	return BowtieSummary{
		Core:         r.Counts[graphmodel.BucketCore],
		In:           r.Counts[graphmodel.BucketIn],
		Out:          r.Counts[graphmodel.BucketOut],
		Tendril:      r.Counts[graphmodel.BucketTendril],
		Disconnected: r.Counts[graphmodel.BucketDisconnected],
		GiantSCC:     r.GiantSCC.String(),
		GiantSCCSize: r.GiantSCCSize,
	}
}

func gaps(gs []graphmodel.Gap) []Gap {
	out := make([]Gap, 0, len(gs))
	for _, g := range gs { // already sorted
		out = append(out, Gap{
			ComponentA:      g.ComponentA.String(),
			ComponentB:      g.ComponentB.String(),
			RepresentativeA: g.RepresentativeA.String(),
			RepresentativeB: g.RepresentativeB.String(),
		})
	}
	return out
}

// suggestedLinks projects the domain link suggestions into the wire shape. The
// Adamic/Adar score is rounded to the fixed precision (newFloat) so graph.json
// is byte-stable. The slice is already ranked (Adamic/Adar DESC, tie-broken).
func suggestedLinks(ss []graphmodel.LinkSuggestion) []SuggestedLink {
	out := make([]SuggestedLink, 0, len(ss))
	for _, s := range ss { // already ranked
		out = append(out, SuggestedLink{
			DocA:             s.DocA.String(),
			DocB:             s.DocB.String(),
			SharedNeighbours: s.SharedNeighbours,
			Coupling:         s.Coupling,
			CoCitation:       s.CoCitation,
			AdamicAdar:       newFloat(s.AdamicAdar),
		})
	}
	return out
}
