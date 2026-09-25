package reference

import (
	"testing"

	"github.com/stacklok/matlatl/internal/domain/identity"
)

func TestResolve_ContentRoots(t *testing.T) {
	cat := newFakeCatalog(
		"user-docs/guide/page.md",
		"user-docs/api/widget.md",
		"docs/guide/page.md",
		"api/widget.md",
	)
	resolver := NewResolverWithContentRoots(cat, nil, LongestSuffix, []string{"user-docs"})

	for _, tc := range []struct {
		name, origin, target, want string
	}{
		{"configured origin", "user-docs/guide/page.md", "/api/widget.md", "user-docs/api/widget.md"},
		{"outside origin retains repository root", "docs/guide/page.md", "/api/widget.md", "api/widget.md"},
		{"relative remains origin relative", "user-docs/guide/page.md", "../api/widget.md", "user-docs/api/widget.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := resolver.Resolve(RawReference{Origin: identity.DocumentID(tc.origin), RawTarget: tc.target, Type: RelativeLink})
			if got.Health != Valid || got.Target.DocumentID.String() != tc.want {
				t.Errorf("Resolve(%s, %q) = %s/%q, want valid/%q", tc.origin, tc.target, got.Health, got.Target.DocumentID, tc.want)
			}
		})
	}
}

func TestResolve_ContentRootsTwoDisjointAndNearPrefixOrigins(t *testing.T) {
	cat := newFakeCatalog(
		"docs/guide/page.md", "docs/api/widget.md",
		"docs-site/guide/page.md", "docs-site/api/widget.md",
	)
	resolver := NewResolverWithContentRoots(cat, nil, LongestSuffix, []string{"docs-site", "docs"})
	for _, tc := range []struct{ origin, want string }{
		{"docs/guide/page.md", "docs/api/widget.md"},
		{"docs-site/guide/page.md", "docs-site/api/widget.md"},
	} {
		got := resolver.Resolve(RawReference{Origin: identity.DocumentID(tc.origin), RawTarget: "/api/widget.md", Type: RelativeLink})
		if got.Health != Valid || got.Target.DocumentID.String() != tc.want {
			t.Errorf("Resolve(%q) = %s/%q, want valid/%q", tc.origin, got.Health, got.Target.DocumentID, tc.want)
		}
	}
}

func TestResolve_ContentRootsPreserveContainmentAndDefault(t *testing.T) {
	cat := newFakeCatalog("user-docs/guide/page.md", "api/widget.md")
	probed := false
	resolver := NewResolverWithContentRoots(cat, assetProbe(func(string) bool { probed = true; return true }), LongestSuffix, []string{"user-docs"})
	got := resolver.Resolve(RawReference{Origin: "user-docs/guide/page.md", RawTarget: "/../outside.md", Type: RelativeLink})
	if got.Health != Broken || probed {
		t.Errorf("content-root traversal = %s, probed=%v; want broken without probe", got.Health, probed)
	}

	defaultResolver := NewResolver(cat, nil, LongestSuffix)
	got = defaultResolver.Resolve(RawReference{Origin: "user-docs/guide/page.md", RawTarget: "/api/widget.md", Type: RelativeLink})
	if got.Health != Valid || got.Target.DocumentID != "api/widget.md" {
		t.Errorf("default root-absolute resolution = %s/%q, want valid api/widget.md", got.Health, got.Target.DocumentID)
	}
}
