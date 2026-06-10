package trails_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/trails"
	"github.com/stacklok/matlatl/internal/infrastructure/fsscanner"
	"github.com/stacklok/matlatl/internal/infrastructure/mdparser"
)

// excludeCorpusView runs the real pipeline over testdata/corpus.
func excludeCorpusView(t *testing.T) emit.View {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := application.DefaultConfig()
	cfg.RootPath = root
	pipe := application.NewPipeline(cfg, fsscanner.New(fsscanner.Config{}), mdparser.NewFactory(mdparser.Config{}), nil)
	_, res, err := pipe.Run(context.Background())
	if err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	return emit.BuildView(res)
}

// TestTrailsJSON_EmitExclude: trails.json is a consumption surface (ADR 0019) —
// emit-excluded docs are dropped from every trail's order, and a trail left
// empty is dropped, while the schema shape (and version) is unchanged.
func TestTrailsJSON_EmitExclude(t *testing.T) {
	v := excludeCorpusView(t)

	plain := trails.Build(v)
	var unfilteredSteps int
	for _, tr := range plain.Trails {
		unfilteredSteps += len(tr.Order)
	}
	if unfilteredSteps == 0 {
		t.Fatal("precondition: the corpus must produce trail steps")
	}

	filtered := trails.Build(v.WithEmitExclude([]string{"docs/island/"}))
	if filtered.SchemaVersion != trails.SchemaVersion {
		t.Errorf("dropping entries must not bump the schema version")
	}
	var filteredSteps int
	for _, tr := range filtered.Trails {
		if len(tr.Order) == 0 {
			t.Errorf("a trail left empty must be dropped, found empty trail rooted at %q", tr.Root)
		}
		for _, step := range tr.Order {
			if len(step) >= len("docs/island/") && step[:len("docs/island/")] == "docs/island/" {
				t.Errorf("excluded doc %q must not appear as a trail step", step)
			}
			filteredSteps++
		}
	}
	if filteredSteps >= unfilteredSteps {
		t.Errorf("filtering must drop steps: %d -> %d", unfilteredSteps, filteredSteps)
	}
}

// TestTrailsJSON_EmitExclude_NoPatternsByteIdentical: an empty pattern list
// leaves trails.json byte-identical (ADR 0019).
func TestTrailsJSON_EmitExclude_NoPatternsByteIdentical(t *testing.T) {
	v := excludeCorpusView(t)
	plain, err := trails.JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	armed, err := trails.JSON(v.WithEmitExclude(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, armed) {
		t.Error("empty emitExclude must be byte-identical to no emitExclude")
	}
}
