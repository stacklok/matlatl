package reference

import (
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/identity"
)

// mentionCatalog is the shared fixture for the mention-resolution tests: a doc
// tree with a repo-root rules dir, a skill dir with no README/index, a docs
// folder with an index, two colliding testing.md files, and two documents that
// declare the same front-matter name (only one is an invocation target).
func mentionCatalog() *fakeCatalog {
	return newFakeCatalog(
		"README.md",
		"docs/guide.md",
		"docs/sub/page.md",
		"docs/sub/local.md",
		"docs/adr/README.md",
		"docs/adr/0001-x.md",
		".claude/rules/metrics.md",
		".claude/skills/panel-review/SKILL.md",
		".claude/skills/panel-review/reference.md",
		"docs/testing.md",
		"internal/testing.md",
		"docs/design/panel-review.md",
		"app/.claude/rules/local.md",
		"app/docs/deep/page.md",
	).
		withAlias("panel-review", ".claude/skills/panel-review/SKILL.md", "docs/design/panel-review.md").
		withAlias("triage", "docs/design/panel-review.md")
}

func newMentionResolver(rules ...InvocationRule) *MentionResolver {
	return NewMentionResolver(NewResolver(mentionCatalog(), nil, LongestSuffix), rules)
}

var skillRule = InvocationRule{Prefix: "/", Targets: []string{"**/.claude/skills/*/SKILL.md"}}

func TestMentionResolve(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		raw        RawMention
		want       identity.DocumentID
		wantHealth LinkHealth // Valid when zero-valued and wantOK
		wantOK     bool
	}{
		{
			name:   "repo-root-relative path from a nested doc",
			raw:    RawMention{Origin: "docs/sub/page.md", Kind: MentionPath, Target: ".claude/rules/metrics.md"},
			want:   ".claude/rules/metrics.md",
			wantOK: true,
		},
		{
			name:   "path relative to an enclosing project directory",
			raw:    RawMention{Origin: "app/docs/deep/page.md", Kind: MentionPath, Target: ".claude/rules/local.md"},
			want:   "app/.claude/rules/local.md",
			wantOK: true,
		},
		{
			name:   "nearest enclosing directory wins over the repo root",
			raw:    RawMention{Origin: "app/docs/deep/page.md", Kind: MentionPath, Target: ".claude/rules/metrics.md"},
			want:   ".claude/rules/metrics.md",
			wantOK: true,
		},
		{
			name:       "stale markdown path is kept as broken",
			raw:        RawMention{Origin: "docs/guide.md", Kind: MentionPath, Target: ".claude/rules/gone.md"},
			want:       ".claude/rules/gone.md",
			wantHealth: Broken,
			wantOK:     true,
		},
		{
			name:   "doc-relative path wins",
			raw:    RawMention{Origin: "docs/sub/page.md", Kind: MentionPath, Target: "./local.md"},
			want:   "docs/sub/local.md",
			wantOK: true,
		},
		{
			name:   "doc-relative parent path",
			raw:    RawMention{Origin: "docs/sub/page.md", Kind: MentionPath, Target: "../guide.md"},
			want:   "docs/guide.md",
			wantOK: true,
		},
		{
			name:   "root-absolute path",
			raw:    RawMention{Origin: "docs/sub/page.md", Kind: MentionPath, Target: "/docs/guide.md"},
			want:   "docs/guide.md",
			wantOK: true,
		},
		{
			name:   "directory mention resolves to its index",
			raw:    RawMention{Origin: "docs/guide.md", Kind: MentionPath, Target: "docs/adr/"},
			want:   "docs/adr/README.md",
			wantOK: true,
		},
		{
			name:   "directory mention without index resolves to SKILL.md",
			raw:    RawMention{Origin: "docs/guide.md", Kind: MentionPath, Target: ".claude/skills/panel-review/"},
			want:   ".claude/skills/panel-review/SKILL.md",
			wantOK: true,
		},
		{
			name: "directory with neither index nor SKILL.md does not resolve",
			raw:  RawMention{Origin: "docs/guide.md", Kind: MentionPath, Target: ".claude/rules/"},
		},
		{
			name: "escaping path does not resolve",
			raw:  RawMention{Origin: "docs/guide.md", Kind: MentionPath, Target: "../../etc/passwd.md"},
		},
		{
			name: "unknown path does not resolve",
			raw:  RawMention{Origin: "docs/guide.md", Kind: MentionPath, Target: "internal/domain/reference"},
		},
		{
			name:   "unique basename",
			raw:    RawMention{Origin: "docs/guide.md", Kind: MentionFilename, Target: "metrics.md"},
			want:   ".claude/rules/metrics.md",
			wantOK: true,
		},
		{
			name:   "shared basename resolves to the one in an enclosing directory",
			raw:    RawMention{Origin: "docs/guide.md", Kind: MentionFilename, Target: "testing.md"},
			want:   "docs/testing.md",
			wantOK: true,
		},
		{
			name:       "shared basename with no enclosing candidate is ambiguous",
			raw:        RawMention{Origin: "README.md", Kind: MentionFilename, Target: "testing.md"},
			wantHealth: Ambiguous,
			wantOK:     true,
		},
		{
			name:       "stale bare file name is kept as broken",
			raw:        RawMention{Origin: "docs/guide.md", Kind: MentionFilename, Target: "ghost.md"},
			want:       "ghost.md",
			wantHealth: Broken,
			wantOK:     true,
		},
		{
			name:   "invocation via front-matter name, filtered by target glob",
			raw:    RawMention{Origin: "docs/guide.md", Kind: MentionInvocation, Prefix: "/", Target: "panel-review"},
			want:   ".claude/skills/panel-review/SKILL.md",
			wantOK: true,
		},
		{
			name: "invocation whose only alias match is outside the target glob",
			raw:  RawMention{Origin: "docs/guide.md", Kind: MentionInvocation, Prefix: "/", Target: "triage"},
		},
		{
			name: "invocation with an unconfigured prefix",
			raw:  RawMention{Origin: "docs/guide.md", Kind: MentionInvocation, Prefix: "@", Target: "panel-review"},
		},
		{
			name: "self-mention is dropped",
			raw:  RawMention{Origin: ".claude/rules/metrics.md", Kind: MentionFilename, Target: "metrics.md"},
		},
	}
	m := newMentionResolver(skillRule)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := m.Resolve(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (got %+v)", ok, tt.wantOK, got)
			}
			if !ok {
				return
			}
			wantHealth := tt.wantHealth
			if wantHealth == Unresolved {
				wantHealth = Valid
			}
			if got.Target.DocumentID != tt.want {
				t.Errorf("target = %q, want %q", got.Target.DocumentID, tt.want)
			}
			if got.Type != Mention || got.Health != wantHealth || got.Target.Kind != TargetDocument {
				t.Errorf("type/health/kind = %s/%s/%s, want mention/%s/document", got.Type, got.Health, got.Target.Kind, wantHealth)
			}
			if got.MentionKind != tt.raw.Kind {
				t.Errorf("mention kind = %s, want %s", got.MentionKind, tt.raw.Kind)
			}
		})
	}
}

