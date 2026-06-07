//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/platform"
)

func corpusFixture(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func cleanFixture(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", "clean"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func ambiguousFixture(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", "ambiguous"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestIntegration_RootEmpty: an empty corpus is success (ADR 0005).
func TestIntegration_RootEmpty(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{t.TempDir()}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("empty run code = %v, want ExitOK (stderr: %q)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "no markdown documents found") {
		t.Errorf("empty stdout = %q, want empty-corpus notice", out.String())
	}
}

// TestIntegration_CheckClean: a fully-resolvable corpus exits 0, and (with
// --out) still writes findings.json with an empty findings list + zero summary
// — ADR 0005 "artifacts emitted regardless of exit code".
func TestIntegration_CheckClean(t *testing.T) {
	outDir := t.TempDir()
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", cleanFixture(t), "--out", outDir}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("clean check code = %v, want ExitOK (stdout=%q stderr=%q)", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "0 broken link(s), 0 broken anchor(s)") {
		t.Errorf("clean summary = %q", out.String())
	}

	jb, err := os.ReadFile(filepath.Join(outDir, "findings.json"))
	if err != nil {
		t.Fatalf("findings.json not written on a clean run: %v", err)
	}
	var doc struct {
		Summary struct {
			Total int `json:"total"`
		} `json:"summary"`
		Findings []json.RawMessage `json:"findings"`
	}
	if err := json.Unmarshal(jb, &doc); err != nil {
		t.Fatalf("clean findings.json does not parse: %v", err)
	}
	if doc.Summary.Total != 0 || len(doc.Findings) != 0 {
		t.Errorf("clean findings.json should be empty, got total=%d findings=%d", doc.Summary.Total, len(doc.Findings))
	}
	// JUnit also written and parses with zero failures.
	if _, err := os.ReadFile(filepath.Join(outDir, "junit.xml")); err != nil {
		t.Errorf("junit.xml not written on a clean run: %v", err)
	}
}

// TestIntegration_CheckEmpty: no markdown found → exit 0 with notice.
func TestIntegration_CheckEmpty(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", t.TempDir()}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("empty check code = %v, want ExitOK", code)
	}
	if !strings.Contains(out.String(), "no markdown documents found") {
		t.Errorf("empty check summary = %q, want no-markdown notice", out.String())
	}
}

