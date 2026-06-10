// emitExclude (ADR 0019): the corpus-membership vs rendering split. A document
// matched by `.matlatl.yml emitExclude` stays IN the corpus — scanned, parsed,
// link-checked, ranked, present in graph.json/findings.json and every
// diagnostic surface — but is dropped from the consumption (navigation)
// surfaces: the llms.txt family, index.md, and trails.json. The filter lives
// here at the emit boundary so the domain and the pipeline never learn it
// exists (ADR 0004), and `check` is byte-identical with or without it.

package emit

import (
	"slices"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"

	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// WithEmitExclude returns a copy of the View carrying a compiled emitExclude
// matcher. Patterns use gitignore syntax — the SAME engine and semantics as
// `.matlatlignore` (go-gitignore), so `.claude/agents/` excludes that subtree at
// any depth and `!` negation re-includes. An empty pattern list is a no-op (the
// returned View filters nothing). The matcher is only ever string-matched
// against in-corpus DocumentIDs — never a filesystem read.
//
// Only the consumption emitters (llmstxt, index, trails) consult the matcher,
// through EmitExcluded / RenderedBacklinks / RenderedTrails; the diagnostic and
// machine emitters (terminal/markdown report, graph.json, findings.json,
// junit.xml, diagrams) ignore it by construction (ADR 0019).
func (v View) WithEmitExclude(patterns []string) View {
	if len(patterns) == 0 {
		return v
	}
	v.emitExclude = compileExclude(patterns)
	return v
}

// compileExclude compiles emitExclude patterns into the shared gitignore-dialect
// matcher (go-gitignore — the SAME engine as `.matlatlignore`, ADR 0019). It is
// the single home for that dialect choice: both the View's consumption-surface
// filter and the fix-prompt advisory filter (ADR 0020) compile through it, so
// the two surfaces cannot silently diverge in semantics.
func compileExclude(patterns []string) *ignore.GitIgnore {
	return ignore.CompileIgnoreLines(patterns...)
}

// EmitExcluded reports whether id is excluded from the consumption surfaces.
// Always false when no emitExclude patterns were applied.
func (v View) EmitExcluded(id identity.DocumentID) bool {
	return v.emitExclude != nil && v.emitExclude.MatchesPath(id.String())
}

// EmitExcludedCount is the number of corpus documents the emitExclude patterns
// match — the honesty figure the filtered surfaces report so the artifact says
// what it dropped. 0 when no patterns were applied.
func (v View) EmitExcludedCount() int {
	if v.emitExclude == nil {
		return 0
	}
	n := 0
	for _, d := range v.Docs {
		if v.EmitExcluded(d.ID) {
			n++
		}
	}
	return n
}

// EmitExcludedRoots returns the reachability roots that emitExclude matches,
// sorted (the RootSet is sorted upstream). Excluding a root is ALLOWED — it
// simply does not render; reachability is computed over the unfiltered corpus —
// but the CLI surfaces a notice so it is not silent (ADR 0019). Empty when no
// patterns were applied, metrics are absent, or the root set is indeterminate.
func (v View) EmitExcludedRoots() []identity.DocumentID {
	if v.emitExclude == nil || v.Metrics == nil || v.Metrics.RootSet.Indeterminate {
		return nil
	}
	var out []identity.DocumentID
	for _, id := range v.Metrics.RootSet.Roots { // sorted upstream
		if v.EmitExcluded(id) {
			out = append(out, id)
		}
	}
	return out
}

// RenderedBacklinks is Backlinks with emit-excluded sources dropped: the
// backlink clauses the consumption surfaces render must not name documents
// those surfaces refuse to list (ADR 0019). Identical to Backlinks when no
// patterns were applied. Order-stable: Backlinks is sorted and filtering
// preserves order.
func (v View) RenderedBacklinks(id identity.DocumentID) []identity.DocumentID {
	in := v.Backlinks(id)
	if v.emitExclude == nil {
		return in
	}
	out := make([]identity.DocumentID, 0, len(in))
	for _, src := range in {
		if !v.EmitExcluded(src) {
			out = append(out, src)
		}
	}
	return out
}

// RenderedTrails is Trails with emit-excluded documents dropped from each
// trail's reading order (trails exist for onboarding readers, so they are a
// consumption surface, ADR 0019). A trail whose order becomes empty is dropped.
// A trail whose Root is excluded is re-rooted at its most important remaining
// member — highest PageRank, tie-broken by ID — mirroring the domain's root
// definition. Identical to Trails when no patterns were applied. Deterministic:
// each order's sequence is preserved, and the list is re-sorted by Root so the
// upstream sorted-by-Root invariant survives re-rooting.
func (v View) RenderedTrails() []graphmodel.Trail {
	if v.emitExclude == nil {
		return v.Trails
	}
	out := make([]graphmodel.Trail, 0, len(v.Trails))
	for _, t := range v.Trails {
		order := make([]identity.DocumentID, 0, len(t.Order))
		for _, id := range t.Order {
			if !v.EmitExcluded(id) {
				order = append(order, id)
			}
		}
		if len(order) == 0 {
			continue
		}
		root := t.Root
		if v.EmitExcluded(root) {
			root = v.bestRemaining(order)
		}
		out = append(out, graphmodel.Trail{Root: root, Order: order})
	}
	slices.SortFunc(out, func(a, b graphmodel.Trail) int {
		return strings.Compare(a.Root.String(), b.Root.String())
	})
	return out
}

// bestRemaining picks the highest-PageRank member of order, tie-broken by the
// lexicographically smaller ID (the domain's trail-root tie-break). order is
// non-empty.
func (v View) bestRemaining(order []identity.DocumentID) identity.DocumentID {
	best := order[0]
	bestScore := v.pageRankOf(best)
	for _, id := range order[1:] {
		s := v.pageRankOf(id)
		if s > bestScore || (s == bestScore && id < best) {
			best, bestScore = id, s
		}
	}
	return best
}

func (v View) pageRankOf(id identity.DocumentID) float64 {
	if d, ok := v.Doc(id); ok {
		return d.PageRank
	}
	return 0
}
