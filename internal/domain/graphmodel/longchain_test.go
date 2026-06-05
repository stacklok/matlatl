package graphmodel

import (
	"fmt"
	"testing"

	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/domain/reference"
)

// chainID is the stable DocumentID for the i-th node of a synthetic chain,
// zero-padded so lexical order matches numeric order (determinism).
func chainID(i int) string { return fmt.Sprintf("d%06d.md", i) }

// buildChainGraph builds an n-node corpus d000000.md .. d{n-1}.md with a
// REFERENCE edge from each node to the next (d_i -> d_{i+1}); if loop is true it
// adds a back-edge d_{n-1} -> d_0 closing the chain into one big cycle.
func buildChainGraph(t *testing.T, n int, loop bool) (*ReferenceGraph, *corpus.Corpus) {
	t.Helper()
	c := corpus.NewCorpus()
	for i := 0; i < n; i++ {
		if err := c.Add(doc(chainID(i), "", nil)); err != nil {
			t.Fatalf("add %s: %v", chainID(i), err)
		}
	}
	var refs []reference.Reference
	for i := 0; i < n-1; i++ {
		refs = append(refs, validRef(chainID(i), chainID(i+1)))
	}
	if loop && n > 1 {
		refs = append(refs, validRef(chainID(n-1), chainID(0)))
	}
	return BuildReferenceGraph(c, refs, BuildOptions{}), c
}

// TestSCC_LongChain_NoStackOverflow proves the iterative Tarjan handles a
// 20,000-deep link chain that recursion would blow the goroutine stack on.
// Two shapes:
//   - acyclic chain  -> 20,000 singleton SCCs
//   - chain + back-edge (one giant cycle) -> exactly 1 SCC of all 20,000 nodes
func TestSCC_LongChain_NoStackOverflow(t *testing.T) {
	const n = 20000

	t.Run("acyclic", func(t *testing.T) {
		g, _ := buildChainGraph(t, n, false)
		sccs := g.StronglyConnectedComponents()
		if len(sccs) != n {
			t.Fatalf("acyclic chain: got %d SCCs, want %d singletons", len(sccs), n)
		}
		for _, s := range sccs {
			if len(s.Members) != 1 {
				t.Fatalf("acyclic chain: SCC %q has %d members, want 1", s.ID, len(s.Members))
			}
		}
	})

	t.Run("one_big_cycle", func(t *testing.T) {
		g, _ := buildChainGraph(t, n, true)
		sccs := g.StronglyConnectedComponents()
		if len(sccs) != 1 {
			t.Fatalf("looped chain: got %d SCCs, want 1", len(sccs))
		}
		if got := len(sccs[0].Members); got != n {
			t.Fatalf("looped chain: SCC has %d members, want %d", got, n)
		}
		// Deterministic ID: sorted-min member is d000000.md.
		if sccs[0].ID != identity.DocumentID(chainID(0)) {
			t.Fatalf("looped chain: SCC ID = %q, want %q", sccs[0].ID, chainID(0))
		}
	})
}
