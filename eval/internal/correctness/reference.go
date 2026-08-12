package correctness

import (
	"context"
	"math"
	"slices"
	"strings"

	"github.com/stacklok/matlatl/internal/domain/graphmodel"
)

func refHITS(ctx context.Context, r *refGraph) (map[string]float64, map[string]float64, error) {
	h, a := map[string]float64{}, map[string]float64{}
	for _, d := range r.docs {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		h[d], a[d] = 1, 1
	}
	for iter := 0; iter < 100 && len(r.docs) > 0; iter++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		nh, na := map[string]float64{}, map[string]float64{}
		for _, d := range r.docs {
			nh[d], na[d] = 0, 0
		}
		for _, d := range r.docs {
			for _, u := range r.in[d] {
				na[d] += h[u]
			}
		}
		for _, d := range r.docs {
			for _, v := range r.out[d] {
				nh[d] += na[v]
			}
		}
		if err := normalize(ctx, na, r.docs); err != nil {
			return nil, nil, err
		}
		if err := normalize(ctx, nh, r.docs); err != nil {
			return nil, nil, err
		}
		da, err := l2(ctx, a, na, r.docs)
		if err != nil {
			return nil, nil, err
		}
		dh, err := l2(ctx, h, nh, r.docs)
		if err != nil {
			return nil, nil, err
		}
		a, h = na, nh
		if da+dh < 1e-8 {
			break
		}
	}
	return h, a, nil
}

func normalize(ctx context.Context, m map[string]float64, docs []string) error {
	sum := 0.0
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return err
		}
		sum += m[d] * m[d]
	}
	if sum == 0 {
		return nil
	}
	n := math.Sqrt(sum)
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return err
		}
		m[d] /= n
	}
	return nil
}

func l2(ctx context.Context, a, b map[string]float64, docs []string) (float64, error) {
	s := 0.0
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		x := a[d] - b[d]
		s += x * x
	}
	return math.Sqrt(s), nil
}

func refPageRank(ctx context.Context, r *refGraph) (map[string]float64, error) {
	p := map[string]float64{}
	n := len(r.docs)
	if n == 0 {
		return p, ctx.Err()
	}
	for _, d := range r.docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p[d] = 1 / float64(n)
	}
	for iter := 0; iter < 100 && n > 1; iter++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dang := 0.0
		for _, d := range r.docs {
			if len(r.out[d]) == 0 {
				dang += p[d]
			}
		}
		np := map[string]float64{}
		for _, v := range r.docs {
			in := 0.0
			for _, u := range r.in[v] {
				in += p[u] / float64(len(r.out[u]))
			}
			np[v] = (1-.85)/float64(n) + .85*in + .85*dang/float64(n)
		}
		delta := 0.0
		for _, d := range r.docs {
			delta += math.Abs(np[d] - p[d])
		}
		p = np
		if delta < float64(n)*1e-6 {
			break
		}
	}
	return p, nil
}

type navRef struct {
	compactness, stratum, cpl, median, clustering float64
	diameter, reachable                           int
}

func refNavigability(ctx context.Context, r *refGraph) (navRef, error) {
	n := len(r.docs)
	if n <= 1 {
		return navRef{}, ctx.Err()
	}
	statusIn, statusOut := map[string]float64{}, map[string]float64{}
	sum := 0.0
	for _, s := range r.docs {
		if err := ctx.Err(); err != nil {
			return navRef{}, err
		}
		d, err := multiBFS(ctx, []string{s}, r.out)
		if err != nil {
			return navRef{}, err
		}
		for _, t := range r.docs {
			if s == t {
				continue
			}
			if x, ok := d[t]; ok {
				sum += float64(x)
				statusOut[s] += float64(x)
				statusIn[t] += float64(x)
			} else {
				sum += float64(n)
			}
		}
	}
	pairs := float64(n*n - n)
	cp := (pairs*float64(n) - sum) / (pairs * float64(n-1))
	ap := 0.0
	for _, d := range r.docs {
		if err := ctx.Err(); err != nil {
			return navRef{}, err
		}
		ap += math.Abs(statusIn[d] - statusOut[d])
	}
	lap := float64(n * n / 2)
	if n%2 == 1 {
		lap = float64((n*n - 1) / 2)
	}
	dists := []int{}
	diam := 0
	for _, s := range r.docs {
		if err := ctx.Err(); err != nil {
			return navRef{}, err
		}
		d, err := multiBFS(ctx, []string{s}, r.und)
		if err != nil {
			return navRef{}, err
		}
		for _, t := range r.docs {
			if x, ok := d[t]; s != t && ok {
				dists = append(dists, x)
				diam = max(diam, x)
			}
		}
	}
	slices.Sort(dists)
	cpl, med := 0.0, 0.0
	if len(dists) > 0 {
		total := 0
		for _, x := range dists {
			total += x
		}
		cpl = float64(total) / float64(len(dists))
		mid := len(dists) / 2
		if len(dists)%2 == 1 {
			med = float64(dists[mid])
		} else {
			med = float64(dists[mid-1]+dists[mid]) / 2
		}
	}
	cluster, count := 0.0, 0
	for _, v := range r.docs {
		if err := ctx.Err(); err != nil {
			return navRef{}, err
		}
		nb, k := r.und[v], len(r.und[v])
		if k < 2 {
			continue
		}
		links := 0
		for i := 0; i < k; i++ {
			for j := i + 1; j < k; j++ {
				if slices.Contains(r.und[nb[i]], nb[j]) {
					links++
				}
			}
		}
		cluster += float64(links) / float64(k*(k-1)/2)
		count++
	}
	if count > 0 {
		cluster /= float64(count)
	}
	return navRef{cp, math.Min(ap/lap, 1), cpl, med, cluster, diam, len(dists)}, nil
}

