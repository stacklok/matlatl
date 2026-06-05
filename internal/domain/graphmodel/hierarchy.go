package graphmodel

import (
	"path"
	"slices"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// HierarchyNode is a node in the document hierarchy: a document plus its
// children (documents whose folder-or-front-matter parent is this document).
type HierarchyNode struct {
	ID       identity.DocumentID
	Children []identity.DocumentID // sorted
}

// HierarchyTree is the folder / front-matter-parent hierarchy over documents,
// for later breadcrumb/index emitters (P4). It does not drive analysis. The
// parent of a document is, in precedence order: its front-matter `parent` (if it
// resolves to a known document), else the README.md/index.md of its directory
// (if one exists and is not itself), else none (a top-level entry).
type HierarchyTree struct {
	nodes map[identity.DocumentID]*HierarchyNode
	roots []identity.DocumentID // sorted top-level documents
}

// BuildHierarchyTree constructs the hierarchy from the corpus. Deterministic:
// children and roots are sorted.
func BuildHierarchyTree(c *corpus.Corpus) *HierarchyTree {
	docs := c.Documents()
	known := make(map[identity.DocumentID]struct{}, len(docs))
	for _, d := range docs {
		known[d.ID] = struct{}{}
	}
	// Directory index lookup: dir -> its README.md/index.md document.
	dirIndex := make(map[string]identity.DocumentID)
	for _, d := range docs {
		// Use the ONE shared directory-index predicate (case-insensitive) so the
		// hierarchy and the root-set resolver never diverge on case (ADR 0007).
		if identity.IsDirectoryIndex(path.Base(d.ID.String())) {
			dir := path.Dir(d.ID.String())
			// Prefer README.md over index.md deterministically if both exist
			// (lexically smaller DocumentID wins; "README.md" < "index.md" since
			// uppercase sorts before lowercase).
			if cur, ok := dirIndex[dir]; !ok || d.ID < cur {
				dirIndex[dir] = d.ID
			}
		}
	}

	t := &HierarchyTree{nodes: make(map[identity.DocumentID]*HierarchyNode, len(docs))}
	for _, d := range docs {
		t.nodes[d.ID] = &HierarchyNode{ID: d.ID}
	}

	childrenOf := make(map[identity.DocumentID][]identity.DocumentID)
	for _, d := range docs {
		parent, ok := resolveParent(d, known, dirIndex)
		if ok && parent != d.ID {
			childrenOf[parent] = append(childrenOf[parent], d.ID)
		} else {
			t.roots = append(t.roots, d.ID)
		}
	}
	for id, kids := range childrenOf {
		slices.Sort(kids)
		t.nodes[id].Children = kids
	}
	slices.Sort(t.roots)
	return t
}

// resolveParent returns the parent document of d per the documented precedence.
func resolveParent(
	d *corpus.Document,
	known map[identity.DocumentID]struct{},
	dirIndex map[string]identity.DocumentID,
) (identity.DocumentID, bool) {
	// 1. front-matter parent (interpreted relative to d's directory).
	if p := d.FrontMatter.Parent; p != "" {
		joined := identity.DocumentID(path.Clean(path.Join(path.Dir(d.ID.String()), p)))
		if _, ok := known[joined]; ok {
			return joined, true
		}
		if _, ok := known[identity.DocumentID(p)]; ok {
			return identity.DocumentID(p), true
		}
	}
	// 2. directory index document (README.md/index.md of d's folder), if it is
	// not d itself.
	dir := path.Dir(d.ID.String())
	if idx, ok := dirIndex[dir]; ok && idx != d.ID {
		return idx, true
	}
	return "", false
}

// Roots returns the sorted top-level documents.
func (t *HierarchyTree) Roots() []identity.DocumentID { return slices.Clone(t.roots) }

// Children returns the sorted children of a document.
func (t *HierarchyTree) Children(id identity.DocumentID) []identity.DocumentID {
	if n, ok := t.nodes[id]; ok {
		return slices.Clone(n.Children)
	}
	return nil
}
