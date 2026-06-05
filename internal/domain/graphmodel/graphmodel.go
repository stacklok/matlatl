// Package graphmodel holds the pure-domain graph types: the directed reference
// graph over documents and sections, and the hierarchy tree. In the skeleton
// these are type stubs only; the real graph algorithms (and the wrapper around
// dominikbraun/graph) land in a later phase. This package depends only on the
// standard library and the sibling reference package — never on the third-party
// graph library, application, or infrastructure (ADR 0004).
package graphmodel

import "github.com/stacklok/doctopus/internal/domain/reference"

// NodeID identifies a vertex in the reference graph. For a document node it is
// the DocumentID string; for a section node it is the DocumentID plus an anchor
// fragment. The exact section encoding is fixed in Phase 3.
type NodeID string

// String returns the identifier as a plain string.
func (n NodeID) String() string { return string(n) }

// NodeKind distinguishes document vertices from section vertices. Both are
// first-class vertices in the mixed-granularity graph (ADR 0004).
type NodeKind int

const (
	// NodeKindDocument is a whole-file vertex. (Named with a NodeKind prefix to
	// avoid shadowing the corpus.Document/Section types this package imports in
	// P3.)
	NodeKindDocument NodeKind = iota
	// NodeKindSection is a heading-scoped vertex.
	NodeKindSection
)

// String returns the canonical name of the node kind.
func (k NodeKind) String() string {
	switch k {
	case NodeKindDocument:
		return "document"
	case NodeKindSection:
		return "section"
	default:
		return "unknown"
	}
}

// Valid reports whether k is a defined NodeKind.
func (k NodeKind) Valid() bool {
	return k >= NodeKindDocument && k <= NodeKindSection
}

// Edge is a directed, typed edge in the reference graph.
type Edge struct {
	From NodeID
	To   NodeID
	Type reference.LinkType
}

// ReferenceGraph is the directed graph over document and section vertices with
// typed edges (ADR 0004).
//
// TODO(P3): wrap dominikbraun/graph behind this type and add BFS, component,
// and HITS support with deterministic (sorted) iteration.
type ReferenceGraph struct{}

// NewReferenceGraph returns an empty reference graph.
func NewReferenceGraph() *ReferenceGraph {
	return &ReferenceGraph{}
}

// HierarchyTree is the folder / front-matter parent hierarchy, overlaid with
// section nesting.
//
// TODO(P3): populate from the corpus and section trees.
type HierarchyTree struct{}

// NewHierarchyTree returns an empty hierarchy tree.
func NewHierarchyTree() *HierarchyTree {
	return &HierarchyTree{}
}
