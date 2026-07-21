// Package okf implements the Open Knowledge Format (OKF) v0.1 conformance check
// (ADR 0023). Given a frozen corpus it returns a Report: the three §9 normative
// rules and a bundle-level verdict. It is pure domain — it imports only the
// standard library and the sibling corpus/identity packages (ADR 0004) and does
// no I/O and no YAML parsing of its own (the parser already recorded whether
// each document's frontmatter was present and whether it parsed).
//
// The three conformance rules (OKF v0.1 §9, verbatim in
// docs/research/okf-spec-pinned.md):
//
//	R1. Every non-reserved `.md` file contains a PARSEABLE YAML frontmatter block.
//	R2. Every such frontmatter block contains a non-empty `type` field.
//	R3. Every reserved file (index.md, log.md) follows its §6/§7 structure.
//
// Reserved filenames are EXACTLY index.md and log.md (case-insensitive); every
// other `.md` — README.md included — is a concept document. Conformance is the
// WHOLE of the verdict: broken links, orphans, and the rest of matlatl's health
// signals are deliberately NOT part of it (OKF §9 says consumers MUST NOT reject
// a bundle for a broken link), so they live entirely outside this package.
package okf

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// Frontmatter-state values for a rule-R1 violation, carried on Violation.State.
const (
	// FrontMatterAbsent means the document had no frontmatter fence block at all.
	FrontMatterAbsent = "absent"
	// FrontMatterUnparseable means a frontmatter block was present but did not
	// decode (malformed YAML, or stripped by the oversized-frontmatter guard).
	FrontMatterUnparseable = "unparseable"
)

