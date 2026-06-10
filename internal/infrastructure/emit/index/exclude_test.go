package index

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/reference"
)

// TestIndex_EmitExclude: an emit-excluded doc is dropped from the entries and
// from the Backlinks column; the header count reflects what is rendered and
// states the excluded count (ADR 0019).
func TestIndex_EmitExclude(t *testing.T) {
	refs := []reference.Reference{
		linkRef("README.md", "docs/guide.md"),
		linkRef(".claude/agents/helper.md", "docs/guide.md"),
	}
	v := buildViewWithRefs(t, refs,
		docWithH1("README.md", "Readme"),
		docWithH1("docs/guide.md", "Guide"),
		docWithH1(".claude/agents/helper.md", "Helper Agent"),
	).WithEmitExclude([]string{".claude/agents/"})

	out := string(Markdown(v))

	if row := rowFor(out, ".claude/agents/helper.md"); row != "" {
		t.Errorf("excluded doc must have no row: %q", row)
	}
	guide := rowFor(out, "docs/guide.md")
	if guide == "" {
		t.Fatalf("non-excluded doc must keep its row:\n%s", out)
	}
	cell := backlinksCellOf(t, guide)
	if strings.Contains(cell, ".claude/agents/helper.md") {
		t.Errorf("Backlinks column must not name an excluded doc: %q", cell)
	}
	if !strings.Contains(cell, "README.md") {
		t.Errorf("Backlinks column must keep the non-excluded source: %q", cell)
	}
	if !strings.Contains(out, "2 document(s).") {
		t.Errorf("header must count rendered docs (3-1=2):\n%s", out)
	}
	if !strings.Contains(out, "1 document(s) excluded from this index by emitExclude") {
		t.Errorf("header must state the excluded count:\n%s", out)
	}
	// An emptied category heading must not linger.
	if strings.Contains(out, ".claude/agents") {
		t.Errorf("excluded paths must not appear anywhere:\n%s", out)
	}
}

// TestIndex_EmitExclude_NoPatternsByteIdentical: an empty pattern list leaves
// index.md byte-identical (the no-filter path is unchanged, ADR 0019).
func TestIndex_EmitExclude_NoPatternsByteIdentical(t *testing.T) {
	v := buildView(t, docWithH1("README.md", "Readme"), docWithH1("docs/a.md", "A"))
	if !bytes.Equal(Markdown(v), Markdown(v.WithEmitExclude(nil))) {
		t.Error("empty emitExclude must be byte-identical to no emitExclude")
	}
}

// TestIndex_EmitExclude_AllExcluded: excluding everything yields the no-documents
// body with an honest header.
func TestIndex_EmitExclude_AllExcluded(t *testing.T) {
	v := buildView(t, docWithH1(".agents/bot.md", "Bot")).WithEmitExclude([]string{".agents/"})
	out := string(Markdown(v))
	if !strings.Contains(out, "0 document(s).") || !strings.Contains(out, "_No documents._") {
		t.Errorf("all-excluded index must render the empty body:\n%s", out)
	}
	if !strings.Contains(out, "1 document(s) excluded from this index by emitExclude") {
		t.Errorf("all-excluded index must state the excluded count:\n%s", out)
	}
}
