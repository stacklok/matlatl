package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoad_ContentRootsCanonicalizesSorted(t *testing.T) {
	file, notices, err := Load(rootWithBytes(t, []byte("version: 1\ncontentRoots:\n  - user-docs\n  - docs\n")))
	if err != nil {
		t.Fatalf("Load valid contentRoots: %v", err)
	}
	if len(notices) != 0 {
		t.Errorf("notices = %v, want none", notices)
	}
	if want := []string{"docs", "user-docs"}; !reflect.DeepEqual(file.ContentRoots, want) {
		t.Errorf("ContentRoots = %v, want %v", file.ContentRoots, want)
	}
}

func TestLoad_ContentRootsRejectInvalidDuplicateAndOverlapping(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, want string
	}{
		{"wrong type", "contentRoots: docs\n", "list of strings"},
		{"non-string entry", "contentRoots: [docs, 2]\n", "must be a string"},
		{"empty", "contentRoots: ['']\n", "canonical repository-relative"},
		{"dot", "contentRoots: ['.']\n", "canonical repository-relative"},
		{"backslash", "contentRoots: ['docs\\\\api']\n", "canonical repository-relative"},
		{"repeated slash", "contentRoots: [docs//api]\n", "canonical repository-relative"},
		{"dot prefix", "contentRoots: ['./docs']\n", "canonical repository-relative"},
		{"internal traversal", "contentRoots: [docs/../api]\n", "canonical repository-relative"},
		{"absolute", "contentRoots: [/docs]\n", "canonical repository-relative"},
		{"traversal", "contentRoots: [../docs]\n", "canonical repository-relative"},
		{"noncanonical", "contentRoots: [docs/]\n", "canonical repository-relative"},
		{"duplicate", "contentRoots: [docs, docs]\n", "duplicate"},
		{"nested", "contentRoots: [docs, docs/api]\n", "overlapping"},
		{"nested despite lexical sibling", "contentRoots: [a, a-b, a/c]\n", "overlapping"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Load(rootWithBytes(t, []byte("version: 1\n"+tc.yaml)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoad_ContentRootsAbsentOrEmptyIsNil(t *testing.T) {
	for _, yaml := range []string{"version: 1\n", "version: 1\ncontentRoots: []\n", "version: 1\ncontentRoots:\n"} {
		file, notices, err := Load(rootWithBytes(t, []byte(yaml)))
		if err != nil {
			t.Fatal(err)
		}
		if file.ContentRoots != nil {
			t.Errorf("ContentRoots = %v, want nil for %q", file.ContentRoots, yaml)
		}
		for _, n := range notices {
			if strings.Contains(n.Detail, "contentRoots") {
				t.Errorf("contentRoots incorrectly reported as unknown: %q", n.Detail)
			}
		}
	}
}

func TestLoad_ContentRootsKnown(t *testing.T) {
	file, notices, err := Load(rootWithBytes(t, []byte("version: 1\ncontentRoots: [user-docs]\n")))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"user-docs"}; !reflect.DeepEqual(file.ContentRoots, want) {
		t.Errorf("ContentRoots = %v, want %v", file.ContentRoots, want)
	}
	for _, n := range notices {
		if strings.Contains(n.Detail, "contentRoots") {
			t.Errorf("contentRoots incorrectly reported as unknown: %q", n.Detail)
		}
	}
}
