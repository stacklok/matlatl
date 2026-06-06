package graphmodel

import (
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

func validSectionRef(origin, targetDoc, anchor string) reference.Reference {
	return reference.Reference{
		RawReference: reference.RawReference{
			Origin: identity.DocumentID(origin), RawTarget: targetDoc, Fragment: anchor,
			Type: reference.RelativeLink, Line: 1,
		},
		Target: reference.ResolvedTarget{Kind: reference.TargetSection, DocumentID: identity.DocumentID(targetDoc), Anchor: anchor},
		Health: reference.Valid,
	}
}

// TestSectionEdge_ReachesDocument: a link to other.md#heading targets the
// section vertex, but the document projection collapses it so other.md is
// REACHED (not unreachable). The doc's only inbound edge is to one of its
// sections.
func TestSectionEdge_ReachesDocument(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "intro", nil),
		doc("target.md", "deep-heading", nil),
	)
	refs := []reference.Reference{
		validSectionRef("README.md", "target.md", "deep-heading"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})

	// Section vertex must exist and the projection must show README -> target.md.
	if _, ok := nodeByID(g, NodeIDForSection("target.md", "deep-heading")); !ok {
		t.Fatal("expected a section vertex target.md#deep-heading")
	}
	if got := ids(g.ProjectionOut("README.md")); !slices.Equal(got, []string{"target.md"}) {
		t.Errorf("projection out = %v, want [target.md] (section edge collapsed to doc)", got)
	}

	m := Analyze(g, c, AnalyzeOptions{})
	if slices.Contains(ids(m.Orphans.Unreachable), "target.md") {
		t.Error("target.md is reached via a section edge; must not be unreachable")
	}
	if slices.Contains(ids(m.Orphans.Isolated), "target.md") {
		t.Error("target.md has an inbound section edge; must not be isolated")
	}
}

func nodeByID(g *ReferenceGraph, id NodeID) (Node, bool) {
	for _, n := range g.Nodes() {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

// TestIntentionalOrphan_Suppressed: a doc marked matlatl: orphan-intentional is
// excluded from both Isolated and Unreachable, but still a vertex.
func TestIntentionalOrphan_Suppressed(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "intro", nil),
		doc("CHANGELOG.md", "changelog", map[string]any{"matlatl": "orphan-intentional"}),
		doc("real-orphan.md", "ro", nil),
	)
	g := BuildReferenceGraph(c, nil, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})

	if slices.Contains(ids(m.Orphans.Isolated), "CHANGELOG.md") {
		t.Error("intentional orphan CHANGELOG.md must be suppressed from Isolated")
	}
	if !slices.Contains(ids(m.Orphans.Isolated), "real-orphan.md") {
		t.Error("real-orphan.md should still be reported as isolated")
	}
	if !g.HasDocument("CHANGELOG.md") {
		t.Error("intentional orphan must still be a vertex")
	}
}

// TestEmptyRootSet_Indeterminate: no README/index/type:index/glob -> reachability
// indeterminate; nothing marked unreachable, but isolated orphans still detected.
func TestEmptyRootSet_Indeterminate(t *testing.T) {
	c := buildCorpus(t,
		doc("a.md", "a", nil),
		doc("b.md", "b", nil),
		doc("lonely.md", "l", nil),
	)
	refs := []reference.Reference{validRef("a.md", "b.md")}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})

	if !m.RootSet.Indeterminate {
		t.Fatalf("expected indeterminate root set, got roots %v", ids(m.RootSet.Roots))
	}
	if !m.Reachability.Indeterminate {
		t.Error("reachability should be indeterminate")
	}
	if len(m.Orphans.Unreachable) != 0 {
		t.Errorf("indeterminate reachability must not mark anything unreachable; got %v", ids(m.Orphans.Unreachable))
	}
	// Isolated orphan detection is independent of the root set.
	if got := ids(m.Orphans.Isolated); !slices.Equal(got, []string{"lonely.md"}) {
		t.Errorf("isolated = %v, want [lonely.md] even when reachability is indeterminate", got)
	}
}

// TestRootSet_Conventions: index.md at depth and type:index front matter both
// count as roots.
func TestRootSet_Conventions(t *testing.T) {
	c := buildCorpus(t,
		doc("docs/index.md", "i", nil),
		doc("guides/start.md", "s", map[string]any{"type": "index"}),
		doc("misc/notes.md", "n", nil),
	)
	g := BuildReferenceGraph(c, nil, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})
	want := []string{"docs/index.md", "guides/start.md"}
	if got := ids(m.RootSet.Roots); !slices.Equal(got, want) {
		t.Errorf("root set = %v, want %v", got, want)
	}
}

