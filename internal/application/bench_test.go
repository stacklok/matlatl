package application_test

import (
	"context"
	"runtime"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
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
				if got := runFullPipeline(b, root, w.workers); got != n+1+genCorpusChainLen {
					b.Fatalf("doc count = %d, want %d", got, n+1+genCorpusChainLen)
				}
			}
		})
	}
}

// TestPipeline_500Docs_MemoryCeiling keeps an ordinary race-enabled smoke guard
// on the full scan→analyze path. The production analysis includes exact streaming
// APSP passes with O(V·(V+E)) runtime, so the separately tagged 5k guard is kept
// out of the ordinary race suite; see TestPipeline_5kDocs_MemoryCeiling.
func TestPipeline_500Docs_MemoryCeiling(t *testing.T) {
	const n = 500
	const ceiling = 128 << 20 // 128 MiB
	runPipelineMemoryCeiling(t, n, ceiling)
}

func runPipelineMemoryCeiling(t *testing.T, n int, ceiling uint64) {
	t.Helper()
	wantDocs := n + 1 + genCorpusChainLen
	root := genCorpus(t, n)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	if got := runFullPipeline(t, root, 0); got != wantDocs {
		t.Fatalf("doc count = %d, want %d", got, wantDocs)
	}

	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	delta := after.TotalAlloc - before.TotalAlloc
	if delta > ceiling {
		t.Fatalf("%d-doc run allocated %d bytes total, exceeds ceiling %d (possible super-linear regression)",
			n, delta, ceiling)
	}
	t.Logf("%d-doc run: total alloc %.1f MiB, retained heap %.1f MiB",
		n, float64(delta)/(1<<20), float64(after.HeapAlloc)/(1<<20))
}
