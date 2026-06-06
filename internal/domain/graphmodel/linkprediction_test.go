package graphmodel

import (
	"math"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// suggestionByPair indexes a result by its unordered (DocA,DocB) pair for
// convenient assertions (DocA<DocB is guaranteed by PredictLinks).
func suggestionByPair(res LinkSuggestionResult) map[[2]string]LinkSuggestion {
	out := make(map[[2]string]LinkSuggestion, len(res.Suggestions))
	for _, s := range res.Suggestions {
		out[[2]string{s.DocA.String(), s.DocB.String()}] = s
	}
	return out
}

// TestPredictLinks_KnownAnswer locks the exact coupling / co-citation /
// shared-neighbour / Adamic-Adar values on a hand-computable graph:
//
//	A->C, B->C, A->D, B->D   (A and B both link to C and D)
//	E->A, E->B               (E links to both A and B)
//
// out(A)=out(B)={C,D}; in(A)=in(B)={E}; N(A)=N(B)={C,D,E}.
// Pair (A,B): unlinked, shared=3, coupling=|{C,D}|=2, cocitation=|{E}|=1,
// AdamicAdar = 3 * 1/log(2) (each of C,D,E has undirected degree 2).
func TestPredictLinks_KnownAnswer(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
		doc("E.md", "e", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "C.md"), validRef("B.md", "C.md"),
		validRef("A.md", "D.md"), validRef("B.md", "D.md"),
		validRef("E.md", "A.md"), validRef("E.md", "B.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	res := g.PredictLinks(LinkPredictionOptions{})

	byPair := suggestionByPair(res)
	ab, ok := byPair[[2]string{"A.md", "B.md"}]
	if !ok {
		t.Fatalf("expected an (A.md,B.md) suggestion, got %+v", res.Suggestions)
	}
	if ab.SharedNeighbours != 3 {
		t.Errorf("(A,B) shared = %d, want 3", ab.SharedNeighbours)
	}
	if ab.Coupling != 2 {
		t.Errorf("(A,B) coupling = %d, want 2", ab.Coupling)
	}
	if ab.CoCitation != 1 {
		t.Errorf("(A,B) cocitation = %d, want 1", ab.CoCitation)
	}
	wantAA := 3 * (1 / math.Log(2))
	if math.Abs(ab.AdamicAdar-wantAA) > 1e-12 {
		t.Errorf("(A,B) AdamicAdar = %v, want %v", ab.AdamicAdar, wantAA)
	}

	// (C,D): both linked-to by A and B (and never link to each other).
	// N(C)=N(D)={A,B}; shared=2, cocitation=|{A,B}|=2, coupling=0,
	// AdamicAdar = 2 * 1/log(3) (A and B each have undirected degree 3).
	cd, ok := byPair[[2]string{"C.md", "D.md"}]
	if !ok {
		t.Fatalf("expected a (C.md,D.md) suggestion")
	}
	if cd.SharedNeighbours != 2 || cd.CoCitation != 2 || cd.Coupling != 0 {
		t.Errorf("(C,D) = %+v, want shared=2 cocitation=2 coupling=0", cd)
	}
	wantCD := 2 * (1 / math.Log(3))
	if math.Abs(cd.AdamicAdar-wantCD) > 1e-12 {
		t.Errorf("(C,D) AdamicAdar = %v, want %v", cd.AdamicAdar, wantCD)
	}

	// Ranking: (A,B) has the highest Adamic/Adar, so it is first.
	if res.Suggestions[0].DocA != "A.md" || res.Suggestions[0].DocB != "B.md" {
		t.Errorf("top suggestion = (%s,%s), want (A.md,B.md)",
			res.Suggestions[0].DocA, res.Suggestions[0].DocB)
	}
}

// TestPredictLinks_UnlinkedOnly: once A links to B directly, the (A,B) pair is no
// longer suggested (they are connected), even though they still share neighbours.
func TestPredictLinks_UnlinkedOnly(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
	)
	base := make([]reference.Reference, 0, 5)
	base = append(base,
		validRef("A.md", "C.md"), validRef("B.md", "C.md"),
		validRef("A.md", "D.md"), validRef("B.md", "D.md"),
	)
	g := BuildReferenceGraph(c, base, BuildOptions{})
	if _, ok := suggestionByPair(g.PredictLinks(LinkPredictionOptions{}))[[2]string{"A.md", "B.md"}]; !ok {
		t.Fatal("expected (A,B) suggested before they are linked")
	}

	// Now add A->B: the pair is linked and must NOT be suggested.
	linked := make([]reference.Reference, 0, len(base)+1)
	linked = append(linked, base...)
	linked = append(linked, validRef("A.md", "B.md"))
	g2 := BuildReferenceGraph(c, linked, BuildOptions{})
	if _, ok := suggestionByPair(g2.PredictLinks(LinkPredictionOptions{}))[[2]string{"A.md", "B.md"}]; ok {
		t.Error("(A,B) must not be suggested once A links to B")
	}
}

// TestPredictLinks_MinSharedThreshold: a pair sharing exactly ONE neighbour is
// dropped at the default (2) and appears when the floor is lowered to 1.
func TestPredictLinks_MinSharedThreshold(t *testing.T) {
	// A->C, B->C : A and B share exactly one neighbour (C). Unlinked.
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil),
	)
	refs := []reference.Reference{validRef("A.md", "C.md"), validRef("B.md", "C.md")}
	g := BuildReferenceGraph(c, refs, BuildOptions{})

	if _, ok := suggestionByPair(g.PredictLinks(LinkPredictionOptions{}))[[2]string{"A.md", "B.md"}]; ok {
		t.Error("(A,B) sharing 1 neighbour must be dropped at the default MinSharedNeighbours=2")
	}
	if _, ok := suggestionByPair(g.PredictLinks(LinkPredictionOptions{MinSharedNeighbours: 1}))[[2]string{"A.md", "B.md"}]; !ok {
		t.Error("(A,B) sharing 1 neighbour must appear when MinSharedNeighbours=1")
	}
}