// TestRootSet_SkillManifestConvention: SKILL.md is auto-detected as a root by
// filename (case-insensitively, at any depth, peer to README.md/index.md), while
// a non-convention filename in the same place is not. The directory path is
// irrelevant — only the base name matters — so no `.claude/...` path is baked in.
func TestRootSet_SkillManifestConvention(t *testing.T) {
	c := buildCorpus(t,
		doc("SKILL.md", "s", nil),                    // exact
		doc("skills/foo/Skill.MD", "f", nil),         // case variant at depth
		doc(".claude/skills/bar/skill.md", "b", nil), // path is irrelevant; filename matches
		doc("docs/manifest.md", "m", nil),            // NOT a convention filename
	)
	g := BuildReferenceGraph(c, nil, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})
	want := []string{".claude/skills/bar/skill.md", "SKILL.md", "skills/foo/Skill.MD"}
	if got := ids(m.RootSet.Roots); !slices.Equal(got, want) {
		t.Errorf("root set = %v, want %v (SKILL.md by filename, manifest.md excluded)", got, want)
	}
}

// TestSkillManifest_SeedsReachability proves SKILL.md acts as a real BFS root,
// not merely an exempt node: a SKILL.md with an OUTBOUND link to references/x.md
// (and nothing linking to either) makes references/x.md REACHED. If SKILL.md were
// only exempt-but-not-a-root, references/x.md would be unreachable.
func TestSkillManifest_SeedsReachability(t *testing.T) {
	c := buildCorpus(t,
		doc("SKILL.md", "skill", nil),
		doc("references/x.md", "x", nil),
	)
	refs := []reference.Reference{validRef("SKILL.md", "references/x.md")}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})

	if m.Reachability.Indeterminate {
		t.Fatal("SKILL.md should seed a determinate root set")
	}
	if !slices.Contains(ids(m.RootSet.Roots), "SKILL.md") {
		t.Errorf("SKILL.md should be a convention root; roots = %v", ids(m.RootSet.Roots))
	}
	if slices.Contains(ids(m.Orphans.Unreachable), "references/x.md") {
		t.Errorf("references/x.md is reached from SKILL.md; must not be unreachable (unreachable=%v)", ids(m.Orphans.Unreachable))
	}
}

// TestRootSet_ConfiguredGlob: a --root glob adds roots.
func TestRootSet_ConfiguredGlob(t *testing.T) {
	c := buildCorpus(t,
		doc("home.md", "h", nil),
		doc("other.md", "o", nil),
	)
	g := BuildReferenceGraph(c, nil, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{RootGlobs: []string{"home.md"}})
	if got := ids(m.RootSet.Roots); !slices.Equal(got, []string{"home.md"}) {
		t.Errorf("root set = %v, want [home.md] from glob", got)
	}
}

// TestEdgelessRoot_NotIsolated is the crux known-answer test for the root/orphan
// exemption (ADR 0007): an EDGELESS document that IS in the root set must NOT be
// reported isolated, while the SAME edgeless document when it is NOT a root MUST
// be reported isolated (proving the general isolated-orphan finding still fires).
func TestEdgelessRoot_NotIsolated(t *testing.T) {
	// SKILL.md is an edgeless auto-root; agent.md is an identical edgeless doc that
	// is NOT a convention filename. README.md gives a determinate root set.
	c := buildCorpus(t,
		doc("README.md", "intro", nil),
		doc("SKILL.md", "skill", nil), // edgeless, but a root by filename
		doc("agent.md", "agent", nil), // edgeless, NOT a root
	)
	g := BuildReferenceGraph(c, nil, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})

	iso := ids(m.Orphans.Isolated)
	if slices.Contains(iso, "SKILL.md") {
		t.Errorf("edgeless root SKILL.md must NOT be reported isolated; isolated = %v", iso)
	}
	if !slices.Contains(iso, "agent.md") {
		t.Errorf("edgeless non-root agent.md MUST be reported isolated; isolated = %v", iso)
	}
	// And the root is exempt from unreachable too (it is itself a root → reached).
	if slices.Contains(ids(m.Orphans.Unreachable), "SKILL.md") {
		t.Error("a root must never be reported unreachable")
	}
}

