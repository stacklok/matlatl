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
