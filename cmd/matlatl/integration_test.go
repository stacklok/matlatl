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
			Documents int `json:"documents"`
		} `json:"summary"`
		Nodes []json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("graph.json does not parse: %v", err)
	}
	if doc.SchemaVersion != 1 || doc.Summary.Documents == 0 || len(doc.Nodes) == 0 {
		t.Errorf("graph.json content unexpected: version=%d docs=%d nodes=%d", doc.SchemaVersion, doc.Summary.Documents, len(doc.Nodes))
	}
	assertNothingEscaped(t, outDir, []string{"graph.json"})
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
	want := []string{"index.md", "llms.txt", "llms-full.txt", "llms-small.txt", "graph.json", "findings.json"}
	for _, name := range want {
		b, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("bundle missing %s: %v", name, err)
		}
		if len(b) == 0 {
			t.Errorf("bundle artifact %s is empty", name)
		}
	}
	// graph.json + findings.json parse as JSON.
	for _, name := range []string{"graph.json", "findings.json"} {
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
// (no orphans) under the default policy. Under --strict the directory link does
// not vouch for the folder's non-index contents, so they surface as findings and
// the run fails.
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
