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
			Details: map[string]string{"target": "nope.md", "linkType": "relative-link"},
		},
		{
			ID: "broken-anchor:a.md:5:b.md#x", Kind: analysis.BrokenAnchor, Severity: analysis.Error,
			Location: analysis.Location{Document: "a.md", Line: 5},
			Message:  "anchor #x does not exist in \"b.md\"",
			Details:  map[string]string{"target": "b.md#x", "expectedSlug": "x", "targetDocument": "b.md"},
		},
		{
			ID: "ambiguous:a.md:7:notes", Kind: analysis.Ambiguous, Severity: analysis.Warning,
			Location: analysis.Location{Document: "a.md", Line: 7},
			Message:  "link target \"notes\" is ambiguous",
			Details:  map[string]string{"target": "notes", "candidates": "x/notes.md\ny/notes.md"},
		},
	})
}

func TestFindingsJSON_ShapeAndParse(t *testing.T) {
	b, err := FindingsJSON(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		SchemaVersion    int               `json:"schemaVersion"`
		Tool             string            `json:"tool"`
		RemediationGuide map[string]string `json:"remediationGuide"`
		Summary          struct {
			Total        int `json:"total"`
			BrokenLink   int `json:"brokenLink"`
			BrokenAnchor int `json:"brokenAnchor"`
			Ambiguous    int `json:"ambiguous"`
		} `json:"summary"`
		Findings []struct {
			ID       string            `json:"id"`
			Kind     string            `json:"kind"`
			Severity string            `json:"severity"`
			Document string            `json:"document"`
			Line     int               `json:"line"`
			Details  map[string]string `json:"details"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("findings.json does not parse: %v", err)
	}
	if doc.SchemaVersion != FindingsSchemaVersion || doc.Tool != "doctopus" {
		t.Errorf("bad header: version=%d tool=%q", doc.SchemaVersion, doc.Tool)
	}
	if doc.Summary.Total != 3 || doc.Summary.BrokenLink != 1 || doc.Summary.BrokenAnchor != 1 || doc.Summary.Ambiguous != 1 {
		t.Errorf("bad summary: %+v", doc.Summary)
	}
	if len(doc.Findings) != 3 {
		t.Fatalf("got %d findings, want 3", len(doc.Findings))
	}
	if doc.Findings[0].Kind != "broken-link" || doc.Findings[0].Document != "a.md" {
		t.Errorf("first finding wrong: %+v", doc.Findings[0])
	}

	// --- v2: self-contained / actionable assertions ---
	byKind := map[string]map[string]string{}
	for _, f := range doc.Findings {
		byKind[f.Kind] = f.Details
	}
	// Broken anchor carries the expected slug (an agent can add that heading).
	if got := byKind["broken-anchor"]["expectedSlug"]; got != "x" {
		t.Errorf("broken-anchor finding missing expectedSlug: %v", byKind["broken-anchor"])
	}
	// Ambiguous carries the candidate alternatives.
	if got := byKind["ambiguous"]["candidates"]; got == "" {
		t.Errorf("ambiguous finding missing candidates: %v", byKind["ambiguous"])
	}
	// remediationGuide covers every emitted kind.
	for _, k := range []string{"broken-link", "broken-anchor", "ambiguous"} {
		if doc.RemediationGuide[k] == "" {
			t.Errorf("remediationGuide missing entry for emitted kind %q", k)
		}
	}
	// It is scoped to emitted kinds only (orphan was not emitted here).
	if _, present := doc.RemediationGuide["orphan"]; present {
		t.Errorf("remediationGuide should not include un-emitted kind 'orphan'")
	}
}

// TestRemediationGuide_CoversAllKinds asserts the guide source has an entry for
// every defined finding kind, so any kind that can be emitted is covered.
func TestRemediationGuide_CoversAllKinds(t *testing.T) {
	for _, k := range []analysis.FindingKind{
		analysis.BrokenLink, analysis.BrokenAnchor, analysis.Ambiguous,
		analysis.Orphan, analysis.Unreachable, analysis.KnowledgeGap,
	} {
		if remediationByKind[k.String()] == "" {
			t.Errorf("remediationByKind missing entry for kind %q", k.String())
		}
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
	// 3 findings: 2 errors (broken link + anchor) + 1 warning (ambiguous). JUnit
	// counts all as tests; failures are the error-severity ones.
	if suites.Tests != 3 {
		t.Errorf("suite counts: tests=%d, want 3", suites.Tests)
	}
	if len(suites.Testsuite) != 1 || len(suites.Testsuite[0].Testcase) == 0 {
		t.Fatalf("expected 1 suite with cases: %+v", suites.Testsuite)
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

// TestFSWriter_RespectsCancellation asserts the writer honors a cancelled
// context (matching the scanner/parser cancellation discipline): a pre-cancelled
// context aborts the artifact loop with the context error and writes nothing.
func TestFSWriter_RespectsCancellation(t *testing.T) {
	dir := t.TempDir()
	w := NewFSWriter(dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := w.Write(ctx, []application.Artifact{{Name: "x.json", Content: []byte("{}")}})
	if err == nil {
		t.Fatal("expected a context error from a cancelled write")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "x.json")); statErr == nil {
		t.Error("cancelled write should not have written the artifact")
	}
}