// TestPredictLinks_Deterministic asserts the result (including float order and
// tie-break) is identical across runs and independent of edge insertion order.
func TestPredictLinks_Deterministic(t *testing.T) {
	docs := []*corpus.Document{
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil), doc("E.md", "e", nil),
	}
	forward := []reference.Reference{
		validRef("A.md", "C.md"), validRef("B.md", "C.md"),
		validRef("A.md", "D.md"), validRef("B.md", "D.md"),
		validRef("E.md", "A.md"), validRef("E.md", "B.md"),
	}
	// Shuffled (reversed) insertion order.
	shuffled := make([]reference.Reference, len(forward))
	for i := range forward {
		shuffled[len(forward)-1-i] = forward[i]
	}

	g1 := BuildReferenceGraph(buildCorpus(t, docs...), forward, BuildOptions{})
	g2 := BuildReferenceGraph(buildCorpus(t, docs...), shuffled, BuildOptions{})
	r1 := g1.PredictLinks(LinkPredictionOptions{})
	r1b := g1.PredictLinks(LinkPredictionOptions{})
	r2 := g2.PredictLinks(LinkPredictionOptions{})

	assertSameSuggestions(t, r1.Suggestions, r1b.Suggestions, "two runs same graph")
	assertSameSuggestions(t, r1.Suggestions, r2.Suggestions, "shuffled edge order")
}

func assertSameSuggestions(t *testing.T, a, b []LinkSuggestion, ctx string) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("%s: lengths differ %d vs %d", ctx, len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("%s: index %d differs: %+v vs %+v", ctx, i, a[i], b[i])
		}
	}
}

