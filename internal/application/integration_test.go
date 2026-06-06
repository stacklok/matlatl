//go:build integration

package application_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/reference"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
)

// fixtureRoot is the committed testdata corpus, relative to this package.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestIntegration_ScanParseFixture wires the real scanner + parser over the
// committed fixture corpus and asserts the Phase 1 contract end-to-end.
func TestIntegration_ScanParseFixture(t *testing.T) {
	root := fixtureRoot(t)
	scanner := fsscanner.New(fsscanner.Config{})
	parser := mdparser.New(mdparser.Config{})

	scan, err := scanner.Scan(context.Background(), root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	c := corpus.NewCorpus()
	for _, f := range scan.Files {
		doc, perr := parser.Parse(context.Background(), f)
		if perr != nil {
			t.Fatalf("parse %s: %v", f.ID, perr)
		}
		if aerr := c.Add(doc); aerr != nil {
			t.Fatalf("add %s: %v", f.ID, aerr)
		}
	}

	// 16 documents (ignored/secret.md and draft-notes.md excluded by
	// .matlatlignore): the P1/P2 docs plus the P3 analysis fixtures
	// (CHANGELOG.md intentional-orphan, docs/cycle/{alpha,beta}.md cycle,
	// docs/island/{one,two}.md disconnected cluster, docs/stray.md unreachable)
	// plus the P7 bow-tie fixtures (docs/flow/{branch OUT, terminal OUT+dead-end,
	// aside TENDRIL}).
	if c.Len() != 16 {
		var got []string
		for _, d := range c.Documents() {
			got = append(got, d.ID.String())
		}
		t.Fatalf("document count = %d, want 16; got %v", c.Len(), got)
	}

	// The two READMEs are DISTINCT identities (ADR 0001).
	rootReadme, ok1 := c.Get("README.md")
	docsReadme, ok2 := c.Get("docs/README.md")
	if !ok1 || !ok2 {
		t.Fatalf("expected both README.md and docs/README.md present (got %v, %v)", ok1, ok2)
	}
	if rootReadme.ID == docsReadme.ID {
		t.Fatal("the two READMEs collapsed to one identity")
	}

	// Front matter: YAML root readme, TOML guide.
	if rootReadme.FrontMatter.Title != "Project Home" {
		t.Errorf("root README title = %q, want 'Project Home'", rootReadme.FrontMatter.Title)
	}
	guide, _ := c.Get("docs/guide.md")
	if guide.FrontMatter.Title != "User Guide" {
		t.Errorf("guide (TOML) title = %q, want 'User Guide'", guide.FrontMatter.Title)
	}

	// Title fallback to H1 for the front-matter-less overview.
	overview, _ := c.Get("docs/sub/overview.md")
	if overview.FrontMatter.Title != "Overview" {
		t.Errorf("overview title fallback = %q, want 'Overview'", overview.FrontMatter.Title)
	}

	// Heading inventory populated; a known slug is present.
	if c.HeadingCount() == 0 {
		t.Error("heading inventory is empty")
	}
	if !c.HasHeading("README.md", "getting-started") {
		t.Error("expected README.md to have the 'getting-started' heading slug")
	}

	// A known internal link from the guide back to the home page is captured.
	var foundInternal bool
	for _, r := range guide.RawReferences {
		if r.Type == reference.RelativeLink && r.RawTarget == "../README.md" {
			foundInternal = true
		}
	}
	if !foundInternal {
		t.Errorf("expected a relative link ../README.md in the guide; refs = %+v", guide.RawReferences)
	}
}
