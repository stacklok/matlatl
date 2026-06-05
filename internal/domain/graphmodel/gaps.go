package graphmodel

import (
	"github.com/stacklok/doctopus/internal/domain/identity"
)

// MaxGaps is the hard defensive cap on the number of gaps DetectGaps will
// produce in a single pass. Gap detection is inherently O(k^2) in the number of
// kept components, so on pathological input (e.g. thousands of disconnected
// clusters) the pair count explodes. We stop at MaxGaps and surface truncation
// (GapResult.Truncated) rather than allocate millions of structs — mirroring how
// the scanner caps discovery at MaxFiles and surfaces a truncation notice (ADR
// 0003). No silent cap: the truncation is always reported.
const MaxGaps = 1000

// GapOptions tunes knowledge-gap detection: finding pairs of weakly-connected
// components that could plausibly be bridged — candidate "knowledge gaps" where
// two clusters of documentation likely should reference each other but do not.
//
// Gap detection is EXPERIMENTAL and intentionally conservative (ADR 0007). By
// construction, two DISTINCT weakly-connected components have ZERO navigational
// links between them — that disconnection is exactly what makes them separate
// weak components. So a "gap" is simply a pair of distinct WCCs (each at or
// above MinComponentSize): two disconnected clusters that may warrant a bridge.
// Callers label gaps Info severity, so they never fail a build. The signal is a
// heuristic, not a correctness claim: linking the two clusters merges them into
// one component and removes the pair.
type GapOptions struct {
	// MinComponentSize ignores trivial components smaller than this on either
	// side. The pipeline sets it to 2: isolated singletons are already reported
	// as orphans (ADR 0007), so they must NOT also generate a combinatorial
	// blow-up of singleton-vs-singleton gaps. The zero value is normalized to 2
	// to keep that safe default even for a zero-value GapOptions.
	MinComponentSize int
}

// Gap is a candidate bridge between two distinct weakly-connected components,
// identified by their IDs (the smaller ID first) and a representative document
// from each side. Because the two components are distinct WCCs, they have no
// navigational links between them by construction.
type Gap struct {
	ComponentA identity.DocumentID
	ComponentB identity.DocumentID
	// RepresentativeA/B are the (sorted-min) member of each component, useful as
	// a concrete bridge suggestion.
	RepresentativeA identity.DocumentID
	RepresentativeB identity.DocumentID
}

// GapResult is the outcome of gap detection: the (sorted) candidate gaps plus
// whether the list was truncated at MaxGaps. Truncated is surfaced as a notice
// by the caller, exactly like the scanner's MaxFiles truncation.
type GapResult struct {
	Gaps      []Gap
	Truncated bool
}

// DetectGaps reports candidate gaps between pairs of distinct weakly-connected
// components. It takes the ALREADY-COMPUTED WCC components (one traversal, in
// metrics.go) rather than recomputing them, so data flow is explicit and there
// is no duplicate union-find pass. Distinct WCCs have zero cross-links by
// construction (ADR 0007), so every pair of kept components is a gap; the only
// tuning knob is MinComponentSize, which drops trivial/singleton clusters.
//
// Results are deterministic: wccs is sorted by ID (see WeaklyConnectedComponents),
// so the nested pair loop yields gaps in (ComponentA, ComponentB) sorted order.
// The total is hard-capped at MaxGaps; on hitting the cap, detection stops and
// GapResult.Truncated is set (no silent truncation).
func DetectGaps(wccs Components, opts GapOptions) GapResult {
	minSize := opts.MinComponentSize
	if minSize < 2 {
		// Default (and floor) is 2: singletons are reported as orphans, never as
		// gaps, which also bounds the O(k^2) pair count on hostile input.
		minSize = 2
	}

	// Filter by size (wccs is already sorted by ID).
	kept := make([]Component, 0, len(wccs))
	for _, c := range wccs {
		if len(c.Members) >= minSize {
			kept = append(kept, c)
		}
	}

	var gaps []Gap
	truncated := false
outer:
	for i := 0; i < len(kept); i++ {
		for j := i + 1; j < len(kept); j++ {
			if len(gaps) >= MaxGaps {
				truncated = true
				break outer
			}
			gaps = append(gaps, Gap{
				ComponentA:      kept[i].ID,
				ComponentB:      kept[j].ID,
				RepresentativeA: kept[i].Members[0],
				RepresentativeB: kept[j].Members[0],
			})
		}
	}
	return GapResult{Gaps: gaps, Truncated: truncated}
}

// memberComponentIndex maps each document to its component's ID (sorted-min
// member), useful for emitters. Built from a Components list.
func memberComponentIndex(comps Components) map[identity.DocumentID]identity.DocumentID {
	idx := make(map[identity.DocumentID]identity.DocumentID)
	for _, c := range comps {
		for _, m := range c.Members {
			idx[m] = c.ID
		}
	}
	return idx
}
