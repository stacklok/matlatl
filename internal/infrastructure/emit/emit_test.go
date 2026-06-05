package emit

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
// every defined finding kind, so any kind that can be emitted is covered. It
// walks the FindingKind iota exhaustively (BrokenLink..DeadLink, the same range
// FindingKind.Valid uses) so a newly-added kind cannot be forgotten here.
func TestRemediationGuide_CoversAllKinds(t *testing.T) {
	for k := analysis.BrokenLink; k.Valid(); k++ {
		if remediationByKind[k.String()] == "" {
			t.Errorf("remediationByKind missing entry for kind %q (%d)", k.String(), int(k))
		}
	}
	// Sanity: the loop actually reached the last kind (DeadLink), so it is not a
	// vacuous pass if Valid's bounds ever regress.
	if remediationByKind[analysis.DeadLink.String()] == "" {
		t.Error("remediationByKind missing the DeadLink entry")
	}
}

// TestFindingsJSON_ValidatesAgainstSchema validates emitted findings.json against
// the committed JSON Schema (docs/schemas/findings.schema.json) using a minimal,
// dependency-free Draft-2020-12 subset checker (the same approach the graphjson
// package uses for graph.schema.json). It enforces required, const, enum, type,
// and additionalProperties (both the `false` form on the top-level objects and
// the schema-object form used for the `details`/`remediationGuide` string maps),
// so a shape drift between findingsDocument and the published schema fails here.
func TestFindingsJSON_ValidatesAgainstSchema(t *testing.T) {
	b, err := FindingsJSON(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	var data any
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	schemaPath, err := filepath.Abs(filepath.Join("..", "..", "..", "docs", "schemas", "findings.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	sb, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(sb, &schema); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if errs := validateFindingsNode(data, schema, "$"); len(errs) > 0 {
		sort.Strings(errs)
		t.Errorf("findings.json does not satisfy findings.schema.json:\n  %v", errs)
	}
}

// TestFindingsJSON_CleanValidatesAgainstSchema asserts a clean (zero-finding)
// report — an empty findings list and empty remediationGuide — still satisfies
// the schema (round-trip on the most common emitted shape).
func TestFindingsJSON_CleanValidatesAgainstSchema(t *testing.T) {
	b, err := FindingsJSON(analysis.NewAnalysisReport(nil))
	if err != nil {
		t.Fatal(err)
	}
	var data any
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	schemaPath, _ := filepath.Abs(filepath.Join("..", "..", "..", "docs", "schemas", "findings.schema.json"))
	sb, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(sb, &schema); err != nil {
		t.Fatal(err)
	}
	if errs := validateFindingsNode(data, schema, "$"); len(errs) > 0 {
		sort.Strings(errs)
		t.Errorf("clean findings.json does not satisfy schema:\n  %v", errs)
	}
}

// validateFindingsNode is a minimal JSON-Schema (Draft 2020-12 subset) checker:
// type, required, const, enum, properties recursion, array items, minimum, and
// additionalProperties in both forms (bool false → no unknown keys; object →
// schema applied to every otherwise-unmatched value). It is intentionally small,
// just enough to assert the published shape contract.
func validateFindingsNode(data, schema any, path string) []string {
	s, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	var errs []string
	switch s["type"] {
	case "object":
		m, ok := data.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want object", path)}
		}
		props, _ := s["properties"].(map[string]any)
		if req, ok := s["required"].([]any); ok {
			for _, r := range req {
				if _, present := m[r.(string)]; !present {
					errs = append(errs, fmt.Sprintf("%s: missing required %q", path, r))
				}
			}
		}
		ap := s["additionalProperties"]
		for k, v := range m {
			if ps, ok := props[k]; ok {
				errs = append(errs, validateFindingsNode(v, ps, path+"."+k)...)
				continue
			}
			switch apt := ap.(type) {
			case bool:
				if !apt {
					errs = append(errs, fmt.Sprintf("%s: unexpected property %q", path, k))
				}
			case map[string]any:
				errs = append(errs, validateFindingsNode(v, apt, path+"."+k)...)
			}
		}
	case "array":
		arr, ok := data.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want array", path)}
		}
		if items, ok := s["items"]; ok {
			for i, e := range arr {
				errs = append(errs, validateFindingsNode(e, items, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	case "string":
		if _, ok := data.(string); !ok {
			errs = append(errs, fmt.Sprintf("%s: want string", path))
		}
	case "integer":
		f, ok := data.(float64)
		if !ok || f != float64(int64(f)) {
			errs = append(errs, fmt.Sprintf("%s: want integer", path))
		} else if min, ok := s["minimum"].(float64); ok && f < min {
			errs = append(errs, fmt.Sprintf("%s: %v < minimum %v", path, f, min))
		}
	}
	if c, ok := s["const"]; ok {
		if cf, okc := c.(float64); okc {
			if df, okd := data.(float64); !okd || df != cf {
				errs = append(errs, fmt.Sprintf("%s: const mismatch (want %v)", path, c))
			}
		} else if data != c {
			errs = append(errs, fmt.Sprintf("%s: const mismatch (want %v)", path, c))
		}
	}
	if en, ok := s["enum"].([]any); ok {
		matched := false
		for _, e := range en {
			if data == e {
				matched = true
				break
			}
		}
		if !matched {
			errs = append(errs, fmt.Sprintf("%s: %v not in enum", path, data))
		}
	}
	return errs
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
