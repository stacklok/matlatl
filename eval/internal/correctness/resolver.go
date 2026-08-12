package correctness

import (
	"context"
	"fmt"
	"slices"

	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

type oracleCatalog struct{ resolverCatalog }

func (c oracleCatalog) HasDocument(id identity.DocumentID) bool {
	return slices.Contains(c.Documents, id.String())
}
func (c oracleCatalog) DocumentIDs() []identity.DocumentID {
	out := make([]identity.DocumentID, len(c.Documents))
	for i, d := range c.Documents {
		out[i] = identity.DocumentID(d)
	}
	return out
}
func (c oracleCatalog) HasHeading(id identity.DocumentID, slug string) bool {
	return slices.Contains(c.Headings[id.String()], slug)
}
func (c oracleCatalog) LookupAlias(alias string) []identity.DocumentID {
	values := c.Aliases[alias]
	out := make([]identity.DocumentID, len(values))
	for i, d := range values {
		out[i] = identity.DocumentID(d)
	}
	return out
}

type assetRecorder struct {
	known map[string]struct{}
	calls []string
}

func (a *assetRecorder) AssetExists(p string) bool {
	a.calls = append(a.calls, p)
	_, ok := a.known[p]
	return ok
}

func runResolver(ctx context.Context, file *resolverFile) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := validateResolverFile(file); err != nil {
		return 0, err
	}
	cat := oracleCatalog{file.Catalog}
	for _, tc := range file.Cases {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		assets := &assetRecorder{known: stringSet(file.Catalog.Assets)}
		got := reference.NewResolver(cat, assets, parsePolicy(tc.Policy)).Resolve(reference.RawReference{Origin: identity.DocumentID(tc.Origin), RawTarget: tc.Target, Fragment: tc.Fragment, Type: parseType(tc.Type)})
		if got.Health.String() != tc.Want.Health || got.Target.Kind.String() != tc.Want.Kind || got.Target.DocumentID.String() != tc.Want.Document || got.Target.Anchor != tc.Want.Anchor || got.Target.Directory != tc.Want.Directory || !equalIDStrings(got.Target.Children, tc.Want.Children) || !equalIDStrings(got.Candidates, tc.Want.Candidates) || !slices.Equal(assets.calls, tc.Want.AssetCalls) {
			return 0, fmt.Errorf("direct-resolver/%s: got health=%s kind=%s document=%q anchor=%q directory=%q children=%v candidates=%v assetCalls=%v; want %+v", tc.ID, got.Health, got.Target.Kind, got.Target.DocumentID, got.Target.Anchor, got.Target.Directory, got.Target.Children, got.Candidates, assets.calls, tc.Want)
		}
	}
	return len(file.Cases), nil
}
func validateResolverFile(f *resolverFile) error {
	if f.SchemaVersion != SchemaVersion || f.Family != "direct-resolver" {
		return errorsf("direct-resolver: unsupported contract")
	}
	if len(f.Cases) == 0 || len(f.Cases) > maxCases || len(f.Catalog.Documents) > maxItems || len(f.Catalog.Assets) > maxItems || len(f.Catalog.Headings) > maxItems || len(f.Catalog.Aliases) > maxItems || !sortedUnique(f.Catalog.Documents) || !sortedUnique(f.Catalog.Assets) {
		return errorsf("direct-resolver: invalid bounded catalog/cases")
	}
	totalItems := len(f.Catalog.Documents) + len(f.Catalog.Assets)
	docs := stringSet(f.Catalog.Documents)
	for _, d := range f.Catalog.Documents {
		if !safe(d) || !identity.IsMarkdownPath(d) {
			return errorsf("direct-resolver: unsafe document")
		}
	}
	for _, asset := range f.Catalog.Assets {
		if !safe(asset) {
			return errorsf("direct-resolver: unsafe asset")
		}
	}
	for d, hs := range f.Catalog.Headings {
		totalItems += len(hs)
		if _, ok := docs[d]; !ok || !sortedUnique(hs) || !nonEmptyShortStrings(hs) {
			return errorsf("direct-resolver: invalid heading catalog")
		}
	}
	for alias, ids := range f.Catalog.Aliases {
		totalItems += len(ids)
		if alias == "" || !shortString(alias) || !sortedUnique(ids) {
			return errorsf("direct-resolver: aliases must be sorted unique")
		}
		for _, d := range ids {
			if _, ok := docs[d]; !ok {
				return errorsf("direct-resolver: alias targets unknown document")
			}
		}
	}
	if totalItems > maxItems {
		return errorsf("direct-resolver: catalog exceeds %d items", maxItems)
	}
	last := ""
	for _, tc := range f.Cases {
		if !safe(tc.ID) || tc.ID <= last {
			return errorsf("direct-resolver: case IDs must be safe, unique, and sorted")
		}
		last = tc.ID
		if _, ok := docs[tc.Origin]; !ok {
			return errorsf("direct-resolver/%s: unknown origin", tc.ID)
		}
		if !validTypeName(tc.Type) || !validPolicyName(tc.Policy) || !validHealthName(tc.Want.Health) || !validKindName(tc.Want.Kind) || !shortStrings(tc.Target, tc.Fragment, tc.Want.Document, tc.Want.Anchor, tc.Want.Directory) || !boundedSortedStrings(tc.Want.Children) || !boundedSortedStrings(tc.Want.Candidates) || len(tc.Want.AssetCalls) > maxItems || !shortStrings(tc.Want.AssetCalls...) {
			return errorsf("direct-resolver/%s: invalid enum, string, bound, or ordering", tc.ID)
		}
		for _, id := range append(slices.Clone(tc.Want.Children), tc.Want.Candidates...) {
			if _, ok := docs[id]; !ok {
				return errorsf("direct-resolver/%s: expected child or candidate is not a document", tc.ID)
			}
		}
		for _, call := range tc.Want.AssetCalls {
			if !safe(call) {
				return errorsf("direct-resolver/%s: unsafe expected asset call", tc.ID)
			}
		}
		if tc.Want.Document != "" && !safe(tc.Want.Document) || tc.Want.Directory != "" && !safe(tc.Want.Directory) {
			return errorsf("direct-resolver/%s: unsafe expected target", tc.ID)
		}
	}
	return nil
}
func parseType(s string) reference.LinkType {
	switch s {
	case "relative-link":
		return reference.RelativeLink
	case "wikilink":
		return reference.Wikilink
	case "anchor":
		return reference.Anchor
	case "image-embed":
		return reference.ImageEmbed
	case "transclusion":
		return reference.Transclusion
	case "frontmatter-related":
		return reference.FrontmatterRelated
	case "external":
		return reference.External
	default:
		return reference.LinkType(99)
	}
}
func validTypeName(s string) bool { return s == "unknown" || parseType(s).Valid() }
func validHealthName(s string) bool {
	switch s {
	case "unresolved", "valid", "broken", "broken-anchor", "non-note", "ambiguous", "external", "ignored":
		return true
	default:
		return false
	}
}
func validKindName(s string) bool {
	switch s {
	case "none", "document", "section", "asset", "external", "directory":
		return true
	default:
		return false
	}
}
func parsePolicy(s string) reference.ResolutionPolicy {
	switch s {
	case "exact":
		return reference.Exact
	case "basename":
		return reference.Basename
	default:
		return reference.LongestSuffix
	}
}
func validPolicyName(s string) bool {
	return s == "" || s == "exact" || s == "basename" || s == "longest-suffix"
}
func equalIDStrings(got []identity.DocumentID, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].String() != want[i] {
			return false
		}
	}
	return true
}
func nonEmptyShortStrings(values []string) bool {
	for _, value := range values {
		if value == "" || !shortString(value) {
			return false
		}
	}
	return true
}
func boundedSortedStrings(values []string) bool {
	return len(values) <= maxItems && (len(values) == 0 || sortedUnique(values)) && shortStrings(values...)
}