func refBetweenness(ctx context.Context, r *refGraph) (map[string]float64, error) {
	b := map[string]float64{}
	for _, d := range r.docs {
		b[d] = 0
	}
	n := len(r.docs)
	if n < 3 {
		return b, ctx.Err()
	}
	for _, s := range r.docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dist := map[string]int{s: 0}
		sigma := map[string]float64{s: 1}
		pred := map[string][]string{}
		q, order := []string{s}, []string{}
		for h := 0; h < len(q); h++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			v := q[h]
			order = append(order, v)
			for _, w := range r.out[v] {
				if _, ok := dist[w]; !ok {
					dist[w] = dist[v] + 1
					q = append(q, w)
				}
				if dist[w] == dist[v]+1 {
					sigma[w] += sigma[v]
					pred[w] = append(pred[w], v)
				}
			}
		}
		delta := map[string]float64{}
		for i := len(order) - 1; i >= 0; i-- {
			w := order[i]
			for _, v := range pred[w] {
				delta[v] += (sigma[v] / sigma[w]) * (1 + delta[w])
			}
			if w != s {
				b[w] += delta[w]
			}
		}
	}
	den := float64((n - 1) * (n - 2))
	for _, d := range r.docs {
		b[d] /= den
	}
	return b, nil
}

func bruteCritical(ctx context.Context, r *refGraph) ([]string, []string, error) {
	base, err := componentCount(ctx, r.docs, r.und, "", "")
	if err != nil {
		return nil, nil, err
	}
	aps := []string{}
	for _, v := range r.docs {
		count, err := componentCount(ctx, r.docs, r.und, v, "")
		if err != nil {
			return nil, nil, err
		}
		if count > base {
			aps = append(aps, v)
		}
	}
	bridges := []string{}
	for _, a := range r.docs {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		for _, b := range r.und[a] {
			if a >= b {
				continue
			}
			count, err := componentCount(ctx, r.docs, r.und, "", a+"|"+b)
			if err != nil {
				return nil, nil, err
			}
			if count > base {
				bridges = append(bridges, a+"|"+b)
			}
		}
	}
	return aps, bridges, nil
}

func componentCount(ctx context.Context, docs []string, adj map[string][]string, skip, edge string) (int, error) {
	seen := map[string]bool{}
	count := 0
	for _, s := range docs {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if s == skip || seen[s] {
			continue
		}
		count++
		q := []string{s}
		seen[s] = true
		for h := 0; h < len(q); h++ {
			v := q[h]
			for _, w := range adj[v] {
				if w == skip || seen[w] || v+"|"+w == edge || w+"|"+v == edge {
					continue
				}
				seen[w] = true
				q = append(q, w)
			}
		}
	}
	return count, nil
}

