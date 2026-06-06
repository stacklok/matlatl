package graphmodel

import (
	"cmp"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// LowScentThreshold is the Jaccard-similarity floor below which a navigational
// link's anchor text is flagged as low-scent (ADR 0016). A link whose label
// shares fewer than 20% of its meaningful tokens with the target's title gives a
// reader (or agent) almost no preview of where it leads — Pirolli & Card's
// "information scent" (1999): a weak scent makes a corpus hard to forage. Below
// this the anchor is reported; at or above it the link is considered to carry
// enough scent. Not a hard cutoff for any gating — the finding is always Info.
const LowScentThreshold = 0.20

// ScentFinding is one low-scent navigational link (ADR 0016): the source
// document and line, the anchor text as written, the target it points at, the
// computed Jaccard score against the target's title, and the suggested
// replacement (the target's title). Pure data; the application layer turns it
// into a non-gating Info finding.
type ScentFinding struct {
	Source     identity.DocumentID
	Target     identity.DocumentID
	Line       int
	AnchorText string
	Score      float64
	Suggestion string
}

// scentFreePhrases is the set of NORMALIZED (lowercased, whitespace-collapsed,
// trimmed) anchor phrases that carry no scent by construction — generic "click
// here" / "read more" labels that tell a reader nothing about the destination
// (ADR 0016). A link whose whole anchor normalizes to one of these scores 0.0
// (maximally low scent) regardless of the target title. Kept as an in-source
// constant set (documented here) so the dialect is auditable and deterministic.
var scentFreePhrases = map[string]struct{}{
	"here": {}, "click here": {}, "this": {}, "this page": {}, "this document": {},
	"this link": {}, "this article": {}, "this section": {}, "read more": {},
	"read this": {}, "learn more": {}, "see more": {}, "more": {}, "more info": {},
	"more information": {}, "more details": {}, "continue": {}, "continue reading": {},
	"keep reading": {}, "go here": {}, "visit": {}, "visit here": {}, "link": {},
	"view": {}, "see": {}, "open": {}, "download": {}, "get": {}, "click": {},
	"tap here": {}, "the link": {}, "the page": {}, "the document": {},
	"source": {}, "url": {}, "http": {}, "https": {},
}

// scentStopwords are common function words dropped before scoring an anchor or
// title (ADR 0016), so a shared "the"/"of" does not inflate similarity. Kept as
// an in-source constant set, documented alongside scentFreePhrases.
var scentStopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {},
	"by": {}, "for": {}, "from": {}, "how": {}, "in": {}, "into": {}, "is": {},
	"it": {}, "of": {}, "on": {}, "or": {}, "the": {}, "to": {}, "with": {},
	"your": {}, "you": {},
}