// Run loads and executes all v1 correctness families. The canonical navigation
// family remains owned by eval/internal/oracle and is counted by the command.
func Run(ctx context.Context, evalRoot string) (Counts, error) {
	if err := ctx.Err(); err != nil {
		return Counts{}, err
	}
	dir, err := oracleDir(evalRoot)
	if err != nil {
		return Counts{}, err
	}
	suite, err := loadSuite(ctx, evalRoot, dir)
	if err != nil {
		return Counts{}, err
	}
	if err := validateSuiteBudget(ctx, suite); err != nil {
		return Counts{}, err
	}
	return runSuite(ctx, suite)
}

func runSuite(ctx context.Context, suite *suiteFiles) (Counts, error) {
	graphs, err := runGraph(ctx, suite.graph)
	if err != nil {
		return Counts{}, err
	}
	resolvers, err := runResolver(ctx, suite.resolver)
	if err != nil {
		return Counts{}, err
	}
	pipeline, _, err := runPipeline(ctx, suite.pipeline, suite.fixtures[suite.pipeline.Fixture])
	if err != nil {
		return Counts{}, err
	}
	scent, err := runScent(ctx, suite.scent)
	if err != nil {
		return Counts{}, err
	}
	gaps, err := runGaps(ctx, suite.gaps)
	if err != nil {
		return Counts{}, err
	}
	suggestions, err := runSuggestions(ctx, suite.suggestions)
	if err != nil {
		return Counts{}, err
	}
	mutations, err := runMutations(ctx, suite.mutations, suite.fixtures)
	if err != nil {
		return Counts{}, err
	}
	backlinks, err := runBacklinks(ctx, suite.backlinks, suite.fixtures[suite.backlinks.Fixture])
	if err != nil {
		return Counts{}, err
	}
	trails, err := runTrails(ctx, suite.trails)
	if err != nil {
		return Counts{}, err
	}
	determinism, err := runDeterminism(ctx, suite.determinism, suite.fixtures[suite.determinism.Fixture])
	if err != nil {
		return Counts{}, err
	}
	return Counts{Graph: graphs, Resolver: resolvers, Pipeline: pipeline, Scent: scent, Gaps: gaps, Suggestions: suggestions, Mutations: mutations, Backlinks: backlinks, Trails: trails, Determinism: determinism}, nil
}
