package correctness

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

type refGraph struct {
	docs         []string
	out, in, und map[string][]string
}

func runGraph(ctx context.Context, file *graphFile) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := validateGraphFile(file); err != nil {
		return 0, err
	}
	for _, tc := range file.Cases {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if err := checkGraphCase(ctx, tc, file.NumericTolerance, file.FarFromRootThreshold); err != nil {
			return 0, fmt.Errorf("canonical-graph/%s: %w", tc.ID, err)
		}
	}
	return len(file.Cases), nil
}

func validateGraphFile(file *graphFile) error {
	if file.SchemaVersion != SchemaVersion || file.Family != "canonical-graph" {
		return errorsf("canonical-graph: unsupported contract")
	}
	if file.NumericTolerance <= 0 || file.NumericTolerance > 0.001 || file.FarFromRootThreshold < 1 {
		return errorsf("canonical-graph: invalid tolerance or threshold")
	}
	if len(file.Cases) == 0 || len(file.Cases) > maxCases {
		return errorsf("canonical-graph: invalid case count")
	}
	last := ""
	for _, tc := range file.Cases {
		if !safe(tc.ID) || tc.ID <= last {
			return errorsf("canonical-graph: case IDs must be safe, unique, and sorted")
		}
		last = tc.ID
		if len(tc.Documents) > maxItems || len(tc.Edges) > maxItems || !sortedUnique(tc.Documents) || !sortedUnique(tc.Roots) {
			return errorsf("canonical-graph/%s: documents and roots must be sorted unique and bounded", tc.ID)
		}
		docSet := stringSet(tc.Documents)
		for _, d := range tc.Documents {
			if !safe(d) || !identity.IsMarkdownPath(d) {
				return errorsf("canonical-graph/%s: unsafe document %q", tc.ID, d)
			}
		}
		prev := [2]string{}
		for i, e := range tc.Edges {
			if _, ok := docSet[e[0]]; !ok {
				return errorsf("canonical-graph/%s: unknown edge source", tc.ID)
			}
			if _, ok := docSet[e[1]]; !ok || e[0] == e[1] {
				return errorsf("canonical-graph/%s: invalid edge", tc.ID)
			}
			if i > 0 && (e[0] < prev[0] || (e[0] == prev[0] && e[1] <= prev[1])) {
				return errorsf("canonical-graph/%s: edges must be sorted unique", tc.ID)
			}
			prev = e
		}
		for _, root := range tc.Roots {
			if _, ok := docSet[root]; !ok {
				return errorsf("canonical-graph/%s: unknown root", tc.ID)
			}
		}
	}
	return nil
}

func checkGraphCase(ctx context.Context, tc graphCase, tol float64, farThreshold int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c := corpus.NewCorpus()
	for _, d := range tc.Documents {
		if err := c.Add(&corpus.Document{ID: identity.DocumentID(d)}); err != nil {
			return err
		}
	}
	c.Freeze()
	refs := make([]reference.Reference, 0, len(tc.Edges))
	for _, e := range tc.Edges {
		refs = append(refs, reference.Reference{RawReference: reference.RawReference{Origin: identity.DocumentID(e[0]), RawTarget: e[1], Type: reference.RelativeLink}, Target: reference.ResolvedTarget{Kind: reference.TargetDocument, DocumentID: identity.DocumentID(e[1])}, Health: reference.Valid})
	}
	g := graphmodel.BuildReferenceGraph(c, refs, graphmodel.BuildOptions{})
	if err := ctx.Err(); err != nil {
		return err
	}
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{RootGlobs: tc.Roots, Gaps: graphmodel.GapOptions{MinComponentSize: 2}, InboundThreshold: 3, FarFromRootThreshold: farThreshold})
	r, err := newRefGraph(ctx, tc)
	if err != nil {
		return err
	}
	if err := compareBasic(ctx, tc, m, r, farThreshold); err != nil {
		return err
	}
	if err := compareComponents(ctx, m, r); err != nil {
		return err
	}
	if err := compareRanks(ctx, m, r, tol); err != nil {
		return err
	}
	if err := compareNavigability(ctx, m.Navigability, r, tol); err != nil {
		return err
	}
	if err := compareCritical(ctx, m, r, tol); err != nil {
		return err
	}
	if err := compareTrails(ctx, m, r, tol); err != nil {
		return err
	}
	if err := compareGapsSuggestions(ctx, m, r, tol); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	forward, err := pureGraphArtifact(ctx, tc, false, farThreshold)
	if err != nil {
		return err
	}
	reversed, err := pureGraphArtifact(ctx, tc, true, farThreshold)
	if err != nil {
		return err
	}
	if !slices.Equal(forward, reversed) {
		return errorsf("graph/trails artifact bytes changed under reversed document/edge insertion")
	}
	return nil
}