// ComputeScent scores every navigational, in-corpus REFERENCE edge's anchor text
// against its target document's title and returns the low-scent links (ADR 0016).
// It runs on the GRAPH (the resolved reference edges carry the anchor text and
// source line), not on a refs parameter, so the analysis stays a method on the
// graph. The corpus supplies target titles (corpus.Document.Title, the same
// resolution the emit layer uses, so they cannot drift).
//
// Per navigational reference edge with an in-corpus document target:
//   - Normalize the anchor (lowercase, collapse whitespace, trim). If it is in
//     scentFreePhrases → score 0.0 (flagged). If the RAW anchor is wholly
//     backtick-wrapped (a code identifier like `Foo`) → SKIP (no finding; code
//     identifiers are legitimate labels).
//   - Tokenize anchor and title (lowercase, split on non-letter/digit, drop
//     stopwords and length-1 tokens, sort+dedup). An empty anchor token set
//     (bare URL / numeric) → score 0.0. If the title yields no tokens, fall back
//     to the union of the target's heading texts.
//   - score = Jaccard(anchorTokens, titleTokens) = |∩| / |∪| (sorted merge-walk;
//     the single division is the only float). A finding is emitted when
//     score < LowScentThreshold.
//
// Two edges are never flagged, regardless of score:
//   - Synthetic directory-expansion edges (ADR 0008): a directory link expands
//     into one anchor-less, line-0 "vouch" edge per directory member. A finding
//     must point at an authored link with a source line, so edges with Line <= 0
//     are skipped (this removes the phantom empty-anchor findings).
//   - Stable-identifier anchors (ADR 0016): an anchor naming the target's stable
//     identifier (e.g. "ADR 0010" → 0010-*.md) points the reader at the exact doc
//     and is exempt via namesTargetIdentifier — but a bare path-/filename-like
//     anchor (e.g. "docs/dev-guide.md") is NOT exempt and stays flagged.
//
// Determinism (CLAUDE.md): edges are iterated in sorted order, token sets are
// sorted, the Jaccard intersection/union are sorted merge-walks (never map
// ranging), and findings are returned sorted by (Source, Line, Target,
// AnchorText). There is NO count cap on scent findings (bounded by the link
// count); like the no-silent-cap convention, this is a deliberate decision
// stated in ADR 0016.
func (g *ReferenceGraph) ComputeScent(c *corpus.Corpus) []ScentFinding {
	if c == nil {
		return nil
	}
	titleTokenCache := make(map[identity.DocumentID][]string)
	target := func(id identity.DocumentID) []string {
		if toks, ok := titleTokenCache[id]; ok {
			return toks
		}
		toks := targetTokens(c, id)
		titleTokenCache[id] = toks
		return toks
	}
	// targetTitle resolves the target's display title (the suggested replacement
	// anchor), falling back to the DocumentID via corpus.Document.Title.
	targetTitle := func(id identity.DocumentID) string {
		if doc, ok := c.Get(id); ok {
			return doc.Title()
		}
		return id.String()
	}

	var out []ScentFinding
	for _, e := range g.Edges() { // sorted (From, To, Kind, Type)
		if e.Kind != EdgeReference {
			continue
		}
		if _, nav := g.navSet[e.Type]; !nav {
			continue
		}
		from := g.docOf(e.From)
		to := g.docOf(e.To)
		if from == "" || to == "" {
			continue
		}
		// Skip synthetic directory-expansion edges (ADR 0008): a directory link
		// `[text](somedir/)` expands in addDirectoryEdges into one "vouch" edge per
		// directory member, carrying NO anchor text and Line 0. A scent finding must
		// point at an authored link with a source line, so we skip any edge with no
		// line (ADR 0016). This also means an authored directory link's own text is
		// not scored — acceptable, since its target is a folder, not a titled doc.
		if e.Line <= 0 {
			continue
		}
		raw := e.AnchorText
		if isBacktickWrapped(raw) {
			continue // a code identifier label — legitimate, not low-scent
		}

		score := scentScore(raw, target(to))
		if score >= LowScentThreshold {
			continue
		}
		// Stable-identifier exemption (ADR 0016): an anchor that names the target's
		// stable identifier (e.g. "ADR 0010" → 0010-*.md) points the reader at the
		// exact doc, so it is NOT low-scent even when the Jaccard score is low. Bare
		// path-/filename-like anchors are explicitly NOT exempt (see
		// namesTargetIdentifier) so raw-path anchors stay flagged.
		if namesTargetIdentifier(raw, to) {
			continue
		}
		out = append(out, ScentFinding{
			Source:     from,
			Target:     to,
			Line:       e.Line,
			AnchorText: raw,
			Score:      score,
			Suggestion: targetTitle(to),
		})
	}

	slices.SortFunc(out, func(a, b ScentFinding) int {
		if c := cmp.Compare(a.Source, b.Source); c != 0 {
			return c
		}
		switch {
		case a.Line < b.Line:
			return -1
		case a.Line > b.Line:
			return 1
		}
		if c := cmp.Compare(a.Target, b.Target); c != 0 {
			return c
		}
		return strings.Compare(a.AnchorText, b.AnchorText)
	})
	return out
}

// namesTargetIdentifier reports whether rawAnchor names the target document's
// stable identifier (ADR 0016) — e.g. "ADR 0010" naming docs/adr/0010-*.md. Such
// an anchor points the reader at the EXACT doc, so it is exempt from the
// low-scent finding even when its Jaccard score is low.
//
// It FIRST rejects path-/filename-like anchors: if the lowercased raw anchor
// contains "/" or ".md", it returns false — a bare-path anchor like
// "docs/dev-guide.md" or "adr/0002-bar.md" is exactly the anti-pattern we WANT
// flagged, even though it may contain the identifier token. Otherwise it exempts
// the anchor iff its token set contains the target's identifier segment.
func namesTargetIdentifier(rawAnchor string, target identity.DocumentID) bool {
	lower := strings.ToLower(rawAnchor)
	if strings.Contains(lower, "/") || strings.Contains(lower, ".md") {
		return false // path-/filename-like anchor: not exempt (stays flagged)
	}
	seg := identifierSegment(target)
	if seg == "" {
		return false
	}
	return slices.Contains(tokenize(rawAnchor), seg)
}

