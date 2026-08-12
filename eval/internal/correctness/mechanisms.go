package correctness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

func validateMechanismGraph(family, id string, g mechanismGraph) error {
	if len(g.Documents) == 0 || len(g.Documents) > maxItems || len(g.Edges) > maxItems || !sortedUnique(g.Documents) {
		return fmt.Errorf("%s/%s: documents must be non-empty, sorted, unique, and bounded", family, id)
	}
	docs := stringSet(g.Documents)
	var previous [2]string
	for i, document := range g.Documents {
		if !safe(document) || !identity.IsMarkdownPath(document) {
			return fmt.Errorf("%s/%s: unsafe document %q", family, id, document)
		}
		_ = i
	}
	for i, edge := range g.Edges {
		_, sourceOK := docs[edge[0]]
		_, targetOK := docs[edge[1]]
		if !sourceOK || !targetOK || edge[0] == edge[1] || i > 0 && (edge[0] < previous[0] || edge[0] == previous[0] && edge[1] <= previous[1]) {
			return fmt.Errorf("%s/%s: edges must be valid, sorted, and unique", family, id)
		}
		previous = edge
	}
	return nil
}

func buildMechanismGraph(ctx context.Context, g mechanismGraph, reverse bool) (*graphmodel.ReferenceGraph, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c := corpus.NewCorpus()
	documents := slices.Clone(g.Documents)
	if reverse {
		slices.Reverse(documents)
	}
	for _, id := range documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := c.Add(&corpus.Document{ID: identity.DocumentID(id)}); err != nil {
			return nil, err
		}
	}
	c.Freeze()
	edges := slices.Clone(g.Edges)
	if reverse {
		slices.Reverse(edges)
	}
	refs := make([]reference.Reference, 0, len(edges))
	for _, edge := range edges {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		refs = append(refs, validMechanismRef(edge[0], edge[1]))
	}
	return graphmodel.BuildReferenceGraph(c, refs, graphmodel.BuildOptions{}), nil
}

func validMechanismRef(source, target string) reference.Reference {
	return reference.Reference{
		RawReference: reference.RawReference{Origin: identity.DocumentID(source), RawTarget: target, Type: reference.RelativeLink},
		Target:       reference.ResolvedTarget{Kind: reference.TargetDocument, DocumentID: identity.DocumentID(target)},
		Health:       reference.Valid,
	}
}

func runGaps(ctx context.Context, file *gapFile) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if file.SchemaVersion != SchemaVersion || file.Family != "knowledge-gap" || len(file.Cases) == 0 || len(file.Cases) > maxCases {
		return 0, errorsf("knowledge-gap: unsupported contract")
	}
	last := ""
	for _, tc := range file.Cases {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if !safe(tc.ID) || tc.ID <= last || tc.MinComponentSize < 0 || len(tc.Want) > maxItems {
			return 0, errorsf("knowledge-gap: cases must be safe, sorted, and bounded")
		}
		last = tc.ID
		if err := validateMechanismGraph(file.Family, tc.ID, tc.Graph); err != nil {
			return 0, err
		}
		var firstPairs [][2]string
		firstTruncated := false
		for run, reverse := range []bool{false, true} {
			g, err := buildMechanismGraph(ctx, tc.Graph, reverse)
			if err != nil {
				return 0, err
			}
			got := graphmodel.DetectGaps(g.WeaklyConnectedComponents(), graphmodel.GapOptions{MinComponentSize: tc.MinComponentSize})
			pairs := make([][2]string, len(got.Gaps))
			for i, gap := range got.Gaps {
				pairs[i] = [2]string{gap.ComponentA.String(), gap.ComponentB.String()}
				if gap.RepresentativeA != gap.ComponentA || gap.RepresentativeB != gap.ComponentB {
					return 0, fmt.Errorf("knowledge-gap/%s: representatives differ from sorted-min component IDs", tc.ID)
				}
			}
			if !slices.Equal(pairs, tc.Want) || got.Truncated != tc.Truncated {
				return 0, fmt.Errorf("knowledge-gap/%s: got=%v truncated=%v want=%v/%v", tc.ID, pairs, got.Truncated, tc.Want, tc.Truncated)
			}
			if run == 0 {
				firstPairs, firstTruncated = slices.Clone(pairs), got.Truncated
			} else if !slices.Equal(pairs, firstPairs) || got.Truncated != firstTruncated {
				return 0, fmt.Errorf("knowledge-gap/%s: reversed insertion changed ordered result or truncation", tc.ID)
			}
		}
	}
	return len(file.Cases), nil
}

const (
	v1SuggestionDefaultMinShared = 2
	v1SuggestionDefaultMaxFanout = 256
	v1SuggestionMaxResults       = 1000
)

type suggestionSemantics struct {
	defaultMinShared int
	defaultMaxFanout int
	maxResults       int
}

type derivedSuggestionResult struct {
	suggestions []suggestionWant
	truncated   bool
	hubsSkipped bool
}

