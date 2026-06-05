package graphmodel

import (
	"slices"
	"testing"

	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/domain/reference"
)

func TestNodeKind_StringValid(t *testing.T) {
	all := []NodeKind{NodeKindDocument, NodeKindSection}
	seen := make(map[string]bool)
	for _, k := range all {
		if !k.Valid() {
			t.Errorf("NodeKind %d reported invalid", int(k))
		}
		s := k.String()
		if s == "" || s == "unknown" {
			t.Errorf("NodeKind %d has bad String() %q", int(k), s)
		}
		if seen[s] {
			t.Errorf("duplicate String() %q", s)
		}
		seen[s] = true
	}
	if (NodeKind(-1)).Valid() || (NodeKind(99)).Valid() {
		t.Error("out-of-range NodeKind reported valid")
	}
	if got := NodeKind(99).String(); got != "unknown" {
		t.Errorf("NodeKind(99).String() = %q, want unknown", got)
	}
}

func TestNodeID_Builders(t *testing.T) {
	if got := NodeID("docs/a.md").String(); got != "docs/a.md" {
		t.Errorf("NodeID.String() = %q, want docs/a.md", got)
	}
	if got := NodeIDForDocument("docs/a.md"); got != "docs/a.md" {
		t.Errorf("NodeIDForDocument = %q, want docs/a.md", got)
	}
	if got := NodeIDForSection("docs/a.md", "intro"); got != "docs/a.md#intro" {
		t.Errorf("NodeIDForSection = %q, want docs/a.md#intro", got)
	}
}

// --- test corpus/graph builders ---

// doc builds a Document with one H1 section (slug) so it has a section vertex,
// plus optional front-matter Extra entries.
func doc(id, slug string, extra map[string]any) *corpus.Document {
	root := &corpus.Section{Level: 0, StartLine: 1, EndLine: 100}
	if slug != "" {
		sec := &corpus.Section{Level: 1, Slug: slug, Text: slug, StartLine: 1, EndLine: 100, Parent: root}
		root.Children = append(root.Children, sec)
	}
	return &corpus.Document{
		ID:          identity.DocumentID(id),
		Root:        root,
		FrontMatter: corpus.FrontMatter{Extra: extra},
	}
}

func validRef(origin, targetDoc string) reference.Reference {
	return reference.Reference{
		RawReference: reference.RawReference{
			Origin: identity.DocumentID(origin), RawTarget: targetDoc, Type: reference.RelativeLink, Line: 1,
		},
		Target: reference.ResolvedTarget{Kind: reference.TargetDocument, DocumentID: identity.DocumentID(targetDoc)},
		Health: reference.Valid,
	}
}

func externalRef(origin, url string) reference.Reference {
	return reference.Reference{
		RawReference: reference.RawReference{Origin: identity.DocumentID(origin), RawTarget: url, Type: reference.External, Line: 1},
		Target:       reference.ResolvedTarget{Kind: reference.TargetExternal},
		Health:       reference.HealthExternal,
	}
}

func buildCorpus(t *testing.T, docs ...*corpus.Document) *corpus.Corpus {
	t.Helper()
	c := corpus.NewCorpus()
	for _, d := range docs {
		if err := c.Add(d); err != nil {
			t.Fatalf("add %s: %v", d.ID, err)
		}
	}
	return c
}

func ids(in []identity.DocumentID) []string {
	out := make([]string, len(in))
	for i, id := range in {
		out[i] = id.String()
	}
	return out
}

// knownAnswerCorpus builds a corpus with a deliberately mixed shape:
//
//	README.md  -> guide.md, api.md   (conventional root; hub)
//	guide.md   -> api.md
//	api.md                            (authority)
//	cycleA.md <-> cycleB.md           (SCC of size 2; unreachable)
//	orphan.md                          (isolated: in=out=0)
//	unreach.md -> guide.md             (links out, no inbound -> unreachable, not isolated)
//	clusterX.md -> clusterY.md         (separate WCC -> gap vs main cluster)
//	README also has an EXTERNAL link (must not create an edge)
func knownAnswerCorpus(t *testing.T) (*corpus.Corpus, []reference.Reference) {
	t.Helper()
	c := buildCorpus(t,
		doc("README.md", "intro", nil),
		doc("guide.md", "guide", nil),
		doc("api.md", "api", nil),
		doc("cycleA.md", "a", nil),
		doc("cycleB.md", "b", nil),
		doc("orphan.md", "orphan", nil),
		doc("unreach.md", "u", nil),
		doc("clusterX.md", "x", nil),
		doc("clusterY.md", "y", nil),
	)
	refs := []reference.Reference{
		validRef("README.md", "guide.md"),
		validRef("README.md", "api.md"),
		validRef("guide.md", "api.md"),
		validRef("cycleA.md", "cycleB.md"),
		validRef("cycleB.md", "cycleA.md"),
		validRef("unreach.md", "guide.md"),
		validRef("clusterX.md", "clusterY.md"),
		externalRef("README.md", "https://example.com"),
	}
	return c, refs
}

