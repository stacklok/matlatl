// Package oracle checks the canonical graph against independent, hand-authored
// expectations. Expectations are never generated from matlatl output.
package oracle

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
)

const expectedGraphSchemaVersion = 7

// Edge is one expected directed graph edge.
type Edge struct{ From, To string }

var (
	expectedDocs  = []string{"README.md", "docs/install.md", "docs/operate.md"}
	expectedEdges = []Edge{
		{From: "README.md", To: "docs/install.md"},
		{From: "docs/install.md", To: "docs/operate.md"},
		{From: "docs/operate.md", To: "README.md"},
	}
	expectedRoots = []string{"README.md"}
	expectedHops  = map[string]int{"README.md": 0, "docs/install.md": 1, "docs/operate.md": 2}
)

// Canonical is the hand-authored canonical-navigation v1 oracle.
type Canonical struct{}

// Check compares emitted graph.json with the hand-authored v1 oracle.
func (Canonical) Check(graphJSON []byte) error {
	var graph graphjson.Document
	if err := json.Unmarshal(graphJSON, &graph); err != nil {
		return fmt.Errorf("oracle: decode graph: %w", err)
	}
	if graph.SchemaVersion != expectedGraphSchemaVersion {
		return fmt.Errorf("oracle: schema version %d, want %d", graph.SchemaVersion, expectedGraphSchemaVersion)
	}
	if graph.Summary.Documents != 3 || graph.Summary.Edges != 3 {
		return fmt.Errorf("oracle: counts docs=%d edges=%d, want 3 and 3", graph.Summary.Documents, graph.Summary.Edges)
	}
	docs := make([]string, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		docs = append(docs, node.ID)
		if want, exists := expectedHops[node.ID]; !exists || node.HopsFromRoot != want {
			return fmt.Errorf("oracle: hops for %q = %d, want %d", node.ID, node.HopsFromRoot, want)
		}
	}
	slices.Sort(docs)
	if !slices.Equal(docs, expectedDocs) || !slices.Equal(graph.RootSet, expectedRoots) {
		return fmt.Errorf("oracle: docs or roots differ: docs=%v roots=%v", docs, graph.RootSet)
	}
	edges := make([]Edge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		edges = append(edges, Edge{From: edge.From, To: edge.To})
	}
	slices.SortFunc(edges, func(a, b Edge) int {
		if n := strings.Compare(a.From, b.From); n != 0 {
			return n
		}
		return strings.Compare(a.To, b.To)
	})
	if !slices.Equal(edges, expectedEdges) {
		return fmt.Errorf("oracle: edges %v, want %v", edges, expectedEdges)
	}
	return nil
}

// Summary returns the deterministic oracle description.
func Summary() string {
	var out strings.Builder
	fmt.Fprintf(&out, "canonical-navigation/v1 oracle: schema=%d documents=3 edges=3\n", expectedGraphSchemaVersion)
	for _, doc := range expectedDocs {
		fmt.Fprintf(&out, "  %s hops=%d\n", doc, expectedHops[doc])
	}
	for _, edge := range expectedEdges {
		fmt.Fprintf(&out, "  %s -> %s\n", edge.From, edge.To)
	}
	return out.String()
}