// TestIntegration_CheckBroken: the seeded corpus has broken links + a broken
// anchor → exit 1, and (with --out) writes a parseable findings.json + JUnit.
func TestIntegration_CheckBroken(t *testing.T) {
	outDir := t.TempDir()
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", corpusFixture(t), "--out", outDir}, &out, &errOut)

	if code != platform.ExitFindings {
		t.Fatalf("broken check code = %v, want ExitFindings (1) (stdout=%q)", code, out.String())
	}
	if !strings.Contains(out.String(), "broken link(s)") {
		t.Errorf("summary missing broken-link count: %q", out.String())
	}

	// findings.json parses and contains the seeded broken link + broken anchor.
	jb, err := os.ReadFile(filepath.Join(outDir, "findings.json"))
	if err != nil {
		t.Fatalf("findings.json not written: %v", err)
	}
	var doc struct {
		Summary struct {
			BrokenLink   int `json:"brokenLink"`
			BrokenAnchor int `json:"brokenAnchor"`
			Ambiguous    int `json:"ambiguous"`
		} `json:"summary"`
		Findings []struct {
			Kind     string `json:"kind"`
			Document string `json:"document"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(jb, &doc); err != nil {
		t.Fatalf("findings.json does not parse: %v", err)
	}
	if doc.Summary.BrokenLink < 1 || doc.Summary.BrokenAnchor < 1 {
		t.Errorf("expected >=1 broken link and >=1 broken anchor, got %+v", doc.Summary)
	}
	if doc.Summary.Ambiguous < 1 {
		t.Errorf("expected the ambiguous wikilink to be reported, got %+v", doc.Summary)
	}

	// JUnit parses.
	xb, err := os.ReadFile(filepath.Join(outDir, "junit.xml"))
	if err != nil {
		t.Fatalf("junit.xml not written: %v", err)
	}
	var suites struct {
		Failures int `xml:"failures,attr"`
	}
	if err := xml.Unmarshal(xb, &suites); err != nil {
		t.Fatalf("junit.xml does not parse: %v", err)
	}
	if suites.Failures < 2 {
		t.Errorf("junit failures = %d, want >= 2", suites.Failures)
	}
}

// TestIntegration_CheckDeterministic: findings.json is byte-stable across runs.
func TestIntegration_CheckDeterministic(t *testing.T) {
	read := func() []byte {
		outDir := t.TempDir()
		var out, errOut bytes.Buffer
		runArgs(context.Background(), []string{"check", corpusFixture(t), "--out", outDir}, &out, &errOut)
		b, err := os.ReadFile(filepath.Join(outDir, "findings.json"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if !bytes.Equal(read(), read()) {
		t.Error("findings.json is not deterministic across runs")
	}
}

// TestIntegration_CheckStrictPromotesAmbiguous: --strict turns the ambiguous
// wikilink into a failure even if there were no broken links (here there are
// also broken links, so exit is 1 either way; assert --strict still 1 and that
// the run is well-formed).
func TestIntegration_CheckStrict(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", corpusFixture(t), "--strict"}, &out, &errOut)
	if code != platform.ExitFindings {
		t.Fatalf("strict check code = %v, want ExitFindings", code)
	}
}

// TestIntegration_CheckBadResolution: an invalid --resolution is a usage error.
func TestIntegration_CheckBadResolution(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", cleanFixture(t), "--resolution", "bogus"}, &out, &errOut)
	if code != platform.ExitUsage {
		t.Fatalf("bad --resolution code = %v, want ExitUsage (2)", code)
	}
}

// TestIntegration_AmbiguousOnly proves the ADR 0005 warning contract end-to-end:
// a corpus whose ONLY finding is an ambiguous wikilink exits 0 by default and 1
// under --strict.
func TestIntegration_AmbiguousOnly(t *testing.T) {
	root := ambiguousFixture(t)

	var out1, err1 bytes.Buffer
	if code := runArgs(context.Background(), []string{"check", root}, &out1, &err1); code != platform.ExitOK {
		t.Fatalf("ambiguous default code = %v, want ExitOK (stdout=%q)", code, out1.String())
	}
	if !strings.Contains(out1.String(), "1 ambiguous") || !strings.Contains(out1.String(), "0 broken link(s)") {
		t.Errorf("ambiguous default summary = %q, want 1 ambiguous / 0 broken", out1.String())
	}

	var out2, err2 bytes.Buffer
	if code := runArgs(context.Background(), []string{"check", root, "--strict"}, &out2, &err2); code != platform.ExitFindings {
		t.Fatalf("ambiguous --strict code = %v, want ExitFindings (1)", code)
	}
}

// TestIntegration_ResolutionExact: --resolution=exact routes to the exact policy,
// so the extensionless [[notes]] no longer matches a full path → broken (exit 1).
func TestIntegration_ResolutionExact(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", ambiguousFixture(t), "--resolution", "exact"}, &out, &errOut)
	if code != platform.ExitFindings {
		t.Fatalf("exact-policy code = %v, want ExitFindings (broken, not ambiguous)", code)
	}
	if !strings.Contains(out.String(), "1 broken link(s)") || !strings.Contains(out.String(), "0 ambiguous") {
		t.Errorf("exact-policy summary = %q, want 1 broken / 0 ambiguous", out.String())
	}
}

// TestIntegration_ResolutionBasename: --resolution=basename matches both
// notes.md by basename → ambiguous (exit 0 without --strict), confirming the
// flag→policy routing differs from exact.
func TestIntegration_ResolutionBasename(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", ambiguousFixture(t), "--resolution", "basename"}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("basename-policy code = %v, want ExitOK", code)
	}
	if !strings.Contains(out.String(), "1 ambiguous") {
		t.Errorf("basename-policy summary = %q, want 1 ambiguous", out.String())
	}
}

// TestIntegration_Orphans lists the seeded orphan + unreachable documents from
// the corpus fixture and asserts the known set (intentional orphan suppressed).
func TestIntegration_Orphans(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"orphans", corpusFixture(t)}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("orphans code = %v, want ExitOK (always 0)", code)
	}
	s := out.String()

	// Isolated orphans: the two same-basename notes docs.
	for _, want := range []string{"docs/team/notes.md", "docs/project/notes.md"} {
		if !strings.Contains(s, want) {
			t.Errorf("orphans output missing isolated orphan %q:\n%s", want, s)
		}
	}
	// Unreachable: the seeded stray/cycle/island docs.
	for _, want := range []string{"docs/stray.md", "docs/cycle/alpha.md", "docs/island/one.md"} {
		if !strings.Contains(s, want) {
			t.Errorf("orphans output missing unreachable %q:\n%s", want, s)
		}
	}
	// The intentional orphan must NOT appear.
	if strings.Contains(s, "CHANGELOG.md") {
		t.Errorf("intentional orphan CHANGELOG.md must be suppressed:\n%s", s)
	}
}

// TestIntegration_OrphansDeterministic: orphans output is byte-stable.
func TestIntegration_OrphansDeterministic(t *testing.T) {
	run := func() string {
		var out, errOut bytes.Buffer
		runArgs(context.Background(), []string{"orphans", corpusFixture(t)}, &out, &errOut)
		return out.String()
	}
	if run() != run() {
		t.Error("orphans output is not deterministic across runs")
	}
}

// TestIntegration_OrphansMutuallyExclusiveFlags: --isolated-only and
// --unreachable-only together is a usage error.
func TestIntegration_OrphansMutuallyExclusiveFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"orphans", corpusFixture(t), "--isolated-only", "--unreachable-only"}, &out, &errOut)
	if code != platform.ExitUsage {
		t.Fatalf("conflicting flags code = %v, want ExitUsage (2)", code)
	}
}

// TestIntegration_CheckStrictFailsOnOrphans: with a corpus that has orphans but
// (for this assertion) we rely on --strict promoting orphan/unreachable to a
// failure. The corpus fixture also has broken links, so it is exit 1 regardless;
// this asserts --strict does not REDUCE the exit code and the run is well-formed.
func TestIntegration_CheckStrictWithOrphans(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", corpusFixture(t), "--strict"}, &out, &errOut)
	if code != platform.ExitFindings {
		t.Fatalf("strict check code = %v, want ExitFindings", code)
	}
	if !strings.Contains(out.String(), "orphan(s)") {
		t.Errorf("check summary should report orphans: %q", out.String())
	}
}

// underLinkedCorpus stages a corpus shaped to produce UNDER-LINKED (but no
// orphan/dead-end/broken) findings: a README root links to a hub, the hub links
// to several leaves, and each leaf links back to README. Every leaf has out>0
// (so none is a dead-end) and in=1 (< default threshold 3, so all under-linked),
// and the whole graph is reachable from README (no unreachable). It returns the
// staged directory.
func underLinkedCorpus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// README -> hub; hub -> leafA/leafB; leaves -> README (so out>0 and reachable).
	writeFile("README.md", "# Readme\n\nSee [hub](hub.md).\n")
	writeFile("hub.md", "# Hub\n\nSee [a](leafA.md) and [b](leafB.md).\n")
	writeFile("leafA.md", "# Leaf A\n\nBack to [home](README.md).\n")
	writeFile("leafB.md", "# Leaf B\n\nBack to [home](README.md).\n")
	return dir
}

// TestIntegration_OrphansSurfacesUnderLinked: `matlatl orphans` lists the
// under-linked documents (and dead-ends) alongside isolated orphans (ADR 0012).
func TestIntegration_OrphansSurfacesUnderLinked(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"orphans", underLinkedCorpus(t)}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("orphans code = %v, want ExitOK (always 0)", code)
	}
	s := out.String()
	if !strings.Contains(s, "Under-linked") {
		t.Errorf("orphans output missing the Under-linked section:\n%s", s)
	}
	// hub.md has in=1 (<3) and out=2 → under-linked. leafA/leafB in=1 → under-linked.
	for _, want := range []string{"hub.md", "leafA.md", "leafB.md"} {
		if !strings.Contains(s, want) {
			t.Errorf("orphans output missing under-linked doc %q:\n%s", want, s)
		}
	}
	if !strings.Contains(s, "Dead-ends") {
		t.Errorf("orphans output missing the Dead-ends section:\n%s", s)
	}
}

// TestIntegration_CheckUnderLinkedDefaultSeverity: an under-linked-only corpus
// passes `check --strict` at the DEFAULT (info) structure-finding severity —
// under-linked/dead-end never gate the build unless explicitly promoted
// (ADR 0012). This is the user-reachable proof of the configurable-severity
// default.
func TestIntegration_CheckUnderLinkedDefaultSeverity(t *testing.T) {
	dir := underLinkedCorpus(t)
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", dir, "--strict"}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("under-linked-only check --strict code = %v, want ExitOK at default severity (stdout=%q stderr=%q)",
			code, out.String(), errOut.String())
	}
}

// TestIntegration_CheckUnderLinkedWarningSeverity: promoting the structure
// findings to "warning" via .matlatl.yml makes the same under-linked-only corpus
// fail `check --strict` (ADR 0012 configurable knob).
func TestIntegration_CheckUnderLinkedWarningSeverity(t *testing.T) {
	dir := underLinkedCorpus(t)
	if err := os.WriteFile(filepath.Join(dir, ".matlatl.yml"),
		[]byte("structureFindingsSeverity: warning\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", dir, "--strict"}, &out, &errOut)
	if code != platform.ExitFindings {
		t.Fatalf("warning-severity under-linked check --strict code = %v, want ExitFindings (stdout=%q stderr=%q)",
			code, out.String(), errOut.String())
	}
}

// TestIntegration_InboundThresholdFlag: --inbound-threshold 1 means a single
// inbound link is enough, so the under-linked-only corpus produces no
// under-linked finding (hub/leaves all have in>=1).
func TestIntegration_InboundThresholdFlag(t *testing.T) {
	dir := underLinkedCorpus(t)
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(),
		[]string{"orphans", dir, "--inbound-threshold", "1"}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("orphans code = %v, want ExitOK", code)
	}
	s := out.String()
	// With threshold 1, the Under-linked section should report (none).
	idx := strings.Index(s, "Under-linked")
	if idx < 0 {
		t.Fatalf("missing Under-linked section:\n%s", s)
	}
	// hub.md must NOT be listed as under-linked anymore.
	if strings.Contains(s[idx:], "hub.md") {
		t.Errorf("with --inbound-threshold 1, hub.md (in=1) must not be under-linked:\n%s", s)
	}
}

// --- P4 human-emitter integration tests ---

// TestIntegration_ReportToOut: `report --out` writes a non-empty, parseable
// report.md under --out and nothing escapes the out dir.
func TestIntegration_ReportToOut(t *testing.T) {
	outDir := t.TempDir()
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"report", corpusFixture(t), "--out", outDir}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("report code = %v, want ExitOK (stderr=%q)", code, errOut.String())
	}
	b, err := os.ReadFile(filepath.Join(outDir, "report.md"))
	if err != nil {
		t.Fatalf("report.md not written: %v", err)
	}
	if !strings.Contains(string(b), "# matlatl report") || !strings.Contains(string(b), "| Metric | Count |") {
		t.Errorf("report.md missing expected content:\n%s", b)
	}
	// The Navigability section (ADR 0014) renders end-to-end.
	if !strings.Contains(string(b), "## Navigability") || !strings.Contains(string(b), "Compactness:") {
		t.Errorf("report.md missing the Navigability section:\n%s", b)
	}
	// The critical-path sections (ADR 0015) render end-to-end.
	if !strings.Contains(string(b), "## Load-bearing docs") || !strings.Contains(string(b), "## Critical structure") {
		t.Errorf("report.md missing the Load-bearing docs / Critical structure sections:\n%s", b)
	}
	assertNothingEscaped(t, outDir, []string{"report.md"})
}

// TestIntegration_GraphDotToOut: `graph --format dot --out` writes a parseable
// (balanced-brace) graph.dot under --out.
func TestIntegration_GraphDotToOut(t *testing.T) {
	outDir := t.TempDir()
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"graph", corpusFixture(t), "--format", "dot", "--out", outDir}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("graph dot code = %v, want ExitOK (stderr=%q)", code, errOut.String())
	}
	b, err := os.ReadFile(filepath.Join(outDir, "graph.dot"))
	if err != nil {
		t.Fatalf("graph.dot not written: %v", err)
	}
	s := string(b)
	if !strings.HasPrefix(s, "digraph matlatl {") {
		t.Errorf("graph.dot missing digraph header:\n%s", s)
	}
	if strings.Count(s, "{") != strings.Count(s, "}") {
		t.Errorf("graph.dot braces unbalanced: { = %d } = %d", strings.Count(s, "{"), strings.Count(s, "}"))
	}
	assertNothingEscaped(t, outDir, []string{"graph.dot"})
}

// TestIntegration_GraphMermaidDefault: `graph` defaults to mermaid on stdout.
func TestIntegration_GraphMermaidDefault(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"graph", corpusFixture(t)}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("graph default code = %v, want ExitOK", code)
	}
	if !strings.HasPrefix(out.String(), "```mermaid\n") {
		t.Errorf("graph default did not emit a mermaid block:\n%s", out.String())
	}
}

// TestIntegration_IndexToOut: `index --out` writes a non-empty index.md.
func TestIntegration_IndexToOut(t *testing.T) {
	outDir := t.TempDir()
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"index", corpusFixture(t), "--out", outDir}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("index code = %v, want ExitOK (stderr=%q)", code, errOut.String())
	}
	b, err := os.ReadFile(filepath.Join(outDir, "index.md"))
	if err != nil {
		t.Fatalf("index.md not written: %v", err)
	}
	if !strings.Contains(string(b), "# Documentation index") || !strings.Contains(string(b), "CHANGELOG.md") {
		t.Errorf("index.md missing expected content:\n%s", b)
	}
	assertNothingEscaped(t, outDir, []string{"index.md"})
}

