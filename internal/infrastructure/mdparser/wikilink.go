package mdparser

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// wikilinkKind is the AST node kind for our custom [[wikilink]] / ![[embed]]
// nodes. They are walked alongside the standard inline nodes in
// extractReferences and turned into reference.RawReference values.
var wikilinkKind = ast.NewNodeKind("DoctopusWikilink")

// wikilinkNode is a parsed wikilink or embed. Resolution-relevant fields (the
// raw target and fragment) are captured verbatim; Display is kept only for
// completeness and is ignored by resolution (ADR 0001). Offset is the byte
// offset of the node's start in the source, used to derive its 1-based line via
// the document's lineIndex (consistent with how standard links are numbered).
type wikilinkNode struct {
	ast.BaseInline
	Target   string
	Fragment string
	Display  string
	Embed    bool // true for ![[...]] transclusions
	Offset   int
}

func (n *wikilinkNode) Kind() ast.NodeKind { return wikilinkKind }

// Dump implements ast.Node for debugging.
func (n *wikilinkNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{
		"Target":   n.Target,
		"Fragment": n.Fragment,
		"Embed":    boolStr(n.Embed),
	}, nil)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// wikilinkParser is a goldmark InlineParser recognizing Obsidian-style
// wikilinks and embeds:
//
//	[[target]]              wikilink
//	[[target|display]]      aliased wikilink (display ignored for resolution)
//	[[target#anchor]]       wikilink with fragment
//	![[target]]             embed/transclusion
//	![[target#anchor]]      embed with fragment
//
// It is deliberately small and single-pass. Malformed forms ([[, [[]], [[a| ,
// stray brackets, an unterminated [[ before end-of-line) are not matched and the
// reader is left untouched so other parsers / text handling proceed normally.
// The scan never crosses a line boundary: a wikilink must open and close on the
// same line.
type wikilinkParser struct{}

// Trigger fires on '[' (for [[...]]) and '!' (for the ![[...]] embed prefix).
func (wikilinkParser) Trigger() []byte { return []byte{'[', '!'} }

// Parse attempts to consume a wikilink/embed at the current position. It returns
// nil (consuming nothing) when the input is not a well-formed wikilink so that
// goldmark falls back to its standard handling.
func (wikilinkParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, seg := block.PeekLine()
	if len(line) < 4 { // shortest is "[[x]" → still need 5; bail early on tiny lines
		return nil
	}

	i := 0
	embed := false
	if line[0] == '!' {
		embed = true
		i = 1
	}
	// Require the "[[" opener at the (possibly post-'!') position.
	if i+1 >= len(line) || line[i] != '[' || line[i+1] != '[' {
		return nil
	}
	contentStart := i + 2

	// Find the closing "]]" on the same line.
	closeRel := indexCloser(line[contentStart:])
	if closeRel < 0 {
		return nil // unterminated on this line — not a wikilink
	}
	inner := string(line[contentStart : contentStart+closeRel])

	// A stray "[[" inside the content means we latched onto a malformed opener
	// (e.g. "[[ and [[]]"); refuse it so the scan resumes at the real opener.
	if strings.Contains(inner, "[[") {
		return nil
	}

	target, fragment, display, ok := parseWikilinkInner(inner)
	if !ok {
		return nil // malformed (e.g. empty target) — leave for other handling
	}

	consumed := contentStart + closeRel + 2 // include the closing "]]"
	node := &wikilinkNode{
		Target:   target,
		Fragment: fragment,
		Display:  display,
		Embed:    embed,
		Offset:   seg.Start, // byte offset of this inline's start
	}
	block.Advance(consumed)
	return node
}

// indexCloser returns the index of the first "]]" in b, or -1 if none. It does
// not cross a newline because b is a single line from PeekLine.
func indexCloser(b []byte) int {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == ']' && b[i+1] == ']' {
			return i
		}
		// A '[' inside the content (nested bracket) is tolerated as a literal;
		// we simply keep scanning for the first "]]".
	}
	return -1
}

// parseWikilinkInner splits the inner text of [[...]] into target, fragment and
// display. Order of operators: a '|' separates target-spec from display; within
// the target-spec a '#' separates path from anchor. An empty target is invalid.
// Whitespace around the parts is trimmed.
func parseWikilinkInner(inner string) (target, fragment, display string, ok bool) {
	spec := inner
	if pipe := strings.IndexByte(inner, '|'); pipe >= 0 {
		spec = inner[:pipe]
		display = strings.TrimSpace(inner[pipe+1:])
	}
	if hash := strings.IndexByte(spec, '#'); hash >= 0 {
		target = strings.TrimSpace(spec[:hash])
		fragment = strings.TrimSpace(spec[hash+1:])
	} else {
		target = strings.TrimSpace(spec)
	}
	// An anchor-only wikilink ([[#frag]]) has an empty target but a fragment —
	// that is valid (resolves within the origin document).
	if target == "" && fragment == "" {
		return "", "", "", false
	}
	return target, fragment, display, true
}