// TestPredictLinks_CapTruncates builds enough mutually-suggestible pairs to
// exceed MaxSuggestedLinks and asserts the list is capped and Truncated is set.
// A star of N leaves all pointing at two shared hubs yields C(N,2) unlinked pairs
// each sharing 2 neighbours; N=46 gives 1035 > 1000.
func TestPredictLinks_CapTruncates(t *testing.T) {
	const leaves = 46
	docs := []*corpus.Document{doc("H1.md", "h1", nil), doc("H2.md", "h2", nil)}
	var refs []reference.Reference
	for i := 0; i < leaves; i++ {
		id := leafID(i)
		docs = append(docs, doc(id, "s", nil))
		refs = append(refs, validRef(id, "H1.md"), validRef(id, "H2.md"))
	}
	g := BuildReferenceGraph(buildCorpus(t, docs...), refs, BuildOptions{})
	res := g.PredictLinks(LinkPredictionOptions{})
	if len(res.Suggestions) != MaxSuggestedLinks {
		t.Errorf("len = %d, want exactly MaxSuggestedLinks=%d", len(res.Suggestions), MaxSuggestedLinks)
	}
	if !res.Truncated {
		t.Error("expected Truncated to be set when the cap is hit")
	}
}

func leafID(i int) string {
	// Fixed-width so DocumentID sort order is the numeric order (irrelevant to the
	// cap count, but keeps the fixture readable).
	const digits = "0123456789"
	return "leaf" + string(digits[i/10]) + string(digits[i%10]) + ".md"
}

// TestPredictLinks_HubFanoutGuard: a common neighbour whose undirected degree
// exceeds MaxFanout is skipped as a pair-generator, so pairs that ONLY co-occur
// under that hub are not produced, and HubsSkipped/Truncated are set.
func TestPredictLinks_HubFanoutGuard(t *testing.T) {
	// One hub linked to by many leaves; with MaxFanout=2 the hub (degree=leaves)
	// is skipped, so no leaf pair is generated through it.
	const leaves = 5
	docs := []*corpus.Document{doc("HUB.md", "hub", nil)}
	var refs []reference.Reference
	for i := 0; i < leaves; i++ {
		id := leafID(i)
		docs = append(docs, doc(id, "s", nil))
		refs = append(refs, validRef(id, "HUB.md"))
	}
	g := BuildReferenceGraph(buildCorpus(t, docs...), refs, BuildOptions{})

	// With the default fanout the hub generates leaf pairs (each shares HUB only =
	// 1 neighbour, so they need MinSharedNeighbours=1 to surface).
	full := g.PredictLinks(LinkPredictionOptions{MinSharedNeighbours: 1})
	if len(full.Suggestions) == 0 {
		t.Fatal("expected leaf pairs through the hub at default fanout")
	}
	if full.HubsSkipped {
		t.Error("did not expect HubsSkipped at default fanout")
	}

	// With MaxFanout=2 the degree-5 hub is skipped as a generator.
	guarded := g.PredictLinks(LinkPredictionOptions{MinSharedNeighbours: 1, MaxFanout: 2})
	if len(guarded.Suggestions) != 0 {
		t.Errorf("expected no suggestions when the hub is skipped, got %d", len(guarded.Suggestions))
	}
	if !guarded.HubsSkipped || !guarded.Truncated {
		t.Errorf("expected HubsSkipped+Truncated when a hub exceeds MaxFanout: %+v", guarded)
	}
}

// TestPredictLinks_EdgeCases: empty, single-doc, and no-shared-neighbour graphs
// all yield zero suggestions without panicking.
func TestPredictLinks_EdgeCases(t *testing.T) {
	// Empty corpus.
	empty := BuildReferenceGraph(buildCorpus(t), nil, BuildOptions{})
	if res := empty.PredictLinks(LinkPredictionOptions{}); len(res.Suggestions) != 0 || res.Truncated {
		t.Errorf("empty graph = %+v, want no suggestions", res)
	}

	// Single document.
	single := BuildReferenceGraph(buildCorpus(t, doc("only.md", "o", nil)), nil, BuildOptions{})
	if res := single.PredictLinks(LinkPredictionOptions{}); len(res.Suggestions) != 0 {
		t.Errorf("single-doc graph = %+v, want no suggestions", res)
	}

	// Two docs, a simple chain, no shared neighbours.
	chain := buildCorpus(t, doc("a.md", "a", nil), doc("b.md", "b", nil), doc("c.md", "c", nil))
	g := BuildReferenceGraph(chain, []reference.Reference{validRef("a.md", "b.md"), validRef("b.md", "c.md")}, BuildOptions{})
	if res := g.PredictLinks(LinkPredictionOptions{}); len(res.Suggestions) != 0 {
		t.Errorf("chain graph = %+v, want no suggestions (no pair shares >=2 neighbours)", res)
	}
}

