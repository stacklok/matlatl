// Package mdparser implements application.DocumentParser. It is the ONLY package
// in doctopus that imports goldmark (ADR 0002): markdown parsing and the
// third-party AST are quarantined here, so the domain stays pure.
//
// It turns markdown bytes into a pure-domain corpus.Document: typed front matter
// (YAML/TOML), a nested Section tree, and the standard-markdown raw references
// (relative links, anchors, images, external links). Wikilink extraction is P2.
//
// Slug dialect: the parser is configured with parser.WithAutoHeadingID(), whose
// GitHub-compatible algorithm is the canonical, validated slug dialect of ADR
// 0006. The slug stored on each Section is exactly goldmark's auto heading id.
package mdparser

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"go.abhg.dev/goldmark/frontmatter"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/domain/reference"
)

// DefaultMaxFrontMatterBytes caps the size of the leading front-matter block
// that will be decoded, guarding against YAML "billion laughs" / deep-alias
// bombs (ADR 0003). A block larger than this is stripped and the document
// degrades to "no front matter" plus a notice.
const DefaultMaxFrontMatterBytes = 64 << 10 // 64 KiB

// Config tunes a Parser. The zero value is valid; New fills safe defaults.
type Config struct {
	// MaxFrontMatterBytes caps the decodable front-matter block size.
	MaxFrontMatterBytes int
}

// Parser parses markdown into corpus.Documents.
//
// Concurrency (P6 note): each Parse/ParseBytes call allocates its own
// parser.Context (the per-call mutable state goldmark threads front matter and
// IDs through), so a single Parser instance is fine for sequential use. The
// underlying goldmark parser is, however, NOT documented as safe for concurrent
// Parse calls on one instance. For fan-out parsing, do not share a Parser across
// goroutines: obtain one parser per worker via the Factory (New/Clone) and merge
// results single-threaded.
type Parser struct {
	md  goldmark.Markdown
	cfg Config
}

// Factory mints Parsers. It implements application.DocumentParserFactory so the
// pipeline can request a fresh parser per worker in P6 without a layering change.
type Factory struct {
	cfg Config
}

// NewFactory returns a parser Factory with the given config (defaults filled).
func NewFactory(cfg Config) *Factory {
	if cfg.MaxFrontMatterBytes <= 0 {
		cfg.MaxFrontMatterBytes = DefaultMaxFrontMatterBytes
	}
	return &Factory{cfg: cfg}
}

// New returns a freshly configured DocumentParser.
func (f *Factory) New() application.DocumentParser { return New(f.cfg) }

// Clone returns an independent DocumentParser safe to use on its own goroutine.
func (f *Factory) Clone() application.DocumentParser { return New(f.cfg) }

// compile-time assertions for the port + factory.
var (
	_ application.DocumentParser        = (*Parser)(nil)
	_ application.DocumentParserFactory = (*Factory)(nil)
)

// New returns a Parser with a single goldmark.Markdown configured for the
// canonical slug dialect (ADR 0006) and YAML/TOML front matter.
func New(cfg Config) *Parser {
	if cfg.MaxFrontMatterBytes <= 0 {
		cfg.MaxFrontMatterBytes = DefaultMaxFrontMatterBytes
	}
	md := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			// Register the custom wikilink/embed inline parser ahead of the
			// standard link parser (lower priority number = higher precedence)
			// so [[...]] / ![[...]] are recognized before '[' becomes a
			// CommonMark link.
			parser.WithInlineParsers(util.Prioritized(wikilinkParser{}, 100)),
		),
		goldmark.WithExtensions(&frontmatter.Extender{
			Formats: frontmatter.DefaultFormats, // YAML (---) and TOML (+++)
		}),
	)
	return &Parser{md: md, cfg: cfg}
}

// Parse reads the scanned file from disk and parses it. The file is assumed to
// already satisfy the scanner's size cap (ADR 0003). Reading is in-root because
// the scanner derived the path.
func (p *Parser) Parse(ctx context.Context, file application.ScannedFile) (*corpus.Document, error) {
	src, err := os.ReadFile(file.Path) //nolint:gosec // path is scanner-derived, in-root
	if err != nil {
		return nil, fmt.Errorf("mdparser: read %q: %w", file.Path, err)
	}
	doc, err := p.ParseBytes(ctx, file.ID, src)
	if err != nil {
		return nil, err
	}
	doc.ModTime = file.ModTime
	return doc, nil
}