func runSuggestions(ctx context.Context, file *suggestionFile) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if file.SchemaVersion != SchemaVersion || file.Family != "suggested-link" || file.NumericTolerance <= 0 || file.NumericTolerance > .001 || file.DefaultMinShared != v1SuggestionDefaultMinShared || file.DefaultMaxFanout != v1SuggestionDefaultMaxFanout || file.MaxResults != v1SuggestionMaxResults || len(file.Cases) == 0 || len(file.Cases) > maxCases {
		return 0, errorsf("suggested-link: unsupported v1 contract")
	}
	semantics := suggestionSemantics{file.DefaultMinShared, file.DefaultMaxFanout, file.MaxResults}
	last := ""
	for _, tc := range file.Cases {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if !safe(tc.ID) || tc.ID <= last || tc.MinShared < 0 || tc.MaxFanout < 0 || len(tc.Want) > maxItems {
			return 0, errorsf("suggested-link: cases must be safe, sorted, and bounded")
		}
		last = tc.ID
		if err := validateMechanismGraph(file.Family, tc.ID, tc.Graph); err != nil {
			return 0, err
		}
		if err := checkSuggestionCase(ctx, tc, file.NumericTolerance, semantics); err != nil {
			return 0, fmt.Errorf("suggested-link/%s: %w", tc.ID, err)
		}
	}
	return len(file.Cases), nil
}

func checkSuggestionCase(ctx context.Context, tc suggestionCase, tolerance float64, semantics suggestionSemantics) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	independent, err := deriveSuggestions(ctx, tc, semantics)
	if err != nil {
		return err
	}
	if err := compareSuggestionWant(independent, tc.Want, tc.Truncated, tc.HubsSkipped, tolerance, "hand-authored"); err != nil {
		return err
	}

	var snapshots [2][]byte
	for run, reverse := range []bool{false, true} {
		g, err := buildMechanismGraph(ctx, tc.Graph, reverse)
		if err != nil {
			return err
		}
		result := g.PredictLinks(graphmodel.LinkPredictionOptions{MinSharedNeighbours: tc.MinShared, MaxFanout: tc.MaxFanout})
		production := derivedSuggestionResult{
			suggestions: make([]suggestionWant, len(result.Suggestions)),
			truncated:   result.Truncated,
			hubsSkipped: result.HubsSkipped,
		}
		for i, suggestion := range result.Suggestions {
			production.suggestions[i] = suggestionWant{
				A: suggestion.DocA.String(), B: suggestion.DocB.String(),
				Shared: suggestion.SharedNeighbours, Coupling: suggestion.Coupling,
				CoCitation: suggestion.CoCitation, AdamicAdar: suggestion.AdamicAdar,
			}
		}
		if err := compareSuggestionWant(production, independent.suggestions, independent.truncated, independent.hubsSkipped, tolerance, "production"); err != nil {
			return err
		}
		snapshots[run], err = json.Marshal(result)
		if err != nil {
			return err
		}
	}
	if !bytes.Equal(snapshots[0], snapshots[1]) {
		return errorsf("result bytes changed under reversed document/edge insertion")
	}
	return nil
}

func deriveSuggestions(ctx context.Context, tc suggestionCase, semantics suggestionSemantics) (derivedSuggestionResult, error) {
	r, err := newRefGraph(ctx, graphCase{Documents: tc.Graph.Documents, Edges: tc.Graph.Edges})
	if err != nil {
		return derivedSuggestionResult{}, err
	}
	minShared := tc.MinShared
	if minShared <= 0 {
		minShared = semantics.defaultMinShared
	}
	maxFanout := tc.MaxFanout
	if maxFanout <= 0 {
		maxFanout = semantics.defaultMaxFanout
	}

	result := derivedSuggestionResult{}
	for _, neighbour := range r.docs {
		if err := ctx.Err(); err != nil {
			return derivedSuggestionResult{}, err
		}
		degree := len(r.und[neighbour])
		if degree >= 2 && degree > maxFanout {
			result.hubsSkipped = true
		}
	}
	for i, a := range r.docs {
		if err := ctx.Err(); err != nil {
			return derivedSuggestionResult{}, err
		}
		for _, b := range r.docs[i+1:] {
			if err := ctx.Err(); err != nil {
				return derivedSuggestionResult{}, err
			}
			if slices.Contains(r.und[a], b) {
				continue
			}
			common := intersection(r.und[a], r.und[b])
			eligible := make([]string, 0, len(common))
			for _, neighbour := range common {
				if len(r.und[neighbour]) <= maxFanout {
					eligible = append(eligible, neighbour)
				}
			}
			if len(eligible) < minShared {
				continue
			}
			aa := 0.0
			for _, neighbour := range eligible { // sorted: freezes float addition order
				aa += 1 / math.Log(float64(len(r.und[neighbour])))
			}
			result.suggestions = append(result.suggestions, suggestionWant{
				A: a, B: b, Shared: len(eligible),
				Coupling:   len(intersection(r.out[a], r.out[b])),
				CoCitation: len(intersection(r.in[a], r.in[b])), AdamicAdar: aa,
			})
		}
	}
	slices.SortFunc(result.suggestions, func(a, b suggestionWant) int {
		switch {
		case a.AdamicAdar > b.AdamicAdar:
			return -1
		case a.AdamicAdar < b.AdamicAdar:
			return 1
		case a.Shared > b.Shared:
			return -1
		case a.Shared < b.Shared:
			return 1
		case a.A < b.A:
			return -1
		case a.A > b.A:
			return 1
		case a.B < b.B:
			return -1
		case a.B > b.B:
			return 1
		default:
			return 0
		}
	})
	result.truncated = result.hubsSkipped
	if len(result.suggestions) > semantics.maxResults {
		result.suggestions = result.suggestions[:semantics.maxResults]
		result.truncated = true
	}
	return result, nil
}

