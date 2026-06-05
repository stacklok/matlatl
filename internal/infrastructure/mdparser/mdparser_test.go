package mdparser

import (
	"context"
	"strings"
	"testing"

	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/reference"
)

func parse(t *testing.T, src string) *corpus.Document {
	t.Helper()
	p := New(Config{})
	doc, err := p.ParseBytes(context.Background(), "test.md", []byte(src))
	if err != nil {
		t.Fatalf("ParseBytes error: %v", err)
	}
	if doc == nil {
		t.Fatal("ParseBytes returned a nil document")
		return nil // unreachable, but makes the non-nil guarantee explicit for linters
	}
	return doc
}

func TestFrontMatter_YAML(t *testing.T) {
	doc := parse(t, "---\ntitle: Hello\ntags:\n  - a\n  - b\nparent: root.md\nunknown: 42\n---\n\n# Body\n")
	fm := doc.FrontMatter
	if fm.Title != "Hello" {
		t.Errorf("Title = %q, want Hello", fm.Title)
	}
	if len(fm.Tags) != 2 || fm.Tags[0] != "a" {
		t.Errorf("Tags = %v, want [a b]", fm.Tags)
	}
	if fm.Parent != "root.md" {
		t.Errorf("Parent = %q, want root.md", fm.Parent)
	}
	if v, ok := fm.Extra["unknown"]; !ok || v == nil {
		t.Errorf("Extra[unknown] missing; Extra = %v", fm.Extra)
	}
}

func TestFrontMatter_TOML(t *testing.T) {
	doc := parse(t, "+++\ntitle = \"Toml Doc\"\nstatus = \"draft\"\ntags = [\"x\"]\n+++\n\n# Body\n")
	fm := doc.FrontMatter
	if fm.Title != "Toml Doc" {
		t.Errorf("Title = %q, want Toml Doc", fm.Title)
	}
	if fm.Status != "draft" {
		t.Errorf("Status = %q, want draft", fm.Status)
	}
	if len(fm.Tags) != 1 || fm.Tags[0] != "x" {
		t.Errorf("Tags = %v, want [x]", fm.Tags)
	}
}

func TestTitleFallbackToH1(t *testing.T) {
	doc := parse(t, "# The Real Title\n\nNo front matter present.\n")
	if doc.FrontMatter.Title != "The Real Title" {
		t.Errorf("Title fallback = %q, want 'The Real Title'", doc.FrontMatter.Title)
	}
}

func TestFrontMatter_MalformedDegrades(t *testing.T) {
	// Broken YAML must not fail the parse; it degrades to no front matter, and
	// title falls back to the H1.
	doc := parse(t, "---\ntitle: [unterminated\n  bad: : :\n---\n\n# Fallback Heading\n")
	if doc == nil {
		t.Fatal("parse returned nil on malformed front matter")
	}
	if doc.FrontMatter.Title != "Fallback Heading" {
		t.Errorf("malformed FM should degrade; Title = %q, want H1 fallback", doc.FrontMatter.Title)
	}
}

func TestFrontMatter_OversizedGuarded(t *testing.T) {
	// A front-matter block over the cap must be stripped (never decoded), so the
	// title is NOT taken from it; it falls back to the H1.
	var b strings.Builder
	b.WriteString("---\ntitle: ShouldBeIgnored\npadding: \"")
	b.WriteString(strings.Repeat("x", 200))
	b.WriteString("\"\n---\n\n# Real H1\n")
	p := New(Config{MaxFrontMatterBytes: 64}) // tiny cap forces the guard
	doc, err := p.ParseBytes(context.Background(), "t.md", []byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if doc.FrontMatter.Title == "ShouldBeIgnored" {
		t.Error("oversized front matter was decoded; guard failed")
	}
	if doc.FrontMatter.Title != "Real H1" {
		t.Errorf("Title = %q, want H1 fallback 'Real H1'", doc.FrontMatter.Title)
	}
}

func TestSectionTreeShape(t *testing.T) {
	doc := parse(t, "# A\n\n## B\n\n### C\n\n## D\n\n# E\n")
	root := doc.Root
	if len(root.Children) != 2 {
		t.Fatalf("root has %d top-level headings, want 2 (A, E)", len(root.Children))
	}
	a := root.Children[0]
	if a.Text != "A" || a.Level != 1 {
		t.Errorf("first heading = %q L%d, want A L1", a.Text, a.Level)
	}
	if len(a.Children) != 2 {
		t.Fatalf("A has %d children, want 2 (B, D)", len(a.Children))
	}
	b := a.Children[0]
	if b.Text != "B" || len(b.Children) != 1 || b.Children[0].Text != "C" {
		t.Errorf("B subtree wrong: %q children=%d", b.Text, len(b.Children))
	}
	if a.Children[1].Text != "D" {
		t.Errorf("A second child = %q, want D", a.Children[1].Text)
	}
	if root.Children[1].Text != "E" {
		t.Errorf("second top heading = %q, want E", root.Children[1].Text)
	}
	// Parent links.
	if b.Parent != a {
		t.Error("B.Parent should be A")
	}
}

// TestSlugParity pins the canonical slug dialect (ADR 0006): goldmark's
// auto-heading-id output. A divergence is a test failure, not a silent FP.
func TestSlugParity(t *testing.T) {
	tests := []struct {
		name    string
		heading string
		want    string
	}{
		{"simple", "Getting Started", "getting-started"},
		{"punctuation", "Hello, World!", "hello-world"},
		{"ampersand", "Cats & Dogs", "cats--dogs"},
		// goldmark's WithAutoHeadingID is GitHub-compatible and ASCII-folding:
		// non-ASCII letters are dropped. This is exactly the canonical dialect
		// ADR 0006 pins; we assert the real output (no silent false positives).
		{"unicode", "Café Crème", "caf-crme"},
		{"emoji", "🚀 Launch Day", "-launch-day"},
		{"mixedcase", "MixedCase Heading", "mixedcase-heading"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parse(t, "# "+tt.heading+"\n")
			got := doc.Root.Children[0].Slug
			if got != tt.want {
				t.Errorf("slug for %q = %q, want %q", tt.heading, got, tt.want)
			}
		})
	}
}

