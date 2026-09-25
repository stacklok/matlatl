// Package mdparser implements application.DocumentParser. It is the ONLY package
// in matlatl that imports goldmark (ADR 0002): markdown parsing and the
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
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"go.abhg.dev/goldmark/frontmatter"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
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
// Concurrency (P6): a Parser is a thin VIEW over a single, shared
// goldmark.Markdown built once at factory time (see newGoldmark / NewFactory).
// Each ParseBytes call allocates its own parser.Context via parser.NewContext()
// — that Context (which goldmark threads front matter and auto-heading IDs
// through) is the ONLY per-call mutable state. The shared goldmark parser itself
// is safe for concurrent Parse calls once warmed: see the verification note on
// newGoldmark. So Factory.New/Clone hand each worker a Parser sharing the same
// goldmark.Markdown, and fan-out parsing is data-race-free without re-building
// (and re-registering the inline parser/extension on) goldmark per worker.
type Parser struct {
	md  goldmark.Markdown
	cfg Config
}

// Factory mints Parsers backed by ONE shared, pre-built goldmark.Markdown. It
// implements application.DocumentParserFactory so the pipeline can request a
// parser per worker in P6 without re-constructing goldmark per worker.
type Factory struct {
	cfg Config
	md  goldmark.Markdown // canonical configured instance, shared by all parsers
}

// NewFactory returns a parser Factory with the given config (defaults filled).
// The single canonical goldmark.Markdown is built and WARMED here, so every
// worker shares one immutable parser (see newGoldmark for the safety argument).
func NewFactory(cfg Config) *Factory {
	if cfg.MaxFrontMatterBytes <= 0 {
		cfg.MaxFrontMatterBytes = DefaultMaxFrontMatterBytes
	}
	return &Factory{cfg: cfg, md: newGoldmark()}
}

// New returns a DocumentParser that shares the Factory's goldmark instance.
func (f *Factory) New() application.DocumentParser { return &Parser{md: f.md, cfg: f.cfg} }

// Clone returns a DocumentParser safe to use on its own goroutine. It does NOT
// rebuild goldmark: each clone is a view over the Factory's single shared,
// already-warmed goldmark.Markdown (concurrency-safe — see newGoldmark), with
// per-call state isolated to the parser.Context allocated in ParseBytes.
func (f *Factory) Clone() application.DocumentParser { return &Parser{md: f.md, cfg: f.cfg} }

// compile-time assertions for the port + factory.
var (
	_ application.DocumentParser        = (*Parser)(nil)
	_ application.DocumentParserFactory = (*Factory)(nil)
)

// New returns a Parser owning its own freshly-built goldmark.Markdown. Prefer
// the Factory for fan-out parsing (it shares one warmed instance across workers);
// this standalone constructor is kept for direct, single-parser use (tests, the
// sequential fast path) and remains valid because each Parser still allocates a
// per-call parser.Context.
func New(cfg Config) *Parser {
	if cfg.MaxFrontMatterBytes <= 0 {
		cfg.MaxFrontMatterBytes = DefaultMaxFrontMatterBytes
	}
	return &Parser{md: newGoldmark(), cfg: cfg}
}

