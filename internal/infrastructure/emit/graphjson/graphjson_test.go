package graphjson_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
)

// corpusID is a small helper to build a DocumentID for the synthetic-corpus tests.
func corpusID(s string) identity.DocumentID { return identity.DocumentID(s) }

// buildCorpusView runs the real pipeline over testdata/corpus and returns the
// render-ready View.
func buildCorpusView(t *testing.T) emit.View {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := application.DefaultConfig()
	cfg.RootPath = root
	scanner := fsscanner.New(fsscanner.Config{})
	parserFac := mdparser.NewFactory(mdparser.Config{})
	pipe := application.NewPipeline(cfg, scanner, parserFac, nil)
	_, res, err := pipe.Run(context.Background())
	if err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	return emit.BuildView(res)
}

// TestJSON_RoundTrip proves the emitted bytes unmarshal back into the typed
// Document (the structural round-trip contract) and re-marshal identically.
func TestJSON_RoundTrip(t *testing.T) {
	v := buildCorpusView(t)
	b, err := graphjson.JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	var doc graphjson.Document
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("graph.json does not round-trip into the typed Document: %v", err)
	}
	if doc.SchemaVersion != graphjson.SchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", doc.SchemaVersion, graphjson.SchemaVersion)
	}
	if doc.Tool != "matlatl" {
		t.Errorf("tool = %q, want matlatl", doc.Tool)
	}
	// Re-marshal the typed struct and compare to the original (fixed float
	// precision means the re-emit is identical).
	b2, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b2 = append(b2, '\n')
	if !bytes.Equal(b, b2) {
		t.Errorf("re-marshal of round-tripped Document differs from original")
	}
}

// TestJSON_ByteStable runs the emitter twice over two independent pipeline runs
// and asserts identical bytes — including float formatting (the P3 concern).
func TestJSON_ByteStable(t *testing.T) {
	b1, err := graphjson.JSON(buildCorpusView(t))
	if err != nil {
		t.Fatal(err)
	}
	b2, err := graphjson.JSON(buildCorpusView(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Error("graph.json is not byte-stable across two pipeline runs")
	}
}

// TestJSON_FloatFixedPrecision asserts every HITS score renders at exactly the
// fixed precision (six decimal places), so output cannot drift across runs.
func TestJSON_FloatFixedPrecision(t *testing.T) {
	b, err := graphjson.JSON(buildCorpusView(t))
	if err != nil {
		t.Fatal(err)
	}
	// Every node hubScore/authorityScore and every hits score is a number with
	// exactly 6 fractional digits in the raw text.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	var nodes []map[string]json.RawMessage
	if err := json.Unmarshal(raw["nodes"], &nodes); err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		for _, k := range []string{"hubScore", "authorityScore"} {
			s := string(n[k])
			if !hasNFractionalDigits(s, graphjson.HITSFloatPrecision) {
				t.Errorf("node %s = %s, want %d fractional digits", k, s, graphjson.HITSFloatPrecision)
			}
		}
	}

	// The hits.topHubs / topAuthorities scores must render at the same fixed
	// precision (they marshal through the same Float type, but the contract is
	// asserted explicitly so a future refactor of that path is caught).
	var hits map[string]json.RawMessage
	if err := json.Unmarshal(raw["hits"], &hits); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"topHubs", "topAuthorities"} {
		var ranked []map[string]json.RawMessage
		if err := json.Unmarshal(hits[group], &ranked); err != nil {
			t.Fatalf("unmarshal hits.%s: %v", group, err)
		}
		if len(ranked) == 0 {
			t.Errorf("hits.%s is empty; expected ranked entries in the fixture", group)
		}
		for _, r := range ranked {
			s := string(r["score"])
			if !hasNFractionalDigits(s, graphjson.HITSFloatPrecision) {
				t.Errorf("hits.%s score = %s, want %d fractional digits", group, s, graphjson.HITSFloatPrecision)
			}
		}
	}
}