// TestPredictLinks_TieBreakOrdering exercises the comparator's secondary/tertiary
// keys (linkprediction.go: SharedNeighbours DESC, then DocA ASC, then DocB ASC) by
// constructing pairs that TIE on AdamicAdar, then asserting the FULL ordered slice.
//
// Fixture: three leaves a1,a2,a3 each link to the same two hubs H1,H2.
//   - out(a_i)={H1,H2}; in(H1)=in(H2)={a1,a2,a3}.
//   - N(a_i)={H1,H2} (deg 2); N(H1)=N(H2)={a1,a2,a3} (deg 3).
//
// Candidate pairs (all unlinked):
//   - (H1,H2): common neighbours {a1,a2,a3}, each deg 2 → AA = 3/ln2 (highest).
//   - (a1,a2),(a1,a3),(a2,a3): common neighbours {H1,H2}, each deg 3 → AA = 2/ln3,
//     all THREE tie on AdamicAdar AND on SharedNeighbours(=2), so only the DocA/DocB
//     tie-breaks distinguish them.
//
// Expected total order: (H1,H2) first (higher AA), then the three leaf pairs in
// (DocA ASC, DocB ASC) order: (a1,a2),(a1,a3),(a2,a3).
func TestPredictLinks_TieBreakOrdering(t *testing.T) {
	c := buildCorpus(t,
		doc("a1.md", "a1", nil), doc("a2.md", "a2", nil), doc("a3.md", "a3", nil),
		doc("h1.md", "h1", nil), doc("h2.md", "h2", nil),
	)
	refs := []reference.Reference{
		validRef("a1.md", "h1.md"), validRef("a1.md", "h2.md"),
		validRef("a2.md", "h1.md"), validRef("a2.md", "h2.md"),
		validRef("a3.md", "h1.md"), validRef("a3.md", "h2.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	res := g.PredictLinks(LinkPredictionOptions{})

	// The exact, full ordered slice (pairs only; values asserted via the index map
	// for AA where it matters, here we assert ORDER which is the point of the test).
	wantOrder := [][2]string{
		{"h1.md", "h2.md"}, // AA = 3/ln2 (highest), sorts first
		{"a1.md", "a2.md"}, // the tied AA = 2/ln3 group, ordered by DocA then DocB
		{"a1.md", "a3.md"},
		{"a2.md", "a3.md"},
	}
	if len(res.Suggestions) != len(wantOrder) {
		t.Fatalf("got %d suggestions, want %d: %+v", len(res.Suggestions), len(wantOrder), res.Suggestions)
	}
	for i, want := range wantOrder {
		got := res.Suggestions[i]
		if got.DocA.String() != want[0] || got.DocB.String() != want[1] {
			t.Errorf("position %d = (%s,%s), want (%s,%s); full slice = %+v",
				i, got.DocA, got.DocB, want[0], want[1], res.Suggestions)
		}
	}

	// Lock the tie premise: the three leaf pairs genuinely tie on AdamicAdar AND
	// SharedNeighbours, so it is ONLY the DocA/DocB tie-break under test that orders
	// them (not an AA difference sneaking in).
	leafPairs := res.Suggestions[1:]
	for i := 1; i < len(leafPairs); i++ {
		if leafPairs[i].AdamicAdar != leafPairs[0].AdamicAdar {
			t.Errorf("leaf pairs must tie on AdamicAdar: %v vs %v", leafPairs[i].AdamicAdar, leafPairs[0].AdamicAdar)
		}
		if leafPairs[i].SharedNeighbours != leafPairs[0].SharedNeighbours {
			t.Errorf("leaf pairs must tie on SharedNeighbours: %d vs %d", leafPairs[i].SharedNeighbours, leafPairs[0].SharedNeighbours)
		}
	}
	wantAA := 2 * (1 / math.Log(3))
	if math.Abs(leafPairs[0].AdamicAdar-wantAA) > 1e-12 {
		t.Errorf("tied leaf-pair AdamicAdar = %v, want %v (2/ln3)", leafPairs[0].AdamicAdar, wantAA)
	}
}

// TestPredictLinks_MaxFanoutBoundary locks the hub-guard comparison as `>` (not
// `>=`): a common neighbour whose undirected degree is EXACTLY MaxFanout is KEPT
// as a pair-generator, while degree MaxFanout+1 is SKIPPED. Two disjoint graphs
// isolate each side of the boundary (MinSharedNeighbours=1 so a single shared hub
// surfaces the pair). MaxFanout is set small (3) to keep the fixtures tiny.
func TestPredictLinks_MaxFanoutBoundary(t *testing.T) {
	const fanout = 3

	// deg == MaxFanout: a hub linked-to by exactly `fanout` leaves (undirected
	// degree == fanout) is KEPT, so its leaf pairs are generated and HubsSkipped is
	// false.
	atLimit := func() *ReferenceGraph {
		docs := []*corpus.Document{doc("HUB.md", "hub", nil)}
		var refs []reference.Reference
		for i := 0; i < fanout; i++ {
			id := leafID(i)
			docs = append(docs, doc(id, "s", nil))
			refs = append(refs, validRef(id, "HUB.md"))
		}
		return BuildReferenceGraph(buildCorpus(t, docs...), refs, BuildOptions{})
	}()
	res := atLimit.PredictLinks(LinkPredictionOptions{MinSharedNeighbours: 1, MaxFanout: fanout})
	if len(res.Suggestions) == 0 {
		t.Errorf("deg == MaxFanout(%d) must be KEPT as a generator (> not >=), got no suggestions", fanout)
	}
	if res.HubsSkipped {
		t.Errorf("deg == MaxFanout(%d) must NOT set HubsSkipped: %+v", fanout, res)
	}

	// deg == MaxFanout+1: one more leaf pushes the hub's degree past the limit, so
	// it is SKIPPED and HubsSkipped/Truncated are set.
	overLimit := func() *ReferenceGraph {
		docs := []*corpus.Document{doc("HUB.md", "hub", nil)}
		var refs []reference.Reference
		for i := 0; i < fanout+1; i++ {
			id := leafID(i)
			docs = append(docs, doc(id, "s", nil))
			refs = append(refs, validRef(id, "HUB.md"))
		}
		return BuildReferenceGraph(buildCorpus(t, docs...), refs, BuildOptions{})
	}()
	res2 := overLimit.PredictLinks(LinkPredictionOptions{MinSharedNeighbours: 1, MaxFanout: fanout})
	if len(res2.Suggestions) != 0 {
		t.Errorf("deg == MaxFanout+1(%d) must be SKIPPED as a generator, got %d suggestions", fanout+1, len(res2.Suggestions))
	}
	if !res2.HubsSkipped || !res2.Truncated {
		t.Errorf("deg == MaxFanout+1(%d) must set HubsSkipped+Truncated: %+v", fanout+1, res2)
	}
}

// TestUndirectedNeighbours sanity-checks the sorted union helper directly.
func TestUndirectedNeighbours(t *testing.T) {
	c := buildCorpus(t, doc("a.md", "a", nil), doc("b.md", "b", nil), doc("c.md", "c", nil))
	// a->b, c->a  =>  out(a)={b}, in(a)={c}, N(a)={b,c} sorted.
	g := BuildReferenceGraph(c, []reference.Reference{validRef("a.md", "b.md"), validRef("c.md", "a.md")}, BuildOptions{})
	got := g.undirectedNeighbours(identity.DocumentID("a.md"))
	want := []identity.DocumentID{"b.md", "c.md"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("N(a) = %v, want %v", got, want)
	}
}
