package application_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
)

// runPipelineGraphJSON runs the full pipeline at a given worker count over root
// and returns the canonical graph.json bytes (the most comprehensive
// deterministic artifact: nodes, edges, components, HITS, orphans, gaps).
func runPipelineGraphJSON(t *testing.T, root string, workers int) []byte {
	t.Helper()
	cfg := application.DefaultConfig()
	cfg.RootPath = root
	cfg.ParseWorkers = workers
	p := application.NewPipeline(cfg,
		fsscanner.New(fsscanner.Config{}),
		mdparser.NewFactory(mdparser.Config{}),
		nil)
	_, res, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("workers=%d: Run: %v", workers, err)
	}
	b, err := graphjson.JSON(emit.BuildView(res))
	if err != nil {
		t.Fatalf("workers=%d: graphjson: %v", workers, err)
	}
	return b
}

// TestPipeline_Determinism_AcrossWorkerCounts proves the fan-out parse + sorted
// single-threaded merge yields BYTE-IDENTICAL output to the single-threaded path
// at worker counts 1, 2, and 8 (P6 determinism contract). Run under -race in CI.
func TestPipeline_Determinism_AcrossWorkerCounts(t *testing.T) {
	root := genCorpus(t, 300)

	baseline := runPipelineGraphJSON(t, root, 1)
	// The hops-from-root surfaces (ADR 0021) are part of graph.json, so the
	// byte-equality below covers their determinism — but assert they are actually
	// present so the coverage cannot silently regress if the fields disappear.
	for _, needle := range []string{`"hopsFromRoot"`, `"farFromRoot"`} {
		if !bytes.Contains(baseline, []byte(needle)) {
			t.Fatalf("graph.json missing %s (hops-from-root not surfaced)", needle)
		}
	}
	for _, w := range []int{1, 2, 8} {
		got := runPipelineGraphJSON(t, root, w)
		if !bytes.Equal(baseline, got) {
			t.Fatalf("graph.json differs at workers=%d (len base=%d got=%d)",
				w, len(baseline), len(got))
		}
	}
	// And a second run at the same worker count is also stable.
	if !bytes.Equal(baseline, runPipelineGraphJSON(t, root, 8)) {
		t.Fatal("graph.json not stable across repeated 8-worker runs")
	}
}