// ParseBytes parses raw markdown bytes into a Document with the given identity.
// It is the testable core of Parse (no filesystem). It never fails on malformed
// front matter — that degrades to "no front matter".
func (p *Parser) ParseBytes(ctx context.Context, id identity.DocumentID, src []byte) (*corpus.Document, error) {
	// Respect cancellation before doing any (potentially non-trivial) parse work.
	// Parsing one file is cheap today, but P6 fan-out parses many concurrently and
	// must abort promptly when the run is canceled; checking here makes ParseBytes
	// itself a cancellation point rather than relying solely on the caller's loop.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("mdparser: canceled before parsing %q: %w", id, err)
	}

	// Front-matter size guard (ADR 0003): if the leading block exceeds the cap,
	// strip it before handing the source to goldmark so the bomb is never decoded.
	src, guarded := guardFrontMatter(src, p.cfg.MaxFrontMatterBytes)

	pctx := parser.NewContext()
	root := p.md.Parser().Parse(text.NewReader(src), parser.WithContext(pctx))

	fm := corpus.FrontMatter{}
	if !guarded {
		fm = decodeFrontMatter(pctx)
	}

	lines := newLineIndex(src)
	doc := &corpus.Document{
		ID:          id,
		FrontMatter: fm,
		Root:        buildSectionTree(root, src, lines),
	}
	doc.RawReferences = extractReferences(root, src, id, lines)

	// Title fallback: if front matter gave no title, use the first H1's text.
	if doc.FrontMatter.Title == "" {
		if h1 := firstH1Text(doc.Root); h1 != "" {
			doc.FrontMatter.Title = h1
		}
	}
	return doc, nil
}

// guardFrontMatter detects a leading YAML(---)/TOML(+++) block and, if it
// exceeds maxBytes, removes it from the source. The bool result is true when a
// block was stripped (i.e. front matter must be treated as absent).
func guardFrontMatter(src []byte, maxBytes int) ([]byte, bool) {
	var fence string
	switch {
	case bytes.HasPrefix(src, []byte("---\n")), bytes.HasPrefix(src, []byte("---\r\n")):
		fence = "---"
	case bytes.HasPrefix(src, []byte("+++\n")), bytes.HasPrefix(src, []byte("+++\r\n")):
		fence = "+++"
	default:
		return src, false
	}

	// Find the closing fence on its own line.
	rest := src[len(fence):]
	closeMarker := "\n" + fence
	idx := bytes.Index(rest, []byte(closeMarker))
	if idx < 0 {
		// Unterminated block: let goldmark/frontmatter handle (likely no FM).
		return src, false
	}
	blockLen := len(fence) + idx + len(closeMarker)
	if blockLen <= maxBytes {
		return src, false
	}
	// Oversized: strip the whole block (advance past the closing fence line).
	after := blockLen
	if nl := bytes.IndexByte(src[after:], '\n'); nl >= 0 {
		after += nl + 1
	} else {
		after = len(src)
	}
	return src[after:], true
}

// knownFMKeys are the lowercase front-matter keys mapped to typed FrontMatter
// fields; everything else is routed to Extra. A test (TestKnownFMKeysMatchTags)
// asserts this set equals the struct's yaml tags so a tag typo cannot silently
// misroute a known field.
var knownFMKeys = map[string]struct{}{
	"title": {}, "description": {}, "tags": {}, "aliases": {},
	"parent": {}, "related": {}, "status": {}, "date": {},
}

// decodeFrontMatter pulls front matter out of the parser context with a SINGLE
// decode into a generic map, then extracts the typed fields and routes the rest
// to Extra. One decode removes the double-decode attack surface. Malformed front
// matter degrades to a zero value.
func decodeFrontMatter(pctx parser.Context) corpus.FrontMatter {
	data := frontmatter.Get(pctx)
	if data == nil {
		return corpus.FrontMatter{}
	}
	var all map[string]any
	if err := data.Decode(&all); err != nil {
		return corpus.FrontMatter{}
	}

	fm := corpus.FrontMatter{
		Title:       fmString(all, "title"),
		Description: fmString(all, "description"),
		Tags:        fmStringSlice(all, "tags"),
		Aliases:     fmStringSlice(all, "aliases"),
		Parent:      fmString(all, "parent"),
		Related:     fmStringSlice(all, "related"),
		Status:      fmString(all, "status"),
		Date:        fmString(all, "date"),
	}
	for k, v := range all {
		if _, known := knownFMKeys[strings.ToLower(k)]; known {
			continue
		}
		if fm.Extra == nil {
			fm.Extra = make(map[string]any)
		}
		fm.Extra[k] = v
	}
	return fm
}

// fmString reads a string value for key (case-insensitive), coercing simple
// scalar types; non-string scalars are stringified, anything else yields "".
func fmString(m map[string]any, key string) string {
	v, ok := lookupCI(m, key)
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case fmt.Stringer:
		return s.String()
	case nil:
		return ""
	default:
		// Numbers/bools from TOML/YAML date or scalar fields.
		return fmt.Sprintf("%v", s)
	}
}

