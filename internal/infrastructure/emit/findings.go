// Package emit renders analysis results into machine- and CI-consumable
// artifacts (findings.json, JUnit XML) and writes them safely under an output
// directory. It is infrastructure: it may import the domain and third-party
// libraries, but the domain never imports it (ADR 0004). All emitted bytes are
// deterministic (the AnalysisReport is pre-sorted) so artifacts are byte-stable.
package emit

import (
	"encoding/json"
	"fmt"

	"github.com/stacklok/matlatl/internal/domain/analysis"
)

// Artifact filenames (stable; CI integrations key on these).
const (
	FindingsJSONName = "findings.json"
	JUnitXMLName     = "junit.xml"
)

// FindingsSchemaVersion is the findings.json schema version. Adding optional
// fields is backward-compatible; renaming/removing a field or a Details key is a
// breaking change that bumps this. v2 (this phase) added per-finding "details"
// (structured, machine-actionable context) and a top-level "remediationGuide"
// mapping each finding kind to a standalone how-to, so every finding is
// self-contained for an agent.
const FindingsSchemaVersion = 2

// findingsDocument is the stable findings.json schema. Adding fields is
// backward-compatible; renaming/removing is a breaking change and must bump
// FindingsSchemaVersion.
type findingsDocument struct {
	SchemaVersion int         `json:"schemaVersion"`
	Tool          string      `json:"tool"`
	Summary       findingsSum `json:"summary"`
	// RemediationGuide maps each finding kind that appears in this report to a
	// one-paragraph, standalone how-to, so an agent can act on a finding using
	// only this document. Keyed by the finding kind string (e.g. "broken-link").
	RemediationGuide map[string]string `json:"remediationGuide"`
	Findings         []findingJSON     `json:"findings"`
}

type findingsSum struct {
	Total        int `json:"total"`
	BrokenLink   int `json:"brokenLink"`
	BrokenAnchor int `json:"brokenAnchor"`
	Ambiguous    int `json:"ambiguous"`
	Orphan       int `json:"orphan"`
	Unreachable  int `json:"unreachable"`
	KnowledgeGap int `json:"knowledgeGap"`
}

type findingJSON struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Severity     string `json:"severity"`
	Document     string `json:"document"`
	Line         int    `json:"line"`
	Message      string `json:"message"`
	SuggestedFix string `json:"suggestedFix,omitempty"`
	// Details is the finding's structured, machine-actionable context (e.g. the
	// ambiguous candidates, the expected anchor slug). Omitted when empty. The
	// map is rendered with sorted keys (encoding/json sorts map keys), so output
	// is deterministic.
	Details map[string]string `json:"details,omitempty"`
}

