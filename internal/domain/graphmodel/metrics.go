package graphmodel

import (
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// GraphMetrics is the frozen carrier of all P3 graph analysis results. It is the
// single struct later phases (P4 human emitters, P5 graph.json/llms.txt) read
// from, alongside the AnalysisReport. Every field is computed deterministically
// and treated as immutable after construction.
type GraphMetrics struct {
	// Graph is the built reference graph (vertices + edges + projection), for
	// emitters that render the graph itself (graph.json, DOT/Mermaid).
	Graph *ReferenceGraph
	// Hierarchy is the folder/front-matter parent tree (breadcrumbs/index).
	Hierarchy *HierarchyTree
	// RootSet is the resolved reachability root set (may be Indeterminate).
	RootSet RootSet
	// Reachability holds the BFS reachability result (empty if Indeterminate).
	Reachability Reachability
	// Degrees is the per-document in/out navigational degree.
	Degrees DegreeIndex
	// Orphans holds isolated-orphan and unreachable classification.
	Orphans OrphanReport
	// WCC / SCC are the weak and strong components (sorted, deterministic IDs).
	WCC Components
	// SCC are the strongly-connected components (cycles collapse to one).
	SCC Components
	// Bowtie is the bow-tie classification of every document relative to the
	// giant SCC (core/in/out/tendril/disconnected). Pure data, not findings.
	Bowtie BowtieReport
	// HITS holds hub/authority scores.
	HITS HitsScores
	// Gaps are experimental knowledge-gap bridge candidates.
	Gaps []Gap
	// GapsTruncated reports that the gap list was capped at MaxGaps (the corpus
	// has pathologically many disconnected clusters); surfaced as a notice.
	GapsTruncated bool
	// SuggestedLinks are topology-based link-prediction suggestions: UNLINKED but
	// structurally-close document pairs (ADR 0013). An ADDITIVE signal alongside
	// Gaps; ranked by Adamic/Adar, capped at MaxSuggestedLinks.
	SuggestedLinks []LinkSuggestion
	// SuggestedLinksTruncated reports the suggestion list was capped (MaxSuggestedLinks)
	// or a hub neighbour was skipped as a generator (MaxNeighbourFanout); surfaced
	// as a notice.
	SuggestedLinksTruncated bool
	// Navigability holds the corpus-level navigability / structural-health scalars
	// (ADR 0014): compactness, stratum, characteristic/median path length,
	// clustering coefficient, diameter. Pure data — never a finding, never gates
	// the check exit code.
	Navigability Navigability

	// componentOf maps each document to its WCC ID, for emitters.
	componentOf map[identity.DocumentID]identity.DocumentID
}

// AnalyzeOptions bundles the tunables for a full analysis pass. The
// navigational-type set is fixed at graph-build time (BuildOptions), so it is
// not repeated here.
type AnalyzeOptions struct {
	// RootGlobs are configured root globs (in addition to conventions).
	RootGlobs []string
	// Hits tunes the HITS power iteration.
	Hits HitsOptions
	// Gaps tunes knowledge-gap detection.
	Gaps GapOptions
	// LinkPrediction tunes topology-based link prediction (ADR 0013). Zero values
	// are normalized to the documented defaults inside PredictLinks.
	LinkPrediction LinkPredictionOptions
	// InboundThreshold is the under-linked discoverability floor (ADR 0012).
	// Analyze normalizes a <=0 value up to DefaultInboundThreshold.
	InboundThreshold int
}

// Analyze runs the full P3 analysis over a pre-built graph and the corpus,
// returning the frozen metrics carrier. The graph must already be built from the
// corpus + resolved references. This is the single entry point the pipeline
// calls. All sub-results are deterministic.
func Analyze(g *ReferenceGraph, c *corpus.Corpus, opts AnalyzeOptions) *GraphMetrics {
	threshold := opts.InboundThreshold
	if threshold <= 0 {
		threshold = DefaultInboundThreshold
	}
	rootSet := ResolveRootSet(c, opts.RootGlobs)
	reach := g.ComputeReachability(rootSet)
	deg := g.BuildDegreeIndex()
	orphans := g.DetectOrphans(c, rootSet, deg, reach, OrphanOptions{InboundThreshold: threshold})
	wcc := g.WeaklyConnectedComponents()
	scc := g.StronglyConnectedComponents()
	bowtie := g.ClassifyBowtie(scc, wcc)
	hits := g.ComputeHITS(opts.Hits)
	// Reuse the WCCs computed above (single traversal, explicit data flow) rather
	// than recomputing them inside gap detection.
	gapResult := DetectGaps(wcc, opts.Gaps)
	// Link prediction is an ADDITIVE signal alongside gaps (ADR 0013): it runs on
	// the same projection but reports concrete unlinked, structurally-close PAIRS
	// rather than wholly-disconnected component pairs.
	linkResult := g.PredictLinks(opts.LinkPrediction)
	// Navigability scalars (ADR 0014): compactness/stratum over the directed
	// projection, path-length/clustering over the undirected closure. Pure data.
	navigability := g.ComputeNavigability()

	return &GraphMetrics{
		Graph:                   g,
		Hierarchy:               BuildHierarchyTree(c),
		RootSet:                 rootSet,
		Reachability:            reach,
		Degrees:                 deg,
		Orphans:                 orphans,
		WCC:                     wcc,
		SCC:                     scc,
		Bowtie:                  bowtie,
		HITS:                    hits,
		Gaps:                    gapResult.Gaps,
		GapsTruncated:           gapResult.Truncated,
		SuggestedLinks:          linkResult.Suggestions,
		SuggestedLinksTruncated: linkResult.Truncated,
		Navigability:            navigability,
		componentOf:             memberComponentIndex(wcc),
	}
}

// ComponentOf returns the WCC ID of a document (the sorted-min member of its
// weak component), or "" if unknown.
func (m *GraphMetrics) ComponentOf(id identity.DocumentID) identity.DocumentID {
	return m.componentOf[id]
}
