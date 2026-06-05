package reference

import (
	"testing"

	"github.com/stacklok/doctopus/internal/domain/identity"
)

// fakeCatalog is an in-test Catalog: a set of documents, a per-doc set of
// heading slugs, and an alias→docs map. It proves Catalog is a real seam.
type fakeCatalog struct {
	docs     map[identity.DocumentID]struct{}
	headings map[identity.DocumentID]map[string]struct{}
	aliases  map[string][]identity.DocumentID
}

func newFakeCatalog(docs ...string) *fakeCatalog {
	c := &fakeCatalog{
		docs:     map[identity.DocumentID]struct{}{},
		headings: map[identity.DocumentID]map[string]struct{}{},
		aliases:  map[string][]identity.DocumentID{},
	}
	for _, d := range docs {
		c.docs[identity.DocumentID(d)] = struct{}{}
	}
	return c
}

func (c *fakeCatalog) withHeading(doc, slug string) *fakeCatalog {
	id := identity.DocumentID(doc)
	if c.headings[id] == nil {
		c.headings[id] = map[string]struct{}{}
	}
	c.headings[id][slug] = struct{}{}
	return c
}

func (c *fakeCatalog) withAlias(alias string, docs ...string) *fakeCatalog {
	for _, d := range docs {
		c.aliases[alias] = append(c.aliases[alias], identity.DocumentID(d))
	}
	return c
}

func (c *fakeCatalog) HasDocument(id identity.DocumentID) bool {
	_, ok := c.docs[id]
	return ok
}

func (c *fakeCatalog) DocumentIDs() []identity.DocumentID {
	out := make([]identity.DocumentID, 0, len(c.docs))
	for id := range c.docs {
		out = append(out, id)
	}
	return out
}

func (c *fakeCatalog) HasHeading(id identity.DocumentID, slug string) bool {
	_, ok := c.headings[id][slug]
	return ok
}

func (c *fakeCatalog) LookupAlias(alias string) []identity.DocumentID {
	return append([]identity.DocumentID(nil), c.aliases[alias]...)
}

// fakeAssets is an in-test AssetExistence over a set of known asset paths.
type fakeAssets map[string]struct{}

func (a fakeAssets) AssetExists(rel string) bool {
	_, ok := a[rel]
	return ok
}

var _ Catalog = (*fakeCatalog)(nil)
var _ AssetExistence = (fakeAssets)(nil)