// TestEdgelessDoc_IsolatedUnlessRooted proves the general case fires for the very
// same document content: the identical edgeless doc is isolated when it is not a
// root, and silenced once it becomes a root (here via a --root glob).
func TestEdgelessDoc_IsolatedUnlessRooted(t *testing.T) {
	mk := func(globs []string) []string {
		c := buildCorpus(t,
			doc("README.md", "intro", nil),
			doc("loner.md", "loner", nil), // edgeless
		)
		g := BuildReferenceGraph(c, nil, BuildOptions{})
		return ids(Analyze(g, c, AnalyzeOptions{RootGlobs: globs}).Orphans.Isolated)
	}
	if iso := mk(nil); !slices.Contains(iso, "loner.md") {
		t.Errorf("without --root, edgeless loner.md must be isolated; got %v", iso)
	}
	if iso := mk([]string{"loner.md"}); slices.Contains(iso, "loner.md") {
		t.Errorf("with --root loner.md, it must be exempt from isolated; got %v", iso)
	}
}

// TestRootWithOutEdges_BehavesAsBefore: a root that has outbound edges is already
// non-isolated by degree, so the root exemption changes nothing for it — it is
// not isolated (out>0) and not unreachable (it is a root). This pins that the
// exemption is narrow (edgeless roots only) and didn't alter existing behavior.
func TestRootWithOutEdges_BehavesAsBefore(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "intro", nil),
		doc("guide.md", "guide", nil),
	)
	refs := []reference.Reference{validRef("README.md", "guide.md")}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})

	if slices.Contains(ids(m.Orphans.Isolated), "README.md") {
		t.Error("a root with out-edges is non-isolated by degree; must not be in Isolated")
	}
	if slices.Contains(ids(m.Orphans.Unreachable), "README.md") {
		t.Error("a root is never unreachable")
	}
	// guide.md is reached from README and has an inbound edge: neither finding.
	if slices.Contains(ids(m.Orphans.Isolated), "guide.md") || slices.Contains(ids(m.Orphans.Unreachable), "guide.md") {
		t.Error("guide.md is reachable and linked; it should be clean")
	}
}

// TestDeterminism_ShuffledInput: shuffling document insertion order and edge
// order yields identical analysis output (component IDs, HITS, orphan sets).
func TestDeterminism_ShuffledInput(t *testing.T) {
	build := func(order []int) *GraphMetrics {
		all := []*corpus.Document{
			doc("README.md", "intro", nil),
			doc("guide.md", "guide", nil),
			doc("api.md", "api", nil),
			doc("cycleA.md", "a", nil),
			doc("cycleB.md", "b", nil),
			doc("orphan.md", "orphan", nil),
		}
		c := corpus.NewCorpus()
		for _, i := range order {
			_ = c.Add(all[i])
		}
		refs := []reference.Reference{
			validRef("README.md", "guide.md"),
			validRef("guide.md", "api.md"),
			validRef("cycleA.md", "cycleB.md"),
			validRef("cycleB.md", "cycleA.md"),
		}
		// Shuffle edges too.
		slices.Reverse(refs)
		g := BuildReferenceGraph(c, refs, BuildOptions{})
		return Analyze(g, c, AnalyzeOptions{})
	}

	m1 := build([]int{0, 1, 2, 3, 4, 5})
	m2 := build([]int{5, 4, 3, 2, 1, 0})
	m3 := build([]int{3, 0, 5, 1, 4, 2})

	for _, m := range []*GraphMetrics{m2, m3} {
		if !slices.Equal(ids(m1.Orphans.Isolated), ids(m.Orphans.Isolated)) {
			t.Errorf("isolated orphans differ across input order: %v vs %v",
				ids(m1.Orphans.Isolated), ids(m.Orphans.Isolated))
		}
		if !slices.Equal(componentIDs(m1.WCC), componentIDs(m.WCC)) {
			t.Errorf("WCC IDs differ across input order")
		}
		if !slices.Equal(componentIDs(m1.SCC), componentIDs(m.SCC)) {
			t.Errorf("SCC IDs differ across input order")
		}
		// HITS scores identical to within float tolerance.
		for _, id := range m1.Graph.Documents() {
			if d := m1.HITS.AuthorityScore(id) - m.HITS.AuthorityScore(id); d > 1e-12 || d < -1e-12 {
				t.Errorf("authority for %s differs across order: %v vs %v", id, m1.HITS.AuthorityScore(id), m.HITS.AuthorityScore(id))
			}
		}
	}
}

func componentIDs(comps Components) []string {
	out := make([]string, len(comps))
	for i, c := range comps {
		out[i] = c.ID.String()
	}
	return out
}
