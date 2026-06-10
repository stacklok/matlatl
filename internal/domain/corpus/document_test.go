package corpus

import "testing"

// headedDoc builds a Document with a small section tree:
//
//	# Setup (setup)
//	  ## Install (install)
//	  ## Setup (setup-1)   ← duplicate heading, suffixed slug (ADR 0006)
//	# Usage (usage)
func headedDoc() *Document {
	root := &Section{Level: 0, StartLine: 1, EndLine: 100}
	setup := &Section{Level: 1, Text: "Setup", Slug: "setup", Parent: root, StartLine: 1, EndLine: 50}
	install := &Section{Level: 2, Text: "Install", Slug: "install", Parent: setup, StartLine: 5, EndLine: 20}
	setupDup := &Section{Level: 2, Text: "Setup", Slug: "setup-1", Parent: setup, StartLine: 21, EndLine: 50}
	usage := &Section{Level: 1, Text: "Usage", Slug: "usage", Parent: root, StartLine: 51, EndLine: 100}
	setup.Children = []*Section{install, setupDup}
	root.Children = []*Section{setup, usage}
	return &Document{ID: "doc.md", Root: root}
}

// TestDocument_HeadingTextBySlug covers the lookup the information-scent
// analysis depends on (ADR 0016): present, absent, nested, and
// duplicate-suffixed slugs, plus the nil/degenerate guards.
func TestDocument_HeadingTextBySlug(t *testing.T) {
	d := headedDoc()
	cases := []struct {
		name     string
		slug     string
		wantText string
		wantOK   bool
	}{
		{"top-level present", "setup", "Setup", true},
		{"nested present", "install", "Install", true},
		{"duplicate-suffixed slug", "setup-1", "Setup", true},
		{"later top-level present", "usage", "Usage", true},
		{"absent", "missing", "", false},
		{"empty slug", "", "", false},
	}
	for _, tc := range cases {
		got, ok := d.HeadingTextBySlug(tc.slug)
		if got != tc.wantText || ok != tc.wantOK {
			t.Errorf("%s: HeadingTextBySlug(%q) = (%q, %v), want (%q, %v)",
				tc.name, tc.slug, got, ok, tc.wantText, tc.wantOK)
		}
	}

	var nilDoc *Document
	if _, ok := nilDoc.HeadingTextBySlug("setup"); ok {
		t.Error("nil document must not resolve a slug")
	}
	if _, ok := (&Document{ID: "x.md"}).HeadingTextBySlug("setup"); ok {
		t.Error("document with nil Root must not resolve a slug")
	}
}
