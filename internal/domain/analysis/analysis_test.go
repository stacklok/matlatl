package analysis

import "testing"

func TestSeverity_StringValid(t *testing.T) {
	all := []Severity{Info, Warning, Error}
	for _, s := range all {
		if !s.Valid() {
			t.Errorf("Severity %d reported invalid", int(s))
		}
		if str := s.String(); str == "" || str == "unknown" {
			t.Errorf("Severity %d has bad String() %q", int(s), str)
		}
	}
	if Info >= Warning || Warning >= Error {
		t.Error("severity ordering must be Info < Warning < Error")
	}
	if (Severity(99)).Valid() {
		t.Error("out-of-range Severity reported valid")
	}
}

func TestFindingKind_StringValid(t *testing.T) {
	all := []FindingKind{BrokenLink, BrokenAnchor, Orphan, Unreachable, Ambiguous, KnowledgeGap}
	seen := make(map[string]bool)
	for _, k := range all {
		if !k.Valid() {
			t.Errorf("FindingKind %d reported invalid", int(k))
		}
		s := k.String()
		if s == "" || s == "unknown" {
			t.Errorf("FindingKind %d has bad String() %q", int(k), s)
		}
		if seen[s] {
			t.Errorf("duplicate String() %q", s)
		}
		seen[s] = true
	}
	if (FindingKind(99)).Valid() {
		t.Error("out-of-range FindingKind reported valid")
	}
}

func TestNewAnalysisReport_SortsAndCounts(t *testing.T) {
	in := []Finding{
		{ID: "f3", Kind: Orphan, Severity: Warning, Location: Location{Document: "b.md", Line: 1}},
		{ID: "f1", Kind: BrokenLink, Severity: Error, Location: Location{Document: "a.md", Line: 10}},
		{ID: "f2", Kind: BrokenAnchor, Severity: Error, Location: Location{Document: "a.md", Line: 2}},
		{ID: "f0", Kind: BrokenLink, Severity: Error, Location: Location{Document: "a.md", Line: 2}},
	}
	r := NewAnalysisReport(in)

	got := r.Findings()
	// Sort key: Document, Line, Kind, ID.
	// a.md/2/BrokenLink/f0, a.md/2/BrokenAnchor/f2 -> BrokenLink(0) < BrokenAnchor(1)
	wantIDs := []string{"f0", "f2", "f1", "f3"}
	if len(got) != len(wantIDs) {
		t.Fatalf("len = %d, want %d", len(got), len(wantIDs))
	}
	for i, id := range wantIDs {
		if got[i].ID != id {
			t.Errorf("sorted[%d].ID = %q, want %q", i, got[i].ID, id)
		}
	}

	if r.Len() != 4 {
		t.Errorf("Len() = %d, want 4", r.Len())
	}
	if r.CountBySeverity(Error) != 3 {
		t.Errorf("CountBySeverity(Error) = %d, want 3", r.CountBySeverity(Error))
	}
	if r.CountBySeverity(Warning) != 1 {
		t.Errorf("CountBySeverity(Warning) = %d, want 1", r.CountBySeverity(Warning))
	}
	if r.CountByKind(BrokenLink) != 2 {
		t.Errorf("CountByKind(BrokenLink) = %d, want 2", r.CountByKind(BrokenLink))
	}
}

func TestNewAnalysisReport_Immutable(t *testing.T) {
	in := []Finding{{ID: "f1", Location: Location{Document: "a.md"}}}
	r := NewAnalysisReport(in)

	// Mutating the input slice must not affect the report.
	in[0].ID = "mutated"
	if r.Findings()[0].ID != "f1" {
		t.Error("report not isolated from input slice mutation")
	}
	// Mutating the returned slice must not affect the report.
	out := r.Findings()
	out[0].ID = "mutated"
	if r.Findings()[0].ID != "f1" {
		t.Error("report not isolated from returned slice mutation")
	}
}

func TestNewAnalysisReport_Nil(t *testing.T) {
	r := NewAnalysisReport(nil)
	if r == nil {
		t.Fatal("NewAnalysisReport(nil) = nil, want empty report")
	}
	if r.Len() != 0 {
		t.Errorf("Len() = %d, want 0", r.Len())
	}
	if len(r.Findings()) != 0 {
		t.Errorf("Findings() len = %d, want 0", len(r.Findings()))
	}
	if r.CountBySeverity(Error) != 0 || r.CountByKind(BrokenLink) != 0 {
		t.Error("empty report should have zero counts")
	}
}

func TestNewAnalysisReport_SeverityTieBreak(t *testing.T) {
	// Same Document/Line/Kind/ID-prefix; only Severity differs. Lower severity
	// must sort first for a stable total order.
	in := []Finding{
		{ID: "x", Kind: BrokenLink, Severity: Error, Location: Location{Document: "a.md", Line: 1}},
		{ID: "x", Kind: BrokenLink, Severity: Info, Location: Location{Document: "a.md", Line: 1}},
	}
	r := NewAnalysisReport(in)
	got := r.Findings()
	if got[0].Severity != Info || got[1].Severity != Error {
		t.Errorf("severity tie-break order = [%v %v], want [info error]", got[0].Severity, got[1].Severity)
	}
}

// TestParseFindingKind_RoundTrip asserts ParseFindingKind is the exact inverse
// of String over the whole enum, and rejects everything else.
func TestParseFindingKind_RoundTrip(t *testing.T) {
	for k := BrokenLink; k.Valid(); k++ {
		got, ok := ParseFindingKind(k.String())
		if !ok || got != k {
			t.Errorf("ParseFindingKind(%q) = (%v, %v), want (%v, true)", k.String(), got, ok, k)
		}
	}
	for _, s := range []string{"", "unknown", "broken_link", "Broken-Link", "suggested-link "} {
		if _, ok := ParseFindingKind(s); ok {
			t.Errorf("ParseFindingKind(%q) = ok, want not ok", s)
		}
	}
}
