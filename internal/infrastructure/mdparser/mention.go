package mdparser

import (
	"bytes"
	"cmp"
	"path"
	"slices"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// Unlinked-mention extraction (ADR 0026). The parser finds path-shaped tokens,
// bare markdown file names, and configured name-prefixed invocations in prose
// code spans and HTML comments; the domain MentionResolver decides which of
// them name an in-corpus document. Text already inside link syntax (links,
// images, autolinks, wikilinks), fenced/indented code blocks, and raw HTML
// other than comments is never scanned, so a link stays a link edge and is
// never double-counted. An HTML comment is scanned because it is prose a
// reader of the source (or an agent) follows even though it never renders.
// Link reference definitions are consumed by goldmark before the AST is built,
// so they never surface as text either.

// textRun is a contiguous [start, stop) byte span of source text.
type textRun struct{ start, stop int }

// extractMentions walks the AST and returns the document's raw mentions in
// source order. prefixes are the configured invocation prefixes (possibly
// none); path and file-name mentions are always extracted.
func extractMentions(root ast.Node, src []byte, origin identity.DocumentID, lines *lineIndex, prefixes []string) []reference.RawMention {
	var (
		runs []textRun
		cur  textRun
		open bool
	)
	flush := func() {
		if open {
			runs = append(runs, cur)
			open = false
		}
	}
	// add appends a text segment, merging it into the current run when it is
	// byte-adjacent. goldmark splits a single word into several Text nodes at
	// inline-trigger characters (e.g. the '_' in foo_bar.md), and merging
	// restores the word exactly as written.
	add := func(seg text.Segment) {
		if open && seg.Start == cur.stop {
			cur.stop = seg.Stop
			return
		}
		flush()
		cur, open = textRun{start: seg.Start, stop: seg.Stop}, true
	}

	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.HTMLBlock:
			flush()
			if isHTMLComment(src, node.Lines()) {
				for i := 0; i < node.Lines().Len(); i++ {
					add(node.Lines().At(i))
				}
				if node.HasClosure() {
					add(node.ClosureLine)
				}
				flush()
			}
			return ast.WalkSkipChildren, nil
		case *ast.RawHTML:
			flush()
			if isHTMLComment(src, node.Segments) {
				for i := 0; i < node.Segments.Len(); i++ {
					add(node.Segments.At(i))
				}
				flush()
			}
			return ast.WalkSkipChildren, nil
		case *ast.Link, *ast.Image, *ast.AutoLink, *wikilinkNode,
			*ast.FencedCodeBlock, *ast.CodeBlock:
			flush()
			return ast.WalkSkipChildren, nil
		case *ast.CodeSpan:
			// A code span is scanned as its own run(s): `.claude/rules/x.md` and
			// `/triage-cve CVE-1` are the canonical mention forms.
			flush()
			for c := node.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					add(t.Segment)
				}
			}
			flush()
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			add(node.Segment)
		default:
			if n.Type() == ast.TypeBlock {
				flush()
			}
		}
		return ast.WalkContinue, nil
	})
	flush()

	type located struct {
		offset int
		raw    reference.RawMention
	}
	var found []located
	emit := func(offset int, kind reference.MentionKind, target, prefix, txt string) {
		found = append(found, located{offset: offset, raw: reference.RawMention{
			Origin: origin,
			Kind:   kind,
			Target: target,
			Prefix: prefix,
			Text:   txt,
			Line:   lines.lineAt(offset),
		}})
	}
	for _, r := range runs {
		scanRun(src, r, prefixes, emit)
	}
	slices.SortStableFunc(found, func(a, b located) int { return cmp.Compare(a.offset, b.offset) })
	out := make([]reference.RawMention, 0, len(found))
	for _, f := range found {
		out = append(out, f.raw)
	}
	return out
}

// isHTMLComment reports whether raw HTML segments open with an HTML comment.
func isHTMLComment(src []byte, segs *text.Segments) bool {
	if segs == nil || segs.Len() == 0 {
		return false
	}
	first := segs.At(0)
	return bytes.HasPrefix(bytes.TrimLeft(first.Value(src), " \t"), []byte("<!--"))
}

// scanRun tokenizes one run into whitespace-delimited words and reports every
// mention candidate in it. A word that is (or contains) a URL is skipped whole,
// so `https://host/docs/x.md` never yields a path or an invocation.
func scanRun(src []byte, r textRun, prefixes []string, emit func(offset int, kind reference.MentionKind, target, prefix, txt string)) {
	for i := r.start; i < r.stop; {
		if asciiSpace(src[i]) {
			i++
			continue
		}
		start := i
		for i < r.stop && !asciiSpace(src[i]) {
			i++
		}
		word := src[start:i]
		if isURLWord(word) {
			continue
		}
		scanPathTokens(word, start, emit)
		for _, p := range prefixes {
			scanInvocations(src, word, start, p, emit)
		}
	}
}

