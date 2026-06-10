package emit

import (
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// excludeFixtureView builds a View over a small corpus with agent-scaffolding
// paths: README.md → docs/guide.md, .claude/agents/helper.md → docs/guide.md
// (a backlink from an excluded doc), and a standalone .agents/deep/bot.md.
func excludeFixtureView(t *testing.T) View {
	t.Helper()
	c := corpus.NewCorpus()
	for _, id := range []string{
		"README.md", "docs/guide.md",
		".claude/agents/helper.md", ".agents/deep/bot.md",
	} {
		doc := &corpus.Document{
			ID:   identity.DocumentID(id),
			Root: &corpus.Section{Level: 0},
		}
		if err := c.Add(doc); err != nil {
			t.Fatal(err)
		}
	}
	ref := func(origin, target string) reference.Reference {
		return reference.Reference{
			RawReference: reference.RawReference{
				Origin: identity.DocumentID(origin), RawTarget: target,
				Type: reference.RelativeLink, Line: 1,
			},
			Target: reference.ResolvedTarget{Kind: reference.TargetDocument, DocumentID: identity.DocumentID(target)},
			Health: reference.Valid,
		}
	}
	refs := []reference.Reference{
		ref("README.md", "docs/guide.md"),
		ref(".claude/agents/helper.md", "docs/guide.md"),
	}
	g := graphmodel.BuildReferenceGraph(c, refs, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	return BuildView(application.Result{DocumentCount: c.Len(), Metrics: m, Corpus: c})
}

// Gitignore semantics (the SAME engine as .matlatlignore): a trailing-slash dir
// pattern excludes the subtree at any depth; non-matching siblings stay.
func TestEmitExcluded_GitignoreSemantics(t *testing.T) {
	v := excludeFixtureView(t).WithEmitExclude([]string{".claude/agents/", ".agents/"})

	for id, want := range map[string]bool{
		".claude/agents/helper.md": true,
		".agents/deep/bot.md":      true, // subtree depth crossed, unlike path.Match
		"docs/guide.md":            false,
		"README.md":                false,
	} {
		if got := v.EmitExcluded(identity.DocumentID(id)); got != want {
			t.Errorf("EmitExcluded(%q) = %v, want %v", id, got, want)
		}
	}
	if n := v.EmitExcludedCount(); n != 2 {
		t.Errorf("EmitExcludedCount = %d, want 2", n)
	}
}

// An empty pattern list is a no-op: nothing is excluded and the View behaves
// exactly as if WithEmitExclude was never called.
func TestWithEmitExclude_EmptyIsNoOp(t *testing.T) {
	v := excludeFixtureView(t).WithEmitExclude(nil)
	if v.emitExclude != nil {
		t.Fatal("empty patterns should leave the matcher nil")
	}
	if v.EmitExcluded("README.md") || v.EmitExcludedCount() != 0 {
		t.Error("no-op View must exclude nothing")
	}
}

// RenderedBacklinks drops excluded sources but keeps the rest, order preserved;
// Backlinks (the unfiltered accessor used by the diagnostic/machine surfaces)
// still reports the complete in-neighbour set.
func TestRenderedBacklinks_FiltersExcludedSources(t *testing.T) {
	v := excludeFixtureView(t).WithEmitExclude([]string{".claude/agents/"})

	full := v.Backlinks("docs/guide.md")
	if len(full) != 2 {
		t.Fatalf("Backlinks must stay unfiltered, got %v", full)
	}
	rendered := v.RenderedBacklinks("docs/guide.md")
	want := []identity.DocumentID{"README.md"}
	if !slices.Equal(rendered, want) {
		t.Errorf("RenderedBacklinks = %v, want %v", rendered, want)
	}
}

// RenderedTrails drops excluded docs from each order, drops a trail left empty,
// and re-roots a trail whose Root is excluded at the highest-PageRank remaining
// member.
func TestRenderedTrails_FiltersDropsAndReroots(t *testing.T) {
	v := excludeFixtureView(t)
	v.Trails = []graphmodel.Trail{
		{Root: ".agents/deep/bot.md", Order: []identity.DocumentID{".agents/deep/bot.md"}},
		{Root: "docs/guide.md", Order: []identity.DocumentID{
			"README.md", ".claude/agents/helper.md", "docs/guide.md",
		}},
	}
	v = v.WithEmitExclude([]string{".claude/agents/", ".agents/"})

	got := v.RenderedTrails()
	if len(got) != 1 {
		t.Fatalf("the all-excluded trail should be dropped, got %v", got)
	}
	wantOrder := []identity.DocumentID{"README.md", "docs/guide.md"}
	if !slices.Equal(got[0].Order, wantOrder) {
		t.Errorf("filtered order = %v, want %v", got[0].Order, wantOrder)
	}
	if got[0].Root != "docs/guide.md" {
		t.Errorf("non-excluded root must be kept, got %q", got[0].Root)
	}

	// Re-root: exclude the trail's root itself; the survivor with the highest
	// PageRank becomes the new root (docs/guide.md has the inlinks here).
	v2 := excludeFixtureView(t)
	v2.Trails = []graphmodel.Trail{
		{Root: ".claude/agents/helper.md", Order: []identity.DocumentID{
			".claude/agents/helper.md", "README.md", "docs/guide.md",
		}},
	}
	v2 = v2.WithEmitExclude([]string{".claude/agents/"})
	got2 := v2.RenderedTrails()
	if len(got2) != 1 {
		t.Fatalf("trail with survivors must be kept, got %v", got2)
	}
	if got2[0].Root != "docs/guide.md" {
		t.Errorf("re-rooted Root = %q, want docs/guide.md (highest PageRank survivor)", got2[0].Root)
	}
}

// RenderedTrails with no matcher returns the Trails slice untouched.
func TestRenderedTrails_NoMatcherPassthrough(t *testing.T) {
	v := excludeFixtureView(t)
	v.Trails = []graphmodel.Trail{{Root: "README.md", Order: []identity.DocumentID{"README.md"}}}
	if got := v.RenderedTrails(); len(got) != 1 || got[0].Root != "README.md" {
		t.Errorf("passthrough trails = %v", got)
	}
}

// EmitExcludedRoots surfaces reachability roots matched by the patterns, so the
// CLI can warn (excluding a root is allowed but must not be silent, ADR 0019).
func TestEmitExcludedRoots(t *testing.T) {
	v := excludeFixtureView(t)
	if v.Metrics == nil || v.Metrics.RootSet.Indeterminate {
		t.Fatal("fixture must have a determinate root set (README.md)")
	}
	if got := v.WithEmitExclude([]string{".claude/agents/"}).EmitExcludedRoots(); len(got) != 0 {
		t.Errorf("no root matches the pattern, got %v", got)
	}
	got := v.WithEmitExclude([]string{"README.md"}).EmitExcludedRoots()
	want := []identity.DocumentID{"README.md"}
	if !slices.Equal(got, want) {
		t.Errorf("EmitExcludedRoots = %v, want %v", got, want)
	}
}

// gitignore negation (`!`) re-includes, matching .matlatlignore semantics.
func TestEmitExcluded_NegationReincludes(t *testing.T) {
	v := excludeFixtureView(t).WithEmitExclude([]string{".claude/agents/", "!.claude/agents/helper.md"})
	if v.EmitExcluded(".claude/agents/helper.md") {
		t.Error("negated pattern must re-include the doc")
	}
}
