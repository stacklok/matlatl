package llmstxt_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/llmstxt"
)

// mapReader serves document bodies from memory for the full/small emitters.
type mapReader map[identity.DocumentID][]byte

func (r mapReader) Read(id identity.DocumentID) ([]byte, error) {
	if b, ok := r[id]; ok {
		return b, nil
	}
	return nil, fmt.Errorf("no body for %s", id)
}

// excludeView builds README.md → docs/guide.md ⇄ .claude/agents/helper.md, the
// agent-scaffolding shape ADR 0019 exists for: the agent file is REACHABLE (it
// would render without the filter) and must keep feeding guide.md's
// rank/backlinks in the corpus, but vanish from the rendering.
func excludeView(t *testing.T) emit.View {
	t.Helper()
	readme := identity.DocumentID("README.md")
	guide := identity.DocumentID("docs/guide.md")
	agent := identity.DocumentID(".claude/agents/helper.md")
	return buildSyntheticView(t,
		[]*corpus.Document{doc(readme, "Readme"), doc(guide, "Guide"), doc(agent, "Helper Agent")},
		[]reference.Reference{link(readme, guide), link(agent, guide), link(guide, agent)},
	)
}

// TestLLMSTxt_EmitExclude: the excluded doc is dropped from the entry list AND
// from the rendered backlink clauses; the header count reflects what is
// rendered; the summary states how many docs were excluded (ADR 0019).
func TestLLMSTxt_EmitExclude(t *testing.T) {
	unfiltered := string(llmstxt.LLMSTxt(excludeView(t), llmstxt.Options{Title: "T"}))
	if !strings.Contains(unfiltered, ".claude/agents/helper.md") {
		t.Fatalf("precondition: the agent doc must render WITHOUT the filter:\n%s", unfiltered)
	}

	v := excludeView(t).WithEmitExclude([]string{".claude/agents/"})
	out := string(llmstxt.LLMSTxt(v, llmstxt.Options{Title: "T"}))

	if strings.Contains(out, ".claude/agents/helper.md") {
		t.Errorf("excluded doc must not appear anywhere (entry or backlink clause):\n%s", out)
	}
	if !strings.Contains(out, "(docs/guide.md)") {
		t.Errorf("non-excluded doc must still render:\n%s", out)
	}
	if !strings.Contains(out, "corpus of 2 document(s)") {
		t.Errorf("header count must reflect rendered docs (3-1=2):\n%s", out)
	}
	if !strings.Contains(out, "1 document(s) excluded from rendering by emitExclude") {
		t.Errorf("summary must state the excluded count:\n%s", out)
	}
	// The backlink clause for guide.md's curated entry keeps the non-excluded source.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "- [") && strings.Contains(line, "(docs/guide.md)") &&
			!strings.Contains(line, "linked from: README.md") {
			t.Errorf("guide.md must keep its README.md backlink: %q", line)
		}
	}
}

// TestLLMSTxt_EmitExclude_NoPatternsByteIdentical: an empty pattern list leaves
// the artifact byte-identical (the no-filter path is unchanged, ADR 0019).
func TestLLMSTxt_EmitExclude_NoPatternsByteIdentical(t *testing.T) {
	v := excludeView(t)
	plain := llmstxt.LLMSTxt(v, llmstxt.Options{Title: "T"})
	armed := llmstxt.LLMSTxt(v.WithEmitExclude(nil), llmstxt.Options{Title: "T"})
	if !bytes.Equal(plain, armed) {
		t.Error("empty emitExclude must be byte-identical to no emitExclude")
	}
}

// TestLLMSFullAndSmall_EmitExclude: the concatenated-body artifacts skip the
// excluded doc's body entirely.
func TestLLMSFullAndSmall_EmitExclude(t *testing.T) {
	v := excludeView(t).WithEmitExclude([]string{".claude/agents/"})
	r := mapReader{
		"README.md":                []byte("# Readme\nbody-readme\n"),
		"docs/guide.md":            []byte("# Guide\nbody-guide\n"),
		".claude/agents/helper.md": []byte("# Helper Agent\nbody-agent\n"),
	}

	full := string(llmstxt.LLMSFull(v, r, llmstxt.Options{Title: "T"}))
	if strings.Contains(full, "body-agent") || strings.Contains(full, ".claude/agents/helper.md") {
		t.Errorf("llms-full must not carry the excluded doc:\n%s", full)
	}
	if !strings.Contains(full, "body-guide") {
		t.Errorf("llms-full must keep the non-excluded body:\n%s", full)
	}

	small := string(llmstxt.LLMSSmall(v, r, llmstxt.Options{Title: "T"}))
	if strings.Contains(small, "body-agent") || strings.Contains(small, ".claude/agents/helper.md") {
		t.Errorf("llms-small must not carry the excluded doc:\n%s", small)
	}
}

// TestLLMSTxt_EmitExclude_ReadingOrderFiltered: the suggested-reading-order
// block renders the filtered trails (no excluded steps).
func TestLLMSTxt_EmitExclude_ReadingOrderFiltered(t *testing.T) {
	v := excludeView(t)
	v.Trails = []graphmodel.Trail{{
		Root: "docs/guide.md",
		Order: []identity.DocumentID{
			"README.md", ".claude/agents/helper.md", "docs/guide.md",
		},
	}}
	v = v.WithEmitExclude([]string{".claude/agents/"})

	out := string(llmstxt.LLMSTxt(v, llmstxt.Options{Title: "T"}))
	if !strings.Contains(out, "## Suggested reading order") {
		t.Fatalf("reading order block expected:\n%s", out)
	}
	if strings.Contains(out, ".claude/agents/helper.md") {
		t.Errorf("excluded doc must not appear as a trail step:\n%s", out)
	}
}
