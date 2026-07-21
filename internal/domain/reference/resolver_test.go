package reference

import (
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/identity"
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
	// "examples" models an existing non-corpus, non-markdown DIRECTORY (the real
	// AssetExistence returns true for an existing dir; the resolver treats it as
	// any other asset path).
	assets := fakeAssets{"assets/logo.png": {}, "examples": {}}

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
			// An existing non-corpus, non-markdown DIRECTORY resolves to NonNote (it
			// exists → not rot), not Broken (ADR 0008: confers no reachability).
			name:       "non-note existing directory",
			raw:        RawReference{Origin: "README.md", RawTarget: "examples/", Type: RelativeLink},
			wantHealth: NonNote, wantKind: TargetAsset, wantDoc: "examples",
		},
		{
			// A missing non-markdown directory/path is Broken.
			name:       "broken missing directory",
			raw:        RawReference{Origin: "README.md", RawTarget: "nodir/", Type: RelativeLink},
			wantHealth: Broken, wantKind: TargetNone,
		},
		{
			name:       "external",
			raw:        RawReference{Origin: "README.md", RawTarget: "https://example.com", Type: External},
			wantHealth: HealthExternal, wantKind: TargetExternal,
		},
		{
			// file:// is classified External by the parser (ADR 0003 SSRF guard); the
			// resolver must keep it HealthExternal and never turn it into a read.
			name:       "external file scheme",
			raw:        RawReference{Origin: "README.md", RawTarget: "file:///etc/passwd", Type: External},
			wantHealth: HealthExternal, wantKind: TargetExternal,
		},
		{
			// data: likewise stays HealthExternal (never an in-corpus path).
			name:       "external data uri",
			raw:        RawReference{Origin: "README.md", RawTarget: "data:text/plain;base64,SGk=", Type: External},
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

// TestResolve_AbsolutePathsStayInRoot documents the handling of absolute-looking
// targets under ADR 0022 (root-absolute /-links). A single leading "/" on a
// relative link now resolves from the SCAN ROOT, independent of the origin's
// directory — so "/etc/passwd" from docs/links.md resolves to the in-root path
// "etc/passwd" (NOT the old origin-folded "docs/etc/passwd"). With no such corpus
// doc or asset it is still Broken — never a filesystem-absolute access, never a
// root-escape read. Wikilinks are OUT OF SCOPE for ADR 0022: "[[/absolute/path]]"
// keeps its prior behaviour and stays Broken.
func TestResolve_AbsolutePathsStayInRoot(t *testing.T) {
	cat := newFakeCatalog("docs/links.md")
	// Record the path the asset probe is asked about (always answering false) to
	// prove it is an in-root relative path, never a filesystem-absolute one.
	var probedPath string
	probe := assetProbe(func(p string) bool { probedPath = p; return false })
	r := NewResolver(cat, probe, LongestSuffix)

	// Root-absolute relative link (ADR 0022): the leading "/" resolves from the
	// scan root → in-root "etc/passwd", not a known doc/asset → Broken.
	abs := r.Resolve(RawReference{Origin: "docs/links.md", RawTarget: "/etc/passwd", Type: RelativeLink})
	if abs.Health != Broken {
		t.Errorf("'/etc/passwd' relative link health = %s, want broken (root-absolute in-root)", abs.Health)
	}
	if probedPath != "etc/passwd" {
		t.Errorf("asset probe saw %q, want root-absolute in-root 'etc/passwd'", probedPath)
	}

	// Absolute-looking wikilink: cleaned target "absolute/path" matches no doc.
	wl := r.Resolve(RawReference{Origin: "docs/links.md", RawTarget: "/absolute/path", Type: Wikilink})
	if wl.Health != Broken {
		t.Errorf("'[[/absolute/path]]' health = %s, want broken", wl.Health)
	}
}

// TestResolve_RootAbsoluteLinks covers the ADR 0022 happy paths: a single
// leading "/" resolves from the scan root, independent of the origin document's
// directory (origin-independence), across markdown docs, anchors, directories,
// and image assets.
func TestResolve_RootAbsoluteLinks(t *testing.T) {
	cat := newFakeCatalog(
		"README.md", "datasets/sales.md", "tables/orders.md", "docs/guide.md",
		"adr/0001.md",
	).withHeading("docs/guide.md", "sec")
	assets := fakeAssets{"assets/logo.png": {}}
	r := NewResolver(cat, assets, LongestSuffix)

	// From a nested origin: "/tables/orders.md" resolves root-absolute.
	nested := r.Resolve(RawReference{Origin: "datasets/sales.md", RawTarget: "/tables/orders.md", Type: RelativeLink})
	if nested.Health != Valid || nested.Target.DocumentID != "tables/orders.md" {
		t.Errorf("nested origin /tables/orders.md = %s doc=%q, want valid tables/orders.md", nested.Health, nested.Target.DocumentID)
	}

	// From a root origin: same target resolves to the SAME doc (origin-independent).
	fromRoot := r.Resolve(RawReference{Origin: "README.md", RawTarget: "/tables/orders.md", Type: RelativeLink})
	if fromRoot.Health != Valid || fromRoot.Target.DocumentID != "tables/orders.md" {
		t.Errorf("root origin /tables/orders.md = %s doc=%q, want valid tables/orders.md", fromRoot.Health, fromRoot.Target.DocumentID)
	}
	if fromRoot.Target.DocumentID != nested.Target.DocumentID {
		t.Errorf("root-absolute target is origin-dependent: %q vs %q", fromRoot.Target.DocumentID, nested.Target.DocumentID)
	}

	// Anchor on a root-absolute link is validated against the heading inventory,
	// and resolves to the root-absolute target document (not the origin).
	anchor := r.Resolve(RawReference{Origin: "datasets/sales.md", RawTarget: "/docs/guide.md", Fragment: "sec", Type: RelativeLink})
	if anchor.Health != Valid || anchor.Target.Kind != TargetSection {
		t.Errorf("/docs/guide.md#sec = %s/%s, want valid section", anchor.Health, anchor.Target.Kind)
	}
	if anchor.Target.DocumentID != "docs/guide.md" {
		t.Errorf("/docs/guide.md#sec doc = %q, want docs/guide.md", anchor.Target.DocumentID)
	}

	// A BAD anchor on a root-absolute link still runs slug validation: the doc
	// resolves root-absolute but the missing fragment yields BrokenAnchor (proves
	// root-absolute does NOT skip heading-inventory validation).
	badAnchor := r.Resolve(RawReference{Origin: "datasets/sales.md", RawTarget: "/docs/guide.md", Fragment: "nope", Type: RelativeLink})
	if badAnchor.Health != BrokenAnchor || badAnchor.Target.DocumentID != "docs/guide.md" {
		t.Errorf("/docs/guide.md#nope = %s doc=%q, want broken-anchor docs/guide.md", badAnchor.Health, badAnchor.Target.DocumentID)
	}

	// Root-absolute directory link.
	dir := r.Resolve(RawReference{Origin: "datasets/sales.md", RawTarget: "/adr/", Type: RelativeLink})
	if dir.Health != Valid || dir.Target.Kind != TargetDirectory || dir.Target.Directory != "adr" {
		t.Errorf("/adr/ = %s/%s dir=%q, want valid directory adr", dir.Health, dir.Target.Kind, dir.Target.Directory)
	}

	// Root-absolute resolution is case-sensitive (identity is the exact path,
	// ADR 0001): "/Tables/orders.md" does not match "tables/orders.md" → Broken.
	mixedCase := r.Resolve(RawReference{Origin: "datasets/sales.md", RawTarget: "/Tables/orders.md", Type: RelativeLink})
	if mixedCase.Health != Broken || mixedCase.Target.Kind != TargetNone {
		t.Errorf("/Tables/orders.md = %s/%s, want broken/none (case-sensitive)", mixedCase.Health, mixedCase.Target.Kind)
	}

	// A bare "/" names the scan root, which is not a linkable directory (ADR 0022):
	// it strips to "" → cleans to "." → Broken, same posture as a relative "./".
	bare := r.Resolve(RawReference{Origin: "datasets/sales.md", RawTarget: "/", Type: RelativeLink})
	if bare.Health != Broken || bare.Target.Kind != TargetNone {
		t.Errorf("'/' = %s/%s, want broken/none (scan root is not linkable)", bare.Health, bare.Target.Kind)
	}

	// Root-absolute image: the asset probe sees the root-relative "assets/logo.png".
	var probedPath string
	probe := assetProbe(func(p string) bool { probedPath = p; return p == "assets/logo.png" })
	ri := NewResolver(cat, probe, LongestSuffix)
	img := ri.Resolve(RawReference{Origin: "datasets/sales.md", RawTarget: "/assets/logo.png", Type: ImageEmbed})
	if img.Health != NonNote || img.Target.Kind != TargetAsset {
		t.Errorf("/assets/logo.png = %s/%s, want nonnote asset", img.Health, img.Target.Kind)
	}
	if probedPath != "assets/logo.png" {
		t.Errorf("asset probe saw %q, want root-absolute 'assets/logo.png'", probedPath)
	}
}

// TestResolve_RootAbsoluteEscape is the ADR 0022 security test: a root-absolute
// target that traverses out of the scan root is Broken and the asset probe is
// NEVER consulted (order: strip leading slash → clean → EscapesRoot). A
// percent-encoded traversal ("/..%2F..") is NOT URL-decoded, so it stays a
// literal in-root filename → Broken, not an escape.
func TestResolve_RootAbsoluteEscape(t *testing.T) {
	cat := newFakeCatalog("datasets/sales.md")
	escapeProbed := false
	// A root-escape target must be rejected BEFORE the probe: this probe records
	// consultation and always answers true, so if an escaping target ever reached
	// it the target would (wrongly) resolve NonNote instead of Broken.
	escapeProbe := assetProbe(func(string) bool { escapeProbed = true; return true })
	r := NewResolver(cat, escapeProbe, LongestSuffix)

	for _, tgt := range []string{"/../etc/passwd", "/./../x", "/../../secret.md"} {
		ref := r.Resolve(RawReference{Origin: "datasets/sales.md", RawTarget: tgt, Type: RelativeLink})
		if ref.Health != Broken || ref.Target.Kind != TargetNone {
			t.Errorf("%q = %s/%s, want broken/none (root escape)", tgt, ref.Health, ref.Target.Kind)
		}
	}
	if escapeProbed {
		t.Error("asset probe consulted for a root-absolute escaping target (must never read)")
	}

	// "/..%2F.." is a literal in-root filename (no URL decoding), so it is NOT an
	// escape: it reaches the probe like any in-root path. With no such asset it is
	// Broken — a genuine escape would have short-circuited above. Use a distinct
	// probe answering false so "no such asset" (not "rejected as escape") is what
	// yields Broken here.
	nfProbe := assetProbe(func(string) bool { return false })
	enc := NewResolver(cat, nfProbe, LongestSuffix).Resolve(RawReference{Origin: "datasets/sales.md", RawTarget: "/..%2F..", Type: RelativeLink})
	if enc.Health != Broken || enc.Target.Kind != TargetNone {
		t.Errorf("'/..%%2F..' = %s/%s, want broken/none (in-root literal)", enc.Health, enc.Target.Kind)
	}
}

// TestResolve_DoubleSlashNotRootAbsolute asserts that a double-slash (or
// more-than-one-slash) target is NOT treated as root-absolute at the resolver
// boundary. The parser classifies "//host/x" as External, but FrontmatterRelated
// edges bypass that classification and reach resolveRelative directly
// (ADR 0022 D3): IsRootAbsolute must refuse "//..." so it falls through relative
// resolution (typically Broken). A positive FrontmatterRelated single-slash case
// proves the type still gets genuine root-absolute treatment.
func TestResolve_DoubleSlashNotRootAbsolute(t *testing.T) {
	cat := newFakeCatalog("docs/sub/page.md", "docs/guide.md")

	// Positive control: a single-slash FrontmatterRelated target IS root-absolute
	// and resolves from the scan root, independent of the nested origin.
	pos := NewResolver(cat, nil, LongestSuffix).
		Resolve(RawReference{Origin: "docs/sub/page.md", RawTarget: "/docs/guide.md", Type: FrontmatterRelated})
	if pos.Health != Valid || pos.Target.DocumentID != "docs/guide.md" {
		t.Errorf("'/docs/guide.md' FrontmatterRelated = %s doc=%q, want valid docs/guide.md", pos.Health, pos.Target.DocumentID)
	}

	// From the nested origin docs/sub/page.md, a genuine root-absolute link would
	// resolve to "host/x"; a non-root-absolute relative join yields
	// "docs/sub/host/x". Both "//host/x" and "///host/x" must take the relative
	// branch, proving the guard rejects any leading run of two-or-more slashes.
	for _, tgt := range []string{"//host/x", "///host/x"} {
		var probedPath string
		probe := assetProbe(func(p string) bool { probedPath = p; return false })
		r := NewResolver(cat, probe, LongestSuffix)
		ref := r.Resolve(RawReference{Origin: "docs/sub/page.md", RawTarget: tgt, Type: FrontmatterRelated})
		if ref.Health != Broken {
			t.Errorf("%q FrontmatterRelated = %s, want broken", tgt, ref.Health)
		}
		if probedPath == "host/x" {
			t.Errorf("%q was folded root-absolute to %q (must not treat multi-slash as root-absolute)", tgt, probedPath)
		}
		if probedPath != "docs/sub/host/x" {
			t.Errorf("%q asset probe saw %q, want relative-joined 'docs/sub/host/x'", tgt, probedPath)
		}
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

// TestResolve_DirectoryLinks covers directory-link resolution (ADR 0008): a
// relative link to a directory resolves Valid with TargetDirectory, finds the
// index doc (if any), enumerates direct children one level deep, treats
// trailing-slash and no-slash as equivalent, keeps the out-of-root guard, and
// stays Broken for a path that is neither a file nor a markdown directory.
func TestResolve_DirectoryLinks(t *testing.T) {
	// Corpus: docs/adr has an index (README.md) + two non-indexed docs; docs/img
	// has only an asset (no markdown). docs/ itself has a direct child guide.md
	// plus the nested adr/ subtree.
	cat := newFakeCatalog(
		"README.md",
		"docs/guide.md",
		"docs/adr/README.md",
		"docs/adr/0001.md",
		"docs/adr/0002.md",
		"notes/index.md",
		"plain/a.md",
		"plain/b.md",
	)
	assets := fakeAssets{"docs/img/logo.png": {}}
	r := NewResolver(cat, assets, LongestSuffix)

	tests := []struct {
		name         string
		raw          RawReference
		wantHealth   LinkHealth
		wantKind     TargetKind
		wantDir      string
		wantIndex    identity.DocumentID
		wantChildren []identity.DocumentID
	}{
		{
			name:       "dir with index (trailing slash)",
			raw:        RawReference{Origin: "README.md", RawTarget: "docs/adr/", Type: RelativeLink},
			wantHealth: Valid, wantKind: TargetDirectory, wantDir: "docs/adr",
			wantIndex:    "docs/adr/README.md",
			wantChildren: []identity.DocumentID{"docs/adr/0001.md", "docs/adr/0002.md", "docs/adr/README.md"},
		},
		{
			name:       "dir with index (no slash) is equivalent",
			raw:        RawReference{Origin: "README.md", RawTarget: "docs/adr", Type: RelativeLink},
			wantHealth: Valid, wantKind: TargetDirectory, wantDir: "docs/adr",
			wantIndex:    "docs/adr/README.md",
			wantChildren: []identity.DocumentID{"docs/adr/0001.md", "docs/adr/0002.md", "docs/adr/README.md"},
		},
		{
			name:       "dir with index.md index",
			raw:        RawReference{Origin: "README.md", RawTarget: "notes/", Type: RelativeLink},
			wantHealth: Valid, wantKind: TargetDirectory, wantDir: "notes",
			wantIndex:    "notes/index.md",
			wantChildren: []identity.DocumentID{"notes/index.md"},
		},
		{
			name:       "dir with NO index still valid, children enumerated",
			raw:        RawReference{Origin: "README.md", RawTarget: "plain/", Type: RelativeLink},
			wantHealth: Valid, wantKind: TargetDirectory, wantDir: "plain",
			wantIndex:    "",
			wantChildren: []identity.DocumentID{"plain/a.md", "plain/b.md"},
		},
		{
			name:       "nested dir reaches direct children only (one level)",
			raw:        RawReference{Origin: "README.md", RawTarget: "docs", Type: RelativeLink},
			wantHealth: Valid, wantKind: TargetDirectory, wantDir: "docs",
			wantIndex: "",
			// docs/adr/* are NOT direct children of docs (they live in docs/adr).
			wantChildren: []identity.DocumentID{"docs/guide.md"},
		},
		{
			name:       "dir from a wikilink",
			raw:        RawReference{Origin: "x.md", RawTarget: "plain", Type: Wikilink},
			wantHealth: Valid, wantKind: TargetDirectory, wantDir: "plain",
			wantChildren: []identity.DocumentID{"plain/a.md", "plain/b.md"},
		},
		{
			// ADR 0008: a fragment on a directory link is not meaningful (a
			// directory has no headings); it is ignored and the link resolves as
			// a plain directory link (Valid, children enumerated).
			name:       "fragment on a directory link is ignored",
			raw:        RawReference{Origin: "README.md", RawTarget: "docs/adr", Fragment: "anything", Type: RelativeLink},
			wantHealth: Valid, wantKind: TargetDirectory, wantDir: "docs/adr",
			wantIndex:    "docs/adr/README.md",
			wantChildren: []identity.DocumentID{"docs/adr/0001.md", "docs/adr/0002.md", "docs/adr/README.md"},
		},
		{
			// A transclusion (![[dir]]) routes through relative resolution like
			// the other path-bearing types (ADR 0008), so a directory target
			// resolves TargetDirectory rather than Broken.
			name:       "transclusion to a directory resolves TargetDirectory",
			raw:        RawReference{Origin: "x.md", RawTarget: "plain", Type: Transclusion},
			wantHealth: Valid, wantKind: TargetDirectory, wantDir: "plain",
			wantChildren: []identity.DocumentID{"plain/a.md", "plain/b.md"},
		},
		{
			name:       "directory with only a non-markdown asset is Broken",
			raw:        RawReference{Origin: "README.md", RawTarget: "docs/img/", Type: RelativeLink},
			wantHealth: Broken, wantKind: TargetNone,
		},
		{
			name:       "path that is neither file nor dir is Broken",
			raw:        RawReference{Origin: "README.md", RawTarget: "ghost/", Type: RelativeLink},
			wantHealth: Broken, wantKind: TargetNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := r.Resolve(tt.raw)
			if ref.Health != tt.wantHealth {
				t.Fatalf("health = %s, want %s", ref.Health, tt.wantHealth)
			}
			if ref.Target.Kind != tt.wantKind {
				t.Fatalf("kind = %s, want %s", ref.Target.Kind, tt.wantKind)
			}
			if tt.wantKind != TargetDirectory {
				return
			}
			if ref.Target.Directory != tt.wantDir {
				t.Errorf("directory = %q, want %q", ref.Target.Directory, tt.wantDir)
			}
			if ref.Target.DocumentID != tt.wantIndex {
				t.Errorf("index = %q, want %q", ref.Target.DocumentID, tt.wantIndex)
			}
			if !slices.Equal(ref.Target.Children, tt.wantChildren) {
				t.Errorf("children = %v, want %v", ref.Target.Children, tt.wantChildren)
			}
		})
	}
}

// TestResolve_DirectoryEscapeNeverInspected is the ADR 0003/0008 ordering test: a
// directory-looking target that escapes the root is Broken and the catalog/asset
// lookups are never consulted to enumerate it.
func TestResolve_DirectoryEscapeNeverInspected(t *testing.T) {
	cat := newFakeCatalog("docs/links.md", "../outside/secret.md")
	probed := false
	probe := assetProbe(func(string) bool { probed = true; return true })
	r := NewResolver(cat, probe, LongestSuffix)

	// "../../etc/" escapes root → Broken, never inspected as a directory.
	ref := r.Resolve(RawReference{Origin: "docs/links.md", RawTarget: "../../etc/", Type: RelativeLink})
	if ref.Health != Broken || ref.Target.Kind != TargetNone {
		t.Fatalf("escaping dir link = %s/%s, want broken/none", ref.Health, ref.Target.Kind)
	}
	if probed {
		t.Error("asset probe consulted for an out-of-root directory target (must never read)")
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