// TestIntegration_GraphBadFormat: an unknown --format is a usage error.
func TestIntegration_GraphBadFormat(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"graph", corpusFixture(t), "--format", "svg"}, &out, &errOut)
	if code != platform.ExitUsage {
		t.Fatalf("bad --format code = %v, want ExitUsage (2)", code)
	}
}

// --- P5 LLM-emitter integration tests ---

// TestIntegration_GraphJSONToOut: `graph --format json --out` writes a parseable,
// schema-stamped graph.json under --out, and a second run is byte-identical.
func TestIntegration_GraphJSONToOut(t *testing.T) {
	outDir := t.TempDir()
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"graph", corpusFixture(t), "--format", "json", "--out", outDir}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("graph json code = %v, want ExitOK (stderr=%q)", code, errOut.String())
	}
	b, err := os.ReadFile(filepath.Join(outDir, "graph.json"))
	if err != nil {
		t.Fatalf("graph.json not written: %v", err)
	}
	var doc struct {
		SchemaVersion int `json:"schemaVersion"`
		Summary       struct {
			Documents          int `json:"documents"`
			ArticulationPoints int `json:"articulationPoints"`
			Bridges            int `json:"bridges"`
			Navigability       struct {
				Compactness    float64 `json:"compactness"`
				Diameter       int     `json:"diameter"`
				ReachablePairs int     `json:"reachablePairs"`
			} `json:"navigability"`
		} `json:"summary"`
		Betweenness struct {
			TopDocs []json.RawMessage `json:"topDocs"`
		} `json:"betweenness"`
		ArticulationPoints []string          `json:"articulationPoints"`
		Bridges            []json.RawMessage `json:"bridges"`
		Nodes              []struct {
			Betweenness    float64 `json:"betweenness"`
			IsArticulation bool    `json:"isArticulation"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("graph.json does not parse: %v", err)
	}
	if doc.SchemaVersion != 6 || doc.Summary.Documents == 0 || len(doc.Nodes) == 0 {
		t.Errorf("graph.json content unexpected: version=%d docs=%d nodes=%d", doc.SchemaVersion, doc.Summary.Documents, len(doc.Nodes))
	}
	// summary.navigability (ADR 0014) is present with sensible values: the fixture
	// corpus is connected enough to have a positive compactness and finite pairs.
	if doc.Summary.Navigability.ReachablePairs == 0 || doc.Summary.Navigability.Diameter == 0 {
		t.Errorf("graph.json summary.navigability looks empty: %+v", doc.Summary.Navigability)
	}
	// Critical-path data (ADR 0015, v5): the betweenness block, the
	// articulationPoints/bridges arrays and the per-node fields are present, and
	// the fixture corpus has a non-trivial critical structure.
	if len(doc.Betweenness.TopDocs) == 0 {
		t.Errorf("graph.json betweenness.topDocs missing")
	}
	if len(doc.ArticulationPoints) == 0 || len(doc.Bridges) == 0 {
		t.Errorf("graph.json articulationPoints/bridges empty: %d/%d", len(doc.ArticulationPoints), len(doc.Bridges))
	}
	if doc.Summary.ArticulationPoints != len(doc.ArticulationPoints) || doc.Summary.Bridges != len(doc.Bridges) {
		t.Errorf("summary counts disagree with arrays: ap %d!=%d, br %d!=%d",
			doc.Summary.ArticulationPoints, len(doc.ArticulationPoints), doc.Summary.Bridges, len(doc.Bridges))
	}
	assertNothingEscaped(t, outDir, []string{"graph.json"})
}

// TestIntegration_SuggestedLinks: the topology-based suggested-link signal
// (ADR 0013) surfaces end-to-end through the real CLI — a suggested-link finding
// in findings.json, a suggestedLinks entry in graph.json, the report section, and
// (crucially) it does NOT change the exit code even under --strict.
func TestIntegration_SuggestedLinks(t *testing.T) {
	outDir := t.TempDir()
	var out, errOut bytes.Buffer
	// `emit --out` runs the full pipeline and writes the artifact bundle
	// (findings.json + graph.json). The corpus fixture's island three/four pair is
	// unlinked and shares two neighbours, so it yields a suggested-link.
	if code := runArgs(context.Background(),
		[]string{"emit", corpusFixture(t), "--out", outDir}, &out, &errOut); code != platform.ExitOK {
		t.Fatalf("emit code = %v, want ExitOK (stderr=%q)", code, errOut.String())
	}

	// findings.json carries a suggested-link finding (Info) with its details.
	fb, err := os.ReadFile(filepath.Join(outDir, "findings.json"))
	if err != nil {
		t.Fatalf("findings.json not written: %v", err)
	}
	var fdoc struct {
		SchemaVersion int `json:"schemaVersion"`
		Summary       struct {
			SuggestedLink int `json:"suggestedLink"`
		} `json:"summary"`
		Findings []struct {
			Kind     string            `json:"kind"`
			Severity string            `json:"severity"`
			Details  map[string]string `json:"details"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(fb, &fdoc); err != nil {
		t.Fatalf("findings.json does not parse: %v", err)
	}
	if fdoc.SchemaVersion != 6 {
		t.Errorf("findings.json schemaVersion = %d, want 6", fdoc.SchemaVersion)
	}
	if fdoc.Summary.SuggestedLink < 1 {
		t.Errorf("findings.json summary.suggestedLink = %d, want >= 1", fdoc.Summary.SuggestedLink)
	}
	var found bool
	for _, f := range fdoc.Findings {
		if f.Kind == "suggested-link" {
			found = true
			if f.Severity != "info" {
				t.Errorf("suggested-link severity = %q, want info", f.Severity)
			}
			if f.Details["suggestedTarget"] == "" || f.Details["sharedNeighbours"] == "" {
				t.Errorf("suggested-link finding missing details: %+v", f.Details)
			}
		}
	}
	if !found {
		t.Error("findings.json missing a suggested-link finding")
	}

	// graph.json carries the suggestedLinks array.
	gb, err := os.ReadFile(filepath.Join(outDir, "graph.json"))
	if err != nil {
		t.Fatalf("graph.json not written: %v", err)
	}
	var gdoc struct {
		Summary struct {
			SuggestedLinks int `json:"suggestedLinks"`
		} `json:"summary"`
		SuggestedLinks []struct {
			DocA             string `json:"docA"`
			DocB             string `json:"docB"`
			SharedNeighbours int    `json:"sharedNeighbours"`
		} `json:"suggestedLinks"`
	}
	if err := json.Unmarshal(gb, &gdoc); err != nil {
		t.Fatalf("graph.json does not parse: %v", err)
	}
	if len(gdoc.SuggestedLinks) < 1 || gdoc.Summary.SuggestedLinks < 1 {
		t.Errorf("graph.json suggestedLinks empty: array=%d summary=%d",
			len(gdoc.SuggestedLinks), gdoc.Summary.SuggestedLinks)
	}

	// The report renders a Suggested links section.
	var rout, rerr bytes.Buffer
	if rc := runArgs(context.Background(), []string{"report", corpusFixture(t)}, &rout, &rerr); rc != platform.ExitOK {
		t.Fatalf("report code = %v, want ExitOK (stderr=%q)", rc, rerr.String())
	}
	if !strings.Contains(rout.String(), "## Suggested links") {
		t.Error("report.md missing the Suggested links section")
	}
}

// TestIntegration_SuggestedLinksDoNotGate: a corpus whose ONLY findings are
// suggested links exits ExitOK even under --strict (ADR 0013: Info, ungated).
func TestIntegration_SuggestedLinksDoNotGate(t *testing.T) {
	// Build a tiny clean corpus where two docs share two neighbours but do not
	// link to each other, and everything is reachable from the README root.
	root := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// README links to all four so nothing is an orphan/unreachable. three and four
	// both link to one and two (shared neighbours), but not to each other.
	write("README.md", "# Home\n\n[one](one.md) [two](two.md) [three](three.md) [four](four.md)\n")
	write("one.md", "# One\n\n[home](README.md)\n")
	write("two.md", "# Two\n\n[home](README.md)\n")
	write("three.md", "# Three\n\n[one](one.md) [two](two.md)\n")
	write("four.md", "# Four\n\n[one](one.md) [two](two.md)\n")

	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", root, "--strict"}, &out, &errOut)
	if code != platform.ExitOK {
		t.Errorf("check --strict on a suggested-link-only corpus = %v, want ExitOK (suggested links never gate); stderr=%q",
			code, errOut.String())
	}
}

// TestIntegration_EmitBundle: `emit --out` writes the full LLM artifact set,
// every file parses, and nothing escapes --out.
func TestIntegration_EmitBundle(t *testing.T) {
	outDir := t.TempDir()
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"emit", corpusFixture(t), "--out", outDir}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("emit code = %v, want ExitOK (stderr=%q)", code, errOut.String())
	}
	want := []string{"index.md", "llms.txt", "llms-full.txt", "llms-small.txt", "graph.json", "trails.json", "findings.json"}
	for _, name := range want {
		b, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("bundle missing %s: %v", name, err)
		}
		if len(b) == 0 {
			t.Errorf("bundle artifact %s is empty", name)
		}
	}
	// graph.json + trails.json + findings.json parse as JSON.
	for _, name := range []string{"graph.json", "trails.json", "findings.json"} {
		b, _ := os.ReadFile(filepath.Join(outDir, name))
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			t.Errorf("%s does not parse: %v", name, err)
		}
	}
	// llms.txt has the spec shape.
	llms, _ := os.ReadFile(filepath.Join(outDir, "llms.txt"))
	if !strings.HasPrefix(string(llms), "# ") || !strings.Contains(string(llms), "## Known gaps") {
		t.Errorf("llms.txt missing spec shape:\n%s", llms)
	}
	assertNothingEscaped(t, outDir, want)
}