// TestJSON_Navigability proves the summary.navigability block is emitted, the
// schemaVersion is 4, the floats render at the fixed precision (byte-stable), and
// the values round-trip into the typed struct. The directed path A->B->C->D has
// hand-computed compactness 14/36 and diameter 3 (see navigability_test.go).
func TestJSON_Navigability(t *testing.T) {
	c := corpus.NewCorpus()
	for _, id := range []string{"a.md", "b.md", "c.md", "d.md"} {
		d := &corpus.Document{
			ID:   corpusID(id),
			Root: &corpus.Section{Level: 0, Children: []*corpus.Section{{Level: 1, Text: "T", Slug: "t", StartLine: 1, EndLine: 2}}},
		}
		if err := c.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	// Build the directed path projection a->b->c->d directly via the graph.
	g := graphmodel.BuildReferenceGraph(c, []reference.Reference{
		pathRef("a.md", "b.md"), pathRef("b.md", "c.md"), pathRef("c.md", "d.md"),
	}, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	v := emit.BuildView(application.Result{DocumentCount: c.Len(), Metrics: m, Corpus: c})

	if v.Metrics.Navigability.Documents != 4 {
		t.Fatalf("expected 4 docs in navigability, got %d", v.Metrics.Navigability.Documents)
	}

	b, err := graphjson.JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	if doc := graphjson.Build(v); doc.SchemaVersion != 6 {
		t.Errorf("schemaVersion = %d, want 6", doc.SchemaVersion)
	}

	// The navigability floats must render at the fixed precision (determinism).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	var summary map[string]json.RawMessage
	if err := json.Unmarshal(raw["summary"], &summary); err != nil {
		t.Fatal(err)
	}
	var nav map[string]json.RawMessage
	if err := json.Unmarshal(summary["navigability"], &nav); err != nil {
		t.Fatalf("summary.navigability missing/invalid: %v", err)
	}
	for _, k := range []string{"compactness", "stratum", "characteristicPathLength", "medianPathLength", "clusteringCoefficient"} {
		if !hasNFractionalDigits(string(nav[k]), graphjson.HITSFloatPrecision) {
			t.Errorf("navigability.%s = %s, want %d fractional digits", k, nav[k], graphjson.HITSFloatPrecision)
		}
	}

	// Round-trip into the typed struct and check the hand-computed values.
	var typed graphjson.Document
	if err := json.Unmarshal(b, &typed); err != nil {
		t.Fatal(err)
	}
	wantCompactness := graphjson.Float(14.0 / 36.0)
	if got := typed.Summary.Navigability.Compactness; got != newFloatForTest(14.0/36.0) {
		t.Errorf("navigability.compactness = %v, want ~%v", got, wantCompactness)
	}
	if got := typed.Summary.Navigability.Diameter; got != 3 {
		t.Errorf("navigability.diameter = %d, want 3", got)
	}
	if got := typed.Summary.Navigability.ReachablePairs; got != 12 {
		t.Errorf("navigability.reachablePairs = %d, want 12", got)
	}
}

// TestJSON_CriticalStructure proves the v5 critical-path fields (ADR 0015) are
// emitted and round-trip: per-node betweenness/isArticulation, the top-level
// betweenness.topDocs block, articulationPoints + bridges arrays, and the
// summary counts. The directed path a->b->c->d has hand-computed betweenness
// b=c=2/6 (a=d=0); its undirected closure makes {b,c} articulation points and
// all three edges bridges.
func TestJSON_CriticalStructure(t *testing.T) {
	c := corpus.NewCorpus()
	for _, id := range []string{"a.md", "b.md", "c.md", "d.md"} {
		d := &corpus.Document{
			ID:   corpusID(id),
			Root: &corpus.Section{Level: 0, Children: []*corpus.Section{{Level: 1, Text: "T", Slug: "t", StartLine: 1, EndLine: 2}}},
		}
		if err := c.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	g := graphmodel.BuildReferenceGraph(c, []reference.Reference{
		pathRef("a.md", "b.md"), pathRef("b.md", "c.md"), pathRef("c.md", "d.md"),
	}, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	v := emit.BuildView(application.Result{DocumentCount: c.Len(), Metrics: m, Corpus: c})

	b, err := graphjson.JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	var typed graphjson.Document
	if err := json.Unmarshal(b, &typed); err != nil {
		t.Fatal(err)
	}
	if typed.SchemaVersion != 6 {
		t.Errorf("schemaVersion = %d, want 6", typed.SchemaVersion)
	}

	// Per-node betweenness + isArticulation.
	byID := map[string]graphjson.Node{}
	for _, n := range typed.Nodes {
		byID[n.ID] = n
	}
	wantBet := newFloatForTest(2.0 / 6.0)
	if got := byID["b.md"].Betweenness; got != wantBet {
		t.Errorf("b.md betweenness = %v, want %v", got, wantBet)
	}
	if !byID["b.md"].IsArticulation || !byID["c.md"].IsArticulation {
		t.Errorf("b.md/c.md must be marked isArticulation")
	}
	if byID["a.md"].IsArticulation || byID["d.md"].IsArticulation {
		t.Errorf("a.md/d.md must NOT be articulation")
	}

	// Top-level blocks + summary counts.
	if len(typed.Betweenness.TopDocs) == 0 || typed.Betweenness.TopDocs[0].ID == "" {
		t.Errorf("betweenness.topDocs missing")
	}
	if got := typed.ArticulationPoints; len(got) != 2 || got[0] != "b.md" || got[1] != "c.md" {
		t.Errorf("articulationPoints = %v, want [b.md c.md]", got)
	}
	if got := typed.Bridges; len(got) != 3 {
		t.Errorf("bridges = %v, want 3", got)
	} else if got[0].From != "a.md" || got[0].To != "b.md" {
		t.Errorf("first bridge = %+v, want a.md->b.md", got[0])
	}
	if typed.Summary.ArticulationPoints != 2 || typed.Summary.Bridges != 3 {
		t.Errorf("summary articulationPoints/bridges = %d/%d, want 2/3",
			typed.Summary.ArticulationPoints, typed.Summary.Bridges)
	}
}

// TestJSON_PageRank proves the v6 PageRank fields (ADR 0016) are emitted and
// round-trip: a per-node `pageRank` field on every node (> 0 for a connected
// doc) and a top-level `pageRank.topDocs` block, parallel to `betweenness`, with
// scores in descending order. The directed path a->b->c->d is connected, so every
// node accrues positive PageRank.
func TestJSON_PageRank(t *testing.T) {
	c := corpus.NewCorpus()
	for _, id := range []string{"a.md", "b.md", "c.md", "d.md"} {
		d := &corpus.Document{
			ID:   corpusID(id),
			Root: &corpus.Section{Level: 0, Children: []*corpus.Section{{Level: 1, Text: "T", Slug: "t", StartLine: 1, EndLine: 2}}},
		}
		if err := c.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	g := graphmodel.BuildReferenceGraph(c, []reference.Reference{
		pathRef("a.md", "b.md"), pathRef("b.md", "c.md"), pathRef("c.md", "d.md"),
	}, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	v := emit.BuildView(application.Result{DocumentCount: c.Len(), Metrics: m, Corpus: c})

	b, err := graphjson.JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	var typed graphjson.Document
	if err := json.Unmarshal(b, &typed); err != nil {
		t.Fatal(err)
	}

	// Per-node pageRank present and > 0 for every (connected) doc.
	for _, n := range typed.Nodes {
		if n.PageRank <= 0 {
			t.Errorf("node %s pageRank = %v, want > 0 (connected corpus)", n.ID, n.PageRank)
		}
	}

	// Top-level pageRank.topDocs block: non-empty, ranked descending.
	top := typed.PageRank.TopDocs
	if len(top) == 0 || top[0].ID == "" {
		t.Fatalf("pageRank.topDocs missing: %+v", top)
	}
	for i := 1; i < len(top); i++ {
		if top[i].Score > top[i-1].Score {
			t.Errorf("pageRank.topDocs not descending at %d: %v > %v", i, top[i].Score, top[i-1].Score)
		}
	}

	// The float renders at the fixed precision (byte-stable determinism), like the
	// other score blocks.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["pageRank"]; !ok {
		t.Errorf("graph.json missing top-level pageRank block")
	}
}

// newFloatForTest mirrors graphjson.newFloat's fixed-precision rounding so the
// test compares against the same value the emitter stores.
func newFloatForTest(f float64) graphjson.Float {
	b, _ := json.Marshal(graphjson.Float(f))
	var out graphjson.Float
	_ = json.Unmarshal(b, &out)
	return out
}

// pathRef builds a minimal valid relative-link reference for the navigability
// fixture.
func pathRef(origin, target string) reference.Reference {
	return reference.Reference{
		RawReference: reference.RawReference{Origin: corpusID(origin), RawTarget: target, Type: reference.RelativeLink, Line: 1},
		Target:       reference.ResolvedTarget{Kind: reference.TargetDocument, DocumentID: corpusID(target)},
		Health:       reference.Valid,
	}
}

func hasNFractionalDigits(numText string, n int) bool {
	dot := bytes.IndexByte([]byte(numText), '.')
	if dot < 0 {
		return false
	}
	return len(numText)-dot-1 == n
}

// TestJSON_IndeterminateReachability is fix #8: when no root set is found,
// reachability is INDETERMINATE and graph.json must NOT mark every node
// unreachable (ADR 0007 — indeterminate is not unreachability). It must report
// reachability.indeterminate=true and every node reachable=true.
func TestJSON_IndeterminateReachability(t *testing.T) {
	// A corpus with no README/index and no type:index front matter has an empty
	// root set, so reachability is indeterminate.
	c := corpus.NewCorpus()
	for _, id := range []string{"docs/a.md", "docs/b.md"} {
		d := &corpus.Document{
			ID: corpusID(id),
			Root: &corpus.Section{Level: 0, Children: []*corpus.Section{
				{Level: 1, Text: "T", Slug: "t", StartLine: 1, EndLine: 2},
			}},
		}
		if err := c.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	g := graphmodel.BuildReferenceGraph(c, nil, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	v := emit.BuildView(application.Result{DocumentCount: c.Len(), Metrics: m, Corpus: c})

	doc := graphjson.Build(v)
	if !doc.Reachability.Indeterminate {
		t.Fatalf("expected reachability.indeterminate=true for an empty root set")
	}
	if len(doc.Nodes) == 0 {
		t.Fatal("expected nodes")
	}
	for _, n := range doc.Nodes {
		if !n.Reachable {
			t.Errorf("node %q marked unreachable under indeterminate reachability; want reachable=true", n.ID)
		}
	}
	if len(doc.Unreachable) != 0 {
		t.Errorf("unreachable list must be empty under indeterminate reachability, got %v", doc.Unreachable)
	}
}

// TestJSON_EdgelessRootNotInOrphans guards the view/emit layer (ADR 0010): an
// edgeless document that IS a root must be ABSENT from the emitted `orphans`
// array, exactly as it is exempt from graphmodel's Orphans.Isolated. This pins
// view.go against silently diverging from the domain classification, and runs in
// the DEFAULT `go test ./...` gate (the integration test needs -tags=integration).
func TestJSON_EdgelessRootNotInOrphans(t *testing.T) {
	c := corpus.NewCorpus()
	for _, id := range []string{"README.md", "agent.md", "loner.md"} {
		d := &corpus.Document{
			ID: corpusID(id),
			Root: &corpus.Section{Level: 0, Children: []*corpus.Section{
				{Level: 1, Text: "T", Slug: "t", StartLine: 1, EndLine: 2},
			}},
		}
		if err := c.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	// README.md is an auto-root; agent.md becomes a root via --root glob. Both are
	// edgeless. loner.md is an edgeless non-root (the control: it MUST be an orphan).
	g := graphmodel.BuildReferenceGraph(c, nil, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{RootGlobs: []string{"agent.md"}})
	v := emit.BuildView(application.Result{DocumentCount: c.Len(), Metrics: m, Corpus: c})

	doc := graphjson.Build(v)
	for _, want := range []string{"README.md", "agent.md"} {
		if sliceHas(doc.Orphans, want) {
			t.Errorf("edgeless root %q must be absent from emitted orphans; orphans = %v", want, doc.Orphans)
		}
	}
	// Control: the edgeless non-root IS an orphan, proving the finding still fires
	// at the emit layer (the test isn't passing because orphans is empty).
	if !sliceHas(doc.Orphans, "loner.md") {
		t.Errorf("edgeless non-root loner.md must be in emitted orphans; orphans = %v", doc.Orphans)
	}
}

func sliceHas(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestJSON_HostileTitleEscaped confirms a hostile title is JSON-escaped (never
// breaks the document). encoding/json handles this; we assert it.
func TestJSON_HostileTitleEscaped(t *testing.T) {
	v := buildCorpusView(t)
	doc := graphjson.Build(v)
	// Inject a hostile title directly into the typed doc and marshal: a title with
	// quotes, a brace, a newline and a backslash must survive as a valid string.
	hostile := "evil\"}{\n\\title"
	if len(doc.Nodes) == 0 {
		t.Skip("no nodes")
	}
	doc.Nodes[0].Title = hostile
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal with hostile title failed: %v", err)
	}
	// It must still parse, and the title must round-trip exactly.
	var back graphjson.Document
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("hostile-title doc does not parse (escaping broke the JSON): %v", err)
	}
	if back.Nodes[0].Title != hostile {
		t.Errorf("hostile title did not round-trip: got %q want %q", back.Nodes[0].Title, hostile)
	}
}

// TestJSON_ValidatesAgainstSchema validates the emitted document against the
// committed JSON Schema (docs/schemas/graph.schema.json). We use a minimal,
// dependency-free validator that enforces the schema's `required` lists and
// `additionalProperties:false` (the two properties that catch a shape drift):
// adding/removing/renaming a field fails this test, keeping the type and the
// published schema in lockstep without pulling in a JSON-schema library.
func TestJSON_ValidatesAgainstSchema(t *testing.T) {
	b, err := graphjson.JSON(buildCorpusView(t))
	if err != nil {
		t.Fatal(err)
	}
	var data any
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	schemaPath, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "docs", "schemas", "graph.schema.json"))
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
	if errs := validateNode(data, schema, schema, "$"); len(errs) > 0 {
		sort.Strings(errs)
		t.Errorf("graph.json does not satisfy graph.schema.json:\n  %v", errs)
	}
}

// validateNode is a minimal JSON-Schema (Draft 2020-12 subset) checker: it
// resolves local $ref, and enforces type, required, additionalProperties:false,
// const, enum, and recurses into properties / array items. It is intentionally
// small — just enough to assert the shape contract this repo publishes.
func validateNode(data any, schema, root map[string]any, path string) []string {
	if ref, ok := schema["$ref"].(string); ok {
		resolved := resolveRef(ref, root)
		if resolved == nil {
			return []string{fmt.Sprintf("%s: unresolved $ref %q", path, ref)}
		}
		return validateNode(data, resolved, root, path)
	}

	var errs []string
	switch schema["type"] {
	case "object":
		m, ok := data.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want object", path)}
		}
		props, _ := schema["properties"].(map[string]any)
		// required
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				name := r.(string)
				if _, present := m[name]; !present {
					errs = append(errs, fmt.Sprintf("%s: missing required %q", path, name))
				}
			}
		}
		// additionalProperties:false → no unknown keys
		if ap, ok := schema["additionalProperties"].(bool); ok && !ap {
			for k := range m {
				if _, known := props[k]; !known {
					errs = append(errs, fmt.Sprintf("%s: unexpected property %q", path, k))
				}
			}
		}
		for k, v := range m {
			if ps, ok := props[k].(map[string]any); ok {
				errs = append(errs, validateNode(v, ps, root, path+"."+k)...)
			}
		}
	case "array":
		arr, ok := data.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want array", path)}
		}
		if items, ok := schema["items"].(map[string]any); ok {
			for i, e := range arr {
				errs = append(errs, validateNode(e, items, root, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	case "string":
		if _, ok := data.(string); !ok {
			errs = append(errs, fmt.Sprintf("%s: want string", path))
		}
	case "integer":
		// JSON numbers decode to float64; integer means no fractional part.
		f, ok := data.(float64)
		if !ok || f != float64(int64(f)) {
			errs = append(errs, fmt.Sprintf("%s: want integer", path))
		}
	case "number":
		if _, ok := data.(float64); !ok {
			errs = append(errs, fmt.Sprintf("%s: want number", path))
		}
	case "boolean":
		if _, ok := data.(bool); !ok {
			errs = append(errs, fmt.Sprintf("%s: want boolean", path))
		}
	}

	if c, ok := schema["const"]; ok {
		if !jsonEqual(data, c) {
			errs = append(errs, fmt.Sprintf("%s: const mismatch (want %v)", path, c))
		}
	}
	if en, ok := schema["enum"].([]any); ok {
		matched := false
		for _, e := range en {
			if jsonEqual(data, e) {
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

func resolveRef(ref string, root map[string]any) map[string]any {
	// Only local "#/$defs/Name" refs are used.
	const prefix = "#/$defs/"
	if len(ref) <= len(prefix) || ref[:len(prefix)] != prefix {
		return nil
	}
	defs, ok := root["$defs"].(map[string]any)
	if !ok {
		return nil
	}
	d, ok := defs[ref[len(prefix):]].(map[string]any)
	if !ok {
		return nil
	}
	return d
}

func jsonEqual(a, b any) bool {
	// const/enum values in the schema are decoded the same way as data (float64
	// for numbers), so a direct compare works for the scalar cases we use.
	if af, ok := a.(float64); ok {
		if bf, ok := b.(float64); ok {
			return af == bf
		}
	}
	return a == b
}
