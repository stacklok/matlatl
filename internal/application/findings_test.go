package application

import (
	"strings"
	"testing"

	"github.com/stacklok/doctopus/internal/domain/analysis"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/domain/reference"
	"github.com/stacklok/doctopus/internal/platform"
)

func TestFindingsFromReferences_KindsAndSeverity(t *testing.T) {
	refs := []reference.Reference{
		// Healthy / external / ignored references produce no findings.
		{RawReference: reference.RawReference{Origin: "a.md", Type: reference.RelativeLink}, Health: reference.Valid},
		{RawReference: reference.RawReference{Origin: "a.md", Type: reference.External}, Health: reference.HealthExternal},
		{RawReference: reference.RawReference{Origin: "a.md", Type: reference.ImageEmbed}, Health: reference.NonNote},
		// Broken link → Error.
		{RawReference: reference.RawReference{Origin: "a.md", RawTarget: "nope.md", Line: 3, Type: reference.RelativeLink}, Health: reference.Broken},
		// Broken anchor → Error.
		{
			RawReference: reference.RawReference{Origin: "a.md", RawTarget: "b.md", Fragment: "x", Line: 4, Type: reference.RelativeLink},
			Target:       reference.ResolvedTarget{Kind: reference.TargetSection, DocumentID: "b.md"},
			Health:       reference.BrokenAnchor,
		},
		// Ambiguous → Warning.
		{
			RawReference: reference.RawReference{Origin: "a.md", RawTarget: "notes", Line: 5, Type: reference.Wikilink},
			Health:       reference.Ambiguous,
			Candidates:   []identity.DocumentID{"x/notes.md", "y/notes.md"},
		},
	}

	got := findingsFromReferences(refs)
	if len(got) != 3 {
		t.Fatalf("got %d findings, want 3 (broken link, broken anchor, ambiguous)", len(got))
	}

	byKind := map[analysis.FindingKind]analysis.Finding{}
	for _, f := range got {
		byKind[f.Kind] = f
	}

	bl, ok := byKind[analysis.BrokenLink]
	if !ok || bl.Severity != analysis.Error {
		t.Errorf("broken link finding missing or not Error: %+v", bl)
	}
	if bl.Location.Line != 3 || bl.SuggestedFix == "" {
		t.Errorf("broken link finding lacks line/fix: %+v", bl)
	}
	if !strings.Contains(bl.SuggestedFix, "nope.md") {
		t.Errorf("broken link SuggestedFix should mention the target: %q", bl.SuggestedFix)
	}

	ba, ok := byKind[analysis.BrokenAnchor]
	if !ok || ba.Severity != analysis.Error {
		t.Errorf("broken anchor finding missing or not Error: %+v", ba)
	}
	if !strings.Contains(ba.Message, "#x") || !strings.Contains(ba.SuggestedFix, "slugif") {
		t.Errorf("broken anchor message/fix wrong: msg=%q fix=%q", ba.Message, ba.SuggestedFix)
	}

	amb, ok := byKind[analysis.Ambiguous]
	if !ok || amb.Severity != analysis.Warning {
		t.Errorf("ambiguous finding missing or not Warning: %+v", amb)
	}
	if !strings.Contains(amb.Message, "x/notes.md") || !strings.Contains(amb.Message, "y/notes.md") {
		t.Errorf("ambiguous message should list candidates: %q", amb.Message)
	}
}

func TestFindingID_Stable(t *testing.T) {
	r := reference.Reference{
		RawReference: reference.RawReference{Origin: "a.md", RawTarget: "nope.md", Line: 3, Type: reference.RelativeLink},
		Health:       reference.Broken,
	}
	f1 := findingsFromReferences([]reference.Reference{r})[0]
	f2 := findingsFromReferences([]reference.Reference{r})[0]
	if f1.ID != f2.ID {
		t.Errorf("finding IDs not stable: %q vs %q", f1.ID, f2.ID)
	}
	if !strings.HasPrefix(f1.ID, "broken-link:a.md:3:") {
		t.Errorf("unexpected finding ID format: %q", f1.ID)
	}
}

func TestCheckExitCode(t *testing.T) {
	tests := []struct {
		name   string
		res    Result
		strict bool
		want   platform.ExitCode
	}{
		{"clean", Result{}, false, platform.ExitOK},
		{"broken link", Result{BrokenLinkCount: 1}, false, platform.ExitFindings},
		{"broken anchor", Result{BrokenAnchorCount: 1}, false, platform.ExitFindings},
		{"ambiguous non-strict", Result{AmbiguousCount: 1}, false, platform.ExitOK},
		{"ambiguous strict", Result{AmbiguousCount: 1}, true, platform.ExitFindings},
		{"broken beats strict-off", Result{BrokenLinkCount: 2, AmbiguousCount: 3}, false, platform.ExitFindings},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.res.CheckExitCode(tt.strict); got != tt.want {
				t.Errorf("CheckExitCode(strict=%v) = %v, want %v", tt.strict, got, tt.want)
			}
		})
	}
}
