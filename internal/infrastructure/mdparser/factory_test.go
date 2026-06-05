package mdparser

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/stacklok/doctopus/internal/domain/identity"
)

// TestFactory_CloneConcurrent locks the P6 per-worker contract: Factory.Clone
// returns parsers that can run concurrently on separate goroutines. Clones share
// the Factory's single warmed goldmark.Markdown (per-call state is isolated to
// the parser.Context each ParseBytes allocates); run under -race, this proves
// concurrent Parse calls on that shared instance are data-race-free.
func TestFactory_CloneConcurrent(t *testing.T) {
	fac := NewFactory(Config{})

	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(n int) {
			defer wg.Done()
			parser, ok := fac.Clone().(*Parser)
			if !ok {
				t.Errorf("worker %d: Clone did not return *Parser", n)
				return
			}
			src := []byte("---\ntitle: T\n---\n\n# Heading\n\n[[wiki]] and [link](x.md#frag)\n")
			id := identity.DocumentID("w" + strconv.Itoa(n) + ".md")
			doc, err := parser.ParseBytes(context.Background(), id, src)
			if err != nil {
				t.Errorf("worker %d: ParseBytes error: %v", n, err)
				return
			}
			if doc.ID != id {
				t.Errorf("worker %d: doc.ID = %q, want %q", n, doc.ID, id)
			}
			if len(doc.RawReferences) != 2 {
				t.Errorf("worker %d: got %d refs, want 2", n, len(doc.RawReferences))
			}
		}(i)
	}
	wg.Wait()
}
