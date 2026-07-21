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

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/analysis"
)

// allKindsReport adds the graduated structure findings (under-linked, dead-end)
// on top of the broken/ambiguous set, so the new findings.schema kind-enum
// entries are POSITIVELY exercised by an emitted value (a kept-distinct fixture
// so sampleReport's exact-count assertions stay meaningful).
func allKindsReport() *analysis.AnalysisReport {
	findings := append(sampleReport().Findings(),
		analysis.Finding{
			ID: "under-linked:c.md", Kind: analysis.UnderLinked, Severity: analysis.Info,
			Location:     analysis.Location{Document: "c.md"},
			Message:      "\"c.md\" has only 1 inbound link(s) (below the discoverability threshold of 3); it is under-linked",
			SuggestedFix: "Add inbound links to \"c.md\" from related pages.",
			Details:      map[string]string{"targetDocument": "c.md", "inboundCount": "1"},
		},
		analysis.Finding{
			ID: "dead-end:d.md", Kind: analysis.DeadEnd, Severity: analysis.Info,
			Location:     analysis.Location{Document: "d.md"},
			Message:      "\"d.md\" is a dead-end: it has inbound links but links to nothing onward",
			SuggestedFix: "Add onward internal links from \"d.md\" to related documents.",
			Details:      map[string]string{"targetDocument": "d.md"},
		},
		analysis.Finding{
			ID: "suggested-link:e.md:f.md", Kind: analysis.SuggestedLink, Severity: analysis.Info,
			Location:     analysis.Location{Document: "e.md"},
			Message:      "\"e.md\" and \"f.md\" share 2 connection(s) but do not link to each other",
			SuggestedFix: "If these documents are related, add a navigational link between \"e.md\" and \"f.md\".",
			Details: map[string]string{
				"targetDocument": "e.md", "suggestedTarget": "f.md",
				"sharedNeighbours": "2", "coupling": "1", "coCitation": "1", "adamicAdar": "1.442695",
			},
		},
		analysis.Finding{
			ID: "articulation-point:g.md", Kind: analysis.ArticulationPoint, Severity: analysis.Info,
			Location:     analysis.Location{Document: "g.md"},
			Message:      "\"g.md\" is an articulation point: it is the only connector between two parts of the doc graph",
			SuggestedFix: "\"g.md\" is the only connector between two parts of the doc graph; add a redundant link path.",
			Details:      map[string]string{"targetDocument": "g.md", "betweenness": "0.250000"},
		},
		analysis.Finding{
			ID: "bridge:g.md:h.md", Kind: analysis.Bridge, Severity: analysis.Info,
			Location:     analysis.Location{Document: "g.md"},
			Message:      "the link between \"g.md\" and \"h.md\" is a bridge: the only connection between two parts of the doc graph",
			SuggestedFix: "the link between \"g.md\" and \"h.md\" is the only connection between two parts of the doc graph; add another path.",
			Details:      map[string]string{"targetDocument": "g.md", "bridgeEndpoint": "h.md"},
		},
	)
	return analysis.NewAnalysisReport(findings)
}

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
	b, err := FindingsJSON(sampleReport(), OKFVerdict{})
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
	if doc.SchemaVersion != FindingsSchemaVersion || doc.Tool != "matlatl" {
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
	b, err := FindingsJSON(sampleReport(), OKFVerdict{})
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

// TestFindingsJSON_StructureKindsValidateAgainstSchema positively exercises the
// findings.schema v3 additions: it emits a report containing under-linked and
// dead-end findings, validates the bytes against the published schema (so the
// new `kind` enum members and the underLinked/deadEnd summary fields are hit by
// real emitted values), and asserts the summary counts + the kinds appear.
func TestFindingsJSON_StructureKindsValidateAgainstSchema(t *testing.T) {
	b, err := FindingsJSON(allKindsReport(), OKFVerdict{})
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
		t.Errorf("findings.json with structure kinds does not satisfy schema:\n  %v", errs)
	}

	// Positively assert the new kinds + summary counts are present in the bytes.
	var doc struct {
		Summary struct {
			UnderLinked int `json:"underLinked"`
			DeadEnd     int `json:"deadEnd"`
		} `json:"summary"`
		RemediationGuide map[string]string `json:"remediationGuide"`
		Findings         []struct {
			Kind     string            `json:"kind"`
			Severity string            `json:"severity"`
			Details  map[string]string `json:"details"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Summary.UnderLinked != 1 || doc.Summary.DeadEnd != 1 {
		t.Errorf("summary structure counts wrong: underLinked=%d deadEnd=%d", doc.Summary.UnderLinked, doc.Summary.DeadEnd)
	}
	kinds := map[string]map[string]string{}
	for _, f := range doc.Findings {
		kinds[f.Kind] = f.Details
		if (f.Kind == "under-linked" || f.Kind == "dead-end") && f.Severity != "info" {
			t.Errorf("%s default severity = %q, want info", f.Kind, f.Severity)
		}
	}
	if _, ok := kinds["under-linked"]; !ok {
		t.Error("emitted findings missing an under-linked finding")
	}
	if got := kinds["under-linked"]["inboundCount"]; got != "1" {
		t.Errorf("under-linked detail inboundCount = %q, want 1", got)
	}
	if _, ok := kinds["dead-end"]; !ok {
		t.Error("emitted findings missing a dead-end finding")
	}
	// ADR 0013: the suggested-link kind + its details validate against the schema.
	if _, ok := kinds["suggested-link"]; !ok {
		t.Error("emitted findings missing a suggested-link finding")
	}
	if got := kinds["suggested-link"]["suggestedTarget"]; got != "f.md" {
		t.Errorf("suggested-link detail suggestedTarget = %q, want f.md", got)
	}
	for _, k := range []string{"under-linked", "dead-end", "suggested-link"} {
		if doc.RemediationGuide[k] == "" {
			t.Errorf("remediationGuide missing entry for emitted kind %q", k)
		}
	}
}

// TestFindingsJSON_FarFromRootValidatesAgainstSchema positively exercises the
// findings.schema v7 addition (ADR 0021): a report with one far-from-root finding
// validates against the published schema (so the `far-from-root` kind enum member
// and the farFromRoot summary field are hit by real emitted values), and the
// summary count + kind + Info severity appear.
func TestFindingsJSON_FarFromRootValidatesAgainstSchema(t *testing.T) {
	rep := analysis.NewAnalysisReport([]analysis.Finding{
		{
			ID: "far-from-root:deep.md", Kind: analysis.FarFromRoot, Severity: analysis.Info,
			Location:     analysis.Location{Document: "deep.md"},
			Message:      "\"deep.md\" is 7 hops from the nearest root (threshold 6): reachable but far from any entry point",
			SuggestedFix: "Add an inbound link to \"deep.md\" from a document closer to a root.",
			Details:      map[string]string{"targetDocument": "deep.md", "hopsFromRoot": "7"},
		},
	})
	b, err := FindingsJSON(rep, OKFVerdict{})
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
		t.Errorf("findings.json with a far-from-root finding does not satisfy schema:\n  %v", errs)
	}

	var doc struct {
		Summary struct {
			FarFromRoot int `json:"farFromRoot"`
		} `json:"summary"`
		RemediationGuide map[string]string `json:"remediationGuide"`
		Findings         []struct {
			Kind     string            `json:"kind"`
			Severity string            `json:"severity"`
			Details  map[string]string `json:"details"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Summary.FarFromRoot != 1 {
		t.Errorf("summary.farFromRoot = %d, want 1", doc.Summary.FarFromRoot)
	}
	if len(doc.Findings) != 1 || doc.Findings[0].Kind != "far-from-root" || doc.Findings[0].Severity != "info" {
		t.Errorf("emitted finding wrong: %+v", doc.Findings)
	}
	if doc.Findings[0].Details["hopsFromRoot"] != "7" {
		t.Errorf("far-from-root detail hopsFromRoot = %q, want 7", doc.Findings[0].Details["hopsFromRoot"])
	}
	if doc.RemediationGuide["far-from-root"] == "" {
		t.Error("remediationGuide missing entry for the emitted far-from-root kind")
	}
}

// TestFindingsJSON_OKFValidatesAgainstSchema positively exercises the
// findings.schema v8 additions (ADR 0023): a report carrying all three OKF
// conformance kinds, emitted with a non-conformant OKFVerdict, validates against
// the published schema (new kind-enum members, the three summary counts, and the
// okfConformance object with checked:true) and surfaces the verdict + counts.
func TestFindingsJSON_OKFValidatesAgainstSchema(t *testing.T) {
	rep := analysis.NewAnalysisReport([]analysis.Finding{
		{
			ID: "okf-missing-frontmatter:a.md", Kind: analysis.OKFMissingFrontmatter, Severity: analysis.Error,
			Location: analysis.Location{Document: "a.md", Line: 1},
			Message:  "\"a.md\" has no YAML frontmatter block (rule R1)",
			Details:  map[string]string{"targetDocument": "a.md", "frontmatterState": "absent"},
		},
		{
			ID: "okf-missing-type:b.md", Kind: analysis.OKFMissingType, Severity: analysis.Error,
			Location: analysis.Location{Document: "b.md", Line: 1},
			Message:  "\"b.md\" does not declare a non-empty OKF `type` (rule R2)",
			Details:  map[string]string{"targetDocument": "b.md", "reason": "frontmatter has no `type` field"},
		},
		{
			ID: "okf-reserved-file-structure:log.md:5", Kind: analysis.OKFReservedFileStructure, Severity: analysis.Error,
			Location: analysis.Location{Document: "log.md", Line: 5},
			Message:  "\"log.md\": heading is not a date (rule R3)",
			Details:  map[string]string{"targetDocument": "log.md", "reservedFile": "log.md", "reason": "bad date", "okfVersion": "0.1"},
		},
	})
	// Pass ONLY the non-derivable parts (Checked, Version) — deliberately omitting
	// the counts and conformant bit. FindingsJSON must DERIVE those from the report
	// (ADR 0023), so the emitted okfConformance cannot disagree with the findings
	// list even though the verdict carries no counts. (Also proves a hand-built
	// verdict can't inject inconsistent counts.)
	verdict := OKFVerdict{Checked: true, Version: "0.1"}
	b, err := FindingsJSON(rep, verdict)
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
		t.Fatalf("read schema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(sb, &schema); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if errs := validateFindingsNode(data, schema, "$"); len(errs) > 0 {
		sort.Strings(errs)
		t.Errorf("OKF findings.json does not satisfy schema:\n  %v", errs)
	}

	var doc struct {
		Summary struct {
			OKFMissingFrontmatter    int `json:"okfMissingFrontmatter"`
			OKFMissingType           int `json:"okfMissingType"`
			OKFReservedFileStructure int `json:"okfReservedFileStructure"`
		} `json:"summary"`
		OKFConformance struct {
			Checked               bool   `json:"checked"`
			Conformant            bool   `json:"conformant"`
			Version               string `json:"version"`
			MissingFrontmatter    int    `json:"missingFrontmatter"`
			MissingType           int    `json:"missingType"`
			ReservedFileStructure int    `json:"reservedFileStructure"`
		} `json:"okfConformance"`
		RemediationGuide map[string]string `json:"remediationGuide"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.OKFConformance.Checked || doc.OKFConformance.Conformant {
		t.Errorf("okfConformance verdict wrong: %+v", doc.OKFConformance)
	}
	if doc.OKFConformance.Version != "0.1" {
		t.Errorf("okfConformance.version = %q, want 0.1", doc.OKFConformance.Version)
	}
	if doc.OKFConformance.MissingFrontmatter != 1 || doc.OKFConformance.MissingType != 1 || doc.OKFConformance.ReservedFileStructure != 1 {
		t.Errorf("okfConformance counts wrong: %+v", doc.OKFConformance)
	}
	if doc.Summary.OKFMissingFrontmatter != 1 || doc.Summary.OKFMissingType != 1 || doc.Summary.OKFReservedFileStructure != 1 {
		t.Errorf("summary okf counts wrong: %+v", doc.Summary)
	}
	for _, k := range []string{"okf-missing-frontmatter", "okf-missing-type", "okf-reserved-file-structure"} {
		if doc.RemediationGuide[k] == "" {
			t.Errorf("remediationGuide missing entry for emitted kind %q", k)
		}
	}
}

// TestFindingsJSON_OKFModeOff asserts the mode-off shape: okfConformance is
// present with checked:false (the zero OKFVerdict) and still validates.
func TestFindingsJSON_OKFModeOff(t *testing.T) {
	b, err := FindingsJSON(sampleReport(), OKFVerdict{})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		OKFConformance struct {
			Checked bool `json:"checked"`
		} `json:"okfConformance"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OKFConformance.Checked {
		t.Error("okfConformance.checked should be false when OKF mode is off")
	}
}

// TestFindingsJSON_OKFConformanceDerivedFromReport asserts FindingsJSON DERIVES
// okfConformance's counts and conformant bit from the report, ignoring lying
// values on the passed verdict (ADR 0023) — so a hand-built verdict can never
// produce an okfConformance inconsistent with the findings list.
func TestFindingsJSON_OKFConformanceDerivedFromReport(t *testing.T) {
	// A report with exactly ONE okf-missing-type finding...
	rep := analysis.NewAnalysisReport([]analysis.Finding{{
		ID: "okf-missing-type:b.md", Kind: analysis.OKFMissingType, Severity: analysis.Error,
		Location: analysis.Location{Document: "b.md", Line: 1},
		Message:  "no type",
	}})
	// ...but a verdict that LIES: claims conformant, wrong counts.
	lying := OKFVerdict{
		Checked: true, Conformant: true, Version: "9.9",
		MissingFrontmatter: 42, MissingType: 0, ReservedFileStructure: 7,
	}
	b, err := FindingsJSON(rep, lying)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		OKFConformance struct {
			Checked               bool   `json:"checked"`
			Conformant            bool   `json:"conformant"`
			Version               string `json:"version"`
			MissingFrontmatter    int    `json:"missingFrontmatter"`
			MissingType           int    `json:"missingType"`
			ReservedFileStructure int    `json:"reservedFileStructure"`
		} `json:"okfConformance"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	// Counts + conformant come from the report, not the verdict.
	if doc.OKFConformance.MissingFrontmatter != 0 || doc.OKFConformance.MissingType != 1 || doc.OKFConformance.ReservedFileStructure != 0 {
		t.Errorf("counts not derived from report: %+v", doc.OKFConformance)
	}
	if doc.OKFConformance.Conformant {
		t.Error("conformant must be derived false (report has a violation), not taken from the lying verdict")
	}
	// Only the non-derivable parts (checked, version) come from the verdict.
	if !doc.OKFConformance.Checked || doc.OKFConformance.Version != "9.9" {
		t.Errorf("non-derivable fields wrong: %+v", doc.OKFConformance)
	}
}

// TestOKFVerdictLine_Lockstep asserts the verdict line is byte-identical whether
// rendered from the check summary path (OKFVerdictFromResult(res).Line()) or the
// report path (BuildView(res).OKF.Line()), for both CONFORMANT and NOT
// CONFORMANT (ADR 0023 3c: the two surfaces share one Line()).
func TestOKFVerdictLine_Lockstep(t *testing.T) {
	cases := []application.Result{
		{OKFMode: true, OKFConformant: true, OKFVersion: "0.1"},
		{OKFMode: true, OKFConformant: false, OKFMissingFrontmatterCount: 1, OKFMissingTypeCount: 2, OKFReservedFileStructureCount: 3},
	}
	for _, res := range cases {
		check := OKFVerdictFromResult(res).Line()
		report := BuildView(res).OKF.Line()
		if check != report {
			t.Errorf("verdict line drift:\n  check:  %q\n  report: %q", check, report)
		}
	}
}

// TestFindingsJSON_CleanValidatesAgainstSchema asserts a clean (zero-finding)
// report — an empty findings list and empty remediationGuide — still satisfies
// the schema (round-trip on the most common emitted shape).
func TestFindingsJSON_CleanValidatesAgainstSchema(t *testing.T) {
	b, err := FindingsJSON(analysis.NewAnalysisReport(nil), OKFVerdict{})
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
	a, _ := FindingsJSON(sampleReport(), OKFVerdict{})
	b, _ := FindingsJSON(sampleReport(), OKFVerdict{})
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