func newRefGraph(ctx context.Context, tc graphCase) (*refGraph, error) {
	r := &refGraph{docs: slices.Clone(tc.Documents), out: map[string][]string{}, in: map[string][]string{}, und: map[string][]string{}}
	for _, d := range r.docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.out[d] = []string{}
		r.in[d] = []string{}
	}
	for _, e := range tc.Edges {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.out[e[0]] = append(r.out[e[0]], e[1])
		r.in[e[1]] = append(r.in[e[1]], e[0])
	}
	for _, d := range r.docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.und[d] = sortedUnion(r.out[d], r.in[d])
	}
	return r, nil
}

func compareBasic(ctx context.Context, tc graphCase, m *graphmodel.GraphMetrics, r *refGraph, threshold int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	roots := slices.Clone(tc.Roots)
	if slices.Contains(tc.Documents, "README.md") && !slices.Contains(roots, "README.md") {
		roots = append(roots, "README.md")
		slices.Sort(roots)
	}
	indeterminate := len(roots) == 0
	if m.RootSet.Indeterminate != indeterminate || !equalIDs(m.RootSet.Roots, roots) {
		return errorsf("root set got=%v indeterminate=%v want=%v/%v", m.RootSet.Roots, m.RootSet.Indeterminate, roots, indeterminate)
	}
	dist, err := multiBFS(ctx, roots, r.out)
	if err != nil {
		return err
	}
	reached, unreachable, far := []string{}, []string{}, []string{}
	for _, d := range r.docs {
		if err := ctx.Err(); err != nil {
			return err
		}
		got, ok := m.Hops.Distance(identity.DocumentID(d))
		want, wok := dist[d]
		if ok != wok || ok && got != want {
			return errorsf("hops %s got=%d/%v want=%d/%v", d, got, ok, want, wok)
		}
		if wok {
			reached = append(reached, d)
			if want >= threshold && !slices.Contains(roots, d) {
				far = append(far, d)
			}
		} else if !indeterminate {
			unreachable = append(unreachable, d)
		}
	}
	if !equalIDs(m.Reachability.Reached, reached) || !equalIDs(m.Reachability.Unreachable, unreachable) || m.Reachability.Indeterminate != indeterminate || !equalIDs(m.Hops.FarFromRoot, far) {
		return errorsf("reachability/hops sets differ")
	}
	isolated, dead, under, reportUnreachable := []string{}, []string{}, []string{}, []string{}
	rootSet := stringSet(roots)
	isolatedSet := map[string]struct{}{}
	for _, d := range r.docs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, exempt := rootSet[d]; exempt {
			continue
		}
		in, out := len(r.in[d]), len(r.out[d])
		switch {
		case in == 0 && out == 0:
			isolated = append(isolated, d)
			isolatedSet[d] = struct{}{}
		case out == 0:
			dead = append(dead, d)
		case in < 3:
			under = append(under, d)
		}
	}
	for _, d := range unreachable {
		if _, iso := isolatedSet[d]; !iso {
			reportUnreachable = append(reportUnreachable, d)
		}
	}
	if !equalIDs(m.Orphans.Isolated, isolated) || !equalIDs(m.Orphans.DeadEnd, dead) || !equalIDs(m.Orphans.UnderLinked, under) || !equalIDs(m.Orphans.Unreachable, reportUnreachable) {
		return errorsf("structure ladder differs: got=%+v", m.Orphans)
	}
	return nil
}

