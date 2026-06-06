package graphmodel

import (
	"math"
	"slices"

	"github.com/stacklok/matlatl/internal/domain/identity"
)

// MaxSuggestedLinks is the hard defensive cap on the number of suggested links
// PredictLinks will produce in a single pass. Like MaxGaps, the candidate space
// is super-linear (every shared neighbour generates O(deg^2) pairs), so on a
// densely-connected corpus the pair count can blow up. We stop at this cap and
// surface truncation (LinkSuggestionResult.Truncated) rather than allocate an
// unbounded slice — mirroring MaxGaps and the scanner's MaxFiles cap (ADR 0003).
// No silent cap: truncation is always reported.
const MaxSuggestedLinks = 1000

// MaxNeighbourFanout bounds the degree of a common neighbour used as a
// pair-GENERATOR. A hub of degree d contributes O(d^2) candidate pairs; an index
// page linking hundreds of docs would otherwise dominate the candidate space and
// produce low-signal suggestions (everything "shares" the index). We skip any
// neighbour whose undirected degree exceeds this as a generator and set
// LinkSuggestionResult.Truncated / HubsSkipped, so the cost is bounded at
// O(Σ deg(c)^2) over non-hub neighbours. The threshold is the Adamic/Adar
// intuition made operational: very high-degree common neighbours carry little
// signal (their 1/log(deg) weight is already tiny), so skipping them as
// generators loses almost no ranking information while bounding the work.
const MaxNeighbourFanout = 256

// DefaultMinSharedNeighbours is the floor on how many neighbours two documents
// must share before they are suggested as a link. A single shared neighbour is
// weak evidence (two docs both linked from one index page); requiring at least
// two keeps the signal conservative, mirroring how GapOptions.MinComponentSize
// defaults to 2 to avoid a blow-up of trivial singleton pairs. The zero value of
// LinkPredictionOptions.MinSharedNeighbours is normalized up to this default.
const DefaultMinSharedNeighbours = 2

// LinkPredictionOptions tunes topology-based link prediction (the suggested-link
// signal). Zero values are normalized to the documented defaults, like
// GapOptions.MinComponentSize.
type LinkPredictionOptions struct {
	// MinSharedNeighbours is the minimum |N(A)∩N(B)| an unlinked pair must have
	// to be suggested. <=0 is normalized to DefaultMinSharedNeighbours (2).
	MinSharedNeighbours int
	// MaxFanout is the undirected-degree ceiling above which a common neighbour is
	// skipped as a pair-generator (the hub guard). <=0 is normalized to
	// MaxNeighbourFanout.
	MaxFanout int
}

// LinkSuggestion is a topology-based suggestion that two UNLINKED documents
// (neither links to the other) may warrant a navigational link, because they are
// structurally close: they share neighbours in the undirected closure
// N(x)=out(x)∪in(x). It reports the primary Adamic/Adar score plus the directed
// components (bibliographic coupling and co-citation) so a consumer can see WHY.
// DocA < DocB by DocumentID string (the pair is unordered, stored canonically).
type LinkSuggestion struct {
	DocA identity.DocumentID
	DocB identity.DocumentID
	// SharedNeighbours is |N(A)∩N(B)| over the undirected closure.
	SharedNeighbours int
	// Coupling is bibliographic coupling: |out(A)∩out(B)| (both link to the same
	// docs).
	Coupling int
	// CoCitation is |in(A)∩in(B)| (the same docs link to both).
	CoCitation int
	// AdamicAdar is the primary similarity score: Σ over common neighbours c (with
	// |N(c)|>1) of 1/log(|N(c)|). Rare shared neighbours weigh more than hubs.
	AdamicAdar float64
}

// LinkSuggestionResult is the outcome of link prediction: the ranked, capped
// suggestions plus whether the list was truncated. Truncated is set when EITHER
// the MaxSuggestedLinks cap was hit OR a hub above MaxFanout was skipped as a
// generator (HubsSkipped) — in both cases the list is not exhaustive and the
// caller surfaces a notice, exactly like GapResult.Truncated.
type LinkSuggestionResult struct {
	Suggestions []LinkSuggestion
	Truncated   bool
	// HubsSkipped reports that at least one common neighbour exceeded MaxFanout and
	// was skipped as a pair-generator (so some structurally-close pairs may be
	// absent). It implies Truncated.
	HubsSkipped bool
}

// pairAccum accumulates the running shared-neighbour count and Adamic/Adar score
// for one candidate (unordered) document pair during the shared-neighbour walk.
type pairAccum struct {
	shared     int
	adamicAdar float64
}

// docPair is the canonical (min,max by DocumentID string) key for an unordered
// document pair.
type docPair struct {
	a identity.DocumentID
	b identity.DocumentID
}

