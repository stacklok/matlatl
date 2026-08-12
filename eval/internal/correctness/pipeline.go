package correctness

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/stacklok/matlatl/eval/internal/harness"
	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
)

type pipelineObservation struct {
	ID           string        `json:"id"`
	Reference    referenceView `json:"reference"`
	EdgesDefault []string      `json:"edgesDefault"`
	EdgesStrict  []string      `json:"edgesStrict,omitempty"`
	Finding      *findingView  `json:"finding,omitempty"`
}

type referenceView struct {
	Path       string   `json:"path"`
	Line       int      `json:"line"`
	Target     string   `json:"target"`
	Fragment   string   `json:"fragment"`
	Type       string   `json:"type"`
	AnchorText string   `json:"anchorText"`
	Health     string   `json:"health"`
	Kind       string   `json:"kind"`
	Document   string   `json:"document"`
	Anchor     string   `json:"anchor"`
	Directory  string   `json:"directory"`
	Children   []string `json:"children,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

type findingView struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Severity string            `json:"severity"`
	Path     string            `json:"path"`
	Line     int               `json:"line"`
	Details  map[string]string `json:"details,omitempty"`
}

type findingsWire struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Findings      []emittedFindingView `json:"findings"`
}

type emittedFindingView struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Severity string            `json:"severity"`
	Document string            `json:"document"`
	Line     int               `json:"line"`
	Details  map[string]string `json:"details,omitempty"`
}

func runPipeline(ctx context.Context, file *pipelineFile, fixture *fixtureSnapshot) (int, []byte, error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	if err := validatePipelineFile(file); err != nil {
		return 0, nil, err
	}
	fixtures, err := fixture.materialize(ctx, "matlatl-pipeline-")
	if err != nil {
		return 0, nil, fmt.Errorf("pipeline-resolver fixture: %w", err)
	}
	defer func() { _ = os.RemoveAll(fixtures) }()
	defaultRun, err := harness.AnalyzePipeline(ctx, fixtures, false)
	if err != nil {
		return 0, nil, fmt.Errorf("pipeline-resolver default run: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	strictRun, err := harness.AnalyzePipeline(ctx, fixtures, true)
	if err != nil {
		return 0, nil, fmt.Errorf("pipeline-resolver strict run: %w", err)
	}
	if defaultRun.View.Counts.References != len(file.Cases) || defaultRun.Result.ReferenceCount != len(file.Cases) || len(defaultRun.Result.ResolvedReferences) != len(file.Cases) {
		return 0, nil, fmt.Errorf("pipeline-resolver: reference counts result=%d seam=%d view=%d want=%d", defaultRun.Result.ReferenceCount, len(defaultRun.Result.ResolvedReferences), defaultRun.View.Counts.References, len(file.Cases))
	}
	if strictRun.Result.ReferenceCount != len(file.Cases) || len(strictRun.Result.ResolvedReferences) != len(file.Cases) {
		return 0, nil, fmt.Errorf("pipeline-resolver: strict reference count differs")
	}
	findingsJSON, err := emit.FindingsJSON(defaultRun.Result.Report, emit.OKFVerdictFromResult(defaultRun.Result))
	if err != nil {
		return 0, nil, fmt.Errorf("pipeline-resolver findings.json: %w", err)
	}
	var findingsDoc findingsWire
	if err := json.Unmarshal(findingsJSON, &findingsDoc); err != nil {
		return 0, nil, fmt.Errorf("pipeline-resolver findings.json decode: %w", err)
	}
	if findingsDoc.SchemaVersion != emit.FindingsSchemaVersion {
		return 0, nil, fmt.Errorf("pipeline-resolver findings.json schema=%d want=%d", findingsDoc.SchemaVersion, emit.FindingsSchemaVersion)
	}

	observations := make([]pipelineObservation, 0, len(file.Cases))
	for _, tc := range file.Cases {
		if err := ctx.Err(); err != nil {
			return 0, nil, err
		}
		got, err := referenceAt(defaultRun.Result.ResolvedReferences, tc.Source)
		if err != nil {
			return 0, nil, fmt.Errorf("pipeline-resolver/%s: %w", tc.ID, err)
		}
		strictRef, err := referenceAt(strictRun.Result.ResolvedReferences, tc.Source)
		if err != nil {
			return 0, nil, fmt.Errorf("pipeline-resolver/%s strict: %w", tc.ID, err)
		}
		if !sameReference(got, strictRef) {
			return 0, nil, fmt.Errorf("pipeline-resolver/%s: strict changed parser/resolver result", tc.ID)
		}
		if err := comparePipelineReference(tc, got); err != nil {
			return 0, nil, fmt.Errorf("pipeline-resolver/%s: %w", tc.ID, err)
		}

		defaultEdges := isolatedEdges(defaultRun.Result.Corpus, got, false)
		if !slices.Equal(defaultEdges, tc.EdgesDefault) {
			return 0, nil, fmt.Errorf("pipeline-resolver/%s: default projected edges=%v want=%v", tc.ID, defaultEdges, tc.EdgesDefault)
		}
		strictEdges := []string(nil)
		if tc.EdgesStrict != nil {
			strictEdges = isolatedEdges(strictRun.Result.Corpus, strictRef, true)
			if !slices.Equal(strictEdges, *tc.EdgesStrict) {
				return 0, nil, fmt.Errorf("pipeline-resolver/%s: strict projected edges=%v want=%v", tc.ID, strictEdges, *tc.EdgesStrict)
			}
		}

		gotFinding, err := pipelineFindingAt(defaultRun.Result.Report.Findings(), tc)
		if err != nil {
			return 0, nil, fmt.Errorf("pipeline-resolver/%s: %w", tc.ID, err)
		}
		if err := compareFinding(tc, gotFinding); err != nil {
			return 0, nil, fmt.Errorf("pipeline-resolver/%s: %w", tc.ID, err)
		}
		if err := compareEmittedFinding(tc, gotFinding, findingsDoc.Findings); err != nil {
			return 0, nil, fmt.Errorf("pipeline-resolver/%s findings.json: %w", tc.ID, err)
		}
		if !viewContainsFinding(defaultRun.View, gotFinding) {
			return 0, nil, fmt.Errorf("pipeline-resolver/%s: result finding is absent from emitter view", tc.ID)
		}
		observations = append(observations, observation(tc.ID, got, defaultEdges, strictEdges, gotFinding))
	}

	if err := compareAggregateProjection(file.Cases, defaultRun.Result.Metrics.Graph, false); err != nil {
		return 0, nil, err
	}
	if err := compareAggregateProjection(file.Cases, strictRun.Result.Metrics.Graph, true); err != nil {
		return 0, nil, err
	}
	snapshot, err := json.Marshal(observations)
	if err != nil {
		return 0, nil, err
	}
	return len(file.Cases), append(snapshot, '\n'), nil
}

func validatePipelineFile(file *pipelineFile) error {
	if file.SchemaVersion != SchemaVersion || file.Family != "pipeline-resolver" || !safe(file.Fixture) {
		return errorsf("pipeline-resolver: unsupported or unsafe contract")
	}
	if len(file.Cases) == 0 || len(file.Cases) > maxCases {
		return errorsf("pipeline-resolver: invalid case count")
	}
	last := ""
	seenSource := map[sourceOracle]struct{}{}
	for _, tc := range file.Cases {
		if !safe(tc.ID) || tc.ID <= last {
			return errorsf("pipeline-resolver: case IDs must be safe, unique, and sorted")
		}
		last = tc.ID
		if !safe(tc.Source.Path) || !identity.IsMarkdownPath(tc.Source.Path) || tc.Source.Line < 1 {
			return errorsf("pipeline-resolver/%s: invalid source", tc.ID)
		}
		if _, exists := seenSource[tc.Source]; exists {
			return errorsf("pipeline-resolver/%s: duplicate source location", tc.ID)
		}
		seenSource[tc.Source] = struct{}{}
		if !validTypeName(tc.Type) || !validHealthName(tc.Want.Health) || !validKindName(tc.Want.Kind) || !shortStrings(tc.Target, tc.Fragment, tc.AnchorText, tc.Want.Document, tc.Want.Anchor, tc.Want.Directory) || !boundedSortedStrings(tc.Want.Children) || !boundedSortedStrings(tc.Want.Candidates) || !boundedSortedStrings(tc.EdgesDefault) {
			return errorsf("pipeline-resolver/%s: invalid enum, string, bound, or ordering", tc.ID)
		}
		if tc.Want.Document != "" && !safe(tc.Want.Document) || tc.Want.Directory != "" && !safe(tc.Want.Directory) || len(tc.Want.AssetCalls) != 0 {
			return errorsf("pipeline-resolver/%s: unsafe target or unsupported asset probe expectation", tc.ID)
		}
		if tc.EdgesStrict != nil && tc.Want.Kind != "directory" {
			return errorsf("pipeline-resolver/%s: strict projection is supported only for directory cases", tc.ID)
		}
		if tc.EdgesStrict != nil && !boundedSortedStrings(*tc.EdgesStrict) {
			return errorsf("pipeline-resolver/%s: invalid strict edges", tc.ID)
		}
		for _, p := range append(append(slices.Clone(tc.Want.Children), tc.Want.Candidates...), tc.EdgesDefault...) {
			if !safe(p) {
				return errorsf("pipeline-resolver/%s: unsafe expected path", tc.ID)
			}
		}
		if tc.EdgesStrict != nil {
			for _, p := range *tc.EdgesStrict {
				if !safe(p) {
					return errorsf("pipeline-resolver/%s: unsafe strict edge", tc.ID)
				}
			}
		}
		if tc.Finding != nil {
			kind, ok := analysis.ParseFindingKind(tc.Finding.Kind)
			if !ok || !isPipelineFinding(kind) || kind == analysis.LowScentAnchor && !tc.CheckLowScent || !safe(tc.Finding.Path) || tc.Finding.Path != tc.Source.Path || tc.Finding.Line != tc.Source.Line || !validFindingOracle(*tc.Finding) {
				return errorsf("pipeline-resolver/%s: invalid finding", tc.ID)
			}
		}
	}
	return nil
}

func validFindingOracle(finding findingOracle) bool {
	if finding.Severity != "" && finding.Severity != "info" && finding.Severity != "warning" && finding.Severity != "error" {
		return false
	}
	for key, value := range finding.Details {
		if !shortStrings(key, value) {
			return false
		}
	}
	if finding.Kind != analysis.LowScentAnchor.String() {
		return finding.Details == nil
	}
	wantKeys := []string{"anchorText", "scentScore", "sourceDocument", "suggestedAnchor", "targetDocument"}
	keys := make([]string, 0, len(finding.Details))
	for key := range finding.Details {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return finding.Severity == analysis.Info.String() && slices.Equal(keys, wantKeys)
}

func referenceAt(refs []reference.Reference, source sourceOracle) (reference.Reference, error) {
	var found []reference.Reference
	for _, ref := range refs {
		if ref.Origin.String() == source.Path && ref.Line == source.Line {
			found = append(found, ref)
		}
	}
	if len(found) != 1 {
		return reference.Reference{}, fmt.Errorf("source %s:%d matched %d references", source.Path, source.Line, len(found))
	}
	return found[0], nil
}

func comparePipelineReference(tc pipelineCase, got reference.Reference) error {
	if got.RawTarget != tc.Target || got.Fragment != tc.Fragment || got.Type.String() != tc.Type || got.AnchorText != tc.AnchorText || got.Health.String() != tc.Want.Health || got.Target.Kind.String() != tc.Want.Kind || got.Target.DocumentID.String() != tc.Want.Document || got.Target.Anchor != tc.Want.Anchor || got.Target.Directory != tc.Want.Directory || !equalIDStrings(got.Target.Children, tc.Want.Children) || !equalIDStrings(got.Candidates, tc.Want.Candidates) {
		return fmt.Errorf("reference got target=%q fragment=%q type=%s anchorText=%q health=%s kind=%s document=%q anchor=%q directory=%q children=%v candidates=%v; want target=%q fragment=%q type=%s anchorText=%q %+v", got.RawTarget, got.Fragment, got.Type, got.AnchorText, got.Health, got.Target.Kind, got.Target.DocumentID, got.Target.Anchor, got.Target.Directory, got.Target.Children, got.Candidates, tc.Target, tc.Fragment, tc.Type, tc.AnchorText, tc.Want)
	}
	return nil
}

func sameReference(a, b reference.Reference) bool {
	return a.Origin == b.Origin && a.RawTarget == b.RawTarget && a.Fragment == b.Fragment && a.Type == b.Type && a.Line == b.Line && a.AnchorText == b.AnchorText && a.Health == b.Health && a.Target.Kind == b.Target.Kind && a.Target.DocumentID == b.Target.DocumentID && a.Target.Anchor == b.Target.Anchor && a.Target.Directory == b.Target.Directory && slices.Equal(a.Target.Children, b.Target.Children) && slices.Equal(a.Candidates, b.Candidates)
}

func isolatedEdges(c *corpus.Corpus, ref reference.Reference, strict bool) []string {
	g := graphmodel.BuildReferenceGraph(c, []reference.Reference{ref}, graphmodel.BuildOptions{StrictDirectoryLinks: strict})
	return idStrings(g.ProjectionOut(ref.Origin))
}

func idStrings(ids []identity.DocumentID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func pipelineFindingAt(findings []analysis.Finding, tc pipelineCase) (*analysis.Finding, error) {
	var found []analysis.Finding
	for _, finding := range findings {
		kindMatches := isReferenceFinding(finding.Kind) || tc.CheckLowScent && finding.Kind == analysis.LowScentAnchor
		if kindMatches && finding.Location.Document.String() == tc.Source.Path && finding.Location.Line == tc.Source.Line {
			found = append(found, finding)
		}
	}
	if len(found) > 1 {
		return nil, fmt.Errorf("source %s:%d matched %d pipeline findings", tc.Source.Path, tc.Source.Line, len(found))
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

func isReferenceFinding(kind analysis.FindingKind) bool {
	return kind == analysis.BrokenLink || kind == analysis.BrokenAnchor || kind == analysis.Ambiguous
}

func isPipelineFinding(kind analysis.FindingKind) bool {
	return isReferenceFinding(kind) || kind == analysis.LowScentAnchor
}

func compareFinding(tc pipelineCase, got *analysis.Finding) error {
	if tc.Finding == nil {
		if got != nil {
			return fmt.Errorf("unexpected finding %s at %s:%d", got.Kind, got.Location.Document, got.Location.Line)
		}
		return nil
	}
	if got == nil {
		return fmt.Errorf("missing finding %+v", *tc.Finding)
	}
	if got.Kind.String() != tc.Finding.Kind || got.Location.Document.String() != tc.Finding.Path || got.Location.Line != tc.Finding.Line || tc.Finding.Severity != "" && got.Severity.String() != tc.Finding.Severity || tc.Finding.Details != nil && !maps.Equal(got.Details, tc.Finding.Details) {
		return fmt.Errorf("finding got %s (%s) at %s:%d details=%v want %+v", got.Kind, got.Severity, got.Location.Document, got.Location.Line, got.Details, *tc.Finding)
	}
	return nil
}

func compareEmittedFinding(tc pipelineCase, finding *analysis.Finding, emitted []emittedFindingView) error {
	if finding == nil {
		return nil
	}
	matches := make([]emittedFindingView, 0, 1)
	for _, candidate := range emitted {
		if candidate.ID == finding.ID {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return fmt.Errorf("finding ID %q matched %d entries", finding.ID, len(matches))
	}
	got := matches[0]
	want := tc.Finding
	if got.Kind != want.Kind || got.Document != want.Path || got.Line != want.Line || want.Severity != "" && got.Severity != want.Severity || want.Details != nil && !maps.Equal(got.Details, want.Details) {
		return fmt.Errorf("finding got kind=%s severity=%s document=%s line=%d details=%v want=%+v", got.Kind, got.Severity, got.Document, got.Line, got.Details, *want)
	}
	return nil
}

func viewContainsFinding(view emit.View, finding *analysis.Finding) bool {
	if finding == nil {
		return true
	}
	var findings []analysis.Finding
	switch finding.Kind {
	case analysis.BrokenLink:
		findings = view.BrokenLinks
	case analysis.BrokenAnchor:
		findings = view.BrokenAnchors
	case analysis.Ambiguous:
		findings = view.Ambiguous
	case analysis.LowScentAnchor:
		findings = view.LowScent
	default:
		return false
	}
	return slices.ContainsFunc(findings, func(candidate analysis.Finding) bool { return candidate.ID == finding.ID })
}

func observation(id string, ref reference.Reference, defaultEdges, strictEdges []string, finding *analysis.Finding) pipelineObservation {
	out := pipelineObservation{
		ID: id,
		Reference: referenceView{
			Path: ref.Origin.String(), Line: ref.Line, Target: ref.RawTarget,
			Fragment: ref.Fragment, Type: ref.Type.String(), AnchorText: ref.AnchorText, Health: ref.Health.String(),
			Kind: ref.Target.Kind.String(), Document: ref.Target.DocumentID.String(),
			Anchor: ref.Target.Anchor, Directory: ref.Target.Directory,
			Children: idStrings(ref.Target.Children), Candidates: idStrings(ref.Candidates),
		},
		EdgesDefault: slices.Clone(defaultEdges),
		EdgesStrict:  slices.Clone(strictEdges),
	}
	if finding != nil {
		out.Finding = &findingView{
			ID: finding.ID, Kind: finding.Kind.String(), Severity: finding.Severity.String(),
			Path: finding.Location.Document.String(), Line: finding.Location.Line, Details: maps.Clone(finding.Details),
		}
	}
	return out
}

func compareAggregateProjection(cases []pipelineCase, graph *graphmodel.ReferenceGraph, strict bool) error {
	wantBySource := map[string][]string{}
	for _, tc := range cases {
		edges := tc.EdgesDefault
		if strict && tc.EdgesStrict != nil {
			edges = *tc.EdgesStrict
		}
		wantBySource[tc.Source.Path] = append(wantBySource[tc.Source.Path], edges...)
	}
	sources := make([]string, 0, len(wantBySource))
	for source := range wantBySource {
		sources = append(sources, source)
	}
	slices.Sort(sources)
	for _, source := range sources {
		want := wantBySource[source]
		slices.Sort(want)
		want = slices.Compact(want)
		got := idStrings(graph.ProjectionOut(identity.DocumentID(source)))
		if !slices.Equal(got, want) {
			mode := "default"
			if strict {
				mode = "strict"
			}
			return fmt.Errorf("pipeline-resolver: %s aggregate projection from %s=%v want=%v", mode, source, got, want)
		}
	}
	return nil
}