func compareComponents(ctx context.Context, m *graphmodel.GraphMetrics, r *refGraph) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wcc, err := components(ctx, r.docs, r.und)
	if err != nil {
		return err
	}
	scc, err := stronglyConnected(ctx, r)
	if err != nil {
		return err
	}
	if !equalComponents(m.WCC, wcc) || !equalComponents(m.SCC, scc) {
		return errorsf("components differ")
	}
	if len(r.docs) == 0 {
		if m.Bowtie.GiantSCC != "" {
			return errorsf("empty bow-tie core")
		}
		return nil
	}
	giant := scc[0]
	for _, c := range scc[1:] {
		if len(c) > len(giant) {
			giant = c
		}
	}
	from, err := multiBFS(ctx, giant, r.out)
	if err != nil {
		return err
	}
	to, err := multiBFS(ctx, giant, r.in)
	if err != nil {
		return err
	}
	core := stringSet(giant)
	coreWCC := map[string]struct{}{}
	for _, c := range wcc {
		if slices.Contains(c, giant[0]) {
			coreWCC = stringSet(c)
		}
	}
	for _, d := range r.docs {
		if err := ctx.Err(); err != nil {
			return err
		}
		want := "disconnected"
		if _, ok := core[d]; ok {
			want = "core"
		} else if _, ok := to[d]; ok {
			want = "in"
		} else if _, ok := from[d]; ok {
			want = "out"
		} else if _, ok := coreWCC[d]; ok {
			want = "tendril"
		}
		if m.Bowtie.BucketOf(identity.DocumentID(d)).String() != want {
			return errorsf("bow-tie %s got=%s want=%s", d, m.Bowtie.BucketOf(identity.DocumentID(d)), want)
		}
	}
	return nil
}

func compareRanks(ctx context.Context, m *graphmodel.GraphMetrics, r *refGraph, tol float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hub, auth, err := refHITS(ctx, r)
	if err != nil {
		return err
	}
	pr, err := refPageRank(ctx, r)
	if err != nil {
		return err
	}
	for _, d := range r.docs {
		if err := ctx.Err(); err != nil {
			return err
		}
		gh, ga := m.HITS.Score(identity.DocumentID(d))
		if !close(gh, hub[d], tol) || !close(ga, auth[d], tol) {
			return errorsf("HITS %s got=%g/%g want=%g/%g", d, gh, ga, hub[d], auth[d])
		}
		if !close(m.PageRank.Score(identity.DocumentID(d)), pr[d], tol) {
			return errorsf("PageRank %s got=%g want=%g", d, m.PageRank.Score(identity.DocumentID(d)), pr[d])
		}
	}
	if !rankOrder(m.HITS.TopHubs(0), hub) {
		return errorsf("HITS hub ranking/tie order differs: got=%v want=%v", m.HITS.TopHubs(0), hub)
	}
	if !rankOrder(m.HITS.TopAuthorities(0), auth) {
		return errorsf("HITS authority ranking/tie order differs: got=%v want=%v", m.HITS.TopAuthorities(0), auth)
	}
	if !rankOrder(m.PageRank.Top(0), pr) {
		return errorsf("PageRank ranking/tie order differs: got=%v want=%v", m.PageRank.Top(0), pr)
	}
	return nil
}

func compareNavigability(ctx context.Context, got graphmodel.Navigability, r *refGraph, tol float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	want, err := refNavigability(ctx, r)
	if err != nil {
		return err
	}
	if got.Documents != len(r.docs) || got.ReachablePairs != want.reachable || got.Diameter != want.diameter || !close(got.Compactness, want.compactness, tol) || !close(got.Stratum, want.stratum, tol) || !close(got.CharacteristicPathLength, want.cpl, tol) || !close(got.MedianPathLength, want.median, tol) || !close(got.ClusteringCoefficient, want.clustering, tol) {
		return errorsf("navigability differs: got=%+v want=%+v", got, want)
	}
	return nil
}

