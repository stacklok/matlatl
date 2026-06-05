package identity

import "testing"

func TestNewDocumentID(t *testing.T) {
	tests := []struct {
		name    string
		root    string
		path    string
		want    DocumentID
		wantErr bool
	}{
		{name: "simple relative", root: ".", path: "README.md", want: "README.md"},
		{name: "nested relative", root: ".", path: "docs/guide.md", want: "docs/guide.md"},
		{name: "uncleaned relative", root: ".", path: "docs/../docs/guide.md", want: "docs/guide.md"},
		{name: "dot-slash prefix", root: ".", path: "./README.md", want: "README.md"},
		{name: "absolute under root", root: "/repo", path: "/repo/docs/a.md", want: "docs/a.md"},
		{name: "relative path under abs root", root: "/repo", path: "docs/a.md", want: "docs/a.md"},
		{name: "escape via dotdot", root: ".", path: "../secret.md", wantErr: true},
		{name: "escape deep dotdot", root: "docs", path: "../../etc/passwd", wantErr: true},
		{name: "absolute outside root", root: "/repo", path: "/etc/passwd", wantErr: true},
		{name: "empty path", root: ".", path: "", wantErr: true},
		{name: "path is root itself", root: ".", path: ".", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewDocumentID(tt.root, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewDocumentID(%q, %q) = %q, want error", tt.root, tt.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewDocumentID(%q, %q) unexpected error: %v", tt.root, tt.path, err)
			}
			if got != tt.want {
				t.Errorf("NewDocumentID(%q, %q) = %q, want %q", tt.root, tt.path, got, tt.want)
			}
		})
	}
}

// TestEscapesRoot pins the single root-containment predicate: empty, ".", "..",
// and any "../"-prefixed cleaned path escape; ordinary in-root paths do not.
func TestEscapesRoot(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", true},
		{".", true},
		{"..", true},
		{"../x", true},
		{"../../x", true},
		{"a", false},
		{"a/b", false},
		{"a/..", false}, // NOTE: EscapesRoot expects an ALREADY-CLEANED path; "a/.."
		// is not cleaned (it would clean to "."), so as written it does not match the
		// escape patterns. This pins the documented contract: callers must pre-clean.
		{"a..b", false},   // ".." only escapes as a whole segment / prefix
		{"..a/b", false},  // not a "../" prefix
		{"x/../y", false}, // uncleaned interior ".." is not a leading "../"
	}
	for _, tt := range tests {
		if got := EscapesRoot(tt.in); got != tt.want {
			t.Errorf("EscapesRoot(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestContains pins the shared root-containment join used by the scanner, the
// llms-full body reader and the artifact writer. It checks the documented
// absolute-path rejection, the ".." escape rejection, and that an in-root path
// (cleaned, even with interior "..") returns the right OS-separator destination.
func TestContains(t *testing.T) {
	const root = "/repo"
	tests := []struct {
		name    string
		root    string
		rel     string
		wantOK  bool
		wantOut string // only checked when wantOK
	}{
		// The doc-comment's explicit claim: an absolute relPath is ALWAYS rejected,
		// even though filepath.Join(root, "/etc/passwd") would otherwise look contained.
		{name: "absolute rejected", root: root, rel: "/etc/passwd", wantOK: false},
		{name: "dotdot escapes", root: root, rel: "../x", wantOK: false},
		{name: "deep dotdot escapes", root: root, rel: "../../etc/passwd", wantOK: false},
		{name: "in-root nested", root: root, rel: "a/b", wantOK: true, wantOut: "/repo/a/b"},
		{name: "interior dotdot cleans in-root", root: root, rel: "a/../b", wantOK: true, wantOut: "/repo/b"},
		{name: "slash form", root: root, rel: "a/b/c.md", wantOK: true, wantOut: "/repo/a/b/c.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, ok := Contains(tt.root, tt.rel)
			if ok != tt.wantOK {
				t.Fatalf("Contains(%q, %q) ok = %v, want %v (out=%q)", tt.root, tt.rel, ok, tt.wantOK, out)
			}
			if tt.wantOK && out != tt.wantOut {
				t.Errorf("Contains(%q, %q) out = %q, want %q", tt.root, tt.rel, out, tt.wantOut)
			}
			if !ok && out != "" {
				t.Errorf("Contains(%q, %q) rejected but out = %q, want empty", tt.root, tt.rel, out)
			}
		})
	}
}

func TestNewDocumentID_DuplicateBasenamesDistinct(t *testing.T) {
	a, err := NewDocumentID(".", "alpha/README.md")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewDocumentID(".", "beta/README.md")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("expected distinct IDs for same basename in different dirs, both = %q", a)
	}
	if a.Base() != b.Base() {
		t.Fatalf("expected same basename, got %q and %q", a.Base(), b.Base())
	}
}

func TestDocumentID_DirAndBase(t *testing.T) {
	id := DocumentID("docs/guide/intro.md")
	if got := id.Dir(); got != "docs/guide" {
		t.Errorf("Dir() = %q, want docs/guide", got)
	}
	if got := id.Base(); got != "intro.md" {
		t.Errorf("Base() = %q, want intro.md", got)
	}
}

func TestIsDirectoryIndex(t *testing.T) {
	tests := []struct {
		base string
		want bool
	}{
		{"README.md", true},
		{"readme.md", true}, // case-insensitive
		{"ReadMe.md", true}, // mixed case
		{"index.md", true},
		{"INDEX.MD", true},
		{"guide.md", false},
		{"readme.markdown", false}, // only .md basenames are directory indexes
		{"docs/README.md", false},  // expects a basename, not a path
		{"", false},
	}
	for _, tt := range tests {
		if got := IsDirectoryIndex(tt.base); got != tt.want {
			t.Errorf("IsDirectoryIndex(%q) = %v, want %v", tt.base, got, tt.want)
		}
	}
}
