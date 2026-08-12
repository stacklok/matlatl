package correctness

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
)

const (
	// V1 suite-wide limits are checked after every manifest is decoded and before
	// any graph analysis or pipeline run. They leave headroom above the current
	// 97-case suite while bounding aggregate work across individually valid files.
	maxSuiteCases        = 128
	maxSuiteVertices     = 4096
	maxSuiteEdges        = 8192
	maxSuiteFixtureFiles = 512
	maxSuiteFixtureBytes = 16 << 20
	maxSuitePipelineRuns = 96
	maxSuiteGraphWork    = 1_000_000 // sum of V*(V+E), including fixture-run estimates
)

type suiteFiles struct {
	graph       *graphFile
	resolver    *resolverFile
	pipeline    *pipelineFile
	scent       *scentFile
	gaps        *gapFile
	suggestions *suggestionFile
	mutations   *mutationFile
	backlinks   *backlinkFile
	trails      *trailFile
	determinism *determinismFile
	fixtures    map[string]*fixtureSnapshot
}

type suiteBudget struct {
	cases, vertices, edges, fixtureFiles, fixtureBytes, pipelineRuns, graphWork int
}

func loadManifest[T any](evalRoot, filename string) (*T, error) {
	rel, err := filepath.Rel(evalRoot, filename)
	if err != nil {
		return nil, err
	}
	body, err := evalfs.Read(evalRoot, filepath.ToSlash(rel))
	if err != nil {
		return nil, err
	}
	return decode[T](body)
}

func loadSuite(ctx context.Context, evalRoot, dir string) (*suiteFiles, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := &suiteFiles{}
	loads := []struct {
		name string
		load func(string) error
	}{
		{"graph.json", func(p string) error { v, e := loadManifest[graphFile](evalRoot, p); s.graph = v; return e }},
		{"resolver.json", func(p string) error { v, e := loadManifest[resolverFile](evalRoot, p); s.resolver = v; return e }},
		{"pipeline.json", func(p string) error { v, e := loadManifest[pipelineFile](evalRoot, p); s.pipeline = v; return e }},
		{"scent.json", func(p string) error { v, e := loadManifest[scentFile](evalRoot, p); s.scent = v; return e }},
		{"gaps.json", func(p string) error { v, e := loadManifest[gapFile](evalRoot, p); s.gaps = v; return e }},
		{"suggestions.json", func(p string) error { v, e := loadManifest[suggestionFile](evalRoot, p); s.suggestions = v; return e }},
		{"mutations.json", func(p string) error { v, e := loadManifest[mutationFile](evalRoot, p); s.mutations = v; return e }},
		{"backlinks.json", func(p string) error { v, e := loadManifest[backlinkFile](evalRoot, p); s.backlinks = v; return e }},
		{"trails.json", func(p string) error { v, e := loadManifest[trailFile](evalRoot, p); s.trails = v; return e }},
		{"determinism.json", func(p string) error { v, e := loadManifest[determinismFile](evalRoot, p); s.determinism = v; return e }},
	}
	for _, item := range loads {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := item.load(filepath.Join(dir, item.name)); err != nil {
			return nil, fmt.Errorf("%s: %w", item.name, err)
		}
	}
	s.fixtures = make(map[string]*fixtureSnapshot)
	refs := []string{s.pipeline.Fixture, s.backlinks.Fixture, s.determinism.Fixture}
	for _, tc := range s.mutations.Cases {
		refs = append(refs, filepath.ToSlash(filepath.Join(s.mutations.Fixture, tc.Directory)))
	}
	fixtureFiles, fixtureBytes := 0, 0
	for _, rel := range refs {
		if _, exists := s.fixtures[rel]; exists {
			continue
		}
		snapshot, err := snapshotFixture(ctx, evalRoot, rel, maxSuiteFixtureFiles-fixtureFiles, maxSuiteFixtureBytes-fixtureBytes)
		if err != nil {
			return nil, fmt.Errorf("fixture %q: %w", rel, err)
		}
		s.fixtures[rel] = snapshot
		fixtureFiles += len(snapshot.entries)
		for _, entry := range snapshot.entries {
			fixtureBytes += len(entry.content)
		}
	}
	return s, nil
}