func TestMentionResolve_NearestScope(t *testing.T) {
	t.Parallel()
	skills := func(ids ...identity.DocumentID) *MentionResolver {
		paths := make([]string, 0, len(ids)+3)
		for _, id := range ids {
			paths = append(paths, id.String())
		}
		skillPaths := slices.Clone(paths)
		paths = append(paths, "app/docs/a.md", "docs/b.md", "other/c.md")
		return NewMentionResolver(NewResolver(newFakeCatalog(paths...).withAlias("x", skillPaths...), nil, LongestSuffix), []InvocationRule{skillRule})
	}
	root, app, lib := identity.DocumentID(".claude/skills/x/SKILL.md"), identity.DocumentID("app/.claude/skills/x/SKILL.md"), identity.DocumentID("lib/.claude/skills/x/SKILL.md")
	tests := []struct {
		name           string
		m              *MentionResolver
		origin         identity.DocumentID
		want           identity.DocumentID
		wantCandidates []identity.DocumentID
	}{
		{name: "deepest enclosing scope wins", m: skills(root, app, lib), origin: "app/docs/a.md", want: app},
		{name: "repo root scope encloses everything", m: skills(root, app, lib), origin: "docs/b.md", want: root},
		{name: "no enclosing scope is ambiguous", m: skills(app, lib), origin: "other/c.md", wantCandidates: []identity.DocumentID{app, lib}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := tt.m.Resolve(RawMention{Origin: tt.origin, Kind: MentionInvocation, Prefix: "/", Target: "x"})
			if !ok {
				t.Fatal("mention dropped")
			}
			if tt.wantCandidates != nil {
				if got.Health != Ambiguous || !slices.Equal(got.Candidates, tt.wantCandidates) {
					t.Fatalf("health %s candidates %v, want ambiguous %v", got.Health, got.Candidates, tt.wantCandidates)
				}
				return
			}
			if got.Health != Valid || got.Target.DocumentID != tt.want {
				t.Fatalf("health %s target %q, want valid %q", got.Health, got.Target.DocumentID, tt.want)
			}
		})
	}
}

