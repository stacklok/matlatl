package mcpserver

import (
	"slices"
	"strings"

	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/identity"
)

// shortestPath returns a navigational path from src to dst over the document
// projection (BFS, so the fewest hops), and whether one exists. The returned
// path includes both endpoints; a src==dst request returns a single-element
// path. Neighbor iteration uses the projection's sorted out-edges so the chosen
// path is deterministic.
func (a *Analysis) shortestPath(src, dst identity.DocumentID) ([]identity.DocumentID, bool) {
	if a.metrics == nil || a.metrics.Graph == nil {
		return nil, false
	}
	if src == dst {
		return []identity.DocumentID{src}, true
	}
	g := a.metrics.Graph
	prev := map[identity.DocumentID]identity.DocumentID{}
	visited := map[identity.DocumentID]struct{}{src: {}}
	queue := []identity.DocumentID{src}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nb := range g.ProjectionOut(cur) { // sorted
			if _, seen := visited[nb]; seen {
				continue
			}
			visited[nb] = struct{}{}
			prev[nb] = cur
			if nb == dst {
				return reconstruct(prev, src, dst), true
			}
			queue = append(queue, nb)
		}
	}
	return nil, false
}

// reconstruct walks the predecessor map from dst back to src and returns the
// path in forward order.
func reconstruct(prev map[identity.DocumentID]identity.DocumentID, src, dst identity.DocumentID) []identity.DocumentID {
	var rev []identity.DocumentID
	for cur := dst; ; cur = prev[cur] {
		rev = append(rev, cur)
		if cur == src {
			break
		}
	}
	slices.Reverse(rev)
	return rev
}

// splitAnchor splits a "doc#slug" reference into its document and slug parts.
// It requires a non-empty slug; a reference without '#' (or with an empty slug)
// is rejected.
func splitAnchor(ref string) (doc, slug string, ok bool) {
	i := strings.IndexByte(ref, '#')
	if i < 0 {
		return "", "", false
	}
	doc, slug = ref[:i], ref[i+1:]
	if doc == "" || slug == "" {
		return "", "", false
	}
	return doc, slug, true
}

// findSection returns the section with the given slug in a document's section
// tree, or nil. The synthetic level-0 root is skipped (it has no slug).
func findSection(root *corpus.Section, slug string) *corpus.Section {
	var found *corpus.Section
	var walk func(s *corpus.Section)
	walk = func(s *corpus.Section) {
		if found != nil {
			return
		}
		for _, child := range s.Children {
			if child.Slug == slug {
				found = child
				return
			}
			walk(child)
		}
	}
	walk(root)
	return found
}

// idStrings turns DocumentIDs into a non-nil sorted string slice.
func idStrings(ids []identity.DocumentID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	slices.Sort(out)
	return out
}