// fmStringSlice reads a []string for key (case-insensitive), coercing each
// element to a string. A scalar string value is treated as a single-element
// slice. Returns nil when absent.
func fmStringSlice(m map[string]any, key string) []string {
	v, ok := lookupCI(m, key)
	if !ok {
		return nil
	}
	switch arr := v.(type) {
	case []string:
		return arr
	case string:
		return []string{arr}
	case []any:
		out := make([]string, 0, len(arr))
		for _, e := range arr {
			if e == nil {
				continue
			}
			if s, isStr := e.(string); isStr {
				out = append(out, s)
			} else {
				out = append(out, fmt.Sprintf("%v", e))
			}
		}
		return out
	default:
		return nil
	}
}

// lookupCI returns the value for key, matching case-insensitively (front-matter
// keys are conventionally lowercase but we tolerate variants).
func lookupCI(m map[string]any, key string) (any, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

// buildSectionTree walks the AST headings and builds the nested Section tree
// rooted at a synthetic Level-0 section spanning the whole document. Each
// section's StartLine is its heading line; EndLine is filled in a post-pass so a
// section's line span runs up to (but not including) the next heading at the
// same-or-shallower level (ADR 0007 origin attribution).
func buildSectionTree(root ast.Node, src []byte, lines *lineIndex) *corpus.Section {
	totalLines := lines.lineCount()
	docRoot := &corpus.Section{Level: 0, Start: 0, End: len(src), StartLine: 1, EndLine: totalLines}
	stack := []*corpus.Section{docRoot}
	var ordered []*corpus.Section // pre-order list of real sections

	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		start, end := nodeSpan(h, src)
		sec := &corpus.Section{
			Level:     h.Level,
			Text:      headingText(h, src),
			Slug:      headingSlug(h),
			Start:     start,
			End:       end,
			StartLine: lines.lineAt(start),
		}
		// Pop until the top of the stack is a strictly-higher-level section.
		for len(stack) > 1 && stack[len(stack)-1].Level >= sec.Level {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1]
		sec.Parent = parent
		parent.Children = append(parent.Children, sec)
		stack = append(stack, sec)
		ordered = append(ordered, sec)
		return ast.WalkSkipChildren, nil
	})

	// EndLine: each section extends to the line before the next heading whose
	// level is <= its own (the next sibling-or-shallower boundary); the last
	// such section runs to the end of the document.
	for i, sec := range ordered {
		sec.EndLine = totalLines
		for j := i + 1; j < len(ordered); j++ {
			if ordered[j].Level <= sec.Level {
				sec.EndLine = ordered[j].StartLine - 1
				break
			}
		}
		if sec.EndLine < sec.StartLine {
			sec.EndLine = sec.StartLine
		}
	}
	return docRoot
}

// headingText returns the concatenated text of a heading's inline children.
func headingText(h *ast.Heading, src []byte) string {
	var b strings.Builder
	for c := h.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Segment.Value(src))
			continue
		}
		// Fallback for nested inlines (emphasis, code): use their raw text.
		b.WriteString(string(textOf(c, src)))
	}
	return strings.TrimSpace(b.String())
}

// textOf extracts raw text from an arbitrary inline node subtree.
func textOf(n ast.Node, src []byte) []byte {
	var buf bytes.Buffer
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if t, ok := c.(*ast.Text); ok {
			buf.Write(t.Segment.Value(src))
		}
		return ast.WalkContinue, nil
	})
	return buf.Bytes()
}

// headingSlug returns the goldmark auto heading id (ADR 0006 canonical slug).
func headingSlug(h *ast.Heading) string {
	if v, ok := h.AttributeString("id"); ok {
		switch id := v.(type) {
		case []byte:
			return string(id)
		case string:
			return id
		}
	}
	return ""
}

// nodeSpan returns the byte span [start, end) covered by a block node's lines.
func nodeSpan(n ast.Node, src []byte) (int, int) {
	lines := n.Lines()
	if lines == nil || lines.Len() == 0 {
		return 0, 0
	}
	first := lines.At(0)
	last := lines.At(lines.Len() - 1)
	start := first.Start
	end := last.Stop
	if end > len(src) {
		end = len(src)
	}
	return start, end
}

// firstH1Text returns the text of the first level-1 heading in the tree, or "".
func firstH1Text(root *corpus.Section) string {
	for _, c := range root.Children {
		if c.Level == 1 {
			return c.Text
		}
		if t := firstH1Text(c); t != "" {
			return t
		}
	}
	return ""
}