func TestSlugParity_DuplicateHeadings(t *testing.T) {
	doc := parse(t, "# Dup\n\n# Dup\n\n# Dup\n")
	want := []string{"dup", "dup-1", "dup-2"}
	if len(doc.Root.Children) != 3 {
		t.Fatalf("got %d headings, want 3", len(doc.Root.Children))
	}
	for i, w := range want {
		if got := doc.Root.Children[i].Slug; got != w {
			t.Errorf("duplicate heading %d slug = %q, want %q", i, got, w)
		}
	}
}

func TestReferenceExtraction(t *testing.T) {
	src := "# Title\n" +
		"\n" +
		"A [relative](other.md) link.\n" +
		"An [anchored](other.md#sec) link.\n" +
		"A [same-page](#title) anchor.\n" +
		"An [external](https://example.com) link.\n" +
		"An ![image](img/pic.png) embed.\n" +
		"An autolink <https://auto.example>.\n"
	doc := parse(t, src)

	type want struct {
		typ  reference.LinkType
		tgt  string
		frag string
		line int
	}
	wants := []want{
		{reference.RelativeLink, "other.md", "", 3},
		{reference.RelativeLink, "other.md", "sec", 4},
		{reference.Anchor, "", "title", 5},
		{reference.External, "https://example.com", "", 6},
		{reference.ImageEmbed, "img/pic.png", "", 7},
		{reference.External, "https://auto.example", "", 8},
	}
	if len(doc.RawReferences) != len(wants) {
		t.Fatalf("got %d refs, want %d: %+v", len(doc.RawReferences), len(wants), doc.RawReferences)
	}
	for i, w := range wants {
		r := doc.RawReferences[i]
		if r.Type != w.typ || r.RawTarget != w.tgt || r.Fragment != w.frag {
			t.Errorf("ref %d = {%s %q #%q}, want {%s %q #%q}",
				i, r.Type, r.RawTarget, r.Fragment, w.typ, w.tgt, w.frag)
		}
		if r.Line != w.line {
			t.Errorf("ref %d line = %d, want %d", i, r.Line, w.line)
		}
		if r.Origin != "test.md" {
			t.Errorf("ref %d origin = %q, want test.md", i, r.Origin)
		}
	}
}

func TestSlugsIndexedIntoCorpus(t *testing.T) {
	doc := parse(t, "# One\n\n## Two\n")
	c := corpus.NewCorpus()
	if err := c.Add(doc); err != nil {
		t.Fatal(err)
	}
	if !c.HasHeading("test.md", "one") || !c.HasHeading("test.md", "two") {
		t.Error("heading slugs were not indexed into the corpus on Add")
	}
}

func TestFrontMatter_AliasesAndRelatedExtracted(t *testing.T) {
	doc := parse(t, "---\ntitle: T\naliases:\n  - home\n  - index\nrelated:\n  - other.md\n---\n\n# Body\n")
	fm := doc.FrontMatter
	if len(fm.Aliases) != 2 || fm.Aliases[0] != "home" || fm.Aliases[1] != "index" {
		t.Errorf("Aliases = %v, want [home index]", fm.Aliases)
	}
	if len(fm.Related) != 1 || fm.Related[0] != "other.md" {
		t.Errorf("Related = %v, want [other.md]", fm.Related)
	}
}

