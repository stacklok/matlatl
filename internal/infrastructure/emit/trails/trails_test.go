package trails_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/trails"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
)

// buildCorpusView runs the real pipeline over testdata/corpus and returns the
// render-ready View, mirroring the emit golden test harness.
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

func TestTrailsJSON_Shape(t *testing.T) {
	v := buildCorpusView(t)
	doc := trails.Build(v)
	if doc.SchemaVersion != trails.SchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", doc.SchemaVersion, trails.SchemaVersion)
	}
	if doc.Tool != "matlatl" {
		t.Errorf("tool = %q, want matlatl", doc.Tool)
	}
	if len(doc.Trails) == 0 {
		t.Errorf("expected at least one trail for the fixture corpus")
	}
	// Every trail has a non-empty root that is a MEMBER of its order (the root is
	// the component's most-important doc, not necessarily the topological head —
	// ADR 0016).
	for _, tr := range doc.Trails {
		if tr.Root == "" {
			t.Errorf("trail has empty root: %+v", tr)
		}
		found := false
		for _, id := range tr.Order {
			if id == tr.Root {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("trail root %q not present in its order %v", tr.Root, tr.Order)
		}
	}
	// Roots are sorted (determinism contract).
	if !sort.SliceIsSorted(doc.Trails, func(i, j int) bool { return doc.Trails[i].Root < doc.Trails[j].Root }) {
		t.Errorf("trails not sorted by root")
	}
}

func TestTrailsJSON_ByteStable(t *testing.T) {
	v1 := buildCorpusView(t)
	v2 := buildCorpusView(t)
	b1, err := trails.JSON(v1)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := trails.JSON(v2)
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Errorf("trails.json is not byte-stable across two runs")
	}
}

// TestTrailsJSON_ValidatesAgainstSchema validates the emitted document against
// docs/schemas/trails.schema.json using the same minimal dependency-free checker
// the graphjson test uses (required + additionalProperties:false + const + type),
// keeping the type and the published schema in lockstep.
func TestTrailsJSON_ValidatesAgainstSchema(t *testing.T) {
	b, err := trails.JSON(buildCorpusView(t))
	if err != nil {
		t.Fatal(err)
	}
	var data any
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	schemaPath, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "docs", "schemas", "trails.schema.json"))
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
	if errs := validateNode(data, schema, "$"); len(errs) > 0 {
		sort.Strings(errs)
		t.Errorf("trails.json does not satisfy trails.schema.json:\n  %v", errs)
	}
}

// validateNode is a minimal JSON-Schema (Draft 2020-12 subset) checker enforcing
// type, required, additionalProperties:false, const, and recursing into
// properties / array items — enough to assert the published shape contract.
func validateNode(data any, schema map[string]any, path string) []string {
	var errs []string
	switch schema["type"] {
	case "object":
		m, ok := data.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want object", path)}
		}
		props, _ := schema["properties"].(map[string]any)
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				if _, present := m[r.(string)]; !present {
					errs = append(errs, fmt.Sprintf("%s: missing required %q", path, r))
				}
			}
		}
		if ap, ok := schema["additionalProperties"].(bool); ok && !ap {
			for k := range m {
				if _, known := props[k]; !known {
					errs = append(errs, fmt.Sprintf("%s: unexpected property %q", path, k))
				}
			}
		}
		for k, val := range m {
			if ps, ok := props[k].(map[string]any); ok {
				errs = append(errs, validateNode(val, ps, path+"."+k)...)
			}
		}
	case "array":
		arr, ok := data.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want array", path)}
		}
		if items, ok := schema["items"].(map[string]any); ok {
			for i, e := range arr {
				errs = append(errs, validateNode(e, items, fmt.Sprintf("%s[%d]", path, i))...)
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
		}
	}
	if c, ok := schema["const"]; ok {
		cf, cok := c.(float64)
		df, dok := data.(float64)
		if cok && dok {
			if cf != df {
				errs = append(errs, fmt.Sprintf("%s: const mismatch (want %v)", path, c))
			}
		}
	}
	return errs
}
