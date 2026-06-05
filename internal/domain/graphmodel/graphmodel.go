// Package graphmodel holds the pure-domain graph model and analyses: the
// directed reference graph over documents and sections, the hierarchy tree, and
// the orphan/component/HITS/gap analyses defined on the document projection
// (see ADR 0007). It depends only on the standard library and the sibling
// corpus/identity/reference packages — never on a third-party graph library,
// application, or infrastructure (ADR 0004). All algorithms are hand-rolled with
// sorted iteration so output is fully deterministic.
package graphmodel

import (
	"cmp"
	"slices"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// NodeID identifies a vertex. A Document vertex's NodeID is exactly the
// DocumentID string; a Section vertex's NodeID is "<DocumentID>#<slug>" (ADR
// 0007).
type NodeID string

// String returns the identifier as a plain string.
func (n NodeID) String() string { return string(n) }

// NodeIDForDocument returns the NodeID of a document vertex (the DocumentID's
// string), so a Document-kind NodeID round-trips to its DocumentID.
func NodeIDForDocument(id identity.DocumentID) NodeID {
	return NodeID(id.String())
}

// NodeIDForSection returns the NodeID of a section vertex: "<DocumentID>#<slug>".
func NodeIDForSection(id identity.DocumentID, slug string) NodeID {
	return NodeID(id.String() + "#" + slug)
}

// NodeKind distinguishes document vertices from section vertices.
type NodeKind int

const (
	// NodeKindDocument is a whole-file vertex.
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

// EdgeKind distinguishes structural containment edges from navigational
// reference edges (ADR 0007).
type EdgeKind int

const (
	// EdgeContains is a structural edge (Document→Section, Section→Section).
	EdgeContains EdgeKind = iota
	// EdgeReference is a navigational edge (a resolved in-corpus link).
	EdgeReference
)

// String returns the canonical name of the edge kind.
func (k EdgeKind) String() string {
	switch k {
	case EdgeContains:
		return "contains"
	case EdgeReference:
		return "reference"
	default:
		return "unknown"
	}
}

// Node is a graph vertex.
type Node struct {
	ID   NodeID
	Kind NodeKind
	// Document is the owning document identity (the document itself for a
	// document vertex, or the section's document for a section vertex).
	Document identity.DocumentID
	// Slug is the section slug for a section vertex; empty for a document vertex.
	Slug string
}

// Edge is a directed, typed edge. Type is meaningful only for EdgeReference
// edges; for EdgeContains it is left at the zero LinkType and ignored.
type Edge struct {
	From NodeID
	To   NodeID
	Kind EdgeKind
	Type reference.LinkType
}

// DefaultNavigationalTypes is the set of LinkTypes that count as navigational in
// the document projection (ADR 0007). External is deliberately absent: an
// external link neither reaches nor is reached.
var DefaultNavigationalTypes = []reference.LinkType{
	reference.RelativeLink,
	reference.Wikilink,
	reference.Anchor,
	reference.ImageEmbed,
	reference.Transclusion,
	reference.FrontmatterRelated,
}

// ReferenceGraph is the mixed-granularity graph: document and section vertices
// with CONTAINS and REFERENCE edges. It exposes the document projection that all
// analyses run on. Built once and treated as immutable.
type ReferenceGraph struct {
	nodes  map[NodeID]Node
	edges  []Edge
	navSet map[reference.LinkType]struct{}
	// strictDirLinks mirrors BuildOptions.StrictDirectoryLinks: when true, a
	// directory link contributes only its primary index edge, not child edges
	// (ADR 0008).
	strictDirLinks bool

	// documents is the sorted set of document identities (vertices of kind
	// document), cached for deterministic iteration.
	documents []identity.DocumentID

	// projAdj / projRev are the document-projection adjacency (out / in), keyed
	// by DocumentID, each a sorted, de-duplicated neighbor list. Self-loops are
	// excluded (ADR 0007).
	projAdj map[identity.DocumentID][]identity.DocumentID
	projRev map[identity.DocumentID][]identity.DocumentID
}

// BuildOptions tunes graph construction.
type BuildOptions struct {
	// NavigationalTypes overrides the default navigational LinkType set. Empty
	// means DefaultNavigationalTypes.
	NavigationalTypes []reference.LinkType
	// StrictDirectoryLinks controls how a directory link (TargetDirectory)
	// confers reachability (ADR 0008). When false (default, the lenient "vouch"
	// policy) a directory link adds navigational edges Origin → each direct-child
	// document, so the folder's contents are reachable. When true (the
	// documentation-hygiene hardline, wired to --strict) a directory link adds
	// only the primary Origin → index edge (when an index exists) and does NOT
	// vouch for the directory's other contents.
	StrictDirectoryLinks bool
}

// BuildReferenceGraph assembles the graph from a frozen corpus and the resolved
// references over it. The corpus supplies vertices (documents + sections) and
// the CONTAINS tree; refs supply REFERENCE edges (only Health==Valid, in-corpus
// targets). Origin is attributed to the containing section when the ref line
// falls in a section span, else the document (ADR 0007).
func BuildReferenceGraph(c *corpus.Corpus, refs []reference.Reference, opts BuildOptions) *ReferenceGraph {
	navTypes := opts.NavigationalTypes
	if len(navTypes) == 0 {
		navTypes = DefaultNavigationalTypes
	}
	navSet := make(map[reference.LinkType]struct{}, len(navTypes))
	for _, t := range navTypes {
		navSet[t] = struct{}{}
	}

	g := &ReferenceGraph{
		nodes:          make(map[NodeID]Node),
		navSet:         navSet,
		strictDirLinks: opts.StrictDirectoryLinks,
		projAdj:        make(map[identity.DocumentID][]identity.DocumentID),
		projRev:        make(map[identity.DocumentID][]identity.DocumentID),
	}

	// Vertices + CONTAINS edges, in sorted document order.
	docs := c.Documents()
	for _, doc := range docs {
		g.documents = append(g.documents, doc.ID)
		docNode := NodeIDForDocument(doc.ID)
		g.nodes[docNode] = Node{ID: docNode, Kind: NodeKindDocument, Document: doc.ID}
		g.addContainsTree(doc)
	}
	// Defensive: c.Documents() already returns IDs in sorted order, so this is a
	// no-op today. Kept (cheap) so g.documents stays sorted by contract even if
	// Documents() iteration order ever changes — downstream BFS/projection rely
	// on it for determinism.
	slices.Sort(g.documents)

	// REFERENCE edges from resolved references.
	for _, ref := range refs {
		g.addReferenceEdge(c, ref)
	}

	g.buildProjection()
	return g
}

// addContainsTree adds section vertices and CONTAINS edges for a document's
// section tree. The synthetic level-0 root is not a vertex; its children are the
// document's top-level sections (Document→Section edges).
func (g *ReferenceGraph) addContainsTree(doc *corpus.Document) {
	if doc.Root == nil {
		return
	}
	docNode := NodeIDForDocument(doc.ID)
	var walk func(parentNode NodeID, sec *corpus.Section)
	walk = func(parentNode NodeID, sec *corpus.Section) {
		for _, child := range sec.Children {
			if child.Slug == "" {
				// A heading with no slug cannot be a stable vertex; still recurse
				// so deeper slugged headings attach to the nearest slugged ancestor.
				walk(parentNode, child)
				continue
			}
			childNode := NodeIDForSection(doc.ID, child.Slug)
			g.nodes[childNode] = Node{
				ID: childNode, Kind: NodeKindSection, Document: doc.ID, Slug: child.Slug,
			}
			g.edges = append(g.edges, Edge{From: parentNode, To: childNode, Kind: EdgeContains})
			walk(childNode, child)
		}
	}
	walk(docNode, doc.Root)
}

// addReferenceEdge adds a single navigational REFERENCE edge for a resolved
// reference, if it is in-corpus and Valid. Target is a section vertex for an
// anchored link, else the document vertex. Origin is the containing section when
// determinable, else the document.
func (g *ReferenceGraph) addReferenceEdge(c *corpus.Corpus, ref reference.Reference) {
	if ref.Health != reference.Valid {
		return
	}
	// Directory links (ADR 0008) contribute their own edge set: the primary edge
	// to the index doc (if any) plus, under the lenient policy, an edge to each
	// direct-child document so the folder's contents are reachable.
	if ref.Target.Kind == reference.TargetDirectory {
		g.addDirectoryEdges(c, ref)
		return
	}
	targetDoc := ref.Target.DocumentID
	if targetDoc == "" {
		return
	}
	// Target must be an in-corpus document vertex.
	if _, ok := c.Get(targetDoc); !ok {
		return
	}

	var to NodeID
	switch ref.Target.Kind {
	case reference.TargetSection:
		to = NodeIDForSection(targetDoc, ref.Target.Anchor)
		if _, ok := g.nodes[to]; !ok {
			// Anchor not a known section vertex (shouldn't happen for Valid); fall
			// back to the document vertex so reachability is not lost.
			to = NodeIDForDocument(targetDoc)
		}
	default:
		to = NodeIDForDocument(targetDoc)
	}

	from := g.originNode(c, ref)
	g.edges = append(g.edges, Edge{From: from, To: to, Kind: EdgeReference, Type: ref.Type})
}

// addDirectoryEdges adds the navigational edges for a directory link (ADR 0008).
// The origin is the containing section (or document) of the reference. Under the
// default (lenient) policy it adds Origin → each direct-child document so the
// folder's contents are reachable; the index doc, if any, is among the children.
// Under the strict policy it adds only the primary Origin → index edge (when an
// index exists), and nothing when the directory has no index. Only in-corpus
// document targets become edges; the edge LinkType mirrors the reference so the
// navigational-set filter in the projection treats it like any other link.
func (g *ReferenceGraph) addDirectoryEdges(c *corpus.Corpus, ref reference.Reference) {
	from := g.originNode(c, ref)
	add := func(target identity.DocumentID) {
		if target == "" {
			return
		}
		if _, ok := c.Get(target); !ok {
			return
		}
		to := NodeIDForDocument(target)
		g.edges = append(g.edges, Edge{From: from, To: to, Kind: EdgeReference, Type: ref.Type})
	}

	if g.strictDirLinks {
		// Hardline: resolve/validate but vouch only for the index, not contents.
		add(ref.Target.DocumentID)
		return
	}
	// Lenient (default): vouch for every direct child (the index is among them).
	for _, child := range ref.Target.Children {
		add(child)
	}
}

// originNode resolves the origin vertex of a reference: the containing section
// (by line within a section's byte span) when determinable, else the document.
func (g *ReferenceGraph) originNode(c *corpus.Corpus, ref reference.Reference) NodeID {
	doc, ok := c.Get(ref.Origin)
	if !ok || doc.Root == nil || ref.Line <= 0 {
		return NodeIDForDocument(ref.Origin)
	}
	if sec := containingSection(doc, ref.Line); sec != nil && sec.Slug != "" {
		return NodeIDForSection(ref.Origin, sec.Slug)
	}
	return NodeIDForDocument(ref.Origin)
}

// buildProjection computes the document-projection adjacency (out + in) from the
// navigational REFERENCE edges, dropping CONTAINS edges, non-navigational types,
// and self-loops, and collapsing section endpoints to their documents.
func (g *ReferenceGraph) buildProjection() {
	// Use sets to de-dup multi-edges before sorting.
	out := make(map[identity.DocumentID]map[identity.DocumentID]struct{})
	in := make(map[identity.DocumentID]map[identity.DocumentID]struct{})
	for _, id := range g.documents {
		out[id] = make(map[identity.DocumentID]struct{})
		in[id] = make(map[identity.DocumentID]struct{})
	}

	for _, e := range g.edges {
		if e.Kind != EdgeReference {
			continue
		}
		if _, ok := g.navSet[e.Type]; !ok {
			continue
		}
		fromDoc := g.docOf(e.From)
		toDoc := g.docOf(e.To)
		if fromDoc == "" || toDoc == "" || fromDoc == toDoc {
			continue // drop self-loops and unknown endpoints
		}
		out[fromDoc][toDoc] = struct{}{}
		in[toDoc][fromDoc] = struct{}{}
	}

	for _, id := range g.documents {
		g.projAdj[id] = sortedKeys(out[id])
		g.projRev[id] = sortedKeys(in[id])
	}
}

// docOf returns the owning document of a vertex, or "" if unknown.
func (g *ReferenceGraph) docOf(n NodeID) identity.DocumentID {
	if node, ok := g.nodes[n]; ok {
		return node.Document
	}
	return ""
}

// Documents returns the sorted document identities (vertices of kind document).
func (g *ReferenceGraph) Documents() []identity.DocumentID {
	return slices.Clone(g.documents)
}

// HasDocument reports whether id is a document vertex.
func (g *ReferenceGraph) HasDocument(id identity.DocumentID) bool {
	_, ok := g.nodes[NodeIDForDocument(id)]
	return ok
}

// ProjectionOut returns the document-projection out-neighbors of id (sorted).
func (g *ReferenceGraph) ProjectionOut(id identity.DocumentID) []identity.DocumentID {
	return slices.Clone(g.projAdj[id])
}

// ProjectionIn returns the document-projection in-neighbors of id (sorted).
func (g *ReferenceGraph) ProjectionIn(id identity.DocumentID) []identity.DocumentID {
	return slices.Clone(g.projRev[id])
}

// Nodes returns all vertices sorted by NodeID (documents and sections), for
// representation/emitters.
func (g *ReferenceGraph) Nodes() []Node {
	out := make([]Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	slices.SortFunc(out, func(a, b Node) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// Edges returns all edges sorted (From, To, Kind, Type), for representation.
func (g *ReferenceGraph) Edges() []Edge {
	out := slices.Clone(g.edges)
	slices.SortFunc(out, func(a, b Edge) int {
		if c := cmp.Compare(a.From, b.From); c != 0 {
			return c
		}
		if c := cmp.Compare(a.To, b.To); c != 0 {
			return c
		}
		if c := cmp.Compare(int(a.Kind), int(b.Kind)); c != 0 {
			return c
		}
		return cmp.Compare(int(a.Type), int(b.Type))
	})
	return out
}

// containingSection returns the deepest section whose [StartLine, EndLine] line
// span contains the given 1-based source line, or nil. Sections carry their
// heading line span (set by the parser); the deepest enclosing section wins.
// Origin attribution is a hint (ADR 0007): when no section encloses the line
// (e.g. content before the first heading), the caller falls back to the document.
func containingSection(doc *corpus.Document, line int) *corpus.Section {
	var best *corpus.Section
	var walk func(sec *corpus.Section)
	walk = func(sec *corpus.Section) {
		for _, child := range sec.Children {
			if child.StartLine <= line && line <= child.EndLine {
				// Deeper/later sections override shallower ones (pre-order means a
				// later qualifying section is at least as deep / specific).
				best = child
			}
			walk(child)
		}
	}
	walk(doc.Root)
	return best
}

// sortedKeys returns the sorted keys of a DocumentID set.
func sortedKeys(m map[identity.DocumentID]struct{}) []identity.DocumentID {
	if len(m) == 0 {
		return nil
	}
	out := make([]identity.DocumentID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