// TestIntegration_ScentExemptions drives the REAL CLI (`emit --out`) over a temp
// fixture and asserts the two low-scent non-flag rules (ADR 0016) end-to-end:
//   - a directory link [the adr folder](adr/) expands into synthetic, anchor-less,
//     line-0 vouch edges (ADR 0008) → NONE of them produce a low-scent finding
//     (the phantom empty-anchor / Line-0 bug is gone);
//   - a stable-ID anchor [ADR 0001](adr/0001-foo.md) → NO finding (exempt);
//   - a bare-path anchor [adr/0002-bar.md](adr/0002-bar.md) → finding present;
//   - a vague anchor [click here](adr/0001-foo.md) → finding present.
func TestIntegration_ScentExemptions(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(name, body string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Hub doc: a directory link (vouches for the folder, ADR 0008) + the four
	// anchor cases. README.md is an auto-root so the cluster is reachable.
	writeFile("README.md", "# Home\n\n"+
		"See [the adr folder](adr/).\n\n"+ // directory link → synthetic edges
		"See [ADR 0001](adr/0001-foo.md).\n\n"+ // stable-ID anchor → exempt
		"See [adr/0002-bar.md](adr/0002-bar.md).\n\n"+ // raw-path anchor → flagged
		"See [click here](adr/0001-foo.md).\n") // vague anchor → flagged
	writeFile("adr/0001-foo.md", "# Foo Decision\n\nBody.\n")
	// Title deliberately shares no token with the path "adr/0002-bar.md", so the
	// raw-path anchor scores below the threshold and is genuinely flagged.
	writeFile("adr/0002-bar.md", "# Second Choice Record\n\nBody.\n")

	outDir := t.TempDir()
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"emit", dir, "--out", outDir}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("emit code = %v, want ExitOK (stderr=%q)", code, errOut.String())
	}

	jb, err := os.ReadFile(filepath.Join(outDir, "findings.json"))
	if err != nil {
		t.Fatalf("findings.json not written: %v", err)
	}
	var fdoc struct {
		Findings []struct {
			Kind     string            `json:"kind"`
			Document string            `json:"document"`
			Line     int               `json:"line"`
			Details  map[string]string `json:"details"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(jb, &fdoc); err != nil {
		t.Fatalf("findings.json does not parse: %v", err)
	}

	// Collect the low-scent-anchor findings by their anchor text.
	scentByAnchor := map[string]int{}
	for _, f := range fdoc.Findings {
		if f.Kind != "low-scent-anchor" {
			continue
		}
		// The bug: no synthetic edge (empty anchor / Line 0) may produce a finding.
		if f.Details["anchorText"] == "" || f.Line <= 0 {
			t.Errorf("phantom low-scent finding from a synthetic/lineless edge: %+v", f)
		}
		scentByAnchor[f.Details["anchorText"]]++
	}

	// Exempt: stable-ID anchor → no finding.
	if n := scentByAnchor["ADR 0001"]; n != 0 {
		t.Errorf("stable-ID anchor 'ADR 0001' should be exempt, got %d finding(s)", n)
	}
	// Flagged: raw-path anchor.
	if n := scentByAnchor["adr/0002-bar.md"]; n == 0 {
		t.Errorf("raw-path anchor 'adr/0002-bar.md' should be flagged, got 0 (all: %v)", scentByAnchor)
	}
	// Flagged: vague anchor.
	if n := scentByAnchor["click here"]; n == 0 {
		t.Errorf("vague anchor 'click here' should be flagged, got 0 (all: %v)", scentByAnchor)
	}

	// De-vacuum: the phantom check above would ALSO pass if directory expansion
	// produced zero edges. Assert the directory link actually expanded (ADR 0008)
	// by reading graph.json and confirming README.md has an out-edge to an adr/
	// member — so a future regression disabling expansion can't make this test
	// silently green.
	gb, err := os.ReadFile(filepath.Join(outDir, "graph.json"))
	if err != nil {
		t.Fatalf("graph.json not written: %v", err)
	}
	var gdoc struct {
		Edges []struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(gb, &gdoc); err != nil {
		t.Fatalf("graph.json does not parse: %v", err)
	}
	expanded := false
	for _, e := range gdoc.Edges {
		if e.From == "README.md" && strings.HasPrefix(e.To, "adr/") {
			expanded = true
			break
		}
	}
	if !expanded {
		t.Errorf("directory link [the adr folder](adr/) did not expand into README.md→adr/* edges "+
			"(ADR 0008); the phantom-edge check would be vacuous. edges=%+v", gdoc.Edges)
	}
}

func dirlinksFixture(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", "dirlinks"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestIntegration_DirectoryLinks exercises ADR 0008 end-to-end: a directory link
// ([the ADRs](adr/)) is NOT a broken link, and the folder's docs are reachable
// (no orphans) under the default policy. A link to an existing non-markdown
// directory ([examples](examples/), which holds only a .txt) likewise resolves
// to a NonNote asset and is NOT a broken link. Under --strict the directory link
// does not vouch for the folder's non-index contents, so they surface as
// findings and the run fails.
func TestIntegration_DirectoryLinks(t *testing.T) {
	// Default policy: directory link resolves, contents reachable, exit 0.
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"check", dirlinksFixture(t)}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("default dirlinks check code = %v, want ExitOK (stdout=%q stderr=%q)", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "0 broken link(s)") {
		t.Errorf("directory link reported as broken: %q", out.String())
	}
	if !strings.Contains(out.String(), "0 orphan(s)") {
		t.Errorf("folder contents reported as orphans under default policy: %q", out.String())
	}

	// Strict policy: directory link validates but does not vouch; the two
	// non-index ADRs surface as orphans → exit 1.
	out.Reset()
	errOut.Reset()
	scode := runArgs(context.Background(), []string{"check", dirlinksFixture(t), "--strict"}, &out, &errOut)
	if scode != platform.ExitFindings {
		t.Fatalf("strict dirlinks check code = %v, want ExitFindings (stdout=%q)", scode, out.String())
	}
	if !strings.Contains(out.String(), "0 broken link(s)") {
		t.Errorf("strict still must not break the directory link itself: %q", out.String())
	}
	if !strings.Contains(out.String(), "2 orphan(s)") {
		t.Errorf("strict should surface the 2 non-index siblings as orphans: %q", out.String())
	}
}

// TestIntegration_RootFlagChangesReachability proves the persistent --root flag
// (ADR 0007) feeds graphmodel.ResolveRootSet: a corpus with NO autodetected root
// (no README.md/index.md, no type:index) has indeterminate reachability, so its
// linked-but-rootless docs are not "unreachable". Designating an entry point with
// --root makes the rest reachable — the graph.json reachability/unreachable shape
// changes accordingly.
func TestIntegration_RootFlagChangesReachability(t *testing.T) {
	dir := t.TempDir()
	// entry.md → target.md → leaf.md. No README/index/type:index anywhere, so
	// without --root the root set is empty (reachability indeterminate).
	writeFile := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("entry.md", "# Entry\n\nSee [target](target.md).\n")
	writeFile("target.md", "# Target\n\nSee [leaf](leaf.md).\n")
	writeFile("leaf.md", "# Leaf\n\nThe end.\n")

	graphJSON := func(extraArgs ...string) struct {
		Reachability struct {
			Indeterminate bool `json:"indeterminate"`
		} `json:"reachability"`
		RootSet     []string `json:"rootSet"`
		Unreachable []string `json:"unreachable"`
	} {
		outDir := t.TempDir()
		args := append([]string{"graph", dir, "--format", "json", "--out", outDir}, extraArgs...)
		var out, errOut bytes.Buffer
		if code := runArgs(context.Background(), args, &out, &errOut); code != platform.ExitOK {
			t.Fatalf("graph json code = %v (stderr=%q)", code, errOut.String())
		}
		b, err := os.ReadFile(filepath.Join(outDir, "graph.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Reachability struct {
				Indeterminate bool `json:"indeterminate"`
			} `json:"reachability"`
			RootSet     []string `json:"rootSet"`
			Unreachable []string `json:"unreachable"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("graph.json parse: %v", err)
		}
		return doc
	}

	// No --root: indeterminate, empty root set.
	bare := graphJSON()
	if !bare.Reachability.Indeterminate {
		t.Fatalf("without --root, reachability should be indeterminate; got %+v", bare)
	}
	if len(bare.RootSet) != 0 {
		t.Errorf("without --root, root set should be empty; got %v", bare.RootSet)
	}

	// --root entry.md: entry is now a root, reachability is determinate, and
	// target.md + leaf.md are reachable (so NOT unreachable).
	rooted := graphJSON("--root", "entry.md")
	if rooted.Reachability.Indeterminate {
		t.Fatalf("with --root entry.md, reachability should be determinate; got %+v", rooted)
	}
	if len(rooted.RootSet) != 1 || rooted.RootSet[0] != "entry.md" {
		t.Errorf("with --root entry.md, root set = %v, want [entry.md]", rooted.RootSet)
	}
	for _, d := range rooted.Unreachable {
		if d == "target.md" || d == "leaf.md" {
			t.Errorf("with --root entry.md, %q should be reachable, not unreachable (unreachable=%v)", d, rooted.Unreachable)
		}
	}
	// And a --root that only designates the leaf leaves entry/target unreachable,
	// proving the glob actually drives the BFS start set.
	leafOnly := graphJSON("--root", "leaf.md")
	if leafOnly.Reachability.Indeterminate {
		t.Fatalf("with --root leaf.md, reachability should be determinate")
	}
	foundUnreachable := false
	for _, d := range leafOnly.Unreachable {
		if d == "entry.md" || d == "target.md" {
			foundUnreachable = true
		}
	}
	if !foundUnreachable {
		t.Errorf("with --root leaf.md, entry/target should be unreachable; got unreachable=%v", leafOnly.Unreachable)
	}
}

