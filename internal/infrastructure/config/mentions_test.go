package config

import (
	"slices"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/reference"
)

// --- mentions (ADR 0026) ---

func TestLoad_MentionsInvocations(t *testing.T) {
	root := rootWithBytes(t, []byte(`version: 1
mentions:
  invocations:
    - prefix: "@"
      targets: [".claude/agents/*.md"]
    - prefix: "/"
      targets: ["**/.claude/skills/*/SKILL.md", "skills/**"]
`))
	file, notices, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(notices) != 0 {
		t.Errorf("valid mentions block should emit no notices, got %v", notices)
	}
	want := []reference.InvocationRule{
		{Prefix: "/", Targets: []string{"**/.claude/skills/*/SKILL.md", "skills/**"}},
		{Prefix: "@", Targets: []string{".claude/agents/*.md"}},
	}
	if !slices.EqualFunc(file.MentionInvocations, want, func(a, b reference.InvocationRule) bool {
		return a.Prefix == b.Prefix && slices.Equal(a.Targets, b.Targets)
	}) {
		t.Errorf("invocations = %+v, want %+v (sorted by prefix)", file.MentionInvocations, want)
	}
}

func TestLoad_MentionsAbsentOrEmpty(t *testing.T) {
	for _, body := range []string{"version: 1\n", "version: 1\nmentions:\n", "version: 1\nmentions: {}\n"} {
		file, notices, err := Load(rootWithBytes(t, []byte(body)))
		if err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		if file.MentionInvocations != nil || len(notices) != 0 {
			t.Errorf("%q: invocations=%v notices=%v, want none", body, file.MentionInvocations, notices)
		}
	}
}

func TestLoad_MentionsUnknownKeysAreNotices(t *testing.T) {
	root := rootWithBytes(t, []byte(`version: 1
mentions:
  paths: false
  invocations:
    - prefix: "/"
      targets: ["**"]
      label: skills
`))
	file, notices, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.MentionInvocations) != 1 {
		t.Errorf("invocations = %v, want one rule", file.MentionInvocations)
	}
	for _, key := range []string{`"mentions.paths"`, `"mentions.invocations[0].label"`} {
		if !hasNoticeContaining(notices, key) {
			t.Errorf("missing unknown-key notice for %s in %v", key, notices)
		}
	}
	if hasNoticeContaining(notices, `"mentions"`) {
		t.Errorf("`mentions` itself must not be flagged as unknown: %v", notices)
	}
}

func TestLoad_MentionsHardErrors(t *testing.T) {
	tests := map[string]struct {
		body, wantSub string
	}{
		"mentions not a mapping":    {"mentions: true\n", "`mentions` must be a mapping"},
		"invocations not a list":    {"mentions:\n  invocations: {}\n", "must be a list"},
		"rule not a mapping":        {"mentions:\n  invocations: [\"/\"]\n", "must be a mapping"},
		"prefix missing":            {"mentions:\n  invocations:\n    - targets: [\"**\"]\n", "prefix` must be a string"},
		"prefix empty":              {"mentions:\n  invocations:\n    - prefix: \"\"\n      targets: [\"**\"]\n", "1 to 4 characters"},
		"prefix too long":           {"mentions:\n  invocations:\n    - prefix: \"/////\"\n      targets: [\"**\"]\n", "1 to 4 characters"},
		"prefix alphanumeric":       {"mentions:\n  invocations:\n    - prefix: \"x\"\n      targets: [\"**\"]\n", "ASCII punctuation"},
		"prefix whitespace":         {"mentions:\n  invocations:\n    - prefix: \" /\"\n      targets: [\"**\"]\n", "ASCII punctuation"},
		"targets missing":           {"mentions:\n  invocations:\n    - prefix: \"/\"\n", "targets` must be a non-empty list"},
		"targets empty":             {"mentions:\n  invocations:\n    - prefix: \"/\"\n      targets: []\n", "must not be empty"},
		"target not a string":       {"mentions:\n  invocations:\n    - prefix: \"/\"\n      targets: [1]\n", "must be a string"},
		"target malformed glob":     {"mentions:\n  invocations:\n    - prefix: \"/\"\n      targets: [\"a/[\"]\n", "syntax error"},
		"target absolute":           {"mentions:\n  invocations:\n    - prefix: \"/\"\n      targets: [\"/abs/*.md\"]\n", "repository-relative"},
		"target empty string":       {"mentions:\n  invocations:\n    - prefix: \"/\"\n      targets: [\"\"]\n", "empty pattern"},
		"invocations scalar string": {"mentions:\n  invocations: \"/\"\n", "must be a list"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := Load(rootWithBytes(t, []byte("version: 1\n"+tt.body)))
			if err == nil {
				t.Fatalf("want hard error containing %q, got nil", tt.wantSub)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q does not contain %q", err, tt.wantSub)
			}
		})
	}
}
