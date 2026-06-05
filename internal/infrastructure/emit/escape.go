package emit

import "strings"

// Escaping helpers shared by every human emitter that writes Markdown
// (report/markdown.go, index/index.go). They live here, in the emit package
// root, so the report and index emitters reference the SAME implementation and
// can never silently drift — a divergence was the ADR 0003 escaping bug these
// helpers exist to prevent. A hostile (attacker-influenced) DocumentID, title,
// description, or category/directory label must never be able to break a GFM
// table, inject Markdown formatting/HTML, or escape an inline code span.
//
// Three contexts, three helpers:
//   - EscapeMarkdownText: flowing Markdown (headings, list items, prose). All
//     Markdown-significant characters are backslash-escaped.
//   - EscapeTableCell: a GFM table cell. Same as text, plus the pipe `|` which
//     would otherwise end the cell.
//   - EscapeInlineCode: the body of a single-backtick code span.

// markdownTextReplacer escapes the characters significant in flowing Markdown.
// Backslash is escaped FIRST (it is listed first in NewReplacer, but Replacer
// scans left-to-right and never re-examines inserted text, so ordering only
// affects which pattern wins on overlap — there is none here). Newlines collapse
// to spaces so a multi-line value cannot inject a new block / table row.
var markdownTextReplacer = strings.NewReplacer(
	`\`, `\\`,
	"\r\n", " ",
	"\n", " ",
	"\r", " ",
	"`", "\\`",
	"*", `\*`,
	"_", `\_`,
	"<", `\<`,
	">", `\>`,
	"[", `\[`,
	"]", `\]`,
)

// tableCellReplacer is markdownTextReplacer plus the pipe, which ends a GFM
// table cell. Kept as a separate replacer (rather than post-escaping the pipe)
// so the single left-to-right pass cannot double-escape.
var tableCellReplacer = strings.NewReplacer(
	`\`, `\\`,
	"\r\n", " ",
	"\n", " ",
	"\r", " ",
	"|", `\|`,
	"`", "\\`",
	"*", `\*`,
	"_", `\_`,
	"<", `\<`,
	">", `\>`,
	"[", `\[`,
	"]", `\]`,
)

// inlineCodeReplacer neutralizes a backtick (which would close a single-backtick
// span) and newlines (which would break it). The backtick is replaced with a
// modifier-letter apostrophe look-alike rather than escaped, because a backslash
// is literal inside a code span and could not neutralize it.
var inlineCodeReplacer = strings.NewReplacer(
	"`", "ʼ",
	"\r\n", " ",
	"\n", " ",
	"\r", " ",
)

// EscapeMarkdownText escapes a string for safe inclusion in flowing Markdown
// (not a table cell): newlines collapse to spaces and Markdown-significant
// characters are backslash-escaped so a hostile value cannot inject formatting
// or HTML (ADR 0003).
func EscapeMarkdownText(s string) string {
	if s == "" {
		return ""
	}
	return markdownTextReplacer.Replace(s)
}

// EscapeTableCell escapes a string for safe inclusion in a GFM table cell. It is
// EscapeMarkdownText plus the pipe `|`, which would otherwise end the cell and
// let a hostile value forge extra columns or break the row (ADR 0003).
func EscapeTableCell(s string) string {
	if s == "" {
		return ""
	}
	return tableCellReplacer.Replace(s)
}

// EscapeInlineCode escapes a string for inclusion inside a single-backtick code
// span: a literal backtick would close the span and a newline would break it,
// so both are neutralized (ADR 0003).
func EscapeInlineCode(s string) string {
	return inlineCodeReplacer.Replace(s)
}