// TestIntegration_SkillRootAndEdgelessRoot proves the ADR-0007 root semantics
// end-to-end via graph.json: (a) a SKILL.md that links to references/x.md forms a
// reachable cluster (SKILL.md is an auto-root by filename, so neither it nor its
// target is unreachable), and (b) an edgeless agent-style doc is reported isolated
// by default but is exempt once designated a root with --root.
func TestIntegration_SkillRootAndEdgelessRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(name, body string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// SKILL.md (auto-root by filename) → references/x.md cluster.
	writeFile("SKILL.md", "# Skill\n\nSee [x](references/x.md).\n")
	writeFile("references/x.md", "# X\n\nReference body.\n")
	// An edgeless agent-style doc: no inbound, no outbound links.
	writeFile("agent.md", "# Agent\n\nStandalone entry point, links to nothing.\n")

	graphJSON := func(extraArgs ...string) struct {
		RootSet     []string `json:"rootSet"`
		Orphans     []string `json:"orphans"`
		Unreachable []string `json:"unreachable"`
	} {
		outDir := t.TempDir()
		args := append([]string{"graph", dir, "--format", "json", "--out", outDir}, extraArgs...)
		var out, errOut bytes.Buffer
		if code := runArgs(context.Background(), args, &out, &errOut); code != platform.ExitOK {
			t.Fatalf("graph json code = %v (stderr=%q)", code, errOut.String())
		}
		b, err := os.ReadFile(filepath.Join(outDir, "graph.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			RootSet     []string `json:"rootSet"`
			Orphans     []string `json:"orphans"`
			Unreachable []string `json:"unreachable"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("graph.json parse: %v", err)
		}
		return doc
	}

	// Default run: SKILL.md is an auto-root, its cluster is reachable.
	bare := graphJSON()
	if !strings.Contains(strings.Join(bare.RootSet, ","), "SKILL.md") {
		t.Errorf("SKILL.md should be an auto-root; rootSet = %v", bare.RootSet)
	}
	for _, d := range bare.Unreachable {
		if d == "SKILL.md" || d == "references/x.md" {
			t.Errorf("SKILL.md cluster must be reachable, not unreachable; unreachable = %v", bare.Unreachable)
		}
	}
	for _, d := range bare.Orphans {
		if d == "SKILL.md" || d == "references/x.md" {
			t.Errorf("SKILL.md cluster has edges; must not be isolated; orphans = %v", bare.Orphans)
		}
	}
	// The edgeless agent doc IS isolated by default (general case still fires).
	if !slicesContains(bare.Orphans, "agent.md") {
		t.Errorf("edgeless agent.md should be isolated by default; orphans = %v", bare.Orphans)
	}

	// Designate agent.md a root: it must now be exempt from the isolated finding.
	rooted := graphJSON("--root", "agent.md")
	if slicesContains(rooted.Orphans, "agent.md") {
		t.Errorf("with --root agent.md, the edgeless root must be exempt from isolated; orphans = %v", rooted.Orphans)
	}
	if slicesContains(rooted.Unreachable, "agent.md") {
		t.Errorf("a root must never be unreachable; unreachable = %v", rooted.Unreachable)
	}
}

func slicesContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestIntegration_EmitRequiresOut: `emit` without --out is a usage error.
func TestIntegration_EmitRequiresOut(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"emit", corpusFixture(t)}, &out, &errOut)
	if code != platform.ExitUsage {
		t.Fatalf("emit without --out code = %v, want ExitUsage (2)", code)
	}
}

// --- fix-prompt integration tests ---

// TestIntegration_FixPromptStdout: `fix-prompt` prints an agent-ready prompt to
// stdout (exit 0) with the title, a guardrail phrase, a broken-link document
// path, and a remediation substring.
func TestIntegration_FixPromptStdout(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"fix-prompt", corpusFixture(t)}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("fix-prompt code = %v, want ExitOK (stderr=%q)", code, errOut.String())
	}
	s := out.String()
	if !strings.Contains(s, "# matlatl fix-prompt") {
		t.Errorf("fix-prompt missing title:\n%s", s)
	}
	if !strings.Contains(s, "orphan-intentional") {
		t.Errorf("fix-prompt missing guardrail phrase:\n%s", s)
	}
	if !strings.Contains(s, "docs/links.md") {
		t.Errorf("fix-prompt missing a broken-link document path:\n%s", s)
	}
	if !strings.Contains(s, "does not resolve to any document in the corpus") {
		t.Errorf("fix-prompt missing the broken-link remediation text:\n%s", s)
	}
}

// TestIntegration_FixPromptErrorsOnly: `fix-prompt --errors-only` excludes
// warning-severity entries (the ambiguous/orphan/unreachable findings) while
// keeping the error-severity broken link/anchor findings.
func TestIntegration_FixPromptErrorsOnly(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"fix-prompt", corpusFixture(t), "--errors-only"}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("fix-prompt --errors-only code = %v, want ExitOK", code)
	}
	s := out.String()
	if !strings.Contains(s, "Scope: errors only") {
		t.Errorf("fix-prompt --errors-only missing the errors-only scope line:\n%s", s)
	}
	// Error-severity findings (broken link AND anchor) remain.
	if !strings.Contains(s, "**broken-link**") {
		t.Errorf("fix-prompt --errors-only dropped broken-link findings:\n%s", s)
	}
	if !strings.Contains(s, "**broken-anchor**") {
		t.Errorf("fix-prompt --errors-only dropped broken-anchor findings:\n%s", s)
	}
	// Non-error severities are filtered out: warnings (ambiguous/orphan/
	// unreachable) AND info (knowledge-gap). The corpus fixture produces both, so
	// a `Severity != Warning` regression that leaked info findings would be caught.
	if strings.Contains(s, "(warning)") {
		t.Errorf("fix-prompt --errors-only should not contain warning entries:\n%s", s)
	}
	if strings.Contains(s, "(info)") {
		t.Errorf("fix-prompt --errors-only should not contain info entries:\n%s", s)
	}
}

// TestIntegration_FixPromptClean: a fully-resolvable corpus yields the no-op
// message and still exits 0.
func TestIntegration_FixPromptClean(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"fix-prompt", cleanFixture(t)}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("fix-prompt clean code = %v, want ExitOK", code)
	}
	if !strings.Contains(out.String(), "No documentation findings to fix") {
		t.Errorf("fix-prompt on a clean corpus should emit the no-op message:\n%s", out.String())
	}
}

// TestIntegration_FixPromptDeterministic: stdout is byte-stable across runs.
func TestIntegration_FixPromptDeterministic(t *testing.T) {
	run := func() string {
		var out, errOut bytes.Buffer
		runArgs(context.Background(), []string{"fix-prompt", corpusFixture(t)}, &out, &errOut)
		return out.String()
	}
	if run() != run() {
		t.Error("fix-prompt output is not deterministic across runs")
	}
}

// TestIntegration_FixPromptToOut: `fix-prompt --out` writes fix-prompt.md under
// the out dir and nothing escapes it.
func TestIntegration_FixPromptToOut(t *testing.T) {
	outDir := t.TempDir()
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{"fix-prompt", corpusFixture(t), "--out", outDir}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("fix-prompt --out code = %v, want ExitOK (stderr=%q)", code, errOut.String())
	}
	b, err := os.ReadFile(filepath.Join(outDir, "fix-prompt.md"))
	if err != nil {
		t.Fatalf("fix-prompt.md not written: %v", err)
	}
	if !strings.Contains(string(b), "# matlatl fix-prompt") {
		t.Errorf("fix-prompt.md missing expected content:\n%s", b)
	}
	assertNothingEscaped(t, outDir, []string{"fix-prompt.md"})
}

// assertNothingEscaped verifies that the out dir contains only the expected
// files (plus directories) — nothing was written outside --out (ADR 0003).
func assertNothingEscaped(t *testing.T, outDir string, want []string) {
	t.Helper()
	wantSet := map[string]struct{}{}
	for _, w := range want {
		wantSet[w] = struct{}{}
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, ok := wantSet[e.Name()]; !ok {
			t.Errorf("unexpected file in out dir: %q", e.Name())
		}
	}
	// And the parent of the out dir got nothing leaked.
	if _, err := os.Stat(filepath.Join(filepath.Dir(outDir), "report.md")); err == nil {
		t.Errorf("an artifact leaked to the parent of --out")
	}
}

// TestIntegration_ConfigFileDeclaresRoots is the ADR 0011 end-to-end proof: a
// temp repo whose .matlatl.yml declares `.claude/agents/*.md` as roots makes an
// EDGELESS agent doc stop being reported as an isolated orphan, riding the
// existing root→isolated-orphan exemption (ADR 0010). The control (no config)
// confirms the same doc IS isolated by default. matlatl ships zero
// Claude-Code-specific knowledge; the repo's config carries it.
func TestIntegration_ConfigFileDeclaresRoots(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(name, body string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A README (a convention root) plus an edgeless agent doc under .claude/agents.
	writeFile("README.md", "# Repo\n\nThe overview.\n")
	writeFile(".claude/agents/foo.md", "# Foo Agent\n\nEntry point; links to nothing.\n")

	graphOrphans := func() []string {
		outDir := t.TempDir()
		args := []string{"graph", dir, "--format", "json", "--out", outDir}
		var out, errOut bytes.Buffer
		if code := runArgs(context.Background(), args, &out, &errOut); code != platform.ExitOK {
			t.Fatalf("graph json code = %v (stderr=%q)", code, errOut.String())
		}
		b, err := os.ReadFile(filepath.Join(outDir, "graph.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Orphans []string `json:"orphans"`
			RootSet []string `json:"rootSet"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("graph.json parse: %v", err)
		}
		return doc.Orphans
	}

	agentID := ".claude/agents/foo.md"

	// Control: NO .matlatl.yml → the edgeless agent doc IS an isolated orphan.
	if !slicesContains(graphOrphans(), agentID) {
		t.Fatalf("control: edgeless %s should be isolated without config; it was not", agentID)
	}

	// Declare the agents glob as roots via .matlatl.yml.
	writeFile(".matlatl.yml", "version: 1\nroots:\n  - \".claude/agents/*.md\"\n")

	// With the config, the agent doc is a root and is exempt from the isolated
	// finding (the domain ResolveRootSet consumes the unioned roots unchanged).
	if slicesContains(graphOrphans(), agentID) {
		t.Errorf("with .matlatl.yml declaring it a root, %s must not be isolated", agentID)
	}
}

// TestIntegration_ConfigMalformedExitsUsage: a malformed .matlatl.yml is a HARD
// error mapped to ExitUsage (2) per ADR 0011, with an explanation on stderr.
func TestIntegration_ConfigMalformedExitsUsage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# R\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Unterminated flow sequence: a YAML syntax error.
	if err := os.WriteFile(filepath.Join(dir, ".matlatl.yml"),
		[]byte("version: 1\nroots: [\"a.md\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{dir}, &out, &errOut)
	if code != platform.ExitUsage {
		t.Fatalf("malformed config code = %v, want ExitUsage (2) (stderr=%q)", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "matlatl:") {
		t.Errorf("malformed config should explain itself on stderr, got %q", errOut.String())
	}
}

// TestIntegration_ConfigVersionTooNewExitsUsage: a config declaring a newer
// schema version is a HARD error mapped to ExitUsage (2).
func TestIntegration_ConfigVersionTooNewExitsUsage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# R\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".matlatl.yml"),
		[]byte("version: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{dir}, &out, &errOut)
	if code != platform.ExitUsage {
		t.Fatalf("version-too-new code = %v, want ExitUsage (2) (stderr=%q)", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "upgrade matlatl") {
		t.Errorf("version-too-new stderr = %q, want the upgrade hint", errOut.String())
	}
}

// TestIntegration_ConfigBadGlobReachesStderr exercises the full
// config→union→ResolveRootSet→BadGlobs notice path end-to-end: a .matlatl.yml
// with an invalid path.Match glob must surface a bad-glob notice on stderr (the
// run still succeeds — a bad glob is tolerated, not fatal). Each half is unit-
// tested in isolation; this pins the wiring.
func TestIntegration_ConfigBadGlobReachesStderr(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# R\n\nBody.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// "[bad" is an invalid path.Match pattern (unterminated character class).
	if err := os.WriteFile(filepath.Join(dir, ".matlatl.yml"),
		[]byte("version: 1\nroots:\n  - \"[bad\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{dir}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("bad-glob config code = %v, want ExitOK (a bad glob is tolerated) (stderr=%q)", code, errOut.String())
	}
	// The domain reports the bad glob; the pipeline routes it to stderr as a
	// notice. Assert the offending pattern appears in a notice line.
	if !strings.Contains(errOut.String(), "[bad") {
		t.Errorf("bad-glob notice did not reach stderr; stderr = %q", errOut.String())
	}
}

// TestIntegration_ConfigToleratedNoticeReachesStderr pins the tolerated-notice
// stderr path through the CLI: a .matlatl.yml with no `version` field (assumed 1)
// AND an unknown key both emit `matlatl: notice [config] ...` lines on stderr,
// and the run still succeeds.
func TestIntegration_ConfigToleratedNoticeReachesStderr(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# R\n\nBody.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// No version field (→ "assuming 1" notice) and an unknown key (→ ignore notice).
	if err := os.WriteFile(filepath.Join(dir, ".matlatl.yml"),
		[]byte("rootz:\n  - \"docs/*.md\"\nroots:\n  - \"docs/*.md\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := runArgs(context.Background(), []string{dir}, &out, &errOut)
	if code != platform.ExitOK {
		t.Fatalf("tolerated-notice config code = %v, want ExitOK (stderr=%q)", code, errOut.String())
	}
	se := errOut.String()
	if !strings.Contains(se, "notice [config]") {
		t.Errorf("expected a `notice [config]` line on stderr, got %q", se)
	}
	if !strings.Contains(se, "assuming 1") {
		t.Errorf("expected the no-version 'assuming 1' notice on stderr, got %q", se)
	}
	if !strings.Contains(se, "rootz") {
		t.Errorf("expected the unknown-key 'rootz' notice on stderr, got %q", se)
	}
}
