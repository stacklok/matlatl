package emit

import (
	"encoding/xml"
	"fmt"

	"github.com/stacklok/doctopus/internal/domain/analysis"
)

// JUnit XML schema (minimal, stable, CI-portable). Each finding becomes a
// failing <testcase>; a clean report emits an empty suite (tests=0, failures=0)
// so dashboards still get a green result. All text is XML-escaped by
// encoding/xml, satisfying the label-escaping requirement of ADR 0003.

type junitTestsuites struct {
	XMLName   xml.Name        `xml:"testsuites"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Testsuite []junitTestsuit `xml:"testsuite"`
}

type junitTestsuit struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Testcase []junitTestcase `xml:"testcase"`
}

type junitTestcase struct {
	// Name identifies the check; Classname carries the document for grouping.
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

// JUnitXML renders an AnalysisReport as JUnit XML bytes (with header and
// trailing newline). Findings are already sorted, so output is deterministic.
func JUnitXML(report *analysis.AnalysisReport) ([]byte, error) {
	cases := make([]junitTestcase, 0, report.Len())
	for _, f := range report.Findings() {
		name := fmt.Sprintf("%s:%d %s", f.Location.Document, f.Location.Line, f.Kind)
		detail := f.Message
		if f.SuggestedFix != "" {
			detail = f.Message + "\nSuggested fix: " + f.SuggestedFix
		}
		cases = append(cases, junitTestcase{
			Name:      name,
			Classname: f.Location.Document.String(),
			Failure: &junitFailure{
				Message: f.Message,
				Type:    f.Kind.String(),
				Text:    detail,
			},
		})
	}

	suites := junitTestsuites{
		Tests:    report.Len(),
		Failures: report.Len(),
		Testsuite: []junitTestsuit{{
			Name:     "doctopus",
			Tests:    report.Len(),
			Failures: report.Len(),
			Testcase: cases,
		}},
	}

	b, err := xml.MarshalIndent(suites, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("emit: marshal junit.xml: %w", err)
	}
	out := append([]byte(xml.Header), b...)
	return append(out, '\n'), nil
}
