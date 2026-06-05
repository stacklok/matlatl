package llmstxt

import "strings"

// cleanBody returns the document body with any leading front-matter block
// stripped, so llms-full/small carry the human-readable markdown only (the
// typed front matter is already projected into the context header). It strips a
// single leading YAML (`---`) or TOML (`+++`) fence delimited block — the two
// forms doctopus parses (ADR 0002) — and nothing else: the body markdown is
// passed through verbatim so the agent sees the real source. Determinism is
// trivial (a pure byte transform).
//
// It is intentionally conservative: a fence must be on the very first line and
// have a matching closing fence; otherwise the input is returned unchanged so we
// never accidentally eat real content.
func cleanBody(raw []byte) string {
	s := string(raw)
	// Normalize only for delimiter detection; the returned slice is from the
	// original string so line endings inside the body are preserved.
	trimmedLeadingBOM := strings.TrimPrefix(s, "\ufeff")
	return stripFrontMatter(trimmedLeadingBOM)
}

func stripFrontMatter(s string) string {
	for _, fence := range []string{"---", "+++"} {
		if rest, ok := stripFenced(s, fence); ok {
			return rest
		}
	}
	return s
}

// stripFenced removes a leading `fence`-delimited block (the fence alone on the
// first line, a matching closing fence on its own line). Returns the remainder
// after the closing fence (and a single following newline) and true, or ("",
// false) if s does not open with such a block.
func stripFenced(s, fence string) (string, bool) {
	// The opening fence must be the first line (allowing a trailing CR).
	rest, ok := afterLine(s, fence)
	if !ok {
		return "", false
	}
	// Scan line by line for the closing fence.
	idx := 0
	for idx < len(rest) {
		lineEnd := strings.IndexByte(rest[idx:], '\n')
		var line string
		var next int
		if lineEnd < 0 {
			line = rest[idx:]
			next = len(rest)
		} else {
			line = rest[idx : idx+lineEnd]
			next = idx + lineEnd + 1
		}
		if strings.TrimRight(line, "\r") == fence {
			return strings.TrimLeft(rest[next:], "\n"), true
		}
		idx = next
	}
	// No closing fence: not a valid front-matter block, leave content intact.
	return "", false
}

// afterLine returns the text after the first line if that first line equals want
// (ignoring a trailing CR), and true; else ("", false).
func afterLine(s, want string) (string, bool) {
	lineEnd := strings.IndexByte(s, '\n')
	if lineEnd < 0 {
		return "", false
	}
	if strings.TrimRight(s[:lineEnd], "\r") != want {
		return "", false
	}
	return s[lineEnd+1:], true
}