// isoDateRE matches a `## YYYY-MM-DD` log heading (OKF §7). It validates the
// FORMAT only, not calendar validity — "2026-13-45" is well-formed here; the
// spec's single MUST is the digit shape, and matlatl does not own a calendar.
var isoDateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Violation is one OKF conformance failure, pinned to a document and (when
// meaningful) a source line. For a rule-R1 (missing-frontmatter) violation the
// State field carries "absent" or "unparseable"; for R2 (missing-type) and R3
// (reserved-file-structure) the Reason field carries a human-readable cause.
// The unused field is "" for its rule.
type Violation struct {
	Doc    identity.DocumentID
	Line   int
	State  string // R1 only: FrontMatterAbsent | FrontMatterUnparseable
	Reason string // R2/R3 only: human-readable cause
}

// Report is the result of an OKF conformance check. Conformant is set by Check:
// true iff all three violation lists are empty (the zero-value Report has
// Conformant=false, which is why callers must run Check rather than rely on the
// zero value). DetectedVersion is the okf_version declared in the bundle-root
// index.md frontmatter (OKF §11), or "" when none is present.
type Report struct {
	Conformant      bool
	DetectedVersion string
	// The three §9 rule buckets, each sorted by (Doc, Line) for determinism.
	MissingFrontmatter []Violation // R1
	MissingType        []Violation // R2
	ReservedStructure  []Violation // R3
}

// Check runs the OKF v0.1 conformance rules over a frozen corpus and returns the
// Report. A nil/empty corpus is trivially conformant. Output is deterministic:
// documents are visited in sorted-ID order and every violation list is sorted by
// (Doc, Line).
func Check(c *corpus.Corpus) Report {
	rep := Report{}
	if c == nil {
		rep.Conformant = true
		return rep
	}
	for _, doc := range c.Documents() { // sorted by DocumentID
		switch base := strings.ToLower(doc.ID.Base()); base {
		case "index.md":
			checkIndex(doc, &rep)
		case "log.md":
			checkLog(doc, &rep)
		default:
			checkConcept(doc, &rep)
		}
	}
	sortViolations(rep.MissingFrontmatter)
	sortViolations(rep.MissingType)
	sortViolations(rep.ReservedStructure)
	rep.Conformant = len(rep.MissingFrontmatter)+len(rep.MissingType)+len(rep.ReservedStructure) == 0
	return rep
}

// checkConcept applies R1 (parseable frontmatter) and R2 (non-empty `type`) to a
// non-reserved concept document. R2 is only reached when R1 passes — an
// unparseable block is already an R1 violation, so we do not also fault its
// (unreadable) type.
func checkConcept(doc *corpus.Document, rep *Report) {
	if !doc.FrontMatterPresent {
		rep.MissingFrontmatter = append(rep.MissingFrontmatter, Violation{
			Doc: doc.ID, Line: 1, State: FrontMatterAbsent,
		})
		return
	}
	if !doc.FrontMatterParsed {
		rep.MissingFrontmatter = append(rep.MissingFrontmatter, Violation{
			Doc: doc.ID, Line: 1, State: FrontMatterUnparseable,
		})
		return
	}
	// R2: a non-empty string `type`. NEVER validate the VALUE against a list —
	// OKF §4.1 forbids a central type registry and requires consumers to tolerate
	// unknown types.
	raw, ok := doc.FrontMatter.Extra["type"]
	if !ok || raw == nil {
		rep.MissingType = append(rep.MissingType, Violation{
			Doc: doc.ID, Line: 1, Reason: "frontmatter has no `type` field (OKF §4.1)",
		})
		return
	}
	s, isStr := raw.(string)
	if !isStr {
		rep.MissingType = append(rep.MissingType, Violation{
			Doc: doc.ID, Line: 1,
			Reason: fmt.Sprintf("`type` must be a non-empty string, got %s (OKF §4.1)", yamlKind(raw)),
		})
		return
	}
	if strings.TrimSpace(s) == "" {
		rep.MissingType = append(rep.MissingType, Violation{
			Doc: doc.ID, Line: 1, Reason: "`type` is empty (OKF §4.1)",
		})
	}
}

// checkLog applies R3 to a log.md: every second-level (`##`) heading MUST be an
// ISO 8601 YYYY-MM-DD date (OKF §7). The rest of the log format (bold lead word,
// entry prose) is explicitly a convention, not a requirement, so it is not
// checked.
func checkLog(doc *corpus.Document, rep *Report) {
	for _, h := range level2Headings(doc) {
		if !isoDateRE.MatchString(strings.TrimSpace(h.text)) {
			rep.ReservedStructure = append(rep.ReservedStructure, Violation{
				Doc:  doc.ID,
				Line: h.line,
				Reason: fmt.Sprintf(
					"log.md `##` heading %q is not an ISO 8601 date (YYYY-MM-DD) (OKF §7)", h.text),
			})
		}
	}
}

// checkIndex applies R3 to an index.md. A NON-ROOT index.md must carry no
// frontmatter (OKF §6). The BUNDLE-ROOT index.md (at the scan root, Dir()==".")
// is the ONE place frontmatter is permitted, and then only the single
// okf_version key (OKF §11, strict reading — ADR 0023). Root detection is
// case-insensitive on the basename, consistent with the reserved-file dispatch —
// a bundle-root INDEX.MD is still the root index. The declared version is
// surfaced on the Report regardless of whether extra keys make the index
// non-conformant.
func checkIndex(doc *corpus.Document, rep *Report) {
	if doc.ID.Dir() == "." { // bundle-root index (basename already matched index.md)
		if !doc.FrontMatterPresent {
			return
		}
		if v := okfVersion(doc.FrontMatter.Extra); v != "" {
			rep.DetectedVersion = v
		}
		// A present-but-unparseable block cannot be shown to contain only
		// okf_version, so it fails R3 — mirroring the non-root "has frontmatter"
		// stance (an undecodable block is still "has frontmatter").
		if !doc.FrontMatterParsed {
			rep.ReservedStructure = append(rep.ReservedStructure, Violation{
				Doc: doc.ID, Line: 1,
				Reason: "root index.md frontmatter does not parse, so it cannot be shown to contain only `okf_version` (OKF §6/§11)",
			})
			return
		}
		if !onlyOKFVersion(doc.FrontMatter) {
			rep.ReservedStructure = append(rep.ReservedStructure, Violation{
				Doc: doc.ID, Line: 1,
				Reason: "root index.md frontmatter may contain only `okf_version` (OKF §6/§11)",
			})
		}
		return
	}
	// Non-root index.md: no frontmatter at all (present-but-unparseable still
	// counts as "has frontmatter").
	if doc.FrontMatterPresent {
		rep.ReservedStructure = append(rep.ReservedStructure, Violation{
			Doc: doc.ID, Line: 1,
			Reason: "non-root index.md must not contain frontmatter (OKF §6)",
		})
	}
}

// onlyOKFVersion reports whether a bundle-root index.md's frontmatter carries
// nothing but okf_version. Title is deliberately excluded from the check: the
// parser fills FrontMatter.Title from the first H1 as a documented fallback
// (see mdparser), so a non-empty Title does NOT imply a `title:` key was present.
// Every other typed field, and any Extra key other than okf_version, is a real
// frontmatter key and makes the index non-conformant.
func onlyOKFVersion(fm corpus.FrontMatter) bool {
	if fm.Description != "" || len(fm.Tags) > 0 || len(fm.Aliases) > 0 ||
		fm.Name != "" || fm.Parent != "" || len(fm.Related) > 0 ||
		fm.Status != "" || fm.Date != "" {
		return false
	}
	for k := range fm.Extra {
		if k != "okf_version" {
			return false
		}
	}
	return true
}

// yamlKind describes a decoded YAML value's kind in plain, user-facing words
// (no Go type names leak into findings). Scalars fall back to a generic
// description; the load-bearing cases are the list/mapping non-string `type`s.
func yamlKind(v any) string {
	switch v.(type) {
	case []any:
		return "a list"
	case map[string]any:
		return "a mapping"
	case bool:
		return "a boolean"
	case int, int64, uint64, float64:
		return "a number"
	default:
		return "a non-string value"
	}
}

// okfVersion returns the okf_version value from a frontmatter Extra map as a
// trimmed string (stringifying a non-string scalar), or "" when absent.
func okfVersion(extra map[string]any) string {
	v, ok := extra["okf_version"]
	if !ok || v == nil {
		return ""
	}
	if s, isStr := v.(string); isStr {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// heading is a document heading with its source line.
type heading struct {
	text string
	line int
}

// level2Headings returns every level-2 (`##`) heading in a document, in document
// order (which is line order).
func level2Headings(doc *corpus.Document) []heading {
	if doc == nil || doc.Root == nil {
		return nil
	}
	var out []heading
	var walk func(s *corpus.Section)
	walk = func(s *corpus.Section) {
		for _, child := range s.Children {
			if child.Level == 2 {
				out = append(out, heading{text: child.Text, line: child.StartLine})
			}
			walk(child)
		}
	}
	walk(doc.Root)
	return out
}

// sortViolations sorts a violation slice by (Doc, Line) in place for
// deterministic output.
func sortViolations(vs []Violation) {
	slices.SortFunc(vs, func(a, b Violation) int {
		if c := cmp.Compare(a.Doc, b.Doc); c != 0 {
			return c
		}
		return cmp.Compare(a.Line, b.Line)
	})
}
