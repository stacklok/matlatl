// Package emit renders analysis results into machine- and CI-consumable
// artifacts (findings.json, JUnit XML) and writes them safely under an output
// directory. It is infrastructure: it may import the domain and third-party
// libraries, but the domain never imports it (ADR 0004). All emitted bytes are
// deterministic (the AnalysisReport is pre-sorted) so artifacts are byte-stable.
package emit

import (
	"encoding/json"
	"fmt"

	"github.com/stacklok/doctopus/internal/domain/analysis"
)

// Artifact filenames (stable; CI integrations key on these).
const (
	FindingsJSONName = "findings.json"
	JUnitXMLName     = "junit.xml"
)

// findingsDocument is the stable findings.json schema. Adding fields is
// backward-compatible; renaming/removing is a breaking change and must bump
// SchemaVersion.
type findingsDocument struct {
	SchemaVersion int           `json:"schemaVersion"`
	Tool          string        `json:"tool"`
	Summary       findingsSum   `json:"summary"`
	Findings      []findingJSON `json:"findings"`
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
}

// FindingsJSON renders an AnalysisReport as the canonical findings.json bytes
// (pretty-printed, trailing newline). The report's findings are already sorted,
// so output is deterministic.
func FindingsJSON(report *analysis.AnalysisReport) ([]byte, error) {
	doc := findingsDocument{
		SchemaVersion: 1,
		Tool:          "doctopus",
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
		})
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("emit: marshal findings.json: %w", err)
	}
	return append(b, '\n'), nil
}
