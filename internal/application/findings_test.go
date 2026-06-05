package application

import (
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
	"github.com/stacklok/matlatl/internal/platform"
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

// TestFindingsFromMetrics covers the graph→Finding mapping: isolated orphans
// (Orphan/Warning), unreachable docs (Unreachable/Warning), and knowledge-gap
// bridge candidates (KnowledgeGap/Info), with severities per ADR 0005.
func TestFindingsFromMetrics(t *testing.T) {
	if got := findingsFromMetrics(nil); got != nil {
		t.Errorf("findingsFromMetrics(nil) = %v, want nil", got)
	}

	m := &graphmodel.GraphMetrics{
		Orphans: graphmodel.OrphanReport{
			Isolated:    []identity.DocumentID{"iso.md"},
			Unreachable: []identity.DocumentID{"stray.md"},
		},
		Gaps: []graphmodel.Gap{
			{ComponentA: "a.md", ComponentB: "b.md", RepresentativeA: "a.md", RepresentativeB: "b.md"},
		},
	}

	got := findingsFromMetrics(m)
	byKind := map[analysis.FindingKind]analysis.Finding{}
	for _, f := range got {
		byKind[f.Kind] = f
	}
	if len(got) != 3 {
		t.Fatalf("got %d findings, want 3 (orphan, unreachable, gap): %+v", len(got), got)
	}

	orphan, ok := byKind[analysis.Orphan]
	if !ok || orphan.Severity != analysis.Warning || orphan.Location.Document != "iso.md" {
		t.Errorf("orphan finding wrong: %+v", orphan)
	}

	// The unreachableFinding path specifically (ADR 0005 Warning).
	un, ok := byKind[analysis.Unreachable]
	if !ok {
		t.Fatal("missing Unreachable finding")
	}
	if un.Severity != analysis.Warning {
		t.Errorf("unreachable severity = %v, want Warning", un.Severity)
	}
	if un.Location.Document != "stray.md" {
		t.Errorf("unreachable location = %q, want stray.md", un.Location.Document)
	}
	if !strings.Contains(un.Message, "stray.md") || !strings.Contains(un.Message, "unreachable") {
		t.Errorf("unreachable message wrong: %q", un.Message)
	}
	if !strings.Contains(un.SuggestedFix, "inbound link") {
		t.Errorf("unreachable fix should suggest an inbound link: %q", un.SuggestedFix)
	}
	if !strings.HasPrefix(un.ID, "unreachable:stray.md") {
		t.Errorf("unreachable ID = %q, want unreachable:stray.md prefix", un.ID)
	}

	gap, ok := byKind[analysis.KnowledgeGap]
	if !ok || gap.Severity != analysis.Info {
		t.Errorf("gap finding missing or not Info: %+v", gap)
	}
	if !strings.Contains(gap.Message, "a.md") || !strings.Contains(gap.Message, "b.md") {
		t.Errorf("gap message should name both clusters: %q", gap.Message)
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
		// ADR 0005: orphans/unreachable are warnings — pass without --strict, fail
		// with it.
		{"orphan non-strict", Result{OrphanCount: 1}, false, platform.ExitOK},
		{"orphan strict", Result{OrphanCount: 1}, true, platform.ExitFindings},
		{"unreachable non-strict", Result{UnreachableCount: 1}, false, platform.ExitOK},
		{"unreachable strict", Result{UnreachableCount: 1}, true, platform.ExitFindings},
		// ADR 0005: knowledge gaps are Info — they NEVER fail, even under --strict.
		{"gap non-strict", Result{KnowledgeGapCount: 5}, false, platform.ExitOK},
		{"gap strict", Result{KnowledgeGapCount: 5}, true, platform.ExitOK},
		// ADR 0005: opt-in external dead-link findings are non-deterministic and
		// are deliberately kept OUT of the exit contract — they NEVER fail the
		// build, even under --strict.
		{"dead-link non-strict", Result{DeadLinkCount: 7}, false, platform.ExitOK},
		{"dead-link strict", Result{DeadLinkCount: 7}, true, platform.ExitOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.res.CheckExitCode(tt.strict); got != tt.want {
				t.Errorf("CheckExitCode(strict=%v) = %v, want %v", tt.strict, got, tt.want)
			}
		})
	}
}
