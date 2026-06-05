package graphmodel

import (
	"slices"
	"testing"

	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/domain/reference"
)

// indexType is the front-matter Extra map that marks a document as a root
// (type: index), used so the test root set is exactly the one doc we pick rather
// than every README.md/index.md convention (ADR 0007).
var indexType = map[string]any{"type": "index"}

// dirRef builds a Valid TargetDirectory reference (ADR 0008): origin links the
// directory dir, which has the given index (may be "") and direct children.
func dirRef(origin, dir, index string, children ...string) reference.Reference {
	kids := make([]identity.DocumentID, len(children))
	for i, c := range children {
		kids[i] = identity.DocumentID(c)
	}
	return reference.Reference{
		RawReference: reference.RawReference{
			Origin: identity.DocumentID(origin), RawTarget: dir, Type: reference.RelativeLink, Line: 1,
		},
		Target: reference.ResolvedTarget{
			Kind:       reference.TargetDirectory,
			Directory:  dir,
			DocumentID: identity.DocumentID(index),
			Children:   kids,
		},
		Health: reference.Valid,
	}
}

// dirLinkCorpus: home.md (the sole type:index root) links the folder sub/, which
// has sub/_index.md as its directory index plus two non-indexed docs. None of the
// sub/ docs uses a README.md/index.md basename, so they are NOT conventional
// roots — their reachability depends entirely on the directory link. sub/deep/
// holds a doc one level deeper, used to prove the one-level scoping (ADR 0008).
func dirLinkCorpus(t *testing.T) (*corpus.Corpus, []reference.Reference, string) {
	t.Helper()
	c := buildCorpus(t,
		doc("home.md", "home", indexType),
		doc("sub/_index.md", "subindex", nil),
		doc("sub/one.md", "one", nil),
		doc("sub/two.md", "two", nil),
		doc("sub/deep/buried.md", "buried", nil),
	)
	refs := []reference.Reference{
		dirRef("home.md", "sub", "sub/_index.md", "sub/_index.md", "sub/one.md", "sub/two.md"),
	}
	return c, refs, "home.md"
}

// TestDirectoryLink_DefaultPolicyReachesChildren: under the default (lenient)
// policy a directory link makes the folder's DIRECT children reachable. The
// one-level scoping (ADR 0008) leaves the deeper sub/deep/buried.md NOT reached.
func TestDirectoryLink_DefaultPolicyReachesChildren(t *testing.T) {
	c, refs, _ := dirLinkCorpus(t)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})

	wantOut := []string{"sub/_index.md", "sub/one.md", "sub/two.md"}
	if got := ids(g.ProjectionOut("home.md")); !slices.Equal(got, wantOut) {
		t.Errorf("home projection out = %v, want %v", got, wantOut)
	}

	reached := ids(m.Reachability.Reached)
	for _, r := range wantOut {
		if !slices.Contains(reached, r) {
			t.Errorf("%s should be reachable via the directory link, reached=%v", r, reached)
		}
	}
	// One-level scoping: the deeper doc is NOT reached (ADR 0007 invariant kept).
	if slices.Contains(reached, "sub/deep/buried.md") {
		t.Errorf("sub/deep/buried.md must NOT be reached by a one-level directory link")
	}
}

// TestDirectoryLink_StrictPolicyDoesNotVouch: under strict policy a directory
// link adds only the index edge; the non-index siblings are NOT vouched for and
// are unreached.
func TestDirectoryLink_StrictPolicyDoesNotVouch(t *testing.T) {
	c, refs, _ := dirLinkCorpus(t)
	g := BuildReferenceGraph(c, refs, BuildOptions{StrictDirectoryLinks: true})
	m := Analyze(g, c, AnalyzeOptions{})

	if got := ids(g.ProjectionOut("home.md")); !slices.Equal(got, []string{"sub/_index.md"}) {
		t.Errorf("strict home projection out = %v, want [sub/_index.md]", got)
	}

	reached := ids(m.Reachability.Reached)
	if !slices.Contains(reached, "sub/_index.md") {
		t.Error("the index should be reachable even under strict policy")
	}
	for _, sibling := range []string{"sub/one.md", "sub/two.md"} {
		if slices.Contains(reached, sibling) {
			t.Errorf("%s must NOT be reachable under strict policy (index does not vouch for siblings)", sibling)
		}
	}
}

// TestDirectoryLink_NoIndexStrictAddsNoEdge: a directory link to an index-less
// folder under strict policy adds no edge at all (nothing to link to); under
// default policy it adds an edge to each direct child.
func TestDirectoryLink_NoIndexStrictAddsNoEdge(t *testing.T) {
	c := buildCorpus(t,
		doc("home.md", "home", indexType),
		doc("plain/a.md", "a", nil),
		doc("plain/b.md", "b", nil),
	)
	refs := []reference.Reference{
		dirRef("home.md", "plain", "", "plain/a.md", "plain/b.md"),
	}

	gStrict := BuildReferenceGraph(c, refs, BuildOptions{StrictDirectoryLinks: true})
	if got := ids(gStrict.ProjectionOut("home.md")); len(got) != 0 {
		t.Errorf("strict no-index directory link projection out = %v, want []", got)
	}

	gLenient := BuildReferenceGraph(c, refs, BuildOptions{})
	wantOut := []string{"plain/a.md", "plain/b.md"}
	if got := ids(gLenient.ProjectionOut("home.md")); !slices.Equal(got, wantOut) {
		t.Errorf("lenient no-index directory link projection out = %v, want %v", got, wantOut)
	}
}

// TestDirectoryLink_Deterministic: shuffled child order yields identical
// projection edges (sorted).
func TestDirectoryLink_Deterministic(t *testing.T) {
	c := buildCorpus(t,
		doc("home.md", "home", indexType),
		doc("sub/a.md", "a", nil),
		doc("sub/b.md", "b", nil),
		doc("sub/c.md", "c", nil),
	)
	r1 := []reference.Reference{dirRef("home.md", "sub", "", "sub/a.md", "sub/b.md", "sub/c.md")}
	r2 := []reference.Reference{dirRef("home.md", "sub", "", "sub/c.md", "sub/a.md", "sub/b.md")}

	g1 := BuildReferenceGraph(c, r1, BuildOptions{})
	g2 := BuildReferenceGraph(c, r2, BuildOptions{})
	if !slices.Equal(ids(g1.ProjectionOut("home.md")), ids(g2.ProjectionOut("home.md"))) {
		t.Errorf("projection differs on shuffled child order: %v vs %v",
			ids(g1.ProjectionOut("home.md")), ids(g2.ProjectionOut("home.md")))
	}
}