func TestResolve_HealthBranches(t *testing.T) {
	cat := newFakeCatalog(
		"README.md", "docs/guide.md", "docs/sub/overview.md",
		"docs/team/notes.md", "docs/project/notes.md",
	).
		withHeading("docs/guide.md", "setup").
		withHeading("README.md", "intro").
		withAlias("home", "README.md")
	assets := fakeAssets{"assets/logo.png": {}}

	r := NewResolver(cat, assets, LongestSuffix)

	tests := []struct {
		name       string
		raw        RawReference
		wantHealth LinkHealth
		wantKind   TargetKind
		wantDoc    identity.DocumentID
		wantCands  []identity.DocumentID
	}{
		{
			name:       "valid relative link",
			raw:        RawReference{Origin: "README.md", RawTarget: "docs/guide.md", Type: RelativeLink},
			wantHealth: Valid, wantKind: TargetDocument, wantDoc: "docs/guide.md",
		},
		{
			name:       "valid relative with good anchor",
			raw:        RawReference{Origin: "README.md", RawTarget: "docs/guide.md", Fragment: "setup", Type: RelativeLink},
			wantHealth: Valid, wantKind: TargetSection, wantDoc: "docs/guide.md",
		},
		{
			name:       "broken relative link",
			raw:        RawReference{Origin: "README.md", RawTarget: "nope.md", Type: RelativeLink},
			wantHealth: Broken, wantKind: TargetNone,
		},
		{
			name:       "broken anchor (doc exists, slug missing)",
			raw:        RawReference{Origin: "README.md", RawTarget: "docs/guide.md", Fragment: "missing", Type: RelativeLink},
			wantHealth: BrokenAnchor, wantKind: TargetSection, wantDoc: "docs/guide.md",
		},
		{
			name:       "non-note asset",
			raw:        RawReference{Origin: "README.md", RawTarget: "assets/logo.png", Type: ImageEmbed},
			wantHealth: NonNote, wantKind: TargetAsset, wantDoc: "assets/logo.png",
		},
		{
			name:       "broken image (asset missing)",
			raw:        RawReference{Origin: "README.md", RawTarget: "assets/ghost.png", Type: ImageEmbed},
			wantHealth: Broken, wantKind: TargetNone,
		},
		{
			name:       "external",
			raw:        RawReference{Origin: "README.md", RawTarget: "https://example.com", Type: External},
			wantHealth: HealthExternal, wantKind: TargetExternal,
		},
		{
			name:       "valid same-page anchor",
			raw:        RawReference{Origin: "README.md", Fragment: "intro", Type: Anchor},
			wantHealth: Valid, wantKind: TargetSection, wantDoc: "README.md",
		},
		{
			name:       "broken same-page anchor",
			raw:        RawReference{Origin: "README.md", Fragment: "ghost", Type: Anchor},
			wantHealth: BrokenAnchor, wantKind: TargetSection, wantDoc: "README.md",
		},
		{
			name:       "valid wikilink (longest-suffix unique)",
			raw:        RawReference{Origin: "docs/links.md", RawTarget: "guide", Type: Wikilink},
			wantHealth: Valid, wantKind: TargetDocument, wantDoc: "docs/guide.md",
		},
		{
			name:       "valid aliased wikilink",
			raw:        RawReference{Origin: "docs/links.md", RawTarget: "home", Type: Wikilink},
			wantHealth: Valid, wantKind: TargetDocument, wantDoc: "README.md",
		},
		{
			name:       "broken wikilink",
			raw:        RawReference{Origin: "docs/links.md", RawTarget: "does-not-exist", Type: Wikilink},
			wantHealth: Broken, wantKind: TargetNone,
		},
		{
			name:       "ambiguous wikilink",
			raw:        RawReference{Origin: "docs/links.md", RawTarget: "notes", Type: Wikilink},
			wantHealth: Ambiguous, wantKind: TargetNone,
			wantCands: []identity.DocumentID{"docs/project/notes.md", "docs/team/notes.md"},
		},
		{
			name:       "transclusion resolves like a wikilink",
			raw:        RawReference{Origin: "docs/links.md", RawTarget: "guide", Type: Transclusion},
			wantHealth: Valid, wantKind: TargetDocument, wantDoc: "docs/guide.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := r.Resolve(tt.raw)
			if ref.Health != tt.wantHealth {
				t.Errorf("health = %s, want %s", ref.Health, tt.wantHealth)
			}
			if ref.Target.Kind != tt.wantKind {
				t.Errorf("target kind = %s, want %s", ref.Target.Kind, tt.wantKind)
			}
			if tt.wantDoc != "" && ref.Target.DocumentID != tt.wantDoc {
				t.Errorf("target doc = %q, want %q", ref.Target.DocumentID, tt.wantDoc)
			}
			if tt.wantCands != nil {
				if len(ref.Candidates) != len(tt.wantCands) {
					t.Fatalf("candidates = %v, want %v", ref.Candidates, tt.wantCands)
				}
				for i := range tt.wantCands {
					if ref.Candidates[i] != tt.wantCands[i] {
						t.Errorf("candidate[%d] = %q, want %q", i, ref.Candidates[i], tt.wantCands[i])
					}
				}
			}
		})
	}
}