// PredictLinks suggests navigational links between UNLINKED but structurally
// close documents over the document projection (ADR 0013). It AUGMENTS the
// WCC-pair knowledge-gap signal (DetectGaps): gaps flag wholly-disconnected
// clusters, whereas this flags concrete unlinked PAIRS within or across clusters
// that already share neighbours.
//
// Algorithm (deterministic, stdlib+math only, ADR 0004): for each potential
// common neighbour c with undirected degree deg(c) in [2, MaxFanout], every
// unordered pair (A,B) within N(c) accumulates shared++ and adamicAdar +=
// 1/log(deg(c)). Iterating c's neighbour list in SORTED order fixes the float
// addition order, so the Adamic/Adar sum is byte-stable. A neighbour with
// deg(c) > MaxFanout is skipped as a generator (HubsSkipped). After accumulation,
// pairs with shared < MinSharedNeighbours or that are already linked are dropped;
// coupling/co-citation are computed via sorted-list merge-intersection. Results
// are sorted by AdamicAdar DESC, then SharedNeighbours DESC, then DocA ASC, then
// DocB ASC (the hits.go rankDesc float-compare pattern, no epsilon), and capped
// at MaxSuggestedLinks (Truncated).
func (g *ReferenceGraph) PredictLinks(opts LinkPredictionOptions) LinkSuggestionResult {
	minShared := opts.MinSharedNeighbours
	if minShared <= 0 {
		minShared = DefaultMinSharedNeighbours
	}
	maxFanout := opts.MaxFanout
	if maxFanout <= 0 {
		maxFanout = MaxNeighbourFanout
	}

	docs := g.documents // sorted
	// Precompute the undirected neighbour closure N(x) and degree deg(x) once.
	neighbours := make(map[identity.DocumentID][]identity.DocumentID, len(docs))
	for _, id := range docs {
		neighbours[id] = g.undirectedNeighbours(id)
	}

	pairs := make(map[docPair]*pairAccum)
	hubsSkipped := false

	// Candidate generation by SHARED NEIGHBOURS (not all pairs): for each common
	// neighbour c, every unordered pair within N(c) gains a shared neighbour.
	for _, c := range docs { // sorted
		members := neighbours[c]
		deg := len(members)
		if deg < 2 {
			continue // a degree-<2 neighbour cannot be common to a pair
		}
		if deg > maxFanout {
			// Hub guard: skip as a generator so a high-degree index page does not
			// dominate the candidate space (and weighs almost nothing anyway).
			hubsSkipped = true
			continue
		}
		// Adamic/Adar weight of this common neighbour: rare neighbours weigh more.
		// deg >= 2 here so log(deg) > 0.
		weight := 1 / math.Log(float64(deg))
		// members is sorted; the i<j nested walk yields pairs in a fixed order, so
		// the running float sum per pair is accumulated deterministically.
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				key := docPair{a: members[i], b: members[j]} // members sorted ⇒ a<b
				acc := pairs[key]
				if acc == nil {
					acc = &pairAccum{}
					pairs[key] = acc
				}
				acc.shared++
				acc.adamicAdar += weight
			}
		}
	}

	// Materialize the surviving suggestions: drop below-threshold and linked pairs,
	// then compute coupling/co-citation.
	suggestions := make([]LinkSuggestion, 0, len(pairs))
	for key, acc := range pairs {
		if acc.shared < minShared {
			continue
		}
		if g.linked(key.a, key.b) {
			continue
		}
		suggestions = append(suggestions, LinkSuggestion{
			DocA:             key.a,
			DocB:             key.b,
			SharedNeighbours: acc.shared,
			Coupling:         intersectSize(g.projAdj[key.a], g.projAdj[key.b]),
			CoCitation:       intersectSize(g.projRev[key.a], g.projRev[key.b]),
			AdamicAdar:       acc.adamicAdar,
		})
	}

	// Rank: Adamic/Adar DESC, shared DESC, DocA ASC, DocB ASC (deterministic total
	// order, mirroring hits.go rankDesc — direct float compare, no epsilon).
	slices.SortFunc(suggestions, func(x, y LinkSuggestion) int {
		switch {
		case x.AdamicAdar > y.AdamicAdar:
			return -1
		case x.AdamicAdar < y.AdamicAdar:
			return 1
		case x.SharedNeighbours > y.SharedNeighbours:
			return -1
		case x.SharedNeighbours < y.SharedNeighbours:
			return 1
		case x.DocA < y.DocA:
			return -1
		case x.DocA > y.DocA:
			return 1
		case x.DocB < y.DocB:
			return -1
		case x.DocB > y.DocB:
			return 1
		default:
			return 0
		}
	})

	truncated := hubsSkipped
	if len(suggestions) > MaxSuggestedLinks {
		suggestions = suggestions[:MaxSuggestedLinks]
		truncated = true
	}

	return LinkSuggestionResult{
		Suggestions: suggestions,
		Truncated:   truncated,
		HubsSkipped: hubsSkipped,
	}
}

// undirectedNeighbours returns the sorted, de-duplicated union N(x)=out(x)∪in(x)
// over the document projection. The projection lists are already sorted, deduped
// and self-loop-free (buildProjection), so this is a merge of two sorted lists.
func (g *ReferenceGraph) undirectedNeighbours(id identity.DocumentID) []identity.DocumentID {
	out := g.projAdj[id]
	in := g.projRev[id]
	merged := make([]identity.DocumentID, 0, len(out)+len(in))
	i, j := 0, 0
	for i < len(out) && j < len(in) {
		switch {
		case out[i] < in[j]:
			merged = append(merged, out[i])
			i++
		case out[i] > in[j]:
			merged = append(merged, in[j])
			j++
		default:
			merged = append(merged, out[i])
			i++
			j++
		}
	}
	merged = append(merged, out[i:]...)
	merged = append(merged, in[j:]...)
	return merged
}

// linked reports whether a and b are directly connected in either direction over
// the document projection (b∈out(a) or a∈out(b)). The projection lists are
// sorted, so membership is a binary search.
func (g *ReferenceGraph) linked(a, b identity.DocumentID) bool {
	if _, found := slices.BinarySearch(g.projAdj[a], b); found {
		return true
	}
	_, found := slices.BinarySearch(g.projAdj[b], a)
	return found
}

// intersectSize returns |a∩b| for two sorted, de-duplicated DocumentID lists via
// a single merge-walk (O(len(a)+len(b))).
func intersectSize(a, b []identity.DocumentID) int {
	n := 0
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			n++
			i++
			j++
		}
	}
	return n
}
