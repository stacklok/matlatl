package mdparser

import (
	"context"
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/reference"
)

// gotMention is the comparable projection of a RawMention used by these tests.
type gotMention struct {
	Kind   reference.MentionKind
	Target string
	Text   string
	Line   int
}

func parseMentions(t *testing.T, prefixes []string, src string) []gotMention {
	t.Helper()
	p := New(Config{InvocationPrefixes: prefixes})
	doc, err := p.ParseBytes(context.Background(), "docs/a.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]gotMention, 0, len(doc.RawMentions))
	for _, m := range doc.RawMentions {
		if m.Origin != "docs/a.md" {
			t.Errorf("origin = %q", m.Origin)
		}
		out = append(out, gotMention{Kind: m.Kind, Target: m.Target, Text: m.Text, Line: m.Line})
	}
	return out
}

func TestExtractMentions(t *testing.T) {
	t.Parallel()
	path, file, inv := reference.MentionPath, reference.MentionFilename, reference.MentionInvocation
	tests := []struct {
		name string
		src  string
		want []gotMention
	}{
		{
			name: "path in a code span",
			src:  "Per `.claude/rules/metrics.md`: dotted names.\n",
			want: []gotMention{{path, ".claude/rules/metrics.md", ".claude/rules/metrics.md", 1}},
		},
		{
			name: "path in plain text with sentence punctuation",
			src:  "Intro.\n\nSee docs/guide.md. Also (../other.md), and /docs/root.md!\n",
			want: []gotMention{
				{path, "docs/guide.md", "docs/guide.md", 3},
				{path, "../other.md", "../other.md", 3},
				{path, "/docs/root.md", "/docs/root.md", 3},
			},
		},
		{
			name: "trailing-slash directory",
			src:  "Skills live under `.claude/skills/foo/`.\n",
			want: []gotMention{{path, ".claude/skills/foo/", ".claude/skills/foo/", 1}},
		},
		{
			name: "bare file name in a code span",
			src:  "see `frontdoor.md § V1 hardcoded`\n",
			want: []gotMention{{file, "frontdoor.md", "frontdoor.md", 1}},
		},
		{
			name: "file name with underscores split by goldmark",
			src:  "read my_notes_file.md now\n",
			want: []gotMention{{file, "my_notes_file.md", "my_notes_file.md", 1}},
		},
		{
			name: "fragment is not part of the token",
			src:  "see metrics.md#units\n",
			want: []gotMention{{file, "metrics.md", "metrics.md", 1}},
		},
		{
			name: "invocations in code spans and plain text",
			src:  "Use the `triage-cve` skill (`/triage-cve CVE-YYYY-NNNNN`).\n\nRun it with /panel-review before pushing.\n",
			want: []gotMention{
				{inv, "triage-cve", "/triage-cve", 1},
				{inv, "panel-review", "/panel-review", 3},
			},
		},
		{
			name: "invocation in parentheses and with trailing punctuation",
			src:  "(/panel-review) then /ship.\n",
			want: []gotMention{
				{inv, "panel-review", "/panel-review", 1},
				{inv, "ship", "/ship", 1},
			},
		},
		{
			name: "URL and path preceding the prefix do not match",
			src:  "https://host/panel-review and docs/x/panel-review and //panel-review\n",
			want: []gotMention{{path, "docs/x/panel-review", "docs/x/panel-review", 1}},
		},
		{
			name: "invocation followed by a slash is a path",
			src:  "/panel-review/notes.md\n",
			want: []gotMention{{path, "/panel-review/notes.md", "/panel-review/notes.md", 1}},
		},
		{
			name: "URLs never yield mentions",
			src:  "https://example.com/docs/a.md and www.example.com/b.md and mailto:x@y.md\n",
		},
		{
			name: "fenced and indented code blocks are skipped",
			src:  "```\nsee docs/guide.md and /panel-review\n```\n\n    docs/indented.md\n",
		},
		{
			name: "text inside link syntax is skipped",
			src:  "[docs/guide.md](docs/guide.md) and ![metrics.md](img.png) and [[metrics.md]] and <https://x/y.md>\n",
		},
		{
			name: "reference-link definition and label are skipped",
			src:  "See [the guide][g].\n\n[g]: docs/guide.md\n",
		},
		{
			name: "raw HTML is skipped",
			src:  "<!-- docs/guide.md -->\n\ntext <span title=\"docs/x.md\">x</span>\n",
		},
		{
			name: "non-markdown and dot-only tokens are not mentions",
			src:  "Taskfile.yml, ./ and ../ and .md and v1.2\n",
		},
		{
			name: "heading text is scanned",
			src:  "# About docs/guide.md\n",
			want: []gotMention{{path, "docs/guide.md", "docs/guide.md", 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseMentions(t, []string{"/"}, tt.src)
			if !slices.Equal(got, tt.want) {
				t.Errorf("mentions\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

func TestExtractMentions_NoPrefixesNoInvocations(t *testing.T) {
	t.Parallel()
	got := parseMentions(t, nil, "run /panel-review and read docs/guide.md\n")
	want := []gotMention{{reference.MentionPath, "docs/guide.md", "docs/guide.md", 1}}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestExtractMentions_CustomPrefix(t *testing.T) {
	t.Parallel()
	got := parseMentions(t, []string{"@"}, "ask @code-reviewer, not user@host.example\n")
	want := []gotMention{{reference.MentionInvocation, "code-reviewer", "@code-reviewer", 1}}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestExtractMentions_LinksUnchanged(t *testing.T) {
	t.Parallel()
	p := New(Config{InvocationPrefixes: []string{"/"}})
	src := "See [guide](docs/guide.md) and docs/other.md.\n"
	doc, err := p.ParseBytes(context.Background(), "a.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.RawReferences) != 1 || doc.RawReferences[0].RawTarget != "docs/guide.md" {
		t.Fatalf("references = %+v, want one link to docs/guide.md", doc.RawReferences)
	}
	if len(doc.RawMentions) != 1 || doc.RawMentions[0].Target != "docs/other.md" {
		t.Fatalf("mentions = %+v, want one mention of docs/other.md", doc.RawMentions)
	}
}
