package corpus

import (
	"errors"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

func TestCorpus_AddDuplicate(t *testing.T) {
	c := NewCorpus()
	doc := &Document{ID: "docs/a.md"}

	if err := c.Add(doc); err != nil {
		t.Fatalf("first Add: unexpected error: %v", err)
	}
	if err := c.Add(&Document{ID: "docs/a.md"}); err == nil {
		t.Fatal("second Add with same ID: want error, got nil")
	}
	if c.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", c.Len())
	}
}

func TestCorpus_AddNilAndEmpty(t *testing.T) {
	c := NewCorpus()
	if err := c.Add(nil); err == nil {
		t.Error("Add(nil): want error")
	}
	if err := c.Add(&Document{ID: ""}); err == nil {
		t.Error("Add(empty ID): want error")
	}
}

func TestCorpus_DocumentsSorted(t *testing.T) {
	c := NewCorpus()
	// Insert out of order.
	for _, id := range []DocumentID{"z.md", "a.md", "m/n.md", "m/a.md"} {
		if err := c.Add(&Document{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	got := c.Documents()
	want := []DocumentID{"a.md", "m/a.md", "m/n.md", "z.md"}
	if len(got) != len(want) {
		t.Fatalf("Documents() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Errorf("Documents()[%d].ID = %q, want %q", i, got[i].ID, want[i])
		}
	}
}

func TestCorpus_Get(t *testing.T) {
	c := NewCorpus()
	doc := &Document{ID: "a.md"}
	_ = c.Add(doc)

	if got, ok := c.Get("a.md"); !ok || got != doc {
		t.Errorf("Get(a.md) = %v, %v; want the added doc, true", got, ok)
	}
	if _, ok := c.Get("missing.md"); ok {
		t.Error("Get(missing.md) ok = true, want false")
	}
}

func TestCorpus_Headings(t *testing.T) {
	c := NewCorpus()
	if err := c.AddHeading("a.md", "intro"); err != nil {
		t.Fatalf("AddHeading: unexpected error %v", err)
	}
	if !c.HasHeading("a.md", "intro") {
		t.Error("HasHeading(a.md, intro) = false, want true")
	}
	if c.HasHeading("a.md", "missing") {
		t.Error("HasHeading(a.md, missing) = true, want false")
	}
	if c.HasHeading("b.md", "intro") {
		t.Error("HasHeading(b.md, intro) = true, want false")
	}
}

func TestCorpus_Aliases(t *testing.T) {
	c := NewCorpus()
	if err := c.AddAlias("foo", "x/foo.md"); err != nil {
		t.Fatalf("AddAlias: unexpected error %v", err)
	}
	if err := c.AddAlias("foo", "y/foo.md"); err != nil {
		t.Fatalf("AddAlias: unexpected error %v", err)
	}
	got := c.LookupAlias("foo")
	if len(got) != 2 {
		t.Fatalf("LookupAlias(foo) len = %d, want 2", len(got))
	}
	// Sorted order.
	if got[0] != "x/foo.md" || got[1] != "y/foo.md" {
		t.Errorf("LookupAlias(foo) = %v, want sorted [x/foo.md y/foo.md]", got)
	}
	if c.LookupAlias("missing") != nil {
		t.Error("LookupAlias(missing) want nil")
	}
	// The returned slice must be a copy: mutating it must not affect the corpus.
	got[0] = "tampered"
	if c.LookupAlias("foo")[0] != "x/foo.md" {
		t.Error("LookupAlias result is not isolated from caller mutation")
	}
}

// section is a small helper to build a section tree for indexing tests.
func section(level int, slug string, children ...*Section) *Section {
	return &Section{Level: level, Slug: slug, Children: children}
}

func TestCorpus_AddIndexesHeadings(t *testing.T) {
	c := NewCorpus()
	doc := &Document{
		ID: "a.md",
		Root: section(0, "",
			section(1, "intro",
				section(2, "details"),
			),
			section(1, "usage"),
		),
	}
	if err := c.Add(doc); err != nil {
		t.Fatal(err)
	}
	// Add must auto-index every non-empty slug in the section tree.
	for _, slug := range []string{"intro", "details", "usage"} {
		if !c.HasHeading("a.md", slug) {
			t.Errorf("HasHeading(a.md, %q) = false; Add did not auto-index it", slug)
		}
	}
	if c.HeadingCount() != 3 {
		t.Errorf("HeadingCount() = %d, want 3", c.HeadingCount())
	}
}

func TestCorpus_AddIndexesAliases(t *testing.T) {
	c := NewCorpus()
	doc := &Document{
		ID:          "guide.md",
		FrontMatter: FrontMatter{Aliases: []string{"guide", "handbook", ""}},
	}
	if err := c.Add(doc); err != nil {
		t.Fatal(err)
	}
	// Front-matter aliases must be indexed so the P2 resolver can use them; the
	// empty alias is ignored.
	if got := c.LookupAlias("guide"); len(got) != 1 || got[0] != "guide.md" {
		t.Errorf("LookupAlias(guide) = %v, want [guide.md]", got)
	}
	if got := c.LookupAlias("handbook"); len(got) != 1 || got[0] != "guide.md" {
		t.Errorf("LookupAlias(handbook) = %v, want [guide.md]", got)
	}
	if c.LookupAlias("") != nil {
		t.Error("empty alias should not be indexed")
	}
}

// TestCorpus_AddIndexesNameAsAlias asserts the single-valued front-matter `name`
// field is indexed into the alias table alongside `aliases`, so a `[[name]]`
// wikilink resolves to the document; an empty `name` is ignored, and two docs
// sharing a name are returned together (the resolver reports that as Ambiguous).
func TestCorpus_AddIndexesNameAsAlias(t *testing.T) {
	c := NewCorpus()
	if err := c.Add(&Document{
		ID:          "foo.md",
		FrontMatter: FrontMatter{Name: "foo-bar"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(&Document{
		ID:          "empty.md",
		FrontMatter: FrontMatter{Name: ""},
	}); err != nil {
		t.Fatal(err)
	}
	// Two docs sharing the same name → both indexed (resolver → Ambiguous).
	if err := c.Add(&Document{
		ID:          "a.md",
		FrontMatter: FrontMatter{Name: "dup"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(&Document{
		ID:          "b.md",
		FrontMatter: FrontMatter{Name: "dup"},
	}); err != nil {
		t.Fatal(err)
	}

	if got := c.LookupAlias("foo-bar"); len(got) != 1 || got[0] != "foo.md" {
		t.Errorf("LookupAlias(foo-bar) = %v, want [foo.md]", got)
	}
	if c.LookupAlias("") != nil {
		t.Error("empty name should not be indexed")
	}
	if got := c.LookupAlias("dup"); len(got) != 2 || got[0] != "a.md" || got[1] != "b.md" {
		t.Errorf("LookupAlias(dup) = %v, want sorted [a.md b.md]", got)
	}
}

// TestResolveWikilinkViaName drives the full resolution path: a Corpus built with
// `name:` front matter feeds the reference.Resolver (via the Catalog seam), and a
// `[[name]]` wikilink resolves to the named document; two docs sharing a name are
// Ambiguous; an `aliases:`-only doc still resolves (regression).
func TestResolveWikilinkViaName(t *testing.T) {
	c := NewCorpus()
	mustAdd := func(id string, fm FrontMatter) {
		t.Helper()
		if err := c.Add(&Document{ID: identity.DocumentID(id), FrontMatter: fm}); err != nil {
			t.Fatal(err)
		}
	}
	mustAdd("origin.md", FrontMatter{})
	mustAdd("target.md", FrontMatter{Name: "foo-bar"})
	mustAdd("aliased.md", FrontMatter{Aliases: []string{"legacy"}})
	mustAdd("dup-a.md", FrontMatter{Name: "dup"})
	mustAdd("dup-b.md", FrontMatter{Name: "dup"})

	r := reference.NewResolver(c, nil, reference.LongestSuffix)

	wl := func(target string) reference.Reference {
		return r.Resolve(reference.RawReference{
			Origin: "origin.md", RawTarget: target, Type: reference.Wikilink,
		})
	}

	if got := wl("foo-bar"); got.Health != reference.Valid || got.Target.DocumentID != "target.md" {
		t.Errorf("[[foo-bar]] = %+v, want Valid → target.md", got)
	}
	if got := wl("legacy"); got.Health != reference.Valid || got.Target.DocumentID != "aliased.md" {
		t.Errorf("[[legacy]] (aliases regression) = %+v, want Valid → aliased.md", got)
	}
	if got := wl("dup"); got.Health != reference.Ambiguous {
		t.Errorf("[[dup]] = %+v, want Ambiguous", got)
	}
}

func TestCorpus_AddNilRootNoPanic(t *testing.T) {
	c := NewCorpus()
	if err := c.Add(&Document{ID: "a.md"}); err != nil { // Root == nil
		t.Fatalf("Add with nil Root: %v", err)
	}
	if c.HeadingCount() != 0 {
		t.Errorf("HeadingCount() = %d, want 0", c.HeadingCount())
	}
}

func TestCorpus_FreezeRejectsMutation(t *testing.T) {
	c := NewCorpus()
	if err := c.Add(&Document{ID: "a.md"}); err != nil {
		t.Fatalf("Add before freeze: %v", err)
	}
	if c.Frozen() {
		t.Fatal("corpus reported frozen before Freeze()")
	}
	c.Freeze()
	if !c.Frozen() {
		t.Fatal("corpus not frozen after Freeze()")
	}
	if err := c.Add(&Document{ID: "b.md"}); err == nil {
		t.Fatal("Add after Freeze() should return ErrFrozen, got nil")
	} else if !errors.Is(err, ErrFrozen) {
		t.Fatalf("Add after Freeze() = %v, want ErrFrozen", err)
	}
	// The frozen document set is unchanged.
	if c.Len() != 1 {
		t.Fatalf("Len() = %d after rejected Add, want 1", c.Len())
	}
	// Freeze is idempotent.
	c.Freeze()
	// Every mutator returns ErrFrozen on a frozen corpus (consistent contract:
	// Add, AddHeading and AddAlias all return the error rather than splitting on
	// panic vs. return).
	for _, mut := range []struct {
		name string
		fn   func() error
	}{
		{"AddHeading", func() error { return c.AddHeading("a.md", "x") }},
		{"AddAlias", func() error { return c.AddAlias("y", "a.md") }},
	} {
		if err := mut.fn(); !errors.Is(err, ErrFrozen) {
			t.Errorf("%s on frozen corpus: err = %v, want ErrFrozen", mut.name, err)
		}
	}
}
