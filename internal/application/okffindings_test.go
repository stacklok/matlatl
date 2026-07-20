package application

import (
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/okf"
)

// TestOKFFindings_Mapping covers the okf.Report → Finding mapping (ADR 0023):
// each violation becomes an Error-severity finding of the matching kind, with
// the state/reason/version surfaced in Details and stable IDs.
func TestOKFFindings_Mapping(t *testing.T) {
	rep := okf.Report{
		DetectedVersion: "0.1",
		MissingFrontmatter: []okf.Violation{
			{Doc: "absent.md", Line: 1, State: okf.FrontMatterAbsent},
			{Doc: "broken.md", Line: 1, State: okf.FrontMatterUnparseable},
		},
		MissingType: []okf.Violation{
			{Doc: "notype.md", Line: 1, Reason: "frontmatter has no `type` field (OKF §4.1)"},
		},
		ReservedStructure: []okf.Violation{
			{Doc: "log.md", Line: 5, Reason: "log.md `##` heading \"bad\" is not an ISO 8601 date (YYYY-MM-DD) (OKF §7)"},
		},
	}

	got := okfFindings(rep)
	if len(got) != 4 {
		t.Fatalf("got %d findings, want 4", len(got))
	}

	byKind := map[analysis.FindingKind][]analysis.Finding{}
	for _, f := range got {
		if f.Severity != analysis.Error {
			t.Errorf("%s severity = %v, want Error", f.Kind, f.Severity)
		}
		byKind[f.Kind] = append(byKind[f.Kind], f)
	}

	// R1: two missing-frontmatter findings; state carried in Details.
	fm := byKind[analysis.OKFMissingFrontmatter]
	if len(fm) != 2 {
		t.Fatalf("got %d missing-frontmatter findings, want 2", len(fm))
	}
	states := map[string]string{} // doc -> state
	for _, f := range fm {
		states[f.Location.Document.String()] = f.Details[DetailFrontmatterState]
	}
	if states["absent.md"] != okf.FrontMatterAbsent {
		t.Errorf("absent.md state = %q, want absent", states["absent.md"])
	}
	if states["broken.md"] != okf.FrontMatterUnparseable {
		t.Errorf("broken.md state = %q, want unparseable", states["broken.md"])
	}
	// Stable ID form kind:doc.
	if fm[0].ID == "" || !strings.HasPrefix(fm[0].ID, "okf-missing-frontmatter:") {
		t.Errorf("missing-frontmatter ID = %q, want okf-missing-frontmatter:<doc>", fm[0].ID)
	}

	// R2: one missing-type finding; reason carried in Details.
	mt := byKind[analysis.OKFMissingType]
	if len(mt) != 1 || mt[0].Details[DetailReason] == "" {
		t.Fatalf("missing-type finding wrong: %+v", mt)
	}
	if mt[0].ID != "okf-missing-type:notype.md" {
		t.Errorf("missing-type ID = %q", mt[0].ID)
	}

	// R3: one reserved-structure finding; reserved-file basename + reason + version.
	rs := byKind[analysis.OKFReservedFileStructure]
	if len(rs) != 1 {
		t.Fatalf("got %d reserved-structure findings, want 1", len(rs))
	}
	if rs[0].Details[DetailReservedFile] != "log.md" {
		t.Errorf("reservedFile = %q, want log.md", rs[0].Details[DetailReservedFile])
	}
	if rs[0].Details[DetailReason] == "" {
		t.Error("reserved-structure finding missing a reason detail")
	}
	if rs[0].Details[DetailOKFVersion] != "0.1" {
		t.Errorf("reserved-structure okfVersion detail = %q, want 0.1", rs[0].Details[DetailOKFVersion])
	}
	if rs[0].ID != "okf-reserved-file-structure:log.md:5" {
		t.Errorf("reserved-structure ID = %q", rs[0].ID)
	}
}

// TestOKFFindings_Empty asserts a conformant report produces no findings.
func TestOKFFindings_Empty(t *testing.T) {
	if got := okfFindings(okf.Report{Conformant: true}); len(got) != 0 {
		t.Errorf("conformant report should produce no findings, got %+v", got)
	}
}
