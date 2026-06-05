package llmstxt

import (
	"fmt"
	"strings"

	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
)

// Reader reads a document's raw markdown bytes (the *BodyReader satisfies this).
// It is the seam through which the full/small emitters reach the on-disk body
// while the domain stays pure — the I/O lives entirely in infrastructure.
type Reader interface {
	Read(id identity.DocumentID) ([]byte, error)
}

// LLMSFull renders llms-full.txt: the concatenated CLEAN markdown of every
// REACHABLE document, importance-ordered, each preceded by a short context
// header (path + title + a one-line situating line per the contextual-retrieval
// finding). The body is read through the root-confined Reader and front matter
// is stripped. A document whose body cannot be read is skipped with an inline
// note rather than aborting the artifact. Deterministic given the same corpus.
func LLMSFull(v emit.View, r Reader, opts Options) []byte {
	return concatDocs(v, r, opts, rankedReachable(v))
}

// LLMSSmall renders llms-small.txt: the same shape as llms-full but filtered to
// the high-signal docs only — hubs (the top HITS hubs) plus root /
// getting-started documents — for tight context windows. Importance-ordered.
func LLMSSmall(v emit.View, r Reader, opts Options) []byte {
	keep := smallSet(v)
	all := rankedReachable(v)
	filtered := make([]rankedDoc, 0, len(all))
	for _, rd := range all {
		if _, ok := keep[rd.view.ID]; ok {
			filtered = append(filtered, rd)
		}
	}
	return concatDocs(v, r, opts, filtered)
}

// concatDocs writes the shared full/small body: a corpus header, then each doc's
// context header + cleaned body.
func concatDocs(v emit.View, r Reader, opts Options, docs []rankedDoc) []byte {
	var b strings.Builder
	corpusTitle := resolveTitle(v, opts)
	fmt.Fprintf(&b, "# %s\n\n", oneLine(corpusTitle))
	fmt.Fprintf(&b, "> %s\n\n", summaryLine(v))

	if len(docs) == 0 {
		b.WriteString("_No documents._\n")
		return []byte(b.String())
	}

	for i, rd := range docs {
		d := rd.view
		// Context header (the contextual-retrieval situating line): every chunk is
		// self-describing — what it is, where it lives, and which corpus/section.
		b.WriteString("---\n\n")
		fmt.Fprintf(&b, "## %s\n\n", oneLine(d.Title))
		// The DocumentID is rendered inside a single-backtick code span, so a
		// (legal) backtick in the path must be neutralized or it would close the
		// span early and corrupt the artifact (mirrors index/index.go, ADR 0003).
		fmt.Fprintf(&b, "Path: `%s`\n\n", emit.EscapeInlineCode(d.ID.String()))
		// Both the corpus title and the category label are attacker-influenced and
		// rendered on a single line, so both are newline-collapsed (a directory
		// name may legally contain a newline on Linux).
		fmt.Fprintf(&b, "This document is part of %s, section %s.\n\n",
			oneLine(corpusTitle), oneLine(emit.CategoryLabel(d.Category)))

		body, err := r.Read(d.ID)
		if err != nil {
			fmt.Fprintf(&b, "_Body unavailable: %s_\n\n", oneLine(err.Error()))
			continue
		}
		clean := strings.TrimRight(cleanBody(body), "\n")
		b.WriteString(clean)
		b.WriteString("\n")
		if i < len(docs)-1 {
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

// smallSet is the high-signal document set for llms-small: the top HITS hubs
// plus every root / getting-started document. Returns IDs to keep.
func smallSet(v emit.View) map[identity.DocumentID]struct{} {
	var keep map[identity.DocumentID]struct{}
	if v.Metrics != nil && !v.Metrics.RootSet.Indeterminate {
		keep = identity.IDSet(v.Metrics.RootSet.Roots)
	} else {
		keep = make(map[identity.DocumentID]struct{})
	}
	for _, h := range v.TopHubs { // already top-N, importance-ordered
		keep[h.ID] = struct{}{}
	}
	// Getting-started heuristic: a doc whose path basename signals onboarding.
	for _, d := range v.Docs {
		if isGettingStarted(d.ID) {
			keep[d.ID] = struct{}{}
		}
	}
	return keep
}

// gettingStartedMarkers are basename substrings (lowercased) that signal an
// onboarding/entry doc worth keeping in the tight llms-small context.
var gettingStartedMarkers = []string{
	"getting-started", "getting_started", "gettingstarted",
	"quickstart", "quick-start", "readme", "index", "intro", "overview",
}

func isGettingStarted(id identity.DocumentID) bool {
	base := strings.ToLower(id.Base())
	for _, m := range gettingStartedMarkers {
		if strings.Contains(base, m) {
			return true
		}
	}
	return false
}