// isURLWord reports whether a whitespace-delimited word carries a URL.
func isURLWord(word []byte) bool {
	lower := bytes.ToLower(word)
	return bytes.Contains(lower, []byte("://")) ||
		bytes.Contains(lower, []byte("mailto:")) ||
		bytes.HasPrefix(bytes.TrimLeft(lower, "(<[\"'`"), []byte("www."))
}

// isPathByte reports whether b may appear in a path-shaped mention token.
func isPathByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' ||
		b == '.' || b == '_' || b == '-' || b == '/' || b == '~' || b == '+' || b == '@'
}

// isNameByte reports whether b may appear in an invocation name.
func isNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' ||
		b == '.' || b == '_' || b == '-' || b == ':'
}

func isAlnum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func isAlnumRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// scanPathTokens reports the path and bare-file-name mentions in one word. A
// token is a maximal run of path bytes with trailing sentence dots removed.
//
//   - path: it contains a '/' beyond a leading one (`docs/x.md`, `.claude/x/`,
//     `../y.md`), or it is a leading-'/' markdown path (`/docs/x.md`).
//   - filename: it has no '/' and ends in a markdown extension (`metrics.md`).
//
// A token starting with "//" (protocol-relative) or directly after a ':'
// (`scheme:path`) is never a mention, and neither is one with no letter or
// digit (`./`, `../`).
func scanPathTokens(word []byte, wordStart int, emit func(offset int, kind reference.MentionKind, target, prefix, txt string)) {
	for i := 0; i < len(word); {
		if !isPathByte(word[i]) {
			i++
			continue
		}
		start := i
		for i < len(word) && isPathByte(word[i]) {
			i++
		}
		tok := strings.TrimRight(string(word[start:i]), ".")
		if tok == "" || strings.HasPrefix(tok, "//") || (start > 0 && word[start-1] == ':') {
			continue
		}
		// A token touching a glob character is a pattern (`*-overlay.md`,
		// `docs/*.md`), not a reference to one file.
		if (start > 0 && isGlobByte(word[start-1])) || (i < len(word) && isGlobByte(word[i])) {
			continue
		}
		if !strings.ContainsFunc(tok, isAlnumRune) {
			continue
		}
		md := identity.IsMarkdownPath(tok) && hasStem(tok)
		switch {
		case strings.Contains(strings.TrimLeft(tok, "/"), "/") || (strings.HasPrefix(tok, "/") && md):
			emit(wordStart+start, reference.MentionPath, tok, "", tok)
		case !strings.Contains(tok, "/") && md:
			emit(wordStart+start, reference.MentionFilename, tok, "", tok)
		}
	}
}

func isGlobByte(b byte) bool {
	return b == '*' || b == '?' || b == '[' || b == ']' || b == '{' || b == '}'
}

// hasStem reports whether a markdown path's base name has a non-empty stem
// (rejecting a bare ".md").
func hasStem(p string) bool {
	base := path.Base(p)
	return strings.Trim(strings.TrimSuffix(base, path.Ext(base)), ".") != ""
}

// scanInvocations reports every `prefix + name` invocation in one word. The
// byte before the prefix (looked up in the full source, so a run boundary
// cannot hide it) must not be a path, word or URL character: that is what keeps
// `https://host/panel-review` and `docs/x/panel-review` from matching. A name
// directly followed by '/' is a path, not an invocation, and a name ending in a
// markdown extension is left to the path/file-name scan.
func scanInvocations(src, word []byte, wordStart int, prefix string, emit func(offset int, kind reference.MentionKind, target, prefix, txt string)) {
	p := []byte(prefix)
	for k := 0; k+len(p) <= len(word); {
		idx := bytes.Index(word[k:], p)
		if idx < 0 {
			return
		}
		k += idx
		abs := wordStart + k
		if abs > 0 && blocksInvocation(src[abs-1]) {
			k += len(p)
			continue
		}
		j := k + len(p)
		for j < len(word) && isNameByte(word[j]) {
			j++
		}
		name := strings.TrimRight(string(word[k+len(p):j]), ".:")
		next := k + len(p) + len(name)
		followedBySlash := j < len(word) && word[j] == '/'
		if name != "" && isAlnum(name[0]) && !followedBySlash && !identity.IsMarkdownPath(name) {
			emit(abs, reference.MentionInvocation, name, prefix, prefix+name)
		}
		if next <= k {
			next = k + len(p)
		}
		k = next
	}
}

// blocksInvocation reports whether a byte immediately before a prefix makes it
// part of a path, word, or URL rather than the start of an invocation.
func blocksInvocation(b byte) bool {
	return isPathByte(b) || isNameByte(b) || strings.IndexByte(`\%#?=&`, b) >= 0
}
