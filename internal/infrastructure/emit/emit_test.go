package emit

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/domain/analysis"
)

func sampleReport() *analysis.AnalysisReport {
	return analysis.NewAnalysisReport([]analysis.Finding{
		{
			ID: "broken-link:a.md:3:nope.md", Kind: analysis.BrokenLink, Severity: analysis.Error,
			Location: analysis.Location{Document: "a.md", Line: 3},
			Message:  "link target \"nope.md\" does not resolve", SuggestedFix: "fix it",
		},
		{
			ID: "broken-anchor:a.md:5:b.md#x", Kind: analysis.BrokenAnchor, Severity: analysis.Error,
			Location: analysis.Location{Document: "a.md", Line: 5},
			Message:  "anchor #x does not exist in \"b.md\"",
		},
	})
}

func TestFindingsJSON_ShapeAndParse(t *testing.T) {
	b, err := FindingsJSON(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		SchemaVersion int    `json:"schemaVersion"`
		Tool          string `json:"tool"`
		Summary       struct {
			Total        int `json:"total"`
			BrokenLink   int `json:"brokenLink"`
			BrokenAnchor int `json:"brokenAnchor"`
		} `json:"summary"`
		Findings []struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			Severity string `json:"severity"`
			Document string `json:"document"`
			Line     int    `json:"line"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("findings.json does not parse: %v", err)
	}
	if doc.SchemaVersion != 1 || doc.Tool != "doctopus" {
		t.Errorf("bad header: version=%d tool=%q", doc.SchemaVersion, doc.Tool)
	}
	if doc.Summary.Total != 2 || doc.Summary.BrokenLink != 1 || doc.Summary.BrokenAnchor != 1 {
		t.Errorf("bad summary: %+v", doc.Summary)
	}
	if len(doc.Findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(doc.Findings))
	}
	if doc.Findings[0].Kind != "broken-link" || doc.Findings[0].Document != "a.md" {
		t.Errorf("first finding wrong: %+v", doc.Findings[0])
	}
}

func TestFindingsJSON_Deterministic(t *testing.T) {
	a, _ := FindingsJSON(sampleReport())
	b, _ := FindingsJSON(sampleReport())
	if !bytes.Equal(a, b) {
		t.Error("findings.json is not byte-stable across runs")
	}
}

func TestJUnitXML_ShapeAndParse(t *testing.T) {
	b, err := JUnitXML(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	var suites struct {
		Tests     int `xml:"tests,attr"`
		Failures  int `xml:"failures,attr"`
		Testsuite []struct {
			Name     string `xml:"name,attr"`
			Testcase []struct {
				Name    string `xml:"name,attr"`
				Failure *struct {
					Message string `xml:"message,attr"`
				} `xml:"failure"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(b, &suites); err != nil {
		t.Fatalf("junit.xml does not parse: %v", err)
	}
	if suites.Tests != 2 || suites.Failures != 2 {
		t.Errorf("suite counts: tests=%d failures=%d, want 2/2", suites.Tests, suites.Failures)
	}
	if len(suites.Testsuite) != 1 || len(suites.Testsuite[0].Testcase) != 2 {
		t.Fatalf("expected 1 suite with 2 cases: %+v", suites.Testsuite)
	}
	if suites.Testsuite[0].Testcase[0].Failure == nil {
		t.Error("first testcase should carry a <failure>")
	}
}

func TestJUnitXML_EmptyReport(t *testing.T) {
	b, err := JUnitXML(analysis.NewAnalysisReport(nil))
	if err != nil {
		t.Fatal(err)
	}
	var suites struct {
		Tests    int `xml:"tests,attr"`
		Failures int `xml:"failures,attr"`
	}
	if err := xml.Unmarshal(b, &suites); err != nil {
		t.Fatalf("empty junit.xml does not parse: %v", err)
	}
	if suites.Tests != 0 || suites.Failures != 0 {
		t.Errorf("empty report counts: tests=%d failures=%d, want 0/0", suites.Tests, suites.Failures)
	}
}

func TestFSWriter_WritesUnderOut(t *testing.T) {
	dir := t.TempDir()
	w := NewFSWriter(dir)
	err := w.Write(context.Background(), []application.Artifact{
		{Name: "findings.json", Content: []byte("{}")},
		{Name: "sub/junit.xml", Content: []byte("<x/>")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "findings.json")); string(b) != "{}" {
		t.Error("findings.json not written correctly")
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "junit.xml")); err != nil {
		t.Errorf("nested artifact not written: %v", err)
	}
}

func TestFSWriter_RejectsZipSlip(t *testing.T) {
	dir := t.TempDir()
	w := NewFSWriter(dir)
	for _, name := range []string{"../escape.json", "../../etc/passwd", "/abs/path"} {
		err := w.Write(context.Background(), []application.Artifact{{Name: name, Content: []byte("x")}})
		if err == nil {
			t.Errorf("zip-slip name %q was accepted; want rejection", name)
		}
		// And nothing escaped.
		if _, statErr := os.Stat(filepath.Join(filepath.Dir(dir), "escape.json")); statErr == nil {
			t.Errorf("zip-slip wrote outside the out dir for %q", name)
		}
	}
}