// TestResolve_OutOfRootIsBrokenNeverRead is the ADR 0003 security test: a
// traversal target resolves to Broken and the AssetExistence lookup is never
// consulted (so it is never turned into a filesystem read).
func TestResolve_OutOfRootIsBrokenNeverRead(t *testing.T) {
	cat := newFakeCatalog("docs/links.md")
	probed := false
	probe := assetProbe(func(string) bool { probed = true; return true })

	r := NewResolver(cat, probe, LongestSuffix)
	targets := []string{
		"../../../../etc/passwd",
		"../../secret.md",
		"../outside.md",
	}
	for _, tgt := range targets {
		ref := r.Resolve(RawReference{Origin: "docs/links.md", RawTarget: tgt, Type: RelativeLink})
		if ref.Health != Broken {
			t.Errorf("%q health = %s, want broken (out-of-root)", tgt, ref.Health)
		}
		if ref.Target.Kind != TargetNone {
			t.Errorf("%q target kind = %s, want none", tgt, ref.Target.Kind)
		}
	}
	if probed {
		t.Error("AssetExistence was consulted for an out-of-root target (must never read)")
	}
}

// TestResolve_AbsolutePathsStayInRoot documents the ADR-0003 handling of
// absolute-looking targets. path.Join folds a leading "/" into the origin
// directory, so "/etc/passwd" and "[[/absolute/path]]" become IN-ROOT relative
// paths (docs/etc/passwd, absolute/path) rather than filesystem-absolute escapes.
// They therefore resolve to Broken (no such corpus doc) — never an absolute-path
// filesystem access, and never a root-escape read.
func TestResolve_AbsolutePathsStayInRoot(t *testing.T) {
	cat := newFakeCatalog("docs/links.md")
	// Record the path the asset probe is asked about (always answering false) to
	// prove it is an in-root relative path, never a filesystem-absolute one.
	var probedPath string
	probe := assetProbe(func(p string) bool { probedPath = p; return false })
	r := NewResolver(cat, probe, LongestSuffix)

	// Absolute-looking relative link: path.Join folds the leading "/" under the
	// origin dir → in-root "docs/etc/passwd", not a known doc/asset → Broken.
	abs := r.Resolve(RawReference{Origin: "docs/links.md", RawTarget: "/etc/passwd", Type: RelativeLink})
	if abs.Health != Broken {
		t.Errorf("'/etc/passwd' relative link health = %s, want broken (folded in-root)", abs.Health)
	}
	if probedPath != "docs/etc/passwd" {
		t.Errorf("asset probe saw %q, want in-root 'docs/etc/passwd' (no absolute escape)", probedPath)
	}

	// Absolute-looking wikilink: cleaned target "absolute/path" matches no doc.
	wl := r.Resolve(RawReference{Origin: "docs/links.md", RawTarget: "/absolute/path", Type: Wikilink})
	if wl.Health != Broken {
		t.Errorf("'[[/absolute/path]]' health = %s, want broken", wl.Health)
	}
}

// assetProbe adapts a func to AssetExistence.
type assetProbe func(string) bool

func (f assetProbe) AssetExists(p string) bool { return f(p) }

// TestResolve_AnchorDialectParity asserts the resolver checks anchors against
// the exact slug dialect stored in the catalog (ADR 0006). The catalog stores
// the GitHub-style slug "getting-started"; a fragment in that dialect is Valid,
// while a raw (un-slugified) heading text is BrokenAnchor.
func TestResolve_AnchorDialectParity(t *testing.T) {
	cat := newFakeCatalog("README.md").withHeading("README.md", "getting-started")
	r := NewResolver(cat, nil, LongestSuffix)

	good := r.Resolve(RawReference{Origin: "README.md", Fragment: "getting-started", Type: Anchor})
	if good.Health != Valid {
		t.Errorf("canonical-slug anchor health = %s, want valid", good.Health)
	}
	bad := r.Resolve(RawReference{Origin: "README.md", Fragment: "Getting Started", Type: Anchor})
	if bad.Health != BrokenAnchor {
		t.Errorf("non-dialect anchor health = %s, want broken-anchor", bad.Health)
	}
}