func compareSuggestionWant(got derivedSuggestionResult, want []suggestionWant, truncated, hubsSkipped bool, tolerance float64, source string) error {
	if got.truncated != truncated || got.hubsSkipped != hubsSkipped || len(got.suggestions) != len(want) {
		return fmt.Errorf("%s flags/count got=%v/%v/%d want=%v/%v/%d", source, got.truncated, got.hubsSkipped, len(got.suggestions), truncated, hubsSkipped, len(want))
	}
	for i, actual := range got.suggestions {
		expected := want[i]
		if actual.A != expected.A || actual.B != expected.B || actual.Shared != expected.Shared || actual.Coupling != expected.Coupling || actual.CoCitation != expected.CoCitation || !close(actual.AdamicAdar, expected.AdamicAdar, tolerance) {
			return fmt.Errorf("%s suggestion %d got=%+v want=%+v", source, i, actual, expected)
		}
	}
	return nil
}

func runScent(ctx context.Context, file *scentFile) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if file.SchemaVersion != SchemaVersion || file.Family != "information-scent" || file.NumericTolerance <= 0 || file.NumericTolerance > .001 || len(file.Cases) == 0 || len(file.Cases) > maxCases {
		return 0, errorsf("information-scent: unsupported contract")
	}
	last := ""
	for _, tc := range file.Cases {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if !safe(tc.ID) || tc.ID <= last || len(tc.Documents) == 0 || len(tc.Documents) > maxItems || len(tc.Links) > maxItems || len(tc.Want) > maxItems {
			return 0, errorsf("information-scent: cases must be safe, sorted, and bounded")
		}
		last = tc.ID
		if err := checkScentCase(ctx, tc, file.NumericTolerance); err != nil {
			return 0, fmt.Errorf("information-scent/%s: %w", tc.ID, err)
		}
	}
	return len(file.Cases), nil
}

func checkScentCase(ctx context.Context, tc scentCase, tolerance float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, proof := range tc.TokenProof {
		if !slices.Equal(evalTokens(proof.Text), proof.Tokens) || !sortedUnique(proof.Tokens) {
			return fmt.Errorf("hand-authored token proof for %q differs: got=%v want=%v", proof.Text, evalTokens(proof.Text), proof.Tokens)
		}
	}
	c := corpus.NewCorpus()
	for _, declaration := range tc.Documents {
		if !safe(declaration.Path) || !identity.IsMarkdownPath(declaration.Path) || !shortString(declaration.Title) {
			return errorsf("unsafe document declaration")
		}
		root := &corpus.Section{Level: 0, StartLine: 1, EndLine: 1000}
		for i, heading := range declaration.Headings {
			section := &corpus.Section{Level: 1, Text: heading[0], Slug: heading[1], StartLine: i + 1, EndLine: i + 1, Parent: root}
			root.Children = append(root.Children, section)
		}
		if err := c.Add(&corpus.Document{ID: identity.DocumentID(declaration.Path), FrontMatter: corpus.FrontMatter{Title: declaration.Title}, Root: root}); err != nil {
			return err
		}
	}
	c.Freeze()
	refs := make([]reference.Reference, 0, len(tc.Links))
	for _, link := range tc.Links {
		if link.Line < 0 || !shortString(link.Anchor) {
			return errorsf("invalid link")
		}
		ref := validMechanismRef(link.Source, link.Target)
		ref.AnchorText, ref.Line = link.Anchor, link.Line
		if link.Section != "" {
			ref.Target.Kind, ref.Target.Anchor = reference.TargetSection, link.Section
		}
		refs = append(refs, ref)
	}
	got := graphmodel.BuildReferenceGraph(c, refs, graphmodel.BuildOptions{}).ComputeScent(c)
	if len(got) != len(tc.Want) {
		return fmt.Errorf("finding count got=%d want=%d: %+v", len(got), len(tc.Want), got)
	}
	for i, finding := range got {
		want := tc.Want[i]
		if finding.Source.String() != want.Source || finding.Target.String() != want.Target || finding.Line != want.Line || finding.AnchorText != want.Anchor || finding.Suggestion != want.Suggestion || !close(finding.Score, want.Score, tolerance) {
			return fmt.Errorf("finding %d got=%+v want=%+v", i, finding, want)
		}
	}
	return nil
}

// evalTokens is intentionally small and oracle-owned. Its stopword list contains
// only words exercised by the checked cases; it is not a copy of the product set.
func evalTokens(value string) []string {
	stop := map[string]bool{"and": true, "the": true, "to": true}
	fields := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) > 1 && !stop[field] {
			out = append(out, field)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
