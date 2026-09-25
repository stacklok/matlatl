package mdparser

import (
	"context"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

func TestDocusaurusExplicitHeadingIDs(t *testing.T) {
	doc := parse(t, "## Classic title {#classic-id}\n\n## Comment title {/* #comment-id */}\n\n## Plain title\n")
	sections := doc.Root.Children
	if len(sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(sections))
	}
	for i, want := range []struct{ text, slug string }{
		{"Classic title", "classic-id"},
		{"Comment title", "comment-id"},
		{"Plain title", "plain-title"},
	} {
		if sections[i].Text != want.text || sections[i].Slug != want.slug {
			t.Errorf("section %d = (%q, %q), want (%q, %q)", i, sections[i].Text, sections[i].Slug, want.text, want.slug)
		}
	}
	c := corpus.NewCorpus()
	if err := c.Add(doc); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"classic-id", "comment-id"} {
		if !c.HasHeading("test.md", id) {
			t.Errorf("explicit heading ID %q was not indexed", id)
		}
	}
	if c.HasHeading("test.md", "classic-title-classic-id") || c.HasHeading("test.md", "comment-title-comment-id") {
		t.Error("automatic heading slugs must not be accepted when an explicit ID replaces them")
	}
}

func TestDocusaurusStaticHeadingAnchorIDs(t *testing.T) {
	src := "import Heading from '@theme/Heading';\n\n<Heading className=\"x\"\n id='api-widget-class'>\n<code>Widget</code>\n</Heading>\n\n<Heading id=\"second\" data-x={value} />\n"
	doc := parse(t, src)
	if len(doc.AnchorIDs) != 2 || doc.AnchorIDs[0] != "api-widget-class" || doc.AnchorIDs[1] != "second" {
		t.Fatalf("AnchorIDs = %q, want [api-widget-class second]", doc.AnchorIDs)
	}
	if len(doc.Root.Children) != 0 {
		t.Errorf("literal Heading anchors must not create section nodes: %+v", doc.Root.Children)
	}
	c := corpus.NewCorpus()
	if err := c.Add(doc); err != nil {
		t.Fatal(err)
	}
	if !c.HasHeading("test.md", "api-widget-class") || !c.HasHeading("test.md", "second") {
		t.Error("literal Heading IDs were not indexed")
	}
}

func TestDocusaurusStaticHeadingAnchorResolvesAsDocumentTarget(t *testing.T) {
	p := New(Config{})
	ctx := context.Background()
	target, err := p.ParseBytes(ctx, "target.md", []byte("# Section Slug\n\n<Heading id=\"literal-anchor\" />\n"))
	if err != nil {
		t.Fatal(err)
	}
	origin, err := p.ParseBytes(ctx, "origin.md", []byte("[literal](target.md#literal-anchor)\n[section](target.md#section-slug)\n"))
	if err != nil {
		t.Fatal(err)
	}

	catalog := corpus.NewCorpus()
	for _, doc := range []*corpus.Document{origin, target} {
		if err := catalog.Add(doc); err != nil {
			t.Fatal(err)
		}
	}
	catalog.Freeze()
	resolved := reference.NewResolver(catalog, nil, reference.LongestSuffix).ResolveAll(origin.RawReferences)
	if len(resolved) != 2 {
		t.Fatalf("resolved references = %d, want 2", len(resolved))
	}
	for i, want := range []struct {
		anchor string
		kind   reference.TargetKind
	}{
		{"literal-anchor", reference.TargetDocument},
		{"section-slug", reference.TargetSection},
	} {
		got := resolved[i]
		if got.Health != reference.Valid || got.Target.Kind != want.kind || got.Target.DocumentID != "target.md" || got.Target.Anchor != want.anchor {
			t.Errorf("resolved[%d] = %+v, want valid %s target.md#%s", i, got, want.kind, want.anchor)
		}
	}
}

func TestDocusaurusAnchorNegativeControls(t *testing.T) {
	src := "---\ntitle: <Heading id=\"front-matter\" />\n---\n\n{/* <Heading id=\"jsx-comment\" /> */}\n<!-- <Heading id=\"html-comment\" /> -->\n\n## Bad {#bad id}\n## Also bad {/* #bad id */}\n\n<Component id=\"component\" />\n<Heading id={dynamic} />\n<Heading id=unquoted />\n<Heading id=\"unterminated>\n\n`<Heading id=\"inline\" />`\n\n    <Heading id=\"indented\" />\n\n```mdx\n<Heading id=\"fenced\" />\n```\n\n<Heading id=\"live\" />\n"
	doc := parse(t, src)
	for _, section := range doc.Root.Children[:2] {
		if section.Slug == "bad" || section.Text == "Bad" || section.Text == "Also bad" {
			t.Errorf("malformed explicit syntax must remain ordinary heading text/slug: %+v", section)
		}
	}
	if len(doc.AnchorIDs) != 1 || doc.AnchorIDs[0] != "live" {
		t.Errorf("negative component controls produced anchors: %q, want [live]", doc.AnchorIDs)
	}
}