func TestFrontMatter_MalformedTOMLDegrades(t *testing.T) {
	// Broken TOML must degrade to no front matter (title falls back to H1).
	doc := parse(t, "+++\ntitle = \"unterminated\nbad ==== value\n+++\n\n# TOML Fallback\n")
	if doc.FrontMatter.Title != "TOML Fallback" {
		t.Errorf("malformed TOML should degrade; Title = %q, want H1 fallback", doc.FrontMatter.Title)
	}
}

func TestExternalImageClassified(t *testing.T) {
	// An image whose destination is an external URL is still an image embed by
	// node type, but reclassified External by destination scheme.
	doc := parse(t, "# T\n\n![remote](https://cdn.example.com/p.png)\n")
	if len(doc.RawReferences) != 1 {
		t.Fatalf("got %d refs, want 1", len(doc.RawReferences))
	}
	r := doc.RawReferences[0]
	if r.Type != reference.External {
		t.Errorf("external image type = %s, want external", r.Type)
	}
	if r.RawTarget != "https://cdn.example.com/p.png" {
		t.Errorf("target = %q", r.RawTarget)
	}
}

func TestLocalImageClassified(t *testing.T) {
	doc := parse(t, "# T\n\n![local](assets/p.png)\n")
	if len(doc.RawReferences) != 1 || doc.RawReferences[0].Type != reference.ImageEmbed {
		t.Errorf("local image refs = %+v, want one image-embed", doc.RawReferences)
	}
}

// TestExternalSchemeClassification asserts the scheme-based external
// classification (ADR 0003). file:// and data: must classify as External so they
// are never treated as in-corpus relative paths (a latent SSRF / local-file-read
// hazard for the opt-in P6 --check-external path) — alongside the usual http(s),
// mailto, ftp and protocol-relative schemes.
func TestExternalSchemeClassification(t *testing.T) {
	tests := []struct {
		name string
		dest string
		want reference.LinkType
	}{
		{"http", "http://example.com", reference.External},
		{"https", "https://example.com", reference.External},
		{"mailto", "mailto:a@b.com", reference.External},
		{"ftp", "ftp://host/f", reference.External},
		{"protocol-relative", "//cdn.example.com/x", reference.External},
		{"file scheme", "file:///etc/passwd", reference.External},
		{"file scheme uppercase", "FILE://localhost/etc/passwd", reference.External},
		{"data uri", "data:text/plain;base64,SGk=", reference.External},
		{"local relative still in-corpus", "docs/page.md", reference.RelativeLink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := parse(t, "# T\n\n[x]("+tt.dest+")\n")
			if len(doc.RawReferences) != 1 {
				t.Fatalf("got %d refs, want 1", len(doc.RawReferences))
			}
			if got := doc.RawReferences[0].Type; got != tt.want {
				t.Errorf("%q classified as %s, want %s", tt.dest, got, tt.want)
			}
		})
	}
}

func TestGuardFrontMatter_Branches(t *testing.T) {
	tests := []struct {
		name       string
		src        string
		maxBytes   int
		wantStrip  bool
		wantInBody string // substring that must remain in the returned source
	}{
		{
			name:       "no front matter",
			src:        "# Just a heading\n",
			maxBytes:   64,
			wantStrip:  false,
			wantInBody: "# Just a heading",
		},
		{
			name:       "small yaml not stripped",
			src:        "---\ntitle: x\n---\n\n# H\n",
			maxBytes:   64 << 10,
			wantStrip:  false,
			wantInBody: "title: x",
		},
		{
			name:       "oversized yaml stripped",
			src:        "---\ntitle: x\npad: \"" + strings.Repeat("y", 200) + "\"\n---\n\n# H\n",
			maxBytes:   32,
			wantStrip:  true,
			wantInBody: "# H",
		},
		{
			name:       "oversized toml stripped",
			src:        "+++\ntitle = \"x\"\npad = \"" + strings.Repeat("z", 200) + "\"\n+++\n\n# H\n",
			maxBytes:   32,
			wantStrip:  true,
			wantInBody: "# H",
		},
		{
			name:       "unterminated yaml not stripped",
			src:        "---\ntitle: x\nno closing fence here\n# H\n",
			maxBytes:   8,
			wantStrip:  false,
			wantInBody: "title: x",
		},
		{
			name:       "crlf yaml fence detected",
			src:        "---\r\ntitle: x\r\npad: \"" + strings.Repeat("y", 200) + "\"\r\n---\r\n\r\n# H\r\n",
			maxBytes:   32,
			wantStrip:  true,
			wantInBody: "# H",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, stripped := guardFrontMatter([]byte(tt.src), tt.maxBytes)
			if stripped != tt.wantStrip {
				t.Errorf("stripped = %v, want %v", stripped, tt.wantStrip)
			}
			if !strings.Contains(string(out), tt.wantInBody) {
				t.Errorf("body %q does not contain %q", out, tt.wantInBody)
			}
			if tt.wantStrip && strings.Contains(string(out), "pad") {
				t.Errorf("stripped output still contains front-matter padding: %q", out)
			}
		})
	}
}
