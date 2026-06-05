package corpus

import "testing"

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
	c.AddHeading("a.md", "intro")
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
	c.AddAlias("foo", "x/foo.md")
	c.AddAlias("foo", "y/foo.md")
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
