package graphmodel

import (
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// mentionRef is a resolved unlinked mention (ADR 0026) whose "anchor text" is
// the token as written.
func mentionRef(origin, target, text string, line int) reference.Reference {
	r := anchorRef(origin, target, text, line)
	r.Type = reference.Mention
	r.MentionKind = reference.MentionFilename
	return r
}

// TestMention_CountsInProjectionButNotLinkProjection: a mention is a
// navigational edge for every analysis (ProjectionOut/In) but is kept out of
// the explicit-link view that emitters use for "reference" edges.
func TestMention_CountsInProjectionButNotLinkProjection(t *testing.T) {
	docs := []*corpus.Document{titledDoc("A.md", "A"), titledDoc("B.md", "B"), titledDoc("C.md", "C")}
	refs := []reference.Reference{
		anchorRef("A.md", "B.md", "B", 3),
		mentionRef("A.md", "C.md", "C.md", 4),
		mentionRef("A.md", "B.md", "B.md", 5),
	}
	c := buildCorpus(t, docs...)
	g := BuildReferenceGraph(c, refs, BuildOptions{})

	if got, want := g.ProjectionOut("A.md"), []identity.DocumentID{"B.md", "C.md"}; !slices.Equal(got, want) {
		t.Errorf("ProjectionOut(A) = %v, want %v", got, want)
	}
	if got, want := g.ProjectionIn("C.md"), []identity.DocumentID{"A.md"}; !slices.Equal(got, want) {
		t.Errorf("ProjectionIn(C) = %v, want %v (a mention confers an inbound edge)", got, want)
	}
	if got, want := g.LinkProjectionOut("A.md"), []identity.DocumentID{"B.md"}; !slices.Equal(got, want) {
		t.Errorf("LinkProjectionOut(A) = %v, want %v (mention-only pairs excluded)", got, want)
	}
}

// TestMention_NotScoredForScent: a mention's text is the target's own name, not
// a label, so information scent never flags it even when it shares no word with
// the target's title.
func TestMention_NotScoredForScent(t *testing.T) {
	docs := []*corpus.Document{titledDoc("A.md", "Source"), titledDoc("B.md", "Installation Guide")}
	refs := []reference.Reference{mentionRef("A.md", "B.md", "B.md", 5)}
	if findings := scentFor(t, docs, refs); len(findings) != 0 {
		t.Errorf("mention must not produce scent findings, got %+v", findings)
	}
}

// TestMention_LiftsOrphan: a document reached only by a mention is neither an
// isolated orphan nor unreachable.
func TestMention_LiftsOrphan(t *testing.T) {
	docs := []*corpus.Document{titledDoc("README.md", "Home"), titledDoc("B.md", "B")}
	refs := []reference.Reference{mentionRef("README.md", "B.md", "B.md", 2)}
	c := buildCorpus(t, docs...)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})
	if slices.Contains(m.Orphans.Isolated, "B.md") || slices.Contains(m.Orphans.Unreachable, "B.md") {
		t.Errorf("B.md is mentioned from the root: isolated=%v unreachable=%v", m.Orphans.Isolated, m.Orphans.Unreachable)
	}
}
