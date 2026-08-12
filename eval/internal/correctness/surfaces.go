package correctness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/stacklok/matlatl/eval/internal/harness"
	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/index"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/llmstxt"
	trailsemit "github.com/stacklok/matlatl/internal/infrastructure/emit/trails"
)

func runBacklinks(ctx context.Context, file *backlinkFile, snapshot *fixtureSnapshot) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if file.SchemaVersion != SchemaVersion || file.Family != "emitted-backlinks" || !safe(file.Fixture) || file.AuthoredReferences < 1 || len(file.Documents) == 0 || len(file.Documents) > maxItems {
		return 0, errorsf("emitted-backlinks: unsupported or unsafe contract")
	}
	last := ""
	documentSet := make(map[string]struct{}, len(file.Documents))
	for _, want := range file.Documents {
		if !safe(want.Path) || !identity.IsMarkdownPath(want.Path) || want.Path <= last || !boundedSortedStrings(want.Backlinks) {
			return 0, errorsf("emitted-backlinks: documents and backlinks must be safe, sorted, and unique")
		}
		documentSet[want.Path] = struct{}{}
		last = want.Path
	}
	for _, want := range file.Documents {
		for _, source := range want.Backlinks {
			if _, ok := documentSet[source]; !ok || source == want.Path {
				return 0, fmt.Errorf("emitted-backlinks/%s: unknown or self backlink %q", want.Path, source)
			}
		}
	}
	fixture, err := snapshot.materialize(ctx, "matlatl-backlinks-")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(fixture) }()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	run, err := harness.AnalyzePipeline(ctx, fixture, false)
	if err != nil {
		return 0, err
	}
	if run.Result.ReferenceCount != file.AuthoredReferences || len(run.Result.ResolvedReferences) != file.AuthoredReferences {
		return 0, fmt.Errorf("emitted-backlinks: authored references got=%d/%d want=%d", run.Result.ReferenceCount, len(run.Result.ResolvedReferences), file.AuthoredReferences)
	}
	gotDocuments := make([]string, len(run.View.Docs))
	for i, document := range run.View.Docs {
		gotDocuments[i] = document.ID.String()
	}
	wantDocuments := make([]string, len(file.Documents))
	for i, document := range file.Documents {
		wantDocuments[i] = document.Path
	}
	if !slices.Equal(gotDocuments, wantDocuments) {
		return 0, fmt.Errorf("emitted-backlinks: documents got=%v want=%v", gotDocuments, wantDocuments)
	}
	indexBytes := index.Markdown(run.View)
	llmsBytes := llmstxt.LLMSTxt(run.View, llmstxt.Options{})
	graphBytes, err := graphjson.JSON(run.View)
	if err != nil {
		return 0, err
	}
	if err := graphHasNoBacklinks(graphBytes); err != nil {
		return 0, err
	}
	for _, want := range file.Documents {
		if got := idStrings(run.View.Backlinks(identity.DocumentID(want.Path))); !slices.Equal(got, want.Backlinks) {
			return 0, fmt.Errorf("emitted-backlinks/%s: view got=%v want=%v", want.Path, got, want.Backlinks)
		}
		if got, err := indexBacklinks(indexBytes, want.Path); err != nil || !slices.Equal(got, want.Backlinks) {
			return 0, fmt.Errorf("emitted-backlinks/%s: index.md got=%v want=%v: %w", want.Path, got, want.Backlinks, err)
		}
		if got, err := llmsBacklinks(llmsBytes, want.Path); err != nil || !slices.Equal(got, want.Backlinks) {
			return 0, fmt.Errorf("emitted-backlinks/%s: llms.txt got=%v want=%v: %w", want.Path, got, want.Backlinks, err)
		}
	}
	return len(file.Documents), nil
}

func graphHasNoBacklinks(body []byte) error {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	var walk func(any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			for key, child := range x {
				if strings.EqualFold(key, "backlinks") || walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range x {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	if walk(value) {
		return errorsf("emitted-backlinks: graph.json contains a backlinks field")
	}
	return nil
}

func indexBacklinks(body []byte, document string) ([]string, error) {
	prefix := "| `" + document + "` |"
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 4 {
			return nil, errorsf("malformed index row")
		}
		cell := strings.TrimSpace(cells[2])
		if cell == "-" {
			return []string{}, nil
		}
		return strings.Split(cell, ", "), nil
	}
	return nil, errorsf("index row absent")
}

func llmsBacklinks(body []byte, document string) ([]string, error) {
	needle := "](" + document + ")"
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "- [") || !strings.Contains(line, needle) {
			continue
		}
		const marker = " (linked from: "
		start := strings.Index(line, marker)
		if start < 0 {
			return []string{}, nil
		}
		clause := line[start+len(marker):]
		if !strings.HasSuffix(clause, ")") {
			return nil, errorsf("malformed backlinks clause")
		}
		return strings.Split(strings.TrimSuffix(clause, ")"), ", "), nil
	}
	return nil, errorsf("curated entry absent")
}