func compareTrails(ctx context.Context, m *graphmodel.GraphMetrics, r *refGraph, tol float64) error {
	_ = tol
	pr, err := refPageRank(ctx, r)
	if err != nil {
		return err
	}
	components, err := components(ctx, r.docs, r.und)
	if err != nil {
		return err
	}
	if len(m.Trails) != len(components) {
		return errorsf("trail count differs")
	}
	seen := map[string]bool{}
	for _, trail := range m.Trails {
		if err := ctx.Err(); err != nil {
			return err
		}
		order := make([]string, len(trail.Order))
		for i, d := range trail.Order {
			order[i] = d.String()
			if seen[order[i]] {
				return errorsf("duplicate trail member")
			}
			seen[order[i]] = true
		}
		members, err := componentsContaining(ctx, r.docs, r.und, trail.Root.String())
		if err != nil {
			return err
		}
		if !sameSet(order, members) {
			return errorsf("trail membership differs")
		}
		root := members[0]
		for _, d := range members[1:] {
			if pr[d] > pr[root] || pr[d] == pr[root] && d < root {
				root = d
			}
		}
		if trail.Root.String() != root {
			return errorsf("trail root got=%s want=%s", trail.Root, root)
		}
		pos := map[string]int{}
		for i, d := range order {
			pos[d] = i
		}
		scc, err := stronglyConnected(ctx, r)
		if err != nil {
			return err
		}
		rep := map[string]string{}
		for _, component := range scc {
			for _, d := range component {
				rep[d] = component[0]
			}
		}
		for a, outs := range r.out {
			for _, b := range outs {
				if rep[a] != rep[b] && pos[a] > pos[b] {
					return errorsf("trail violates condensation order %s -> %s", a, b)
				}
			}
		}
	}
	if len(seen) != len(r.docs) {
		return errorsf("trails omit documents")
	}
	return nil
}

func componentsContaining(ctx context.Context, docs []string, adj map[string][]string, root string) ([]string, error) {
	d, err := multiBFS(ctx, []string{root}, adj)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, x := range docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, ok := d[x]; ok {
			out = append(out, x)
		}
	}
	return out, nil
}

func sameSet(a, b []string) bool {
	aa, bb := slices.Clone(a), slices.Clone(b)
	slices.Sort(aa)
	slices.Sort(bb)
	return slices.Equal(aa, bb)
}

func compareGapsSuggestions(ctx context.Context, m *graphmodel.GraphMetrics, r *refGraph, tol float64) error {
	wcc, err := components(ctx, r.docs, r.und)
	if err != nil {
		return err
	}
	wantGaps := []string{}
	for i := 0; i < len(wcc); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(wcc[i]) < 2 {
			continue
		}
		for j := i + 1; j < len(wcc); j++ {
			if len(wcc[j]) >= 2 {
				wantGaps = append(wantGaps, wcc[i][0]+"|"+wcc[j][0])
			}
		}
	}
	gotGaps := make([]string, 0, len(m.Gaps))
	for _, gap := range m.Gaps {
		gotGaps = append(gotGaps, gap.ComponentA.String()+"|"+gap.ComponentB.String())
	}
	if !slices.Equal(gotGaps, wantGaps) {
		return errorsf("knowledge gaps got=%v want=%v", gotGaps, wantGaps)
	}
	type suggestion struct {
		a, b                         string
		shared, coupling, cocitation int
		aa                           float64
	}
	want := []suggestion{}
	for i, a := range r.docs {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, b := range r.docs[i+1:] {
			if slices.Contains(r.und[a], b) {
				continue
			}
			common := intersection(r.und[a], r.und[b])
			if len(common) < 2 {
				continue
			}
			aa := 0.0
			for _, c := range common {
				aa += 1 / math.Log(float64(len(r.und[c])))
			}
			want = append(want, suggestion{a, b, len(common), len(intersection(r.out[a], r.out[b])), len(intersection(r.in[a], r.in[b])), aa})
		}
	}
	slices.SortFunc(want, func(a, b suggestion) int {
		if a.aa > b.aa {
			return -1
		}
		if a.aa < b.aa {
			return 1
		}
		if a.shared > b.shared {
			return -1
		}
		if a.shared < b.shared {
			return 1
		}
		if x := strings.Compare(a.a, b.a); x != 0 {
			return x
		}
		return strings.Compare(a.b, b.b)
	})
	if len(m.SuggestedLinks) != len(want) {
		return errorsf("suggestion count got=%d want=%d", len(m.SuggestedLinks), len(want))
	}
	for i, got := range m.SuggestedLinks {
		if err := ctx.Err(); err != nil {
			return err
		}
		expected := want[i]
		if got.DocA.String() != expected.a || got.DocB.String() != expected.b || got.SharedNeighbours != expected.shared || got.Coupling != expected.coupling || got.CoCitation != expected.cocitation || !close(got.AdamicAdar, expected.aa, tol) {
			return errorsf("suggestion %d differs", i)
		}
	}
	return nil
}

func intersection(a, b []string) []string {
	out := []string{}
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	return out
}
