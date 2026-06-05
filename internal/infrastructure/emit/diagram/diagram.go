// Package diagram renders the reference graph as hand-rolled Mermaid and DOT /
// Graphviz text. Both formats are emitted by hand: no mature Go library emits
// Mermaid (ADR 0002), and DOT is hand-rolled too so we keep total control over
// label escaping (the ADR 0003 hostile-label requirement) and deterministic
// ordering — see the DOT-library decision note in dot.go. This is
// infrastructure: it reads the frozen emit.View / graphmodel and never mutates
// the domain (ADR 0004).
package diagram

import (
	"crypto/sha256"
	"fmt"

	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
)

// LargeGraphThreshold is the document-vertex count above which the diagram
// emitters switch to a focused subgraph (orphans/broken + their neighborhoods)
// rather than a giant unreadable blob. No silent truncation: a truncation note
// is always emitted (ADR: Mermaid is unreadable past a few hundred nodes).
const LargeGraphThreshold = 200

// nodeIDFor returns a stable, syntactically-safe identifier for a document node
// in both Mermaid and DOT. Document IDs contain slashes, dots, dashes and may
// be hostile, none of which are safe as a bare node identifier, so we derive a
// deterministic opaque ID ("n_" + short sha256) and carry the real path only in
// the (escaped) label. Deterministic: the same DocumentID always maps to the
// same node ID.
func nodeIDFor(id identity.DocumentID) string {
	sum := sha256.Sum256([]byte(id))
	return fmt.Sprintf("n_%x", sum[:8])
}

// brokenNodeIDFor returns the node ID for a broken-link *target* placeholder
// (a target that is referenced but does not resolve to a corpus document). It is
// namespaced separately so it never collides with a real document node.
func brokenNodeIDFor(target string) string {
	sum := sha256.Sum256([]byte("broken\x00" + target))
	return fmt.Sprintf("b_%x", sum[:8])
}

// classifyDoc reports the visual class of a document for both emitters.
type docClass int

const (
	classNormal docClass = iota
	classOrphan
	classUnreachable
)

// docClasses computes the per-document visual class (orphan/unreachable/normal)
// from the View. Orphan takes precedence over unreachable (ADR 0007 reports the
// more specific orphan).
func docClasses(v emit.View) map[identity.DocumentID]docClass {
	cls := make(map[identity.DocumentID]docClass, len(v.Docs))
	for _, id := range v.Unreachable {
		cls[id] = classUnreachable
	}
	for _, id := range v.Orphans {
		cls[id] = classOrphan
	}
	return cls
}

// projEdge is a directed document-projection edge (both endpoints are corpus
// documents), used by both emitters.
type projEdge struct {
	From identity.DocumentID
	To   identity.DocumentID
}

// projectionEdges returns the document-projection navigational edges in sorted
// (From, To) order, reading the frozen graph via its public projection
// accessors. Self-loops are already excluded by the projection (ADR 0007).
func projectionEdges(v emit.View) []projEdge {
	var edges []projEdge
	if v.Metrics == nil || v.Metrics.Graph == nil {
		return edges
	}
	g := v.Metrics.Graph
	for _, from := range g.Documents() { // sorted
		for _, to := range g.ProjectionOut(from) { // sorted
			edges = append(edges, projEdge{From: from, To: to})
		}
	}
	return edges
}

// focusSet computes the focused subgraph node set for a large graph: the
// orphans and unreachable documents, every document with a broken edge, and
// their immediate projection neighborhood (one hop in/out). Returns the set and
// true when focusing was applied (graph exceeded the threshold), or the full set
// and false otherwise. The decision is deterministic and never silent — callers
// emit a truncation note when focused is true.
func focusSet(v emit.View) (set map[identity.DocumentID]struct{}, focused bool) {
	all := make(map[identity.DocumentID]struct{}, len(v.Docs))
	for _, d := range v.Docs {
		all[d.ID] = struct{}{}
	}
	if len(v.Docs) <= LargeGraphThreshold {
		return all, false
	}

	seed := make(map[identity.DocumentID]struct{})
	for _, id := range v.Orphans {
		seed[id] = struct{}{}
	}
	for _, id := range v.Unreachable {
		seed[id] = struct{}{}
	}
	for _, e := range v.BrokenEdges {
		seed[e.Origin] = struct{}{}
	}

	focus := make(map[identity.DocumentID]struct{})
	for id := range seed {
		focus[id] = struct{}{}
		if v.Metrics != nil && v.Metrics.Graph != nil {
			g := v.Metrics.Graph
			for _, nb := range g.ProjectionOut(id) {
				focus[nb] = struct{}{}
			}
			for _, nb := range g.ProjectionIn(id) {
				focus[nb] = struct{}{}
			}
		}
	}
	return focus, true
}

// componentColorIndex maps each component ID (sorted-min member) to a stable
// 0-based palette index, in component-sort order, so coloring is deterministic.
func componentColorIndex(v emit.View) map[identity.DocumentID]int {
	idx := make(map[identity.DocumentID]int)
	if v.Metrics == nil {
		return idx
	}
	for _, c := range v.Metrics.WCC { // sorted by ID
		if _, ok := idx[c.ID]; !ok {
			idx[c.ID] = len(idx)
		}
	}
	return idx
}