// remediationByKind is the standalone how-to for each finding kind. It is keyed
// by the analysis.FindingKind.String() value and is the single source of truth
// for the findings.json remediationGuide. Every kind a finding can carry MUST
// have an entry (asserted in tests) so the guide always covers the report.
var remediationByKind = map[string]string{
	analysis.BrokenLink.String(): "The reference points at a path that does not resolve to any document in the corpus. " +
		"Open the finding's `document` at `line`, then either correct the link target to a real, " +
		"existing document path (the `details.target` field holds the path as written, relative to the " +
		"origin document), move/create the intended target file, or remove the dead link.",
	analysis.BrokenAnchor.String(): "The target document exists but has no heading whose slug matches the fragment. " +
		"Either add a heading to `details.targetDocument` that slugifies to `details.expectedSlug` " +
		"(slugs are GitHub-style: lowercase, spaces→dashes, punctuation dropped), or change the link's " +
		"`#fragment` to an existing heading slug in that document.",
	analysis.Ambiguous.String(): "The target matches more than one document, so the link is non-deterministic. " +
		"Replace it with one of the unique paths in `details.candidates` (newline-separated): pick the " +
		"intended document and use a path long enough to be unambiguous.",
	analysis.Orphan.String(): "The document is isolated: nothing links to it and it links to nothing, so no reader " +
		"or agent can navigate to it. Add an inbound link from a relevant page (an index or a related doc) " +
		"and outbound links to its neighbors, or delete it if obsolete. To keep it intentionally unlinked, " +
		"add front matter `matlatl: orphan-intentional`.",
	analysis.Unreachable.String(): "The document cannot be reached by following links from any root (README.md/index.md " +
		"or a `type: index` doc). Add an inbound link from a page that is itself reachable from a root. " +
		"To keep it intentionally unlinked, add front matter `matlatl: orphan-intentional`.",
	analysis.KnowledgeGap.String(): "Two clusters of documentation (`details.componentA` and `details.componentB`) have no " +
		"navigational links between them. This is an experimental heuristic, not an error. If the two areas " +
		"are related, add a link between `details.representativeA` and `details.representativeB` to connect them.",
	analysis.DeadLink.String(): "An external (http/https) link failed an opt-in liveness check (--check-external): it was " +
		"unreachable, returned an error status (`details.statusCode`), or was refused by the SSRF guard " +
		"(`details.blocked`). Verify the URL is correct and reachable; if it moved, update it, otherwise " +
		"remove or replace the link. Dead-link findings appear only under --check-external.",
}

// kindPresentationOrder is the single source of truth for the order finding
// kinds are presented in across the emit package: the findings.json
// remediationGuide (remediationGuideFor) and the fix-prompt "How to fix each
// kind" section (kindsPresent) both iterate it, so a newly-added FindingKind
// cannot slip past one and be ordered inconsistently in the other.
var kindPresentationOrder = []analysis.FindingKind{
	analysis.BrokenLink, analysis.BrokenAnchor, analysis.Ambiguous,
	analysis.Orphan, analysis.Unreachable, analysis.KnowledgeGap, analysis.DeadLink,
}

// remediationGuideFor returns the remediation entries for exactly the kinds
// present in the report, so the guide is scoped to what was emitted. A clean
// report yields an empty (but non-nil) guide.
func remediationGuideFor(report *analysis.AnalysisReport) map[string]string {
	guide := make(map[string]string)
	for _, k := range kindPresentationOrder {
		if report.CountByKind(k) > 0 {
			guide[k.String()] = remediationByKind[k.String()]
		}
	}
	return guide
}

// FindingsJSON renders an AnalysisReport as the canonical findings.json bytes
// (pretty-printed, trailing newline). The report's findings are already sorted,
// so output is deterministic.
func FindingsJSON(report *analysis.AnalysisReport) ([]byte, error) {
	doc := findingsDocument{
		SchemaVersion:    FindingsSchemaVersion,
		Tool:             "matlatl",
		RemediationGuide: remediationGuideFor(report),
		Summary: findingsSum{
			Total:        report.Len(),
			BrokenLink:   report.CountByKind(analysis.BrokenLink),
			BrokenAnchor: report.CountByKind(analysis.BrokenAnchor),
			Ambiguous:    report.CountByKind(analysis.Ambiguous),
			Orphan:       report.CountByKind(analysis.Orphan),
			Unreachable:  report.CountByKind(analysis.Unreachable),
			KnowledgeGap: report.CountByKind(analysis.KnowledgeGap),
		},
		Findings: make([]findingJSON, 0, report.Len()),
	}
	for _, f := range report.Findings() {
		doc.Findings = append(doc.Findings, findingJSON{
			ID:           f.ID,
			Kind:         f.Kind.String(),
			Severity:     f.Severity.String(),
			Document:     f.Location.Document.String(),
			Line:         f.Location.Line,
			Message:      f.Message,
			SuggestedFix: f.SuggestedFix,
			Details:      f.Details,
		})
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("emit: marshal findings.json: %w", err)
	}
	return append(b, '\n'), nil
}
