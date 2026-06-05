package graphmodel

import (
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// docWithParent builds a document with a front-matter parent (and optional
// directory-index basename via the id) for hierarchy tests.
func docWithParent(id, parent string) *corpus.Document {
	root := &corpus.Section{Level: 0, StartLine: 1, EndLine: 1}
	return &corpus.Document{
		ID:          identity.DocumentID(id),
		Root:        root,
		FrontMatter: corpus.FrontMatter{Parent: parent},
	}
}

// TestBuildHierarchyTree covers parent resolution precedence (ADR 0007): the
// front-matter parent (dir-relative first, then repo-relative fallback), then
// the directory index (README.md preferred over index.md), then top-level; plus
// the self-parent guard and deterministic Roots()/Children() ordering.
func TestBuildHierarchyTree(t *testing.T) {
	tests := []struct {
		name         string
		docs         []*corpus.Document
		wantRoots    []string
		wantChildren map[string][]string
	}{
		{
			name: "front-matter parent dir-relative",
			docs: []*corpus.Document{
				doc("docs/index.md", "i", nil),
				// parent "index.md" is interpreted relative to docs/ → docs/index.md.
				docWithParent("docs/page.md", "index.md"),
			},
			wantRoots:    []string{"docs/index.md"},
			wantChildren: map[string][]string{"docs/index.md": {"docs/page.md"}},
		},
		{
			name: "front-matter parent repo-relative fallback",
			docs: []*corpus.Document{
				doc("guide.md", "g", nil),
				// parent "guide.md" is NOT under docs/ (docs/guide.md unknown), so the
				// repo-relative fallback resolves it to top-level guide.md.
				docWithParent("docs/page.md", "guide.md"),
			},
			wantRoots:    []string{"guide.md"},
			wantChildren: map[string][]string{"guide.md": {"docs/page.md"}},
		},
		{
			name: "directory index README preferred over index",
			docs: []*corpus.Document{
				doc("docs/README.md", "r", nil),
				doc("docs/index.md", "i", nil),
				doc("docs/a.md", "a", nil),
			},
			// docs/a.md's directory index is README.md (preferred); README.md and
			// index.md are themselves top-level (index.md's own parent would be
			// README.md → so index.md is a child of README.md).
			wantRoots: []string{"docs/README.md"},
			wantChildren: map[string][]string{
				"docs/README.md": {"docs/a.md", "docs/index.md"},
			},
		},
		{
			name: "directory index does not self-parent",
			docs: []*corpus.Document{
				// The lone directory index of its folder must not be its own parent.
				doc("docs/README.md", "r", nil),
				doc("docs/child.md", "c", nil),
			},
			wantRoots:    []string{"docs/README.md"},
			wantChildren: map[string][]string{"docs/README.md": {"docs/child.md"}},
		},
		{
			name: "front-matter self-parent guarded to top-level",
			docs: []*corpus.Document{
				// parent resolves to itself (page.md in its own dir) → guarded, becomes
				// a top-level root rather than its own child.
				docWithParent("page.md", "page.md"),
			},
			wantRoots:    []string{"page.md"},
			wantChildren: map[string][]string{},
		},
		{
			name: "deterministic children ordering",
			docs: []*corpus.Document{
				doc("README.md", "r", nil),
				docWithParent("z.md", "README.md"),
				docWithParent("a.md", "README.md"),
				docWithParent("m.md", "README.md"),
			},
			wantRoots:    []string{"README.md"},
			wantChildren: map[string][]string{"README.md": {"a.md", "m.md", "z.md"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := buildCorpus(t, tt.docs...)
			tree := BuildHierarchyTree(c)

			if got := ids(tree.Roots()); !slices.Equal(got, tt.wantRoots) {
				t.Errorf("Roots() = %v, want %v", got, tt.wantRoots)
			}
			for parent, wantKids := range tt.wantChildren {
				if got := ids(tree.Children(identity.DocumentID(parent))); !slices.Equal(got, wantKids) {
					t.Errorf("Children(%q) = %v, want %v", parent, got, wantKids)
				}
			}
		})
	}
}

// TestHierarchyTree_RootsChildrenAreCopies asserts Roots()/Children() return
// independent slices (mutating the result must not corrupt the tree).
func TestHierarchyTree_RootsChildrenAreCopies(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "r", nil),
		docWithParent("a.md", "README.md"),
		docWithParent("b.md", "README.md"),
	)
	tree := BuildHierarchyTree(c)

	roots := tree.Roots()
	if len(roots) > 0 {
		roots[0] = "MUTATED"
	}
	if got := ids(tree.Roots()); slices.Contains(got, "MUTATED") {
		t.Error("Roots() must return a copy; mutation leaked into the tree")
	}

	kids := tree.Children("README.md")
	if len(kids) > 0 {
		kids[0] = "MUTATED"
	}
	if got := ids(tree.Children("README.md")); slices.Contains(got, "MUTATED") {
		t.Error("Children() must return a copy; mutation leaked into the tree")
	}

	if tree.Children("does-not-exist.md") != nil {
		t.Error("Children of an unknown document should be nil")
	}
}

// TestResolveParent covers the resolveParent precedence helper directly.
func TestResolveParent(t *testing.T) {
	known := map[identity.DocumentID]struct{}{
		"docs/index.md": {}, "guide.md": {}, "docs/page.md": {},
	}
	dirIndex := map[string]identity.DocumentID{
		"docs": "docs/index.md",
	}

	tests := []struct {
		name       string
		doc        *corpus.Document
		wantParent identity.DocumentID
		wantOK     bool
	}{
		{
			name:       "dir-relative front-matter parent",
			doc:        docWithParent("docs/page.md", "index.md"),
			wantParent: "docs/index.md", wantOK: true,
		},
		{
			name:       "repo-relative front-matter fallback",
			doc:        docWithParent("docs/other.md", "guide.md"),
			wantParent: "guide.md", wantOK: true,
		},
		{
			name:       "unknown front-matter parent falls through to dir index",
			doc:        docWithParent("docs/page.md", "nope.md"),
			wantParent: "docs/index.md", wantOK: true,
		},
		{
			name:       "no parent, no dir index → top level",
			doc:        doc("top.md", "t", nil),
			wantParent: "", wantOK: false,
		},
		{
			name:       "dir index is self → not returned as own parent",
			doc:        doc("docs/index.md", "i", nil),
			wantParent: "", wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotParent, gotOK := resolveParent(tt.doc, known, dirIndex)
			if gotOK != tt.wantOK || gotParent != tt.wantParent {
				t.Errorf("resolveParent = (%q,%v), want (%q,%v)", gotParent, gotOK, tt.wantParent, tt.wantOK)
			}
		})
	}
}