// extractReferences collects outbound edges from the AST: standard markdown
// links/images/autolinks (P1) plus the custom [[wikilink]] / ![[embed]] nodes
// (P2).
func extractReferences(root ast.Node, src []byte, origin identity.DocumentID, lines *lineIndex) []reference.RawReference {
	var refs []reference.RawReference
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.Link:
			refs = append(refs, makeRef(string(node.Destination), reference.RelativeLink, node, src, origin, lines))
		case *ast.Image:
			refs = append(refs, makeRef(string(node.Destination), reference.ImageEmbed, node, src, origin, lines))
		case *ast.AutoLink:
			// <https://...> / bare-URL autolinks are always external.
			refs = append(refs, makeRef(string(node.URL(src)), reference.External, node, src, origin, lines))
		case *wikilinkNode:
			refs = append(refs, makeWikilinkRef(node, origin, lines))
		}
		return ast.WalkContinue, nil
	})
	return refs
}

// makeWikilinkRef converts a parsed wikilink/embed node into a RawReference.
// Embeds (![[...]]) classify as Transclusion; plain [[...]] as Wikilink. An
// anchor-only wikilink ([[#frag]]) is classified as Anchor (resolves within the
// origin document, like [](#frag)).
func makeWikilinkRef(n *wikilinkNode, origin identity.DocumentID, lines *lineIndex) reference.RawReference {
	typ := reference.Wikilink
	switch {
	case n.Embed:
		typ = reference.Transclusion
	case n.Target == "" && n.Fragment != "":
		typ = reference.Anchor
	}
	return reference.RawReference{
		Origin:    origin,
		RawTarget: n.Target,
		Fragment:  n.Fragment,
		Type:      typ,
		Line:      lines.lineAt(n.Offset),
	}
}

// makeRef builds a RawReference, classifying target/fragment and resolving the
// source line of the inline node.
func makeRef(dest string, defType reference.LinkType, n ast.Node, src []byte, origin identity.DocumentID, lines *lineIndex) reference.RawReference {
	target, fragment := splitFragment(dest)
	typ := defType
	switch {
	case isExternal(dest):
		typ = reference.External
	case target == "" && fragment != "" && defType == reference.RelativeLink:
		// A pure same-document anchor like [x](#heading).
		typ = reference.Anchor
	}
	return reference.RawReference{
		Origin:    origin,
		RawTarget: target,
		Fragment:  fragment,
		Type:      typ,
		Line:      lines.lineAt(inlineOffset(n, src)),
	}
}

// splitFragment separates a link destination into its path and fragment parts.
func splitFragment(dest string) (target, fragment string) {
	if i := strings.IndexByte(dest, '#'); i >= 0 {
		return dest[:i], dest[i+1:]
	}
	return dest, ""
}

// isExternal reports whether a destination is an off-corpus URL. file:// and
// data: are included so they classify as External (HealthExternal) rather than
// being treated as in-corpus relative paths — a latent SSRF/local-file-read
// hazard for the opt-in P6 --check-external path (ADR 0003).
func isExternal(dest string) bool {
	lower := strings.ToLower(dest)
	for _, p := range []string{"http://", "https://", "mailto:", "ftp://", "file://", "data:", "//"} {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// inlineOffset returns the best-effort byte offset of an inline node, used to
// derive its source line. Inline nodes do not carry Lines(), so we first look
// for a reachable text segment within the node; failing that (e.g. AutoLink,
// whose value text is not a child node), we walk up to the nearest ancestor
// block that does carry Lines() and use its start. The result is clamped to src.
func inlineOffset(n ast.Node, src []byte) int {
	off := -1
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || off >= 0 {
			return ast.WalkContinue, nil
		}
		if t, ok := c.(*ast.Text); ok {
			off = t.Segment.Start
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	if off < 0 {
		off = siblingOffset(n)
	}
	if off < 0 {
		off = enclosingBlockStart(n)
	}
	if off < 0 {
		off = 0
	}
	if off > len(src) {
		off = len(src)
	}
	return off
}

// siblingOffset estimates a node's offset from adjacent text siblings: the end
// of the previous text sibling, else the start of the next text sibling. This
// pins inline nodes without a reachable text segment (e.g. AutoLink) to the
// correct source line even inside a multi-line paragraph.
func siblingOffset(n ast.Node) int {
	for p := n.PreviousSibling(); p != nil; p = p.PreviousSibling() {
		if t, ok := p.(*ast.Text); ok {
			return t.Segment.Stop
		}
	}
	for s := n.NextSibling(); s != nil; s = s.NextSibling() {
		if t, ok := s.(*ast.Text); ok {
			return t.Segment.Start
		}
	}
	return -1
}

// enclosingBlockStart returns the byte start of the nearest ancestor block that
// carries line information, or -1 if none.
func enclosingBlockStart(n ast.Node) int {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if lines := p.Lines(); lines != nil && lines.Len() > 0 {
			return lines.At(0).Start
		}
	}
	return -1
}
