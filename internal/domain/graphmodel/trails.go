package graphmodel

import (
	"cmp"
	"slices"

	"github.com/stacklok/matlatl/internal/domain/identity"
)

// Trail is a suggested reading order for one weakly-connected component of the
// corpus (ADR 0016): a topologically-valid sequence that, among the documents
// currently AVAILABLE to read (their prerequisites already placed), prefers
// higher-authority docs by PageRank.
//
// This is the modern realization of Vannevar Bush's associative "trails" (Bush,
// "As We May Think", 1945): a curated path through linked documents. The order is
// topological over the SCC condensation, so a doc never appears before something
// it depends on; the PageRank tie-break makes the path prefer globally-important
// docs at each step. NOTE: a high-PageRank SINK is topologically LATE and will
// appear near the end — that is correct ("prefer authority among the available
// frontier", not literal "hubs first"). The Root is an IMPORTANCE pointer, not
// the head of Order: it may appear anywhere in the sequence.
type Trail struct {
	// Root is the component's most-important document: its highest-PageRank member
	// (ties broken by smallest DocumentID). It is NOT necessarily the first element
	// of Order — a high-importance sink is topologically late.
	Root identity.DocumentID
	// Order is the full topological reading sequence of the component's documents,
	// PageRank-preferred among the available frontier. Root may appear anywhere in
	// it (not necessarily first).
	Order []identity.DocumentID
}

// ComputeTrails builds one Trail per weakly-connected component (ADR 0016),
// sorted by Root. Within each component it runs a priority Kahn topological sort
// over the SCC CONDENSATION (so cycles cannot deadlock the ordering): the
// frontier is the set of zero-in-degree SCC representatives; at each step it pops
// the representative whose maximum-member PageRank is highest (ties broken by
// representative DocumentID ascending), appends that SCC's members (a multi-node
// SCC emits its members by PageRank DESC, then DocumentID ASC), and decrements
// the in-degree of its condensation successors.
//
// Determinism (CLAUDE.md): the frontier is a re-sorted slice (no heap, no map
// ranging for output); WCCs are iterated in sorted order; the condensation
// adjacency and member lists are sorted upstream. The Root of a component is its
// highest-PageRank document (tie min-ID). A singleton component yields [root].
func ComputeTrails(pr PageRankScores, wcc Components, scc Components, cond func() (map[identity.DocumentID]identity.DocumentID, map[identity.DocumentID][]identity.DocumentID)) []Trail {
	repOf, condAdj := cond()

	// Members of each SCC representative, in emit order (PageRank DESC, ID ASC).
	membersByRep := make(map[identity.DocumentID][]identity.DocumentID, len(scc))
	for _, comp := range scc {
		m := slices.Clone(comp.Members)
		sortByPageRankDesc(m, pr)
		membersByRep[comp.ID] = m
	}

	trails := make([]Trail, 0, len(wcc))
	for _, comp := range wcc { // sorted by ID
		trails = append(trails, trailForComponent(comp, pr, repOf, condAdj, membersByRep))
	}
	slices.SortFunc(trails, func(a, b Trail) int {
		return cmp.Compare(a.Root, b.Root)
	})
	return trails
}

// trailForComponent runs priority Kahn over the SCC condensation restricted to
// one weak component's representatives.
func trailForComponent(
	comp Component,
	pr PageRankScores,
	repOf map[identity.DocumentID]identity.DocumentID,
	condAdj map[identity.DocumentID][]identity.DocumentID,
	membersByRep map[identity.DocumentID][]identity.DocumentID,
) Trail {
	root := highestPageRank(comp.Members, pr)

	// The distinct SCC representatives that live in this weak component.
	repSet := make(map[identity.DocumentID]struct{})
	for _, m := range comp.Members {
		repSet[repOf[m]] = struct{}{}
	}

	// In-degree of each representative, counting only edges INSIDE this component
	// (the condensation is global, so filter to repSet).
	indeg := make(map[identity.DocumentID]int, len(repSet))
	for rep := range repSet {
		indeg[rep] = 0
	}
	for rep := range repSet {
		for _, succ := range condAdj[rep] {
			if _, in := repSet[succ]; in {
				indeg[succ]++
			}
		}
	}

	// Frontier: zero-in-degree representatives, kept as a re-sorted slice.
	frontier := make([]identity.DocumentID, 0, len(repSet))
	for rep := range repSet {
		if indeg[rep] == 0 {
			frontier = append(frontier, rep)
		}
	}

	order := make([]identity.DocumentID, 0, len(comp.Members))
	for len(frontier) > 0 {
		// Re-sort the frontier by (−maxMemberPageRank, repID) and pop the best.
		sortFrontier(frontier, pr, membersByRep)
		rep := frontier[0]
		frontier = frontier[1:]

		order = append(order, membersByRep[rep]...)

		for _, succ := range condAdj[rep] {
			if _, in := repSet[succ]; !in {
				continue
			}
			indeg[succ]--
			if indeg[succ] == 0 {
				frontier = append(frontier, succ)
			}
		}
	}

	return Trail{Root: root, Order: order}
}

// sortFrontier orders representatives by maximum-member PageRank descending, ties
// broken by representative DocumentID ascending (deterministic; no map ranging
// for output).
func sortFrontier(reps []identity.DocumentID, pr PageRankScores, membersByRep map[identity.DocumentID][]identity.DocumentID) {
	slices.SortFunc(reps, func(a, b identity.DocumentID) int {
		pa := maxMemberPageRank(membersByRep[a], pr)
		pb := maxMemberPageRank(membersByRep[b], pr)
		switch {
		case pa > pb:
			return -1
		case pa < pb:
			return 1
		}
		return cmp.Compare(a, b)
	})
}

// maxMemberPageRank returns the highest PageRank score among an SCC's members.
// membersByRep is non-empty for every real representative.
func maxMemberPageRank(members []identity.DocumentID, pr PageRankScores) float64 {
	best := -1.0
	for _, m := range members {
		if s := pr.Score(m); s > best {
			best = s
		}
	}
	return best
}

// sortByPageRankDesc sorts documents by PageRank score descending, ties broken by
// DocumentID ascending (deterministic).
func sortByPageRankDesc(ids []identity.DocumentID, pr PageRankScores) {
	slices.SortFunc(ids, func(a, b identity.DocumentID) int {
		sa, sb := pr.Score(a), pr.Score(b)
		switch {
		case sa > sb:
			return -1
		case sa < sb:
			return 1
		}
		return cmp.Compare(a, b)
	})
}

// highestPageRank returns the member with the highest PageRank (tie: min ID).
func highestPageRank(members []identity.DocumentID, pr PageRankScores) identity.DocumentID {
	best := members[0]
	bestScore := pr.Score(best)
	for _, m := range members[1:] {
		s := pr.Score(m)
		if s > bestScore || (s == bestScore && m < best) {
			best, bestScore = m, s
		}
	}
	return best
}
