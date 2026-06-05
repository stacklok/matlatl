package llmstxt

import (
	"strings"
	"testing"

	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
)

// TestLinkText_NeutralizesBracketsNewlinesAndBackslash asserts the link-text
// sanitizer (ADR 0003 inv. 5): brackets are turned into parens, newlines into
// spaces, and a backslash — including a TRAILING one that would otherwise escape
// a following character in some parsers — is replaced with the set-minus
// look-alike, matching escapeMermaidLabel's convention.
func TestLinkText_NeutralizesBracketsNewlinesAndBackslash(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Getting Started", "Getting Started"},
		{"brackets", "a [b] c", "a (b) c"},
		{"newline", "a\nb", "a b"},
		{"trailing backslash", `foo\`, "foo∖"},
		{"backslash before bracket", `foo\]`, "foo∖)"},
		{"interior backslash", `a\b`, "a∖b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := linkText(tc.in); got != tc.want {
				t.Errorf("linkText(%q) = %q, want %q", tc.in, got, tc.want)
			}
			// The result must never contain a raw backslash (the forge vector).
			if strings.ContainsRune(linkText(tc.in), '\\') {
				t.Errorf("linkText(%q) leaked a raw backslash: %q", tc.in, linkText(tc.in))
			}
		})
	}
}

// TestAfterLine covers the first-line predicate that gates front-matter
// stripping: it returns the remainder only when the first line equals want
// (a trailing CR tolerated), else ("", false).
func TestAfterLine(t *testing.T) {
	cases := []struct {
		name     string
		in, want string
		rest     string
		ok       bool
	}{
		{"match", "---\nbody\n", "---", "body\n", true},
		{"match-crlf", "---\r\nbody\n", "---", "body\n", true},
		{"no-newline", "---", "---", "", false},
		{"first-line-differs", "title: x\n---\n", "---", "", false},
		{"prefix-not-equal", "----\nbody\n", "---", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest, ok := afterLine(tc.in, tc.want)
			if ok != tc.ok || rest != tc.rest {
				t.Errorf("afterLine(%q,%q) = (%q,%v), want (%q,%v)", tc.in, tc.want, rest, ok, tc.rest, tc.ok)
			}
		})
	}
}

// TestStripFenced covers the leading-fence block removal: a fence alone on the
// first line with a matching closing fence is removed (remainder returned), and
// anything else is left intact.
func TestStripFenced(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		fence    string
		wantRest string
		wantOK   bool
	}{
		{"yaml-block", "---\ntitle: x\n---\nbody line\n", "---", "body line\n", true},
		{"toml-block", "+++\na = 1\n+++\nbody\n", "+++", "body\n", true},
		{"no-opening-fence", "# Heading\nbody\n", "---", "", false},
		{"unterminated", "---\ntitle: x\nbody but no close\n", "---", "", false},
		{"leading-newlines-trimmed", "---\nk: v\n---\n\n\nbody\n", "---", "body\n", true},
		{"close-with-cr", "---\nk: v\n---\r\nbody\n", "---", "body\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest, ok := stripFenced(tc.in, tc.fence)
			if ok != tc.wantOK || rest != tc.wantRest {
				t.Errorf("stripFenced(%q,%q) = (%q,%v), want (%q,%v)",
					tc.in, tc.fence, rest, ok, tc.wantRest, tc.wantOK)
			}
		})
	}
}

// TestCleanBody covers the public entry: BOM stripping plus a single leading
// YAML/TOML front-matter block, with the body passed through verbatim otherwise.
func TestCleanBody(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"strips-yaml", "---\ntitle: T\n---\n# Body\n", "# Body\n"},
		{"strips-bom-then-yaml", "\ufeff" + "---\ntitle: T\n---\nx\n", "x\n"},
		{"no-front-matter-verbatim", "# Just markdown\n\ntext\n", "# Just markdown\n\ntext\n"},
		{"unterminated-left-intact", "---\nnot closed\n", "---\nnot closed\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanBody([]byte(tc.in)); got != tc.want {
				t.Errorf("cleanBody(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSortByImportance covers the documented total order directly: authority
// DESC, then in-degree DESC, then DocumentID ASC. Built to exercise BOTH
// tie-break levels (equal authority falls to in-degree; equal authority AND
// in-degree falls to the ID).
func TestSortByImportance(t *testing.T) {
	rd := func(id string, auth float64, in int) rankedDoc {
		return rankedDoc{view: emit.DocView{ID: identity.DocumentID(id)}, authority: auth, inDegree: in}
	}
	in := []rankedDoc{
		rd("z.md", 0.5, 1), // same authority as a.md, lower in-degree
		rd("a.md", 0.5, 9), // highest in-degree among the 0.5 group
		rd("m.md", 0.9, 1), // highest authority overall
		rd("b.md", 0.5, 9), // ties a.md on authority+in-degree → ID breaks (a<b)
		rd("c.md", 0.0, 0), // lowest
	}
	sortByImportance(in)
	got := make([]string, len(in))
	for i, r := range in {
		got[i] = r.view.ID.String()
	}
	want := []string{"m.md", "a.md", "b.md", "z.md", "c.md"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortByImportance order = %v, want %v", got, want)
		}
	}
}
