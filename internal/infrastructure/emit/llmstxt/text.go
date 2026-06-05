package llmstxt

import (
	"strings"

	"github.com/stacklok/doctopus/internal/domain/identity"
)

// Text-safety helpers for the llms.txt family. The corpus title, document
// titles, descriptions and paths are attacker-influenced (front matter / file
// names), so they are sanitized before they go into markdown link text, link
// targets, or headings — a hostile value must not break the link, inject a new
// line/section, or forge markdown (ADR 0003). These mirror the intent of the
// shared emit escape helpers but target the link/heading contexts llms.txt uses.

// newlineCollapser turns any newline form into a single space so a multi-line
// value cannot inject a new markdown block, list item, or heading.
var newlineCollapser = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

// oneLine collapses newlines to spaces and trims surrounding whitespace, so a
// value is safe to drop into a single-line context (heading, blockquote,
// description). It does not otherwise escape markdown: the value is plain prose.
func oneLine(s string) string {
	return strings.TrimSpace(newlineCollapser.Replace(s))
}

// linkTextReplacer neutralizes the characters that would break or escape a
// markdown link's [text]: brackets close the text early, and newlines break it.
var linkTextReplacer = strings.NewReplacer(
	"\r\n", " ",
	"\n", " ",
	"\r", " ",
	"[", "(",
	"]", ")",
)

// linkText sanitizes a string for use as the [text] of a markdown link.
func linkText(s string) string {
	return strings.TrimSpace(linkTextReplacer.Replace(s))
}

// linkPathReplacer neutralizes the characters that would break a markdown link
// target (...) or change how a CommonMark parser reads it: a closing paren ends
// the target, whitespace/newlines split it, angle brackets would need escaping,
// and a '#' would be read as the start of a URL fragment (so a DocumentID like
// "notes#old.md" would silently resolve to path "notes" + fragment "old.md" — a
// broken link and a fragment-forgery surface). A DocumentID is a cleaned slash
// path and will not normally contain these, but it is sanitized as defense in
// depth against a hostile path.
//
// Order matters: '%' is encoded FIRST (to "%25") so the percent signs we
// introduce for the other characters are not themselves double-encoded.
var linkPathReplacer = strings.NewReplacer(
	"\r\n", "",
	"\n", "",
	"\r", "",
	"%", "%25",
	" ", "%20",
	"#", "%23",
	"(", "%28",
	")", "%29",
	"<", "%3C",
	">", "%3E",
)

// linkPath sanitizes a DocumentID for use as the (target) of a markdown link.
func linkPath(id identity.DocumentID) string {
	return linkPathReplacer.Replace(id.String())
}
