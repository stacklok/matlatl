package application_test

import (
	"context"
	"runtime"
	"testing"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/infrastructure/fsscanner"
	"github.com/stacklok/doctopus/internal/infrastructure/mdparser"
)

// runFullPipeline runs scan→analyze over root at the given worker count and
// returns the document count (to defeat dead-code elimination).
func runFullPipeline(tb testing.TB, root string, workers int) int {
	tb.Helper()
	cfg := application.DefaultConfig()
	cfg.RootPath = root
	cfg.ParseWorkers = workers
	p := application.NewPipeline(cfg,
		fsscanner.New(fsscanner.Config{}),
		mdparser.NewFactory(mdparser.Config{}),
		nil)
	_, res, err := p.Run(context.Background())
	if err != nil {
		tb.Fatalf("workers=%d: Run: %v", workers, err)
	}
	return res.DocumentCount
}

// BenchmarkPipeline_5kDocs measures scan→analyze wall-time and allocations over
// ~5,000 synthetic cross-linked docs, comparing single-threaded (1 worker) vs
// the autodetected fan-out pool (0 = GOMAXPROCS). -benchmem reports B/op and
// allocs/op so the in-memory-everything risk is visible. The corpus is generated
// ONCE per sub-benchmark (outside the timed loop via b.ResetTimer).
func BenchmarkPipeline_5kDocs(b *testing.B) {
	const n = 5000
	for _, w := range []struct {
		name    string
		workers int
	}{
		{"workers1", 1},
		{"workersAuto", 0},
	} {
		b.Run(w.name, func(b *testing.B) {
			root := genCorpus(b, n)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if got := runFullPipeline(b, root, w.workers); got != n+1 {
					b.Fatalf("doc count = %d, want %d", got, n+1)
				}
			}
		})
	}
}

// TestPipeline_5kDocs_MemoryCeiling guards the in-memory-everything risk: a full
// 5k-doc scan→analyze run must complete and stay under a generous heap ceiling.
// It is a correctness gate (not a precise benchmark): peak HeapAlloc growth over
// the run is asserted below a ceiling that a linear O(V+E) implementation
// comfortably meets, so a future super-linear regression (e.g. quadratic edge
// blow-up) trips it. Skipped in -short.
func TestPipeline_5kDocs_MemoryCeiling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 5k-doc memory test in -short")
	}
	const n = 5000
	root := genCorpus(t, n)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	if got := runFullPipeline(t, root, 0); got != n+1 {
		t.Fatalf("doc count = %d, want %d", got, n+1)
	}

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// TotalAlloc is cumulative bytes allocated; the delta is total allocation
	// over the run. For 5k small docs with ~5 links each a linear pipeline
	// allocates well under 1 GiB total; a super-linear regression would blow far
	// past it. This is a coarse ceiling, deliberately generous.
	const ceiling = 1 << 30 // 1 GiB
	delta := after.TotalAlloc - before.TotalAlloc
	if delta > ceiling {
		t.Fatalf("5k-doc run allocated %d bytes total, exceeds ceiling %d (possible super-linear regression)",
			delta, ceiling)
	}
	t.Logf("5k-doc run: total alloc %.1f MiB, peak heap %.1f MiB",
		float64(delta)/(1<<20), float64(after.HeapAlloc)/(1<<20))
}