// identifierSegment returns the target's stable identifier segment, or "" when it
// has none (ADR 0016). It takes the DocumentID basename, strips the extension,
// lowercases it, takes the first "-"/"_"-delimited segment, and returns that
// segment only when it has length > 1 AND contains a digit. So
// docs/adr/0010-agent-scaffolding.md → "0010", v1-getting-started.md → "v1",
// while dev-guide.md → "" and README.md → "".
func identifierSegment(target identity.DocumentID) string {
	base := path.Base(target.String())
	base = strings.TrimSuffix(base, path.Ext(base))
	base = strings.ToLower(base)
	seg := base
	if i := strings.IndexAny(base, "-_"); i >= 0 {
		seg = base[:i]
	}
	if len(seg) <= 1 {
		return ""
	}
	if !strings.ContainsFunc(seg, unicode.IsDigit) {
		return ""
	}
	return seg
}

// scentScore computes the anchor's scent score in [0,1] against the target's
// title tokens. A scent-free phrase or an empty/numeric anchor scores 0.0 (always
// emitted, since 0 < threshold). Backtick-wrapped code identifiers are skipped by
// the caller before this is reached.
func scentScore(rawAnchor string, titleTokens []string) float64 {
	norm := normalizeAnchor(rawAnchor)
	if _, free := scentFreePhrases[norm]; free {
		return 0.0
	}
	anchorTokens := tokenize(rawAnchor)
	if len(anchorTokens) == 0 {
		return 0.0 // bare URL / numeric / punctuation-only
	}
	return jaccard(anchorTokens, titleTokens)
}

// targetTokens returns the scoreable tokens of a target document's title, or,
// when the title yields none, the union of its heading texts (ADR 0016).
func targetTokens(c *corpus.Corpus, id identity.DocumentID) []string {
	doc, ok := c.Get(id)
	if !ok {
		return nil
	}
	toks := tokenize(doc.Title())
	if len(toks) > 0 {
		return toks
	}
	// Fallback: the union of the document's heading texts.
	var headings strings.Builder
	for _, h := range doc.HeadingTexts() {
		headings.WriteByte(' ')
		headings.WriteString(h)
	}
	return tokenize(headings.String())
}

// normalizeAnchor lowercases, collapses internal whitespace to single spaces and
// trims, for the scent-free-phrase lookup.
func normalizeAnchor(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// isBacktickWrapped reports whether the trimmed anchor is wholly wrapped in
// backticks (a code identifier like `Foo` or “a`b“), so it is skipped.
func isBacktickWrapped(s string) bool {
	t := strings.TrimSpace(s)
	if len(t) < 2 || t[0] != '`' || t[len(t)-1] != '`' {
		return false
	}
	// There must be at least one non-backtick character between the fences.
	inner := strings.Trim(t, "`")
	return inner != ""
}

// tokenize lowercases s, splits on any non-(letter|digit) rune, drops stopwords
// and length-1 tokens, then sorts and de-duplicates. The result is a sorted,
// unique token set suitable for the sorted-merge Jaccard.
func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) <= 1 {
			continue
		}
		if _, stop := scentStopwords[f]; stop {
			continue
		}
		out = append(out, f)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// jaccard returns |a∩b| / |a∪b| for two SORTED, de-duplicated token sets via a
// single merge-walk (never map ranging). Two empty sets are perfectly similar
// (1.0) — but callers never pass an empty anchor set here (it short-circuits to
// 0.0 in scentScore); an empty title set with a non-empty anchor yields 0.0.
func jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	inter, union := 0, 0
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			i++
			union++
		case a[i] > b[j]:
			j++
			union++
		default:
			i++
			j++
			inter++
			union++
		}
	}
	union += (len(a) - i) + (len(b) - j)
	if union == 0 {
		return 0.0
	}
	return float64(inter) / float64(union)
}
