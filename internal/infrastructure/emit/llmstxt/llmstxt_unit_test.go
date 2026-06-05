package llmstxt_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/graphmodel"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/domain/reference"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
	"github.com/stacklok/doctopus/internal/infrastructure/emit/llmstxt"
)

// doc builds a minimal corpus.Document with a single H1 section so it carries a
// title and a stable identity for synthetic-view tests.
func doc(id identity.DocumentID, title string) *corpus.Document {
	return &corpus.Document{
		ID:          id,
		FrontMatter: corpus.FrontMatter{Title: title},
		Root: &corpus.Section{Level: 0, Children: []*corpus.Section{
			{Level: 1, Text: title, Slug: "h1", StartLine: 1, EndLine: 2},
		}},
	}
}

// link builds a Valid document-level relative reference edge (origin → target).
func link(origin, target identity.DocumentID) reference.Reference {
	return reference.Reference{
		RawReference: reference.RawReference{
			Origin:    origin,
			RawTarget: target.String(),
			Type:      reference.RelativeLink,
			Line:      1,
		},
		Target: reference.ResolvedTarget{Kind: reference.TargetDocument, DocumentID: target},
		Health: reference.Valid,
	}
}

// buildSyntheticView assembles a render-ready View from explicit documents and
// resolved reference edges, running the real graph analysis so HITS/roots/
// reachability are computed exactly as in production.
func buildSyntheticView(t *testing.T, docs []*corpus.Document, refs []reference.Reference) emit.View {
	t.Helper()
	c := corpus.NewCorpus()
	for _, d := range docs {
		if err := c.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	g := graphmodel.BuildReferenceGraph(c, refs, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	return emit.BuildView(application.Result{
		DocumentCount:  c.Len(),
		ReferenceCount: len(refs),
		Metrics:        m,
		Corpus:         c,
	})
}

// TestLLMSTxt_LinkPathPercentEncodes is fix #2: a DocumentID containing '#' or
// '%' must be percent-encoded in the curated llms.txt link target, or a
// CommonMark parser would read the '#' as a fragment delimiter (a silently
// broken link / fragment-forgery surface). With no conventional root the root
// set is indeterminate, so every document is surfaced.
func TestLLMSTxt_LinkPathPercentEncodes(t *testing.T) {
	hash := identity.DocumentID("notes#old.md")
	pct := identity.DocumentID("a%2fb.md")
	v := buildSyntheticView(t, []*corpus.Document{
		doc(hash, "Hash Doc"),
		doc(pct, "Percent Doc"),
	}, nil)

	out := string(llmstxt.LLMSTxt(v, llmstxt.Options{Title: "T"}))

	// The raw '#' / '%' must never appear inside a link target.
	if strings.Contains(out, "(notes#old.md)") {
		t.Errorf("'#' in DocumentID not encoded — link target is broken:\n%s", out)
	}
	if !strings.Contains(out, "(notes%23old.md)") {
		t.Errorf("expected '#' encoded as %%23 in link target:\n%s", out)
	}
	// '%' must be encoded first (to %25) so it is not double-encoded.
	if !strings.Contains(out, "(a%252fb.md)") {
		t.Errorf("expected '%%' encoded as %%25 (encoded first):\n%s", out)
	}
}

// TestLLMSSmall_Filters is fix #3a: llms-small keeps hubs + root /
// getting-started docs and EXCLUDES a reachable non-hub leaf that llms-full
// includes. The fixture has 6 docs with outbound edges (positive hub scores) and
// one reachable leaf with none, so the leaf falls outside the top-5 hubs and is
// neither a root nor getting-started.
func TestLLMSSmall_Filters(t *testing.T) {
	root := identity.DocumentID("README.md") // a conventional root
	leaf := identity.DocumentID("docs/leaf.md")
	hubs := []identity.DocumentID{
		"docs/h1.md", "docs/h2.md", "docs/h3.md", "docs/h4.md", "docs/h5.md",
	}
	authority := identity.DocumentID("docs/authority.md")

	docs := make([]*corpus.Document, 0, 3+len(hubs))
	docs = append(docs, doc(root, "Home"), doc(leaf, "Leaf"), doc(authority, "Authority"))
	for _, h := range hubs {
		docs = append(docs, doc(h, "Hub "+h.Base()))
	}

	var refs []reference.Reference
	// Root links to every doc so all are reachable.
	for _, d := range docs {
		if d.ID != root {
			refs = append(refs, link(root, d.ID))
		}
	}
	// Each hub points at the authority → positive hub score; the leaf points at
	// nothing → hub score 0, so it ranks below the top-5 hubs.
	for _, h := range hubs {
		refs = append(refs, link(h, authority))
	}

	v := buildSyntheticView(t, docs, refs)
	full := string(llmstxt.LLMSFull(v, &nopReader{}, llmstxt.Options{}))
	small := string(llmstxt.LLMSSmall(v, &nopReader{}, llmstxt.Options{}))

	if !strings.Contains(full, "Path: `docs/leaf.md`") {
		t.Errorf("llms-full should include the reachable leaf:\n%s", full)
	}
	if strings.Contains(small, "Path: `docs/leaf.md`") {
		t.Errorf("llms-small should EXCLUDE the non-hub/non-root/non-getting-started leaf:\n%s", small)
	}
	// The root must be kept in small (sanity: the filter is not dropping everything).
	if !strings.Contains(small, "Path: `README.md`") {
		t.Errorf("llms-small should keep the root README:\n%s", small)
	}
}

// TestLLMSTxt_OptionalThreshold is fix #3b: with more than optionalThreshold (20)
// reachable docs, llms.txt spills the lower-signal remainder into a "## Optional"
// section, importance-ordered (the higher-importance docs stay in Documentation).
func TestLLMSTxt_OptionalThreshold(t *testing.T) {
	const n = 25 // > optionalThreshold (20)
	root := identity.DocumentID("README.md")
	docs := []*corpus.Document{doc(root, "Home")}
	var refs []reference.Reference
	for i := 0; i < n; i++ {
		id := identity.DocumentID(fmt.Sprintf("docs/d%02d.md", i))
		docs = append(docs, doc(id, fmt.Sprintf("Doc %02d", i)))
		refs = append(refs, link(root, id)) // reachable from the root
	}
	// Give d00 inbound links from many docs so it ranks high (stays in Documentation),
	// and leave d24 with no inbound links so it lands in Optional.
	high := identity.DocumentID("docs/d00.md")
	for i := 1; i < n; i++ {
		from := identity.DocumentID(fmt.Sprintf("docs/d%02d.md", i))
		refs = append(refs, link(from, high))
	}

	v := buildSyntheticView(t, docs, refs)
	out := string(llmstxt.LLMSTxt(v, llmstxt.Options{Title: "T"}))

	optIdx := strings.Index(out, "## Optional")
	if optIdx < 0 {
		t.Fatalf("expected an Optional section for >%d reachable docs:\n%s", 20, out)
	}
	docsIdx := strings.Index(out, "## Documentation")
	if docsIdx < 0 || docsIdx > optIdx {
		t.Fatalf("Documentation section must precede Optional:\n%s", out)
	}

	// The high-importance doc must be in the Documentation section (before Optional),
	// and the low-signal d24 must be in the Optional section (after the header).
	highIdx := strings.Index(out, "(docs/d00.md)")
	lowIdx := strings.Index(out, "(docs/d24.md)")
	if highIdx < 0 || lowIdx < 0 {
		t.Fatalf("expected both d00 and d24 in output:\n%s", out)
	}
	if highIdx >= optIdx {
		t.Errorf("high-importance d00 should be in Documentation (before Optional):\n%s", out)
	}
	if lowIdx <= optIdx {
		t.Errorf("low-signal d24 should be under Optional:\n%s", out)
	}
}

// nopReader is a Reader whose body is always empty (the body content is
// irrelevant to the filtering/threshold tests, which key on the path headers).
type nopReader struct{}

func (*nopReader) Read(identity.DocumentID) ([]byte, error) { return nil, nil }
