//go:build performance

package application_test

import "testing"

// TestPipeline_5kDocs_MemoryCeiling is the full-scale in-memory regression
// guard. It is intentionally excluded from the ordinary race-enabled suite:
// exact APSP analysis is O(V·(V+E)), and race instrumentation makes this test
// take several minutes. Run it explicitly with task test-performance.
func TestPipeline_5kDocs_MemoryCeiling(t *testing.T) {
	const n = 5000
	const ceiling = 1 << 30 // 1 GiB
	runPipelineMemoryCeiling(t, n, ceiling)
}