func validateSuiteBudget(ctx context.Context, s *suiteFiles) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var b suiteBudget
	addGraph := func(vertices, edges, runs int) error {
		if vertices < 0 || edges < 0 || runs < 1 {
			return fmt.Errorf("correctness suite: invalid work dimensions")
		}
		work := vertices * (vertices + edges)
		if vertices != 0 && work/vertices != vertices+edges {
			return fmt.Errorf("correctness suite: graph-work overflow")
		}
		if runs > 1 && work > maxSuiteGraphWork/runs {
			return fmt.Errorf("correctness suite exceeds graph-work budget %d", maxSuiteGraphWork)
		}
		b.vertices += vertices
		b.edges += edges
		b.graphWork += work * runs
		return nil
	}
	addCases := func(n int) { b.cases += n }
	for _, tc := range s.graph.Cases {
		addCases(1)
		if err := addGraph(len(tc.Documents), len(tc.Edges), 1); err != nil {
			return err
		}
	}
	addCases(len(s.resolver.Cases))
	addCases(len(s.pipeline.Cases))
	for _, tc := range s.scent.Cases {
		addCases(1)
		if err := addGraph(len(tc.Documents), len(tc.Links), 1); err != nil {
			return err
		}
	}
	for _, tc := range s.gaps.Cases {
		addCases(1)
		if err := addGraph(len(tc.Graph.Documents), len(tc.Graph.Edges), 1); err != nil {
			return err
		}
	}
	for _, tc := range s.suggestions.Cases {
		addCases(1)
		if err := addGraph(len(tc.Graph.Documents), len(tc.Graph.Edges), 2); err != nil {
			return err
		}
	}
	addCases(len(s.mutations.Cases))
	addCases(len(s.backlinks.Documents))
	for _, tc := range s.trails.Cases {
		addCases(1)
		if err := addGraph(len(tc.Graph.Documents), len(tc.Graph.Edges), 1); err != nil {
			return err
		}
	}
	addCases(1)

	fixture := func(rel string, runs, edgeEstimate int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		snapshot, ok := s.fixtures[rel]
		if !ok {
			return fmt.Errorf("fixture %q was not snapshotted", rel)
		}
		b.pipelineRuns += runs
		return addGraph(len(snapshot.entries), edgeEstimate, runs)
	}
	if err := fixture(s.pipeline.Fixture, 2, len(s.pipeline.Cases)); err != nil {
		return fmt.Errorf("pipeline fixture budget: %w", err)
	}
	for _, tc := range s.mutations.Cases {
		if err := fixture(filepath.ToSlash(filepath.Join(s.mutations.Fixture, tc.Directory)), 6, 0); err != nil {
			return fmt.Errorf("mutation fixture budget: %w", err)
		}
	}
	if err := fixture(s.backlinks.Fixture, 1, s.backlinks.AuthoredReferences); err != nil {
		return fmt.Errorf("backlink fixture budget: %w", err)
	}
	runs := len(s.determinism.CreationOrders) * s.determinism.RunsPerOrder
	if err := fixture(s.determinism.Fixture, runs, 0); err != nil {
		return fmt.Errorf("determinism fixture budget: %w", err)
	}

	for _, snapshot := range s.fixtures {
		b.fixtureFiles += len(snapshot.entries)
		for _, entry := range snapshot.entries {
			b.fixtureBytes += len(entry.content)
		}
	}
	if b.cases > maxSuiteCases || b.vertices > maxSuiteVertices || b.edges > maxSuiteEdges || b.fixtureFiles > maxSuiteFixtureFiles || b.fixtureBytes > maxSuiteFixtureBytes || b.pipelineRuns > maxSuitePipelineRuns || b.graphWork > maxSuiteGraphWork {
		return fmt.Errorf("correctness suite exceeds cumulative budget: cases=%d/%d vertices=%d/%d edges=%d/%d fixtureFiles=%d/%d fixtureBytes=%d/%d pipelineRuns=%d/%d graphWork=%d/%d", b.cases, maxSuiteCases, b.vertices, maxSuiteVertices, b.edges, maxSuiteEdges, b.fixtureFiles, maxSuiteFixtureFiles, b.fixtureBytes, maxSuiteFixtureBytes, b.pipelineRuns, maxSuitePipelineRuns, b.graphWork, maxSuiteGraphWork)
	}
	return nil
}