func TestKnownAnswer_RootsAndOrphans(t *testing.T) {
	c, refs := knownAnswerCorpus(t)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})

	if got := ids(m.RootSet.Roots); !slices.Equal(got, []string{"README.md"}) {
		t.Errorf("root set = %v, want [README.md]", got)
	}
	if m.RootSet.Indeterminate {
		t.Error("root set should not be indeterminate")
	}
	if got := ids(m.Orphans.Isolated); !slices.Equal(got, []string{"orphan.md"}) {
		t.Errorf("isolated orphans = %v, want [orphan.md]", got)
	}
	wantUnreach := []string{"clusterX.md", "clusterY.md", "cycleA.md", "cycleB.md", "unreach.md"}
	if got := ids(m.Orphans.Unreachable); !slices.Equal(got, wantUnreach) {
		t.Errorf("unreachable = %v, want %v", got, wantUnreach)
	}
}

func TestKnownAnswer_ExternalExcludedFromProjection(t *testing.T) {
	c, refs := knownAnswerCorpus(t)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	if got := ids(g.ProjectionOut("README.md")); !slices.Equal(got, []string{"api.md", "guide.md"}) {
		t.Errorf("README projection out = %v, want [api.md guide.md] (external excluded)", got)
	}
}

func TestKnownAnswer_SCC(t *testing.T) {
	c, refs := knownAnswerCorpus(t)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	scc := g.StronglyConnectedComponents()

	var cycle *Component
	for i := range scc {
		if slices.Contains(scc[i].Members, identity.DocumentID("cycleA.md")) {
			cycle = &scc[i]
			break
		}
	}
	if cycle == nil {
		t.Fatal("no SCC contains cycleA.md")
	}
	if got := ids(cycle.Members); !slices.Equal(got, []string{"cycleA.md", "cycleB.md"}) {
		t.Errorf("cycle SCC = %v, want [cycleA.md cycleB.md]", got)
	}
	if cycle.ID != "cycleA.md" {
		t.Errorf("cycle SCC ID = %q, want cycleA.md (sorted-min member)", cycle.ID)
	}
	if len(scc) != 8 { // 9 docs, one size-2 SCC => 8 components
		t.Errorf("SCC count = %d, want 8", len(scc))
	}
}

func TestKnownAnswer_WCC(t *testing.T) {
	c, refs := knownAnswerCorpus(t)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	wcc := g.WeaklyConnectedComponents()

	byID := map[string][]string{}
	for _, comp := range wcc {
		byID[comp.ID.String()] = ids(comp.Members)
	}
	want := map[string][]string{
		"README.md":   {"README.md", "api.md", "guide.md", "unreach.md"},
		"clusterX.md": {"clusterX.md", "clusterY.md"},
		"cycleA.md":   {"cycleA.md", "cycleB.md"},
		"orphan.md":   {"orphan.md"},
	}
	if len(byID) != len(want) {
		t.Fatalf("WCC count = %d (%v), want %d", len(byID), byID, len(want))
	}
	for id, members := range want {
		if !slices.Equal(byID[id], members) {
			t.Errorf("WCC %s = %v, want %v", id, byID[id], members)
		}
	}
}

func TestKnownAnswer_HITS(t *testing.T) {
	c, refs := knownAnswerCorpus(t)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	h := g.ComputeHITS(HitsOptions{})

	if top := h.TopAuthorities(1); len(top) == 0 || top[0].ID != "api.md" {
		t.Errorf("top authority = %v, want api.md", top)
	}
	if h.Hub["README.md"] < h.Hub["guide.md"] {
		t.Errorf("README hub %.4f should exceed guide hub %.4f", h.Hub["README.md"], h.Hub["guide.md"])
	}
}

func TestKnownAnswer_Gaps(t *testing.T) {
	c, refs := knownAnswerCorpus(t)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	wcc := g.WeaklyConnectedComponents()
	res := DetectGaps(wcc, GapOptions{MinComponentSize: 2})

	if res.Truncated {
		t.Error("small corpus must not truncate gaps")
	}
	if len(res.Gaps) != 3 { // C(3,2) among the three size>=2 WCCs
		t.Fatalf("gap count = %d, want 3: %+v", len(res.Gaps), res.Gaps)
	}
	for _, gap := range res.Gaps {
		if gap.ComponentA >= gap.ComponentB {
			t.Errorf("gap components not sorted: %v", gap)
		}
	}
}
