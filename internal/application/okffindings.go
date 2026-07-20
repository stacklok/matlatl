package application

import (
	"fmt"
	"strings"

	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/okf"
)

// okfFindings maps an okf.Report (ADR 0023) into analysis Findings. Every OKF
// conformance violation is an Error-severity finding of one of the three
// mode-scoped kinds; they are produced ONLY when OKF mode is on (the caller runs
// okf.Check only under --okf) and, when present, gate `check` (exit 1) regardless
// of --strict. Output preserves the report's per-list (Doc, Line) sort, so the
// findings are deterministic.
func okfFindings(rep okf.Report) []analysis.Finding {
	out := make([]analysis.Finding, 0,
		len(rep.MissingFrontmatter)+len(rep.MissingType)+len(rep.ReservedStructure))
	for _, v := range rep.MissingFrontmatter {
		out = append(out, okfMissingFrontmatterFinding(v))
	}
	for _, v := range rep.MissingType {
		out = append(out, okfMissingTypeFinding(v))
	}
	for _, v := range rep.ReservedStructure {
		out = append(out, okfReservedStructureFinding(v, rep.DetectedVersion))
	}
	return out
}

// okfMissingFrontmatterFinding builds the rule-R1 finding: a concept document
// with absent or present-but-unparseable frontmatter. The state is carried in
// Details so an agent can tell "add a block" from "fix the YAML".
func okfMissingFrontmatterFinding(v okf.Violation) analysis.Finding {
	msg := fmt.Sprintf(
		"%q has no YAML frontmatter block; OKF v0.1 requires a parseable frontmatter block "+
			"on every concept document (rule R1)", v.Doc)
	fix := "Add a YAML frontmatter block delimited by `---` at the top of the file, " +
		"carrying at least a non-empty `type:` field."
	if v.State == okf.FrontMatterUnparseable {
		msg = fmt.Sprintf(
			"%q has a frontmatter block that could not be parsed (invalid YAML, or the block "+
				"exceeds the size cap); OKF v0.1 requires a PARSEABLE frontmatter block on every "+
				"concept document (rule R1)", v.Doc)
		fix = "Make the frontmatter block delimited by `---` at the top of the file parse — fix the " +
			"YAML syntax or shrink an oversized block — and ensure it carries a non-empty `type:` field."
	}
	return analysis.Finding{
		ID:           fmt.Sprintf("%s:%s", analysis.OKFMissingFrontmatter, v.Doc),
		Kind:         analysis.OKFMissingFrontmatter,
		Severity:     analysis.Error,
		Location:     analysis.Location{Document: v.Doc, Line: v.Line},
		Message:      msg,
		SuggestedFix: fix,
		Details: map[string]string{
			DetailTargetDocument:   v.Doc.String(),
			DetailFrontmatterState: v.State,
		},
	}
}

// okfMissingTypeFinding builds the rule-R2 finding: a concept document whose
// (parseable) frontmatter lacks a non-empty string `type`. matlatl NEVER checks
// the type VALUE — OKF §4.1 forbids a central registry — so the fix names no
// allowed values.
func okfMissingTypeFinding(v okf.Violation) analysis.Finding {
	return analysis.Finding{
		ID:       fmt.Sprintf("%s:%s", analysis.OKFMissingType, v.Doc),
		Kind:     analysis.OKFMissingType,
		Severity: analysis.Error,
		Location: analysis.Location{Document: v.Doc, Line: v.Line},
		Message: fmt.Sprintf(
			"%q does not declare a non-empty OKF `type`: %s; OKF v0.1 requires every concept "+
				"document's frontmatter to carry a non-empty `type` field (rule R2)", v.Doc, v.Reason),
		SuggestedFix: "Add a non-empty `type:` string to the frontmatter. OKF does not restrict the " +
			"vocabulary (consumers must tolerate any value), so use a short, descriptive type, " +
			"e.g. `type: Reference` or `type: Playbook`.",
		Details: map[string]string{
			DetailTargetDocument: v.Doc.String(),
			DetailReason:         v.Reason,
		},
	}
}

// okfReservedStructureFinding builds the rule-R3 finding: a reserved file
// (index.md / log.md) that does not follow its §6/§7 structure. The detected
// bundle okf_version is attached when known, so an agent has the full context.
func okfReservedStructureFinding(v okf.Violation, version string) analysis.Finding {
	details := map[string]string{
		DetailTargetDocument: v.Doc.String(),
		DetailReservedFile:   strings.ToLower(v.Doc.Base()),
		DetailReason:         v.Reason,
	}
	if version != "" {
		details[DetailOKFVersion] = version
	}
	return analysis.Finding{
		ID:       fmt.Sprintf("%s:%s:%d", analysis.OKFReservedFileStructure, v.Doc, v.Line),
		Kind:     analysis.OKFReservedFileStructure,
		Severity: analysis.Error,
		Location: analysis.Location{Document: v.Doc, Line: v.Line},
		Message:  fmt.Sprintf("%q: %s (rule R3)", v.Doc, v.Reason),
		SuggestedFix: "Make the reserved file follow the OKF structure: `log.md` `##` headings must be " +
			"`YYYY-MM-DD` dates; a non-root `index.md` carries no frontmatter; and the bundle-root " +
			"`index.md` may carry only an `okf_version` key.",
		Details: details,
	}
}