func TestDocusaurusStaticHeadingAnchorBlockOnly(t *testing.T) {
	src := "<Heading\n  className=\"x\"\n  id='multiline'\n/>\n   <Heading id=\"indented-three\" />\ntext <Heading id=\"prose\" />\nconst text = \"<Heading id='string' />\"\nconst template = `\n<Heading id=\"template\" />\n`\n`<Heading id=\"inline\" />`\n<!--\n<Heading id=\"html-comment\" />\n-->\n{/* comment */ }\n<Heading id=\"live\" />\n"
	doc := parse(t, src)
	want := []string{"multiline", "indented-three", "live"}
	if len(doc.AnchorIDs) != len(want) {
		t.Fatalf("AnchorIDs = %q, want %q", doc.AnchorIDs, want)
	}
	for i, id := range want {
		if doc.AnchorIDs[i] != id {
			t.Errorf("AnchorIDs[%d] = %q, want %q", i, doc.AnchorIDs[i], id)
		}
	}
}

func TestDocusaurusStaticHeadingAnchorSkipsMultilineLiterals(t *testing.T) {
	src := "const single = '\n<Heading id=\"single-string\" />\n';\nconst double = \"\n<Heading id='double-string' />\n\";\n``\n<Heading id=\"double-backtick-code\" />\n``\ncode ```\n<Heading id=\"triple-backtick-code\" />\n```\n<Heading id=\"live\" />\n"
	doc := parse(t, src)
	if len(doc.AnchorIDs) != 1 || doc.AnchorIDs[0] != "live" {
		t.Errorf("AnchorIDs = %q, want [live]", doc.AnchorIDs)
	}
}

func TestDocusaurusStaticHeadingAnchorSuppressesMultilineJavaScriptStrings(t *testing.T) {
	for _, prefix := range []string{"const value =", "let value =", "var value =", "export default", "import"} {
		t.Run(prefix, func(t *testing.T) {
			doc := parse(t, prefix+" '\n<Heading id=\"hidden\" />\n';\n<Heading id=\"live\" />\n")
			if len(doc.AnchorIDs) != 1 || doc.AnchorIDs[0] != "live" {
				t.Errorf("AnchorIDs = %q, want [live]", doc.AnchorIDs)
			}
		})
	}
}

func TestDocusaurusStaticHeadingAnchorDoesNotTreatProseQuotesAsStrings(t *testing.T) {
	src := "Here's the API.\nA quoted thought: \"use the API.\n<Heading id=\"after-prose\" />\n"
	doc := parse(t, src)
	if len(doc.AnchorIDs) != 1 || doc.AnchorIDs[0] != "after-prose" {
		t.Errorf("AnchorIDs = %q, want [after-prose]", doc.AnchorIDs)
	}
}

func TestDocusaurusStaticHeadingAnchorTracksTagLineTrailingContext(t *testing.T) {
	src := "<Heading id=\"accepted\" /> {/*\n<Heading id=\"hidden\" />\n*/}\n<Heading id=\"live\" />\n"
	doc := parse(t, src)
	want := []string{"accepted", "live"}
	if len(doc.AnchorIDs) != len(want) {
		t.Fatalf("AnchorIDs = %q, want %q", doc.AnchorIDs, want)
	}
	for i, id := range want {
		if doc.AnchorIDs[i] != id {
			t.Errorf("AnchorIDs[%d] = %q, want %q", i, doc.AnchorIDs[i], id)
		}
	}
}

func TestDocusaurusStaticHeadingAnchorMalformedAndFenced(t *testing.T) {
	src := "---\ntitle: <Heading id=\"front-matter\" />\n---\n```mdx\n<Heading id=\"fenced\" />\n``` not a close\n<Heading id=\"still-fenced\" />\n```\n<Heading id={dynamic} />\n<Heading id=unquoted />\n<Heading id=\"unterminated>\n<Heading x={\n<Heading x={\n<Heading x={\n<Heading id=\"live\" />\n"
	doc := parse(t, src)
	if len(doc.AnchorIDs) != 1 || doc.AnchorIDs[0] != "live" {
		t.Errorf("AnchorIDs = %q, want [live]", doc.AnchorIDs)
	}
}

func TestDocusaurusStaticHeadingAnchorRepeatedMalformedInput(t *testing.T) {
	doc := parse(t, strings.Repeat("<Heading x={\n", 1024)+"<Heading id=\"live\" />\n")
	if len(doc.AnchorIDs) != 1 || doc.AnchorIDs[0] != "live" {
		t.Errorf("AnchorIDs = %q, want [live]", doc.AnchorIDs)
	}
	comment := parse(t, "{/* unmatched comment\n"+strings.Repeat("<Heading x={\n", 1024)+"<Heading id=\"hidden\" />\n")
	if len(comment.AnchorIDs) != 0 {
		t.Errorf("unclosed JSX comment produced anchors: %q", comment.AnchorIDs)
	}
}
