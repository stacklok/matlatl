package graphmodel

import "testing"

func TestNodeKind_StringValid(t *testing.T) {
	all := []NodeKind{NodeKindDocument, NodeKindSection}
	seen := make(map[string]bool)
	for _, k := range all {
		if !k.Valid() {
			t.Errorf("NodeKind %d reported invalid", int(k))
		}
		s := k.String()
		if s == "" || s == "unknown" {
			t.Errorf("NodeKind %d has bad String() %q", int(k), s)
		}
		if seen[s] {
			t.Errorf("duplicate String() %q", s)
		}
		seen[s] = true
	}
	if (NodeKind(-1)).Valid() || (NodeKind(99)).Valid() {
		t.Error("out-of-range NodeKind reported valid")
	}
	if got := NodeKind(99).String(); got != "unknown" {
		t.Errorf("NodeKind(99).String() = %q, want unknown", got)
	}
}

func TestNodeID_String(t *testing.T) {
	if got := NodeID("docs/a.md").String(); got != "docs/a.md" {
		t.Errorf("NodeID.String() = %q, want docs/a.md", got)
	}
}

func TestConstructors_NonNil(t *testing.T) {
	if NewReferenceGraph() == nil {
		t.Error("NewReferenceGraph() = nil")
	}
	if NewHierarchyTree() == nil {
		t.Error("NewHierarchyTree() = nil")
	}
}