// newGoldmark builds and WARMS the canonical goldmark.Markdown for the
// project's slug dialect (ADR 0006) and YAML/TOML front matter.
//
// Concurrency safety, verified against goldmark v1.8.2 source
// (parser/parser.go func (*parser) Parse, struct parser):
//   - The parser's mutable build-time state (block/inline parser tables,
//     transformers, escapedSpace) is populated EXACTLY ONCE, guarded by a
//     `sync.Once` (p.initSync) on the FIRST Parse; that init then sets
//     `p.config = nil`. After it completes, Parse only READS those tables — it
//     never writes parser fields again.
//   - The only per-call mutable state is the ParseConfig/Context (pc): Parse
//     does `if c.Context == nil { c.Context = NewContext() }` and threads pc
//     plus a fresh root AST through the walk. We pass our own parser.NewContext()
//     per ParseBytes, so no Context is ever shared across goroutines.
//
// Therefore a single goldmark.Markdown is safe for concurrent Parse calls: the
// one-time init is serialized by sync.Once and everything after is read-only +
// per-call. To remove any reliance on the race detector blessing the very first
// concurrent init, we WARM the instance here with one throwaway parse so the
// sync.Once has already fired before any worker touches it — after newGoldmark
// returns, the parser tables are strictly immutable.
func newGoldmark() goldmark.Markdown {
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
	// Warm: trigger the parser's one-time sync.Once init now (single-threaded)
	// so all subsequent concurrent Parse calls only read immutable tables.
	md.Parser().Parse(text.NewReader(nil), parser.WithContext(parser.NewContext()))
	return md
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
	// frontMatterPresent / frontMatterParsed are pure-data signals for the OKF
	// conformance mode (ADR 0023): "was there a frontmatter block at all" vs "did
	// it decode". The oversized-guard path (ADR 0003) counts as present-but-
	// unparsed: a block WAS there, we just refused to decode it.
	var frontMatterPresent, frontMatterParsed bool
	if guarded {
		frontMatterPresent = true
	} else {
		fm, frontMatterPresent, frontMatterParsed = decodeFrontMatter(pctx)
	}

	lines := newLineIndex(src)
	doc := &corpus.Document{
		ID:                 id,
		FrontMatter:        fm,
		FrontMatterPresent: frontMatterPresent,
		FrontMatterParsed:  frontMatterParsed,
		Root:               buildSectionTree(root, src, lines),
		AnchorIDs:          staticHeadingAnchorIDs(src),
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
	"title": {}, "description": {}, "tags": {}, "aliases": {}, "name": {},
	"parent": {}, "related": {}, "status": {}, "date": {},
}

// decodeFrontMatter pulls front matter out of the parser context with a SINGLE
// decode into a generic map, then extracts the typed fields and routes the rest
// to Extra. One decode removes the double-decode attack surface. Malformed front
// matter degrades to a zero value.
//
// The two bools report, for the OKF conformance mode (ADR 0023): present — a
// frontmatter fence block was detected by goldmark's frontmatter extension; and
// parsed — that block decoded successfully. A present-but-undecodable block
// (present=true, parsed=false) is exactly OKF's "present-but-unparseable" state;
// no block at all is (false, false).
func decodeFrontMatter(pctx parser.Context) (fm corpus.FrontMatter, present, parsed bool) {
	data := frontmatter.Get(pctx)
	if data == nil {
		return corpus.FrontMatter{}, false, false
	}
	var all map[string]any
	if err := data.Decode(&all); err != nil {
		return corpus.FrontMatter{}, true, false
	}

	fm = corpus.FrontMatter{
		Title:       fmString(all, "title"),
		Description: fmString(all, "description"),
		Tags:        fmStringSlice(all, "tags"),
		Aliases:     fmStringSlice(all, "aliases"),
		Name:        fmString(all, "name"),
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
	return fm, true, true
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
		heading, slug := headingPresentation(headingText(h, src), headingSlug(h))
		sec := &corpus.Section{
			Level:     h.Level,
			Text:      heading,
			Slug:      slug,
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

var explicitHeadingID = regexp.MustCompile(`^(.*?)[ \t]+(?:\{#([A-Za-z0-9][A-Za-z0-9_.:-]*)\}|\{/\*[ \t]*#([A-Za-z0-9][A-Za-z0-9_.:-]*)[ \t]*\*/\})[ \t]*$`)

// headingPresentation recognizes Docusaurus's documented explicit heading IDs.
// They replace Goldmark's automatic slug and are omitted from the displayed text.
func headingPresentation(text, automaticSlug string) (string, string) {
	match := explicitHeadingID.FindStringSubmatch(text)
	if match == nil {
		return text, automaticSlug
	}
	id := match[2]
	if id == "" {
		id = match[3]
	}
	return strings.TrimSpace(match[1]), id
}

// staticHeadingAnchorIDs recognizes block-level literal Docusaurus <Heading> components.
// It intentionally does not attempt to lex Markdown or MDX generally: an opening
// tag must start a Markdown line after at most three spaces. Tags are parsed once,
// forward-only, with a bounded size, so malformed input cannot trigger suffix scans.
func staticHeadingAnchorIDs(src []byte) []string {
	const maxHeadingTagBytes = 16 << 10

	var ids []string
	frontMatterEnd := staticHeadingFrontMatterEnd(src)
	inFence := byte(0)
	fenceLen := 0
	lex := staticHeadingLexState{}

	for lineStart := 0; lineStart < len(src); {
		lineEnd := lineStart + bytes.IndexByte(src[lineStart:], '\n')
		if lineEnd < lineStart {
			lineEnd = len(src)
		} else {
			lineEnd++
		}
		line := src[lineStart:lineEnd]

		if lineStart < frontMatterEnd {
			lineStart = lineEnd
			continue
		}
		if lex.inLiteral() {
			lex.advance(line)
			lineStart = lineEnd
			continue
		}
		indent := markdownIndent(line)
		if inFence != 0 {
			if fenceClose(line, indent, inFence, fenceLen) {
				inFence, fenceLen = 0, 0
			}
			lineStart = lineEnd
			continue
		}
		if marker, run := fenceOpen(line, indent); marker != 0 {
			inFence, fenceLen = marker, run
			lineStart = lineEnd
			continue
		}
		if indent >= 4 || (len(line) > 0 && line[0] == '\t') {
			lineStart = lineEnd
			continue
		}

		if !lex.inLiteral() && indent <= 3 && bytes.HasPrefix(line[indent:], []byte("<Heading")) {
			if id, end, ok := staticHeadingTagID(src, lineStart+indent, maxHeadingTagBytes); ok {
				ids = append(ids, id)
				lineStart = nextLineStart(src, end)
				// The tag itself was already parsed, but its trailing bytes can open a
				// comment or literal that conceals candidates on following lines.
				lex.advance(src[end:lineStart])
				continue
			} else if end > lineStart {
				// The tag parser consumed this malformed construct. Do not reconsider
				// candidate-looking lines in it, which keeps malformed JSX linear.
				lineStart = nextLineStart(src, end)
				continue
			}
		}
		lex.advance(line)
		lineStart = lineEnd
	}
	return ids
}

// staticHeadingTagID parses one bounded opening tag. It accepts normal quoted
// JSX attributes and expression-valued non-id attributes, but exactly one id must
// be a quoted literal. end is always forward progress for a recognized opener.
func staticHeadingTagID(src []byte, start, limit int) (id string, end int, ok bool) {
	end = start + len("<Heading")
	stop := end + limit
	if stop > len(src) {
		stop = len(src)
	}
	if end >= stop || (!asciiSpace(src[end]) && src[end] != '>' && src[end] != '/') {
		return "", end, false
	}
	seenID := false
	for end < stop {
		for end < stop && asciiSpace(src[end]) {
			end++
		}
		if end >= stop {
			return "", end, false
		}
		if src[end] == '>' {
			return id, end + 1, seenID
		}
		if src[end] == '/' && end+1 < stop && src[end+1] == '>' {
			return id, end + 2, seenID
		}
		nameStart := end
		for end < stop && asciiAttr(src[end]) {
			end++
		}
		if nameStart == end {
			return "", end + 1, false
		}
		name := string(src[nameStart:end])
		for end < stop && asciiSpace(src[end]) {
			end++
		}
		if end >= stop {
			return "", end, false
		}
		if src[end] != '=' {
			if name == "id" {
				return "", end, false
			}
			continue
		}
		end++
		for end < stop && asciiSpace(src[end]) {
			end++
		}
		if end >= stop {
			return "", end, false
		}
		switch src[end] {
		case '\'', '"':
			quote := src[end]
			valueStart := end + 1
			for end++; end < stop && src[end] != quote; end++ {
				if src[end] == '<' || src[end] == '\n' || src[end] == '\r' {
					return "", end + 1, false
				}
			}
			if end >= stop {
				return "", end, false
			}
			if name == "id" {
				if seenID || !validLiteralID(string(src[valueStart:end])) {
					return "", end + 1, false
				}
				id, seenID = string(src[valueStart:end]), true
			}
			end++
		case '{':
			if name == "id" {
				return "", end + 1, false
			}
			depth := 1
			for end++; end < stop && depth > 0; end++ {
				switch src[end] {
				case '\n', '\r':
					return "", end + 1, false
				case '{':
					depth++
				case '}':
					depth--
				}
			}
			if depth != 0 {
				return "", end, false
			}
		default:
			return "", end + 1, false
		}
	}
	return "", end, false
}

func markdownIndent(line []byte) int {
	indent := 0
	for indent < len(line) && indent < 4 && line[indent] == ' ' {
		indent++
	}
	return indent
}

func fenceOpen(line []byte, indent int) (byte, int) {
	if indent > 3 || indent >= len(line) || (line[indent] != '`' && line[indent] != '~') {
		return 0, 0
	}
	marker := line[indent]
	run := 0
	for indent+run < len(line) && line[indent+run] == marker {
		run++
	}
	if run < 3 {
		return 0, 0
	}
	return marker, run
}

func fenceClose(line []byte, indent int, marker byte, minRun int) bool {
	if indent > 3 || indent >= len(line) || line[indent] != marker {
		return false
	}
	run := 0
	for indent+run < len(line) && line[indent+run] == marker {
		run++
	}
	if run < minRun {
		return false
	}
	for _, b := range line[indent+run:] {
		if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
			return false
		}
	}
	return true
}

// staticHeadingLexState tracks only contexts that can conceal a line-start JSX
// tag. It is a single forward pass: Markdown code spans (with their exact
// backtick-run delimiter), MDX comments, and JavaScript strings introduced by a
// declaration may cross lines, so candidate recognition never runs while one is open.
type staticHeadingLexState struct {
	htmlComment bool
	jsxComment  bool
	quote       byte
	codeRun     int
}

func (s staticHeadingLexState) inLiteral() bool {
	return s.htmlComment || s.jsxComment || s.quote != 0 || s.codeRun != 0
}

func (s *staticHeadingLexState) advance(line []byte) {
	jsStringLine := staticHeadingJSStringLine(line)
	for i := 0; i < len(line); {
		if s.htmlComment {
			end := bytes.Index(line[i:], []byte("-->"))
			if end < 0 {
				return
			}
			s.htmlComment, i = false, i+end+3
			continue
		}
		if s.jsxComment {
			end := bytes.Index(line[i:], []byte("*/"))
			if end < 0 {
				return
			}
			i += end + 2
			for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
				i++
			}
			if i < len(line) && line[i] == '}' {
				s.jsxComment, i = false, i+1
				continue
			}
			return
		}
		if s.quote != 0 {
			if line[i] == '\\' {
				i += 2 // escaped quote (including an escaped line ending)
				continue
			}
			if line[i] == s.quote {
				s.quote = 0
			}
			i++
			continue
		}
		if s.codeRun != 0 {
			if line[i] != '`' {
				i++
				continue
			}
			run := backtickRun(line, i)
			i += run
			if run == s.codeRun {
				s.codeRun = 0
			}
			continue
		}

		switch {
		case bytes.HasPrefix(line[i:], []byte("<!--")):
			s.htmlComment, i = true, i+4
		case bytes.HasPrefix(line[i:], []byte("{/*")):
			s.jsxComment, i = true, i+3
		case jsStringLine && (line[i] == '\'' || line[i] == '"'):
			s.quote, i = line[i], i+1
		case line[i] == '`':
			s.codeRun = backtickRun(line, i)
			i += s.codeRun
		default:
			i++
		}
	}
}

// staticHeadingJSStringLine limits multiline quote tracking to conventional MDX
// JavaScript declaration/import lines. Markdown prose commonly contains apostrophes
// and quotes, so treating every quote as JavaScript would hide later components.
func staticHeadingJSStringLine(line []byte) bool {
	line = bytes.TrimLeft(line, " \t")
	for _, keyword := range [...]string{"const", "let", "var", "export", "import"} {
		if !bytes.HasPrefix(line, []byte(keyword)) {
			continue
		}
		return len(line) == len(keyword) || line[len(keyword)] == ' ' || line[len(keyword)] == '\t'
	}
	return false
}

func backtickRun(line []byte, start int) int {
	run := 0
	for start+run < len(line) && line[start+run] == '`' {
		run++
	}
	return run
}

func staticHeadingFrontMatterEnd(src []byte) int {
	var fence []byte
	switch {
	case bytes.HasPrefix(src, []byte("---\n")), bytes.HasPrefix(src, []byte("---\r\n")):
		fence = []byte("---")
	case bytes.HasPrefix(src, []byte("+++\n")), bytes.HasPrefix(src, []byte("+++\r\n")):
		fence = []byte("+++")
	default:
		return 0
	}
	for lineStart := len(fence); lineStart < len(src); {
		lineEnd := nextLineStart(src, lineStart)
		line := bytes.TrimRight(src[lineStart:lineEnd], "\r\n")
		if bytes.Equal(line, fence) || (bytes.Equal(fence, []byte("---")) && bytes.Equal(line, []byte("..."))) {
			return lineEnd
		}
		lineStart = lineEnd
	}
	return 0
}

func nextLineStart(src []byte, offset int) int {
	if offset >= len(src) {
		return len(src)
	}
	if newline := bytes.IndexByte(src[offset:], '\n'); newline >= 0 {
		return offset + newline + 1
	}
	return len(src)
}

func validLiteralID(id string) bool {
	return id != "" && strings.TrimSpace(id) == id && !strings.ContainsAny(id, "{}<>\\\"'")
}

func asciiSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }
func asciiAttr(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-' || b == ':'
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
			// The link label ([label](dest)) is the inline children's text.
			refs = append(refs, makeRef(string(node.Destination), string(textOf(node, src)), reference.RelativeLink, node, src, origin, lines))
		case *ast.Image:
			// An image's display text is its alt text (the inline children's text).
			refs = append(refs, makeRef(string(node.Destination), string(textOf(node, src)), reference.ImageEmbed, node, src, origin, lines))
		case *ast.AutoLink:
			// <https://...> / bare-URL autolinks are always external; their display
			// text is the URL itself.
			refs = append(refs, makeRef(string(node.URL(src)), string(node.URL(src)), reference.External, node, src, origin, lines))
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
	// Display text: the wikilink alias ([[t|alias]]) when present, else the bare
	// target text ([[t]]) so the link still carries a scent-able label (ADR 0016).
	anchor := n.Display
	if anchor == "" {
		anchor = n.Target
	}
	return reference.RawReference{
		Origin:     origin,
		RawTarget:  n.Target,
		Fragment:   n.Fragment,
		Type:       typ,
		Line:       lines.lineAt(n.Offset),
		AnchorText: anchor,
	}
}

// makeRef builds a RawReference, classifying target/fragment and resolving the
// source line of the inline node.
func makeRef(dest, anchorText string, defType reference.LinkType, n ast.Node, src []byte, origin identity.DocumentID, lines *lineIndex) reference.RawReference {
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
		Origin:     origin,
		RawTarget:  target,
		Fragment:   fragment,
		Type:       typ,
		Line:       lines.lineAt(inlineOffset(n, src)),
		AnchorText: strings.TrimSpace(anchorText),
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
	// The "//" (protocol-relative) entry is the parser-side counterpart of
	// reference.IsRootAbsolute, which refuses "//" root-absolute treatment on the
	// resolver side (ADR 0022): a single leading "/" is an in-corpus root-absolute
	// link, a double leading "/" is external. Keep the two in sync.
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
