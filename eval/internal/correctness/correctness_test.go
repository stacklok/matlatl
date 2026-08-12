package correctness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
)

func evalRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func loadedSuite(t *testing.T) (*suiteFiles, string) {
	t.Helper()
	root := evalRoot(t)
	dir, err := oracleDir(root)
	if err != nil {
		t.Fatal(err)
	}
	suite, err := loadSuite(t.Context(), root, dir)
	if err != nil {
		t.Fatal(err)
	}
	return suite, root
}

func TestCorrectnessOracle(t *testing.T) {
	root := evalRoot(t)
	fixtures := filepath.Join(root, "fixtures", "correctness-mutations", "v1")
	before, err := evalfs.TreeHash(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	counts, err := Run(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := evalfs.TreeHash(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("correctness run mutated checked-in fixtures")
	}
	if counts.Graph != 12 || counts.Resolver != 24 || counts.Pipeline != 22 || counts.Scent != 10 || counts.Gaps != 4 || counts.Suggestions != 7 || counts.Mutations != 8 || counts.Backlinks != 4 || counts.Trails != 4 || counts.Determinism != 1 {
		t.Fatalf("counts = %+v", counts)
	}
}
func TestPerturbedResolverExpectationFails(t *testing.T) {
	root := evalRoot(t)
	source, err := os.ReadFile(filepath.Join(root, "oracles", "correctness", "v1", "resolver.json"))
	if err != nil {
		t.Fatal(err)
	}
	perturbed := strings.Replace(string(source), `"health":"valid","kind":"document","document":"README.md"`, `"health":"broken","kind":"document","document":"README.md"`, 1)
	file := filepath.Join(t.TempDir(), "resolver.json")
	if err := os.WriteFile(file, []byte(perturbed), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := load[resolverFile](file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runResolver(t.Context(), loaded); err == nil || !strings.Contains(err.Error(), "direct-resolver/") {
		t.Fatalf("perturbed expectation error = %v", err)
	}
}
func TestPerturbedPipelineExpectationFails(t *testing.T) {
	root := evalRoot(t)
	oraclePath := filepath.Join(root, "oracles", "correctness", "v1", "pipeline.json")
	source, err := os.ReadFile(oraclePath)
	if err != nil {
		t.Fatal(err)
	}
	perturbed := strings.Replace(string(source), `"id":"relative-target-from-deep-origin","source":{"path":"docs/deep/source.md","line":9},"target":"../guide.md","fragment":"","type":"relative-link","anchorText":"relative guide","want":{"health":"valid"`, `"id":"relative-target-from-deep-origin","source":{"path":"docs/deep/source.md","line":9},"target":"../guide.md","fragment":"","type":"relative-link","anchorText":"relative guide","want":{"health":"broken"`, 1)
	if perturbed == string(source) {
		t.Fatal("test perturbation did not match oracle")
	}
	file := filepath.Join(t.TempDir(), "pipeline.json")
	if err := os.WriteFile(file, []byte(perturbed), 0o600); err != nil {
		t.Fatal(err)
	}
	suite, _ := loadedSuite(t)
	loaded, err := load[pipelineFile](file)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runPipeline(t.Context(), loaded, suite.fixtures[suite.pipeline.Fixture]); err == nil || !strings.Contains(err.Error(), "pipeline-resolver/relative-target-from-deep-origin") {
		t.Fatalf("perturbed expectation error = %v", err)
	}
}

func TestPerturbedMechanismExpectationsFail(t *testing.T) {
	suite, root := loadedSuite(t)
	tests := []struct {
		name, filename, old, replacement string
		run                              func(string) error
	}{
		{"scent", "scent.json", `"score":0.16666666666666666`, `"score":0.5`, func(path string) error {
			file, err := load[scentFile](path)
			if err != nil {
				return err
			}
			_, err = runScent(t.Context(), file)
			return err
		}},
		{"gap", "gaps.json", `"want":[["A.md","C.md"]]`, `"want":[]`, func(path string) error {
			file, err := load[gapFile](path)
			if err != nil {
				return err
			}
			_, err = runGaps(t.Context(), file)
			return err
		}},
		{"suggestion", "suggestions.json", `"shared":2,"coupling":0`, `"shared":9,"coupling":0`, func(path string) error {
			file, err := load[suggestionFile](path)
			if err != nil {
				return err
			}
			_, err = runSuggestions(t.Context(), file)
			return err
		}},
		{"mutation-file-hash", "mutations.json", `"baseHash":"4ffe1c6aa8b937f927d46082f51fdc02b409f5ab4cf807923d23094e78ef25cc"`, `"baseHash":"0000000000000000000000000000000000000000000000000000000000000000"`, func(path string) error {
			file, err := load[mutationFile](path)
			if err != nil {
				return err
			}
			_, err = runMutations(t.Context(), file, suite.fixtures)
			return err
		}},
		{"mutation-fixture-hash", "mutations.json", `"fixtureHash":"2e1552d215d4b2896c421b0ed24322d02302da264ebfeb8490a11c1528ded752"`, `"fixtureHash":"0000000000000000000000000000000000000000000000000000000000000000"`, func(path string) error {
			file, err := load[mutationFile](path)
			if err != nil {
				return err
			}
			_, err = runMutations(t.Context(), file, suite.fixtures)
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join(root, "oracles", "correctness", "v1", tc.filename))
			if err != nil {
				t.Fatal(err)
			}
			perturbed := strings.Replace(string(source), tc.old, tc.replacement, 1)
			if perturbed == string(source) {
				t.Fatal("perturbation did not match")
			}
			path := filepath.Join(t.TempDir(), tc.filename)
			if err := os.WriteFile(path, []byte(perturbed), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := tc.run(path); err == nil {
				t.Fatal("perturbed oracle passed")
			}
		})
	}
}

func TestPerturbedSurfaceExpectationsFail(t *testing.T) {
	suite, root := loadedSuite(t)
	tests := []struct {
		name, filename, old, replacement string
		run                              func(string) error
	}{
		{"backlink", "backlinks.json", `"backlinks":["README.md","docs/a.md","docs/z.md"]`, `"backlinks":["README.md","docs/z.md"]`, func(path string) error {
			file, err := load[backlinkFile](path)
			if err != nil {
				return err
			}
			_, err = runBacklinks(t.Context(), file, suite.fixtures[suite.backlinks.Fixture])
			return err
		}},
		{"trail", "trails.json", `"root":"D.md","order":["A.md","B.md","C.md","D.md"]`, `"root":"D.md","order":["A.md","C.md","B.md","D.md"]`, func(path string) error {
			file, err := load[trailFile](path)
			if err != nil {
				return err
			}
			_, err = runTrails(t.Context(), file)
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join(root, "oracles", "correctness", "v1", tc.filename))
			if err != nil {
				t.Fatal(err)
			}
			perturbed := strings.Replace(string(source), tc.old, tc.replacement, 1)
			if perturbed == string(source) {
				t.Fatal("perturbation did not match")
			}
			path := filepath.Join(t.TempDir(), tc.filename)
			if err := os.WriteFile(path, []byte(perturbed), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := tc.run(path); err == nil {
				t.Fatal("perturbed surface oracle passed")
			}
		})
	}
}

func TestSuggestedLinkOrderAndScoresSurvivePermutedInsertion(t *testing.T) {
	path := filepath.Join(evalRoot(t), "oracles", "correctness", "v1", "suggestions.json")
	file, err := load[suggestionFile](path)
	if err != nil {
		t.Fatal(err)
	}
	semantics := suggestionSemantics{file.DefaultMinShared, file.DefaultMaxFanout, file.MaxResults}
	for _, tc := range file.Cases {
		if err := checkSuggestionCase(t.Context(), tc, file.NumericTolerance, semantics); err != nil {
			t.Fatalf("%s: %v", tc.ID, err)
		}
	}
}

func TestSuggestedLinkFullUniversePerturbationsFail(t *testing.T) {
	path := filepath.Join(evalRoot(t), "oracles", "correctness", "v1", "suggestions.json")
	tests := []struct {
		name   string
		mutate func(*suggestionFile)
	}{
		{"omission", func(file *suggestionFile) { file.Cases[1].Want = file.Cases[1].Want[:len(file.Cases[1].Want)-1] }},
		{"extra-candidate", func(file *suggestionFile) {
			file.Cases[4].Want = append(file.Cases[4].Want, suggestionWant{A: "A.md", B: "C.md", Shared: 1, AdamicAdar: 1})
		}},
		{"flag-truncated", func(file *suggestionFile) { file.Cases[3].Truncated = false }},
		{"flag-hubs-skipped", func(file *suggestionFile) { file.Cases[3].HubsSkipped = false }},
		{"boundary", func(file *suggestionFile) { file.Cases[5].MinShared = 3 }},
		{"tie-order", func(file *suggestionFile) {
			file.Cases[2].Want[0], file.Cases[2].Want[1] = file.Cases[2].Want[1], file.Cases[2].Want[0]
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file, err := load[suggestionFile](path)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(file)
			body, err := json.Marshal(file)
			if err != nil {
				t.Fatal(err)
			}
			perturbed := filepath.Join(t.TempDir(), "suggestions.json")
			if err := os.WriteFile(perturbed, body, 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := load[suggestionFile](perturbed)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runSuggestions(t.Context(), loaded); err == nil {
				t.Fatal("perturbed full-universe expectation passed")
			}
		})
	}
}

func TestIndependentSuggestedLinkResultCap(t *testing.T) {
	graph := mechanismGraph{Documents: []string{"H1.md", "H2.md"}}
	for i := 0; i < 46; i++ {
		leaf := fmt.Sprintf("leaf%02d.md", i)
		graph.Documents = append(graph.Documents, leaf)
		graph.Edges = append(graph.Edges, [2]string{leaf, "H1.md"}, [2]string{leaf, "H2.md"})
	}
	slices.Sort(graph.Documents)
	slices.SortFunc(graph.Edges, func(a, b [2]string) int {
		if a[0] < b[0] || a[0] == b[0] && a[1] < b[1] {
			return -1
		}
		if a == b {
			return 0
		}
		return 1
	})
	result, err := deriveSuggestions(t.Context(), suggestionCase{Graph: graph}, suggestionSemantics{2, 256, 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.suggestions) != 1000 || !result.truncated || result.hubsSkipped {
		t.Fatalf("independent cap result = %d/%v/%v", len(result.suggestions), result.truncated, result.hubsSkipped)
	}
}

func TestMutationObservationPerturbationsFail(t *testing.T) {
	suite, _ := loadedSuite(t)
	path := filepath.Join(evalRoot(t), "oracles", "correctness", "v1", "mutations.json")
	dimensions := []struct {
		name      string
		caseIndex int
		field     func(*mutationObservation) *[]string
		added     string
	}{
		{"edges", 4, func(o *mutationObservation) *[]string { return &o.Edges }, "zz.md->zzz.md"},
		{"findings", 4, func(o *mutationObservation) *[]string { return &o.Findings }, "zz-kind|zz.md|0"},
		{"wcc", 4, func(o *mutationObservation) *[]string { return &o.WCC }, "zz.md"},
		{"far-from-root", 3, func(o *mutationObservation) *[]string { return &o.FarFromRoot }, "zz.md"},
		{"articulations", 6, func(o *mutationObservation) *[]string { return &o.Articulations }, "zz.md"},
		{"bridges", 4, func(o *mutationObservation) *[]string { return &o.Bridges }, "zz.md|zzz.md"},
	}
	operations := []struct {
		name   string
		mutate func(*[]string, string)
	}{
		{"delete", func(v *[]string, _ string) { *v = (*v)[:len(*v)-1] }},
		{"add", func(v *[]string, added string) { *v = append(*v, added) }},
		{"change", func(v *[]string, added string) { (*v)[len(*v)-1] = added }},
		{"null", func(v *[]string, _ string) { *v = nil }},
	}
	phases := []struct {
		name string
		get  func(*mutationCase) *mutationObservation
	}{
		{"base", func(tc *mutationCase) *mutationObservation { return &tc.Base }},
		{"mutated", func(tc *mutationCase) *mutationObservation { return &tc.Mutated }},
	}
	for _, dimension := range dimensions {
		for _, phase := range phases {
			for _, operation := range operations {
				t.Run(dimension.name+"/"+phase.name+"/"+operation.name, func(t *testing.T) {
					file, err := load[mutationFile](path)
					if err != nil {
						t.Fatal(err)
					}
					values := dimension.field(phase.get(&file.Cases[dimension.caseIndex]))
					if len(*values) == 0 && operation.name != "add" && operation.name != "null" {
						t.Fatal("test selected an empty observation")
					}
					operation.mutate(values, dimension.added)
					if _, err := runMutations(t.Context(), file, suite.fixtures); err == nil {
						t.Fatal("perturbed mutation observation passed")
					}
				})
			}
		}
	}
}

func TestMutationReplacementSecurity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "doc.md"), []byte("one one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := evalfs.FileHash(root, "doc.md")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path, old, hash string
	}{
		{"unsafe path", "../doc.md", "one", hash},
		{"non-unique", "doc.md", "one", hash},
		{"absent", "doc.md", "missing", hash},
		{"hash mismatch", "doc.md", "one one", strings.Repeat("0", 64)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := replaceExactlyOnce(root, tc.path, tc.old, "replacement", tc.hash); err == nil {
				t.Fatal("unsafe replacement accepted")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		if err := os.Symlink("doc.md", filepath.Join(root, "link.md")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := replaceExactlyOnce(root, "link.md", "one", "replacement", hash); err == nil {
			t.Fatal("symlink replacement accepted")
		}
	})
}

func TestPipelineResultsAreByteAndOrderDeterministic(t *testing.T) {
	suite, _ := loadedSuite(t)
	countA, snapshotA, err := runPipeline(t.Context(), suite.pipeline, suite.fixtures[suite.pipeline.Fixture])
	if err != nil {
		t.Fatal(err)
	}
	countB, snapshotB, err := runPipeline(t.Context(), suite.pipeline, suite.fixtures[suite.pipeline.Fixture])
	if err != nil {
		t.Fatal(err)
	}
	if countA != 22 || countB != countA || !bytes.Equal(snapshotA, snapshotB) {
		t.Fatalf("pipeline snapshots differ: counts=%d/%d equal=%v", countA, countB, bytes.Equal(snapshotA, snapshotB))
	}
	var observations []pipelineObservation
	if err := json.Unmarshal(snapshotA, &observations); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(observations); i++ {
		if observations[i-1].ID >= observations[i].ID {
			t.Fatalf("pipeline result order is unstable at %q, %q", observations[i-1].ID, observations[i].ID)
		}
	}
}

func TestPipelineArtifactsRemainStableAcrossCreationOrdersAndRepeatedRuns(t *testing.T) {
	suite, _ := loadedSuite(t)
	for run := 0; run < 2; run++ {
		count, err := runDeterminism(t.Context(), suite.determinism, suite.fixtures[suite.determinism.Fixture])
		if err != nil {
			t.Fatalf("repeat %d: %v", run+1, err)
		}
		if count != 1 {
			t.Fatalf("repeat %d count = %d", run+1, count)
		}
	}
}

func TestDeterminismRejectsArtifactKeyPerturbations(t *testing.T) {
	suite, _ := loadedSuite(t)
	file := suite.determinism

	t.Run("renamed-manifest-key", func(t *testing.T) {
		original := file.Artifacts[0]
		file.Artifacts[0] = "renamed.json"
		t.Cleanup(func() { file.Artifacts[0] = original })
		if _, err := runDeterminism(t.Context(), file, suite.fixtures[file.Fixture]); err == nil {
			t.Fatal("renamed manifest artifact key passed")
		}
	})

	for _, tc := range []struct {
		name      string
		artifacts map[string][]byte
	}{
		{"missing", map[string][]byte{"findings.json": {}, "graph.json": {}, "index.md": {}, "llms.txt": {}}},
		{"extra", map[string][]byte{"findings.json": {}, "graph.json": {}, "index.md": {}, "llms.txt": {}, "trails.json": {}, "extra.json": {}}},
		{"renamed", map[string][]byte{"renamed.json": {}, "graph.json": {}, "index.md": {}, "llms.txt": {}, "trails.json": {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateDeterminismArtifacts(file, tc.artifacts); err == nil {
				t.Fatal("perturbed artifact keys passed")
			}
		})
	}
}

func TestRunHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := Run(ctx, evalRoot(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context canceled", err)
	}
}

func TestSuiteBudgetRejectsAggregateCaseCount(t *testing.T) {
	root := evalRoot(t)
	dir, err := oracleDir(root)
	if err != nil {
		t.Fatal(err)
	}
	suite, err := loadSuite(t.Context(), root, dir)
	if err != nil {
		t.Fatal(err)
	}
	suite.resolver.Cases = make([]resolverCase, maxSuiteCases+1)

	if err := validateSuiteBudget(t.Context(), suite); err == nil || !strings.Contains(err.Error(), "exceeds cumulative budget") {
		t.Fatalf("suite budget error = %v", err)
	}
}

func TestSuiteBudgetRejectsAggregateGraphWork(t *testing.T) {
	root := evalRoot(t)
	dir, err := oracleDir(root)
	if err != nil {
		t.Fatal(err)
	}
	suite, err := loadSuite(t.Context(), root, dir)
	if err != nil {
		t.Fatal(err)
	}
	suite.graph.Cases = append(suite.graph.Cases, graphCase{Documents: make([]string, maxItems)})

	if err := validateSuiteBudget(t.Context(), suite); err == nil || !strings.Contains(err.Error(), "exceeds cumulative budget") {
		t.Fatalf("suite budget error = %v", err)
	}
}

func TestLoadedSuiteIsolatedFromManifestAndFixtureReplacement(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(evalRoot(t))); err != nil {
		t.Fatal(err)
	}
	dir, err := oracleDir(root)
	if err != nil {
		t.Fatal(err)
	}
	suite, err := loadSuite(t.Context(), root, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"graph.json", "resolver.json", "pipeline.json", "scent.json", "gaps.json", "suggestions.json", "mutations.json", "backlinks.json", "trails.json", "determinism.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for rel, snapshot := range suite.fixtures {
		for _, entry := range snapshot.entries {
			name := filepath.Join(root, filepath.FromSlash(rel), filepath.FromSlash(entry.path))
			if err := os.WriteFile(name, []byte("replaced after preflight\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := validateSuiteBudget(t.Context(), suite); err != nil {
		t.Fatal(err)
	}
	counts, err := runSuite(t.Context(), suite)
	if err != nil {
		t.Fatal(err)
	}
	if counts != (Counts{Graph: 12, Resolver: 24, Pipeline: 22, Scent: 10, Gaps: 4, Suggestions: 7, Mutations: 8, Backlinks: 4, Trails: 4, Determinism: 1}) {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestLoadSuiteRejectsAggregateFixtureFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(evalRoot(t))); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "fixtures", "correctness-resolver", "v1")
	for i := 0; i <= maxSuiteFixtureFiles; i++ {
		name := filepath.Join(fixture, fmt.Sprintf("budget-%03d.md", i))
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := oracleDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadSuite(t.Context(), root, dir); err == nil || !strings.Contains(err.Error(), "cumulative fixture file count") {
		t.Fatalf("suite fixture-file budget error = %v", err)
	}
}

func TestLoadSuiteRejectsAggregateFixtureBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(evalRoot(t))); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "fixtures", "correctness-resolver", "v1")
	content := make([]byte, evalfs.MaxFileBytes)
	for i := 0; i <= maxSuiteFixtureBytes/evalfs.MaxFileBytes; i++ {
		name := filepath.Join(fixture, fmt.Sprintf("budget-%02d.md", i))
		if err := os.WriteFile(name, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := oracleDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadSuite(t.Context(), root, dir); err == nil || !strings.Contains(err.Error(), "cumulative fixture bytes") {
		t.Fatalf("suite fixture-byte budget error = %v", err)
	}
}

func TestLoaderRejectsInvalidJSONContracts(t *testing.T) {
	tests := map[string]string{
		"unknown":   `{"schemaVersion":1,"family":"direct-resolver","catalog":{"documents":[],"headings":{},"aliases":{},"assets":[]},"cases":[],"extra":true}`,
		"duplicate": `{"schemaVersion":1,"schemaVersion":1}`,
		"trailing":  `{"schemaVersion":1} {}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "x.json")
			if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := load[resolverFile](file); err == nil {
				t.Fatal("invalid JSON accepted")
			}
		})
	}
}
func TestValidationRejectsResolverBoundsAndEnums(t *testing.T) {
	valid := func() *resolverFile {
		return &resolverFile{
			SchemaVersion: 1,
			Family:        "direct-resolver",
			Catalog: resolverCatalog{
				Documents: []string{"README.md"},
				Headings:  map[string][]string{},
				Aliases:   map[string][]string{},
				Assets:    []string{},
			},
			Cases: []resolverCase{{
				ID: "case", Origin: "README.md", Type: "unknown",
				Want: resolverWant{Health: "unresolved", Kind: "none", AssetCalls: []string{}},
			}},
		}
	}
	tests := map[string]func(*resolverFile){
		"health enum": func(file *resolverFile) { file.Cases[0].Want.Health = "maybe" },
		"kind enum":   func(file *resolverFile) { file.Cases[0].Want.Kind = "maybe" },
		"string":      func(file *resolverFile) { file.Cases[0].Target = strings.Repeat("x", maxStringBytes+1) },
		"items": func(file *resolverFile) {
			file.Cases[0].Want.Children = make([]string, maxItems+1)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			file := valid()
			mutate(file)
			if err := validateResolverFile(file); err == nil {
				t.Fatal("invalid resolver contract accepted")
			}
		})
	}
}

func TestLoaderRejectsOversizedFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "large.json")
	if err := os.WriteFile(file, []byte(strings.Repeat(" ", maxOracleBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := load[resolverFile](file); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized oracle error = %v", err)
	}
}

func TestValidationRejectsUnsafeAndUnstableCases(t *testing.T) {
	base := &graphFile{SchemaVersion: 1, Family: "canonical-graph", NumericTolerance: 1e-6, FarFromRootThreshold: 2, Cases: []graphCase{{ID: "b", Documents: []string{"A.md"}}, {ID: "a", Documents: []string{"../A.md"}}}}
	if err := validateGraphFile(base); err == nil {
		t.Fatal("unsafe/unsorted graph contract accepted")
	}
}