func compareCritical(ctx context.Context, m *graphmodel.GraphMetrics, r *refGraph, tol float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	bet, err := refBetweenness(ctx, r)
	if err != nil {
		return err
	}
	for _, d := range r.docs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !close(m.Betweenness.Score(identity.DocumentID(d)), bet[d], tol) {
			return errorsf("betweenness %s differs", d)
		}
	}
	aps, bridges, err := bruteCritical(ctx, r)
	if err != nil {
		return err
	}
	if !equalIDs(m.Critical.ArticulationPoints, aps) {
		return errorsf("articulation points got=%v want=%v", m.Critical.ArticulationPoints, aps)
	}
	gotBr := make([]string, len(m.Critical.Bridges))
	for i, b := range m.Critical.Bridges {
		gotBr[i] = b.A.String() + "|" + b.B.String()
	}
	if !slices.Equal(gotBr, bridges) {
		return errorsf("bridges got=%v want=%v", gotBr, bridges)
	}
	return nil
}

func errorsf(format string, args ...any) error { return fmt.Errorf(format, args...) }
func close(a, b, t float64) bool               { return math.Abs(a-b) <= t }
func stringSet(v []string) map[string]struct{} {
	m := make(map[string]struct{}, len(v))
	for _, x := range v {
		m[x] = struct{}{}
	}
	return m
}
func equalIDs(ids []identity.DocumentID, stringsWant []string) bool {
	if len(ids) != len(stringsWant) {
		return false
	}
	for i := range ids {
		if ids[i].String() != stringsWant[i] {
			return false
		}
	}
	return true
}
func sortedUnion(a, b []string) []string {
	set := stringSet(a)
	for _, x := range b {
		set[x] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for x := range set {
		out = append(out, x)
	}
	slices.Sort(out)
	return out
}
func multiBFS(ctx context.Context, seeds []string, adj map[string][]string) (map[string]int, error) {
	d := map[string]int{}
	q := []string{}
	for _, s := range seeds {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, ok := adj[s]; ok {
			if _, seen := d[s]; !seen {
				d[s] = 0
				q = append(q, s)
			}
		}
	}
	for h := 0; h < len(q); h++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		v := q[h]
		for _, w := range adj[v] {
			if _, ok := d[w]; !ok {
				d[w] = d[v] + 1
				q = append(q, w)
			}
		}
	}
	return d, nil
}
func components(ctx context.Context, docs []string, adj map[string][]string) ([][]string, error) {
	seen := map[string]bool{}
	out := [][]string{}
	for _, s := range docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if seen[s] {
			continue
		}
		d, err := multiBFS(ctx, []string{s}, adj)
		if err != nil {
			return nil, err
		}
		component := []string{}
		for _, x := range docs {
			if _, ok := d[x]; ok {
				seen[x] = true
				component = append(component, x)
			}
		}
		out = append(out, component)
	}
	return out, nil
}
func stronglyConnected(ctx context.Context, r *refGraph) ([][]string, error) {
	reach := map[string]map[string]int{}
	for _, d := range r.docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var err error
		reach[d], err = multiBFS(ctx, []string{d}, r.out)
		if err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	out := [][]string{}
	for _, a := range r.docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if seen[a] {
			continue
		}
		component := []string{}
		for _, b := range r.docs {
			if !seen[b] {
				_, ab := reach[a][b]
				_, ba := reach[b][a]
				if ab && ba {
					component = append(component, b)
					seen[b] = true
				}
			}
		}
		out = append(out, component)
	}
	return out, nil
}
func equalComponents(got graphmodel.Components, want [][]string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !equalIDs(got[i].Members, want[i]) {
			return false
		}
	}
	return true
}
func rankOrder(got []graphmodel.RankedDocument, want map[string]float64) bool {
	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		if want[a] > want[b] {
			return -1
		}
		if want[a] < want[b] {
			return 1
		}
		return strings.Compare(a, b)
	})
	if len(got) != len(ids) {
		return false
	}
	for i := range got {
		if got[i].ID.String() != ids[i] {
			return false
		}
	}
	return true
}
