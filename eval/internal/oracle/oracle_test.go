package oracle

import "testing"

func TestCanonicalCheckRejectsInvalidGraph(t *testing.T) {
	for _, graph := range [][]byte{
		[]byte("not json"),
		[]byte(`{"schemaVersion":6}`),
	} {
		if err := (Canonical{}).Check(graph); err == nil {
			t.Fatalf("Canonical.Check accepted %q", graph)
		}
	}
}