func TestResolve_PolicyExact(t *testing.T) {
	cat := newFakeCatalog("docs/guide.md")
	r := NewResolver(cat, nil, Exact)

	// Exact: extensionless "guide" does NOT match "docs/guide.md".
	if got := r.Resolve(RawReference{Origin: "x.md", RawTarget: "guide", Type: Wikilink}); got.Health != Broken {
		t.Errorf("exact policy 'guide' health = %s, want broken", got.Health)
	}
	// Exact: full path matches.
	if got := r.Resolve(RawReference{Origin: "x.md", RawTarget: "docs/guide.md", Type: Wikilink}); got.Health != Valid {
		t.Errorf("exact policy full path health = %s, want valid", got.Health)
	}
}

func TestResolve_PolicyBasename(t *testing.T) {
	cat := newFakeCatalog("docs/team/notes.md", "docs/project/notes.md", "README.md")
	r := NewResolver(cat, nil, Basename)

	// Basename: "notes" matches both notes.md → ambiguous.
	got := r.Resolve(RawReference{Origin: "x.md", RawTarget: "notes", Type: Wikilink})
	if got.Health != Ambiguous || len(got.Candidates) != 2 {
		t.Errorf("basename 'notes' = %s cands=%v, want ambiguous with 2", got.Health, got.Candidates)
	}
}

func TestResolve_LongestSuffixPrefersLongerMatch(t *testing.T) {
	// "guide.md" suffix-matches both "docs/guide.md" and "docs/sub/guide.md" at
	// 1 segment → ambiguous; but "sub/guide.md" matches only the deeper one at
	// 2 segments → unique Valid.
	cat := newFakeCatalog("docs/guide.md", "docs/sub/guide.md")
	r := NewResolver(cat, nil, LongestSuffix)

	amb := r.Resolve(RawReference{Origin: "x.md", RawTarget: "guide.md", Type: Wikilink})
	if amb.Health != Ambiguous {
		t.Errorf("'guide.md' health = %s, want ambiguous", amb.Health)
	}
	uniq := r.Resolve(RawReference{Origin: "x.md", RawTarget: "sub/guide.md", Type: Wikilink})
	if uniq.Health != Valid || uniq.Target.DocumentID != "docs/sub/guide.md" {
		t.Errorf("'sub/guide.md' = %s doc=%q, want valid docs/sub/guide.md", uniq.Health, uniq.Target.DocumentID)
	}
}

func TestResolve_SuffixBoundaryNoSubstringMatch(t *testing.T) {
	// "guide.md" must NOT match "myguide.md" (not a path-segment suffix).
	cat := newFakeCatalog("myguide.md")
	r := NewResolver(cat, nil, LongestSuffix)
	got := r.Resolve(RawReference{Origin: "x.md", RawTarget: "guide.md", Type: Wikilink})
	if got.Health != Broken {
		t.Errorf("'guide.md' vs 'myguide.md' = %s, want broken (no substring match)", got.Health)
	}
}

func TestResolve_InvalidPolicyFallsBackToDefault(t *testing.T) {
	cat := newFakeCatalog("docs/guide.md")
	r := NewResolver(cat, nil, ResolutionPolicy(99))
	// Default is longest-suffix, so extensionless 'guide' resolves.
	if got := r.Resolve(RawReference{Origin: "x.md", RawTarget: "guide", Type: Wikilink}); got.Health != Valid {
		t.Errorf("invalid policy did not fall back to longest-suffix; health = %s", got.Health)
	}
}

func TestResolveAll_PreservesOrder(t *testing.T) {
	cat := newFakeCatalog("a.md")
	r := NewResolver(cat, nil, LongestSuffix)
	raws := []RawReference{
		{Origin: "x.md", RawTarget: "a.md", Type: RelativeLink},
		{Origin: "x.md", RawTarget: "missing.md", Type: RelativeLink},
	}
	got := r.ResolveAll(raws)
	if len(got) != 2 || got[0].Health != Valid || got[1].Health != Broken {
		t.Errorf("ResolveAll = %+v, want [valid, broken] in order", got)
	}
}