func runTrails(ctx context.Context, file *trailFile) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if file.SchemaVersion != SchemaVersion || file.Family != "emitted-trails" || len(file.Cases) == 0 || len(file.Cases) > maxCases {
		return 0, errorsf("emitted-trails: unsupported contract")
	}
	last := ""
	for _, tc := range file.Cases {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if !safe(tc.ID) || tc.ID <= last || len(tc.Want) == 0 || len(tc.Want) > maxItems {
			return 0, errorsf("emitted-trails: cases must be safe, sorted, and bounded")
		}
		last = tc.ID
		if err := validateMechanismGraph(file.Family, tc.ID, tc.Graph); err != nil {
			return 0, err
		}
		previousRoot := ""
		seenMembers := map[string]struct{}{}
		graphDocuments := stringSet(tc.Graph.Documents)
		for _, want := range tc.Want {
			if !safe(want.Root) || want.Root <= previousRoot || !boundedSortedTrail(want.Order) {
				return 0, fmt.Errorf("emitted-trails/%s: invalid expected trail", tc.ID)
			}
			if _, ok := graphDocuments[want.Root]; !ok || !slices.Contains(want.Order, want.Root) {
				return 0, fmt.Errorf("emitted-trails/%s: unknown root or root absent from order %q", tc.ID, want.Root)
			}
			for _, member := range want.Order {
				if _, ok := graphDocuments[member]; !ok {
					return 0, fmt.Errorf("emitted-trails/%s: unknown member %q", tc.ID, member)
				}
				if _, duplicate := seenMembers[member]; duplicate {
					return 0, fmt.Errorf("emitted-trails/%s: duplicate member %q", tc.ID, member)
				}
				seenMembers[member] = struct{}{}
			}
			previousRoot = want.Root
		}
		if len(seenMembers) != len(tc.Graph.Documents) {
			return 0, fmt.Errorf("emitted-trails/%s: expected trails do not cover every document", tc.ID)
		}
		if err := checkTrailCase(ctx, tc); err != nil {
			return 0, fmt.Errorf("emitted-trails/%s: %w", tc.ID, err)
		}
	}
	return len(file.Cases), nil
}

func boundedSortedTrail(order []string) bool {
	if len(order) == 0 || len(order) > maxItems || !shortStrings(order...) {
		return false
	}
	seen := map[string]struct{}{}
	for _, id := range order {
		if !safe(id) {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func checkTrailCase(ctx context.Context, tc trailCase) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g, err := buildMechanismGraph(ctx, tc.Graph, false)
	if err != nil {
		return err
	}
	c := corpus.NewCorpus()
	for _, id := range tc.Graph.Documents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.Add(&corpus.Document{ID: identity.DocumentID(id)}); err != nil {
			return err
		}
	}
	c.Freeze()
	metrics := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	view := emit.BuildView(application.Result{DocumentCount: len(tc.Graph.Documents), Corpus: c, Metrics: metrics})
	doc := trailsemit.Build(view)
	if doc.SchemaVersion != 1 || doc.Tool != "matlatl" || len(doc.Trails) != len(tc.Want) {
		return fmt.Errorf("shape/version got schema=%d tool=%q trails=%d", doc.SchemaVersion, doc.Tool, len(doc.Trails))
	}
	for i, got := range doc.Trails {
		want := tc.Want[i]
		if got.Root != want.Root || !slices.Equal(got.Order, want.Order) {
			return fmt.Errorf("trail %d got=%+v want=%+v", i, got, want)
		}
	}
	body, err := trailsemit.JSON(view)
	if err != nil {
		return err
	}
	var wire trailsemit.Document
	if err := json.Unmarshal(body, &wire); err != nil {
		return err
	}
	if wire.SchemaVersion != 1 || wire.Tool != "matlatl" || len(wire.Trails) != len(tc.Want) {
		return errorsf("trails.json shape/version differs from hand-authored expectation")
	}
	for i, got := range wire.Trails {
		want := tc.Want[i]
		if got.Root != want.Root || !slices.Equal(got.Order, want.Order) {
			return fmt.Errorf("trails.json trail %d got=%+v want=%+v", i, got, want)
		}
	}
	return nil
}
