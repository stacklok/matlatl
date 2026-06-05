package mdparser

import (
	"testing"

	"github.com/stacklok/matlatl/internal/domain/reference"
)

// wikiRefs parses src and returns only the wikilink/transclusion/anchor refs the
// custom inline parser produced (filtering out standard markdown links so the
// table focuses on wikilink behavior).
func wikiRefs(t *testing.T, src string) []reference.RawReference {
	t.Helper()
	doc := parse(t, src)
	var out []reference.RawReference
	for _, r := range doc.RawReferences {
		switch r.Type {
		case reference.Wikilink, reference.Transclusion, reference.Anchor:
			out = append(out, r)
		}
	}
	return out
}

func TestWikilinkParser_Forms(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantType reference.LinkType
		target   string
		fragment string
	}{
		{"plain", "x [[guide]] y", reference.Wikilink, "guide", ""},
		{"nested-path", "[[docs/guide]]", reference.Wikilink, "docs/guide", ""},
		{"with-ext", "[[guide.md]]", reference.Wikilink, "guide.md", ""},
		{"aliased", "[[guide|the guide]]", reference.Wikilink, "guide", ""},
		{"fragment", "[[guide#setup]]", reference.Wikilink, "guide", "setup"},
		{"fragment-and-alias", "[[guide#setup|See setup]]", reference.Wikilink, "guide", "setup"},
		{"embed", "![[guide]]", reference.Transclusion, "guide", ""},
		{"embed-fragment", "![[guide#setup]]", reference.Transclusion, "guide", "setup"},
		{"anchor-only", "[[#self]]", reference.Anchor, "", "self"},
		{"trim-spaces", "[[  guide  #  setup  ]]", reference.Wikilink, "guide", "setup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := wikiRefs(t, "# H\n\n"+tt.src+"\n")
			if len(refs) != 1 {
				t.Fatalf("got %d wikilink refs, want 1: %+v", len(refs), refs)
			}
			r := refs[0]
			if r.Type != tt.wantType {
				t.Errorf("type = %s, want %s", r.Type, tt.wantType)
			}
			if r.RawTarget != tt.target {
				t.Errorf("target = %q, want %q", r.RawTarget, tt.target)
			}
			if r.Fragment != tt.fragment {
				t.Errorf("fragment = %q, want %q", r.Fragment, tt.fragment)
			}
		})
	}
}

func TestWikilinkParser_Malformed(t *testing.T) {
	// None of these should produce a wikilink ref, and none should panic.
	malformed := []string{
		"[[",           // unterminated opener
		"[[]]",         // empty
		"[[a|",         // unclosed with pipe
		"[[ and [[]]",  // stray opener swallowed -> rejected
		"[single]",     // normal bracket, not a wikilink
		"text [[ more", // opener, no closer on line
		"[[|display]]", // empty target with display
		"a ] b [[ c",   // brackets but no valid pair
	}
	for _, src := range malformed {
		t.Run(src, func(t *testing.T) {
			refs := wikiRefs(t, "# H\n\n"+src+"\n")
			if len(refs) != 0 {
				t.Errorf("malformed %q produced %d wikilink refs, want 0: %+v", src, len(refs), refs)
			}
		})
	}
}

func TestWikilinkParser_DoesNotCrossLine(t *testing.T) {
	// An opener on one line and a closer on the next must NOT match.
	refs := wikiRefs(t, "# H\n\n[[guide\nstuff]]\n")
	if len(refs) != 0 {
		t.Errorf("wikilink spanning a line boundary matched: %+v", refs)
	}
}

func TestWikilinkParser_MultiplePerLine(t *testing.T) {
	refs := wikiRefs(t, "# H\n\n[[a]] and [[b]] and ![[c]]\n")
	if len(refs) != 3 {
		t.Fatalf("got %d refs, want 3: %+v", len(refs), refs)
	}
	if refs[0].RawTarget != "a" || refs[1].RawTarget != "b" || refs[2].RawTarget != "c" {
		t.Errorf("targets = %q/%q/%q, want a/b/c", refs[0].RawTarget, refs[1].RawTarget, refs[2].RawTarget)
	}
	if refs[2].Type != reference.Transclusion {
		t.Errorf("third ref type = %s, want transclusion", refs[2].Type)
	}
}

func TestWikilinkParser_StandardLinksStillWork(t *testing.T) {
	// Registering the wikilink parser must not break CommonMark links/images.
	doc := parse(t, "# H\n\n[std](other.md) and ![img](x.png)\n")
	var rel, img int
	for _, r := range doc.RawReferences {
		switch r.Type {
		case reference.RelativeLink:
			rel++
		case reference.ImageEmbed:
			img++
		}
	}
	if rel != 1 || img != 1 {
		t.Errorf("standard links broken: rel=%d img=%d, want 1/1", rel, img)
	}
}

func TestWikilinkParser_LineNumbers(t *testing.T) {
	refs := wikiRefs(t, "# H\n\nfirst\n\n[[onLine5]]\n")
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1", len(refs))
	}
	if refs[0].Line != 5 {
		t.Errorf("line = %d, want 5", refs[0].Line)
	}
}