func TestMentionResolve_UnmatchedInvocationIsDropped(t *testing.T) {
	t.Parallel()
	if got, ok := newMentionResolver(skillRule).Resolve(RawMention{Origin: "README.md", Kind: MentionInvocation, Prefix: "/", Target: "no-such-skill"}); ok {
		t.Fatalf("an invocation naming no target must be dropped, got %+v", got)
	}
}

func TestMentionResolveAll_KeepsStaleDropsOtherUnresolvedAndDuplicates(t *testing.T) {
	t.Parallel()
	m := newMentionResolver(skillRule)
	raws := []RawMention{
		{Origin: "README.md", Kind: MentionPath, Target: "docs/guide.md", Text: "docs/guide.md", Line: 3},
		{Origin: "README.md", Kind: MentionPath, Target: "docs/guide.md", Text: "docs/guide.md", Line: 3},
		{Origin: "README.md", Kind: MentionPath, Target: "docs/guide.md", Text: "docs/guide.md", Line: 4},
		{Origin: "README.md", Kind: MentionFilename, Target: "nope.md", Text: "nope.md", Line: 5},
		{Origin: "README.md", Kind: MentionPath, Target: "internal/domain/reference", Text: "internal/domain/reference", Line: 6},
	}
	got := m.ResolveAll(raws)
	if len(got) != 3 {
		t.Fatalf("got %d mentions, want 3: %+v", len(got), got)
	}
	if got[0].Line != 3 || got[1].Line != 4 || got[2].Line != 5 {
		t.Errorf("lines = %d,%d,%d, want 3,4,5", got[0].Line, got[1].Line, got[2].Line)
	}
	if got[2].Health != Broken || got[2].Target.DocumentID != "nope.md" {
		t.Errorf("stale mention = %s %q, want broken nope.md", got[2].Health, got[2].Target.DocumentID)
	}
	if got[0].AnchorText != "docs/guide.md" {
		t.Errorf("anchor text = %q", got[0].AnchorText)
	}
}

func TestMentionResolve_ContentRoots(t *testing.T) {
	t.Parallel()
	catalog := newFakeCatalog("site/docs/a.md", "site/docs/b.md")
	r := NewResolverWithContentRoots(catalog, nil, LongestSuffix, []string{"site"})
	m := NewMentionResolver(r, nil)
	got, ok := m.Resolve(RawMention{Origin: "site/docs/a.md", Kind: MentionPath, Target: "/docs/b.md"})
	if !ok || got.Target.DocumentID != "site/docs/b.md" {
		t.Fatalf("got %q ok=%v, want site/docs/b.md", got.Target.DocumentID, ok)
	}
}

func TestMatchGlob(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"**/.claude/skills/*/SKILL.md", ".claude/skills/x/SKILL.md", true},
		{"**/.claude/skills/*/SKILL.md", "pkg/.claude/skills/x/SKILL.md", true},
		{"**/.claude/skills/*/SKILL.md", ".claude/skills/x/y/SKILL.md", false},
		{".claude/agents/*.md", ".claude/agents/a.md", true},
		{".claude/agents/*.md", ".claude/agents/sub/a.md", false},
		{"docs/**", "docs/a/b/c.md", true},
		{"docs/**", "docs", true},
		{"**", "anything/at/all.md", true},
		{"a/**/b.md", "a/b.md", true},
		{"a/**/b.md", "a/x/y/b.md", true},
		{"a/[/b.md", "a/[/b.md", false},
	}
	for _, tt := range tests {
		if got := MatchGlob(tt.pattern, tt.name); got != tt.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}

func TestValidateGlob(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"**/.claude/skills/*/SKILL.md", "docs/*.md", "**"} {
		if err := ValidateGlob(ok); err != nil {
			t.Errorf("ValidateGlob(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "/abs/*.md", "docs/[.md"} {
		if err := ValidateGlob(bad); err == nil {
			t.Errorf("ValidateGlob(%q) = nil, want error", bad)
		}
	}
}

func TestMentionKindString(t *testing.T) {
	t.Parallel()
	for k, want := range map[MentionKind]string{
		MentionNone: "none", MentionPath: "path", MentionFilename: "filename", MentionInvocation: "invocation",
	} {
		if k.String() != want || !k.Valid() {
			t.Errorf("%d: String=%q Valid=%v, want %q", k, k.String(), k.Valid(), want)
		}
	}
	if Mention.String() != "mention" || !Mention.Valid() {
		t.Errorf("Mention link type: %q valid=%v", Mention.String(), Mention.Valid())
	}
}
