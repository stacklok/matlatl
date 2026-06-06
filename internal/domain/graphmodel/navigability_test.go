package graphmodel

import (
	"math"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// navEps is the float tolerance for navigability comparisons. The metrics are
// small rationals, so 1e-9 is comfortably tight.
const navEps = 1e-9

func nearly(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > navEps {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

// TestNavigability_DirectedPath pins every scalar on the directed path
// A->B->C->D (N=4) by hand.
//
//	Directed distances (forward only): AB1 AC2 AD3 BC1 BD2 CD1 -> finite sum 10.
//	6 backward ordered pairs are unreachable, each charged K=N=4 -> +24.
//	sumCD = 34; nn = N^2-N = 12.
//	Cp = (nn*N - sumCD)/(nn*(N-1)) = (48-34)/36 = 14/36.
//	Stratum: a pure linear chain -> exactly 1.
//	Undirected closure A-B-C-D: distances {1,1,1,2,2,3} (per unordered pair);
//	mean = 10/6, median = (1+2)/2 = 1.5, diameter = 3,
//	ReachablePairs (ordered, finite) = 12.
//	Clustering: a path has no triangles -> 0.
func TestNavigability_DirectedPath(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"), validRef("C.md", "D.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	n := g.ComputeNavigability()

	if n.Documents != 4 {
		t.Errorf("Documents = %d, want 4", n.Documents)
	}
	nearly(t, "Compactness", n.Compactness, 14.0/36.0)
	nearly(t, "Stratum", n.Stratum, 1)
	nearly(t, "CPL", n.CharacteristicPathLength, 10.0/6.0)
	nearly(t, "Median", n.MedianPathLength, 1.5)
	if n.Diameter != 3 {
		t.Errorf("Diameter = %d, want 3", n.Diameter)
	}
	if n.ReachablePairs != 12 {
		t.Errorf("ReachablePairs = %d, want 12", n.ReachablePairs)
	}
	nearly(t, "Clustering", n.ClusteringCoefficient, 0)
}

// TestNavigability_DirectedCycle pins the pure-cycle case A->B->C->A (N=3):
// every node has equal in/out status, so Stratum == 0.
//
//	All 6 ordered pairs reachable; finite sum = (1+2)*3 = 9; nn = 6.
//	Cp = (6*3 - 9)/(6*2) = 9/12 = 0.75.
func TestNavigability_DirectedCycle(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"), validRef("C.md", "A.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	n := g.ComputeNavigability()

	nearly(t, "Stratum", n.Stratum, 0)
	nearly(t, "Compactness", n.Compactness, 0.75)
}

// TestNavigability_Star pins the directed out-star H->{L1..L4} (N=5):
//
//	Reachable: 4 pairs at distance 1 -> finite sum 4; 16 unreachable pairs charged
//	K=5 -> +80; sumCD = 84; nn = 20.
//	Cp = (20*5 - 84)/(20*4) = 16/80 = 0.2.
//	Stratum: statusOut[H]=4, statusIn[Li]=1; AP = 4 + 4 = 8; N odd -> LAP=(25-1)/2=12;
//	Stratum = 8/12 = 2/3.
//	Clustering: leaves are not adjacent, so the hub's local clustering is 0 (and
//	leaves have degree 1, excluded) -> global 0.
func TestNavigability_Star(t *testing.T) {
	c := buildCorpus(t,
		doc("H.md", "h", nil),
		doc("L1.md", "l1", nil), doc("L2.md", "l2", nil),
		doc("L3.md", "l3", nil), doc("L4.md", "l4", nil),
	)
	refs := []reference.Reference{
		validRef("H.md", "L1.md"), validRef("H.md", "L2.md"),
		validRef("H.md", "L3.md"), validRef("H.md", "L4.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	n := g.ComputeNavigability()

	nearly(t, "Compactness", n.Compactness, 0.2)
	nearly(t, "Stratum", n.Stratum, 8.0/12.0)
	nearly(t, "Clustering", n.ClusteringCoefficient, 0)
}

// TestNavigability_K4Mutual pins the mutually-complete K4 (every ordered pair
// linked both ways): Cp == 1, Stratum == 0, clustering == 1, CPL == 1.
func TestNavigability_K4Mutual(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
	)
	docs := []string{"A.md", "B.md", "C.md", "D.md"}
	var refs []reference.Reference
	for _, from := range docs {
		for _, to := range docs {
			if from != to {
				refs = append(refs, validRef(from, to))
			}
		}
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	n := g.ComputeNavigability()

	nearly(t, "Compactness", n.Compactness, 1)
	nearly(t, "Stratum", n.Stratum, 0)
	nearly(t, "Clustering", n.ClusteringCoefficient, 1)
	nearly(t, "CPL", n.CharacteristicPathLength, 1)
	nearly(t, "Median", n.MedianPathLength, 1)
	if n.Diameter != 1 {
		t.Errorf("Diameter = %d, want 1", n.Diameter)
	}
	if n.ReachablePairs != 12 {
		t.Errorf("ReachablePairs = %d, want 12", n.ReachablePairs)
	}
}

// TestNavigability_TwoDisconnectedPairs pins the two-component case A<->B, C<->D
// (N=4): compactness is low (most pairs unreachable) and the undirected
// path-length stats see only the within-pair finite distances.
//
//	Reachable directed: 4 pairs at d1; 8 unreachable charged K=4 -> sumCD=4+32=36;
//	nn=12; Cp = (48-36)/36 = 1/3.
//	Undirected finite ordered pairs: 4 (AB,BA,CD,DC) all d1.
func TestNavigability_TwoDisconnectedPairs(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "A.md"),
		validRef("C.md", "D.md"), validRef("D.md", "C.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	n := g.ComputeNavigability()

	nearly(t, "Compactness", n.Compactness, 1.0/3.0)
	if n.ReachablePairs != 4 {
		t.Errorf("ReachablePairs = %d, want 4", n.ReachablePairs)
	}
	nearly(t, "CPL", n.CharacteristicPathLength, 1)
	nearly(t, "Median", n.MedianPathLength, 1)
	if n.Diameter != 1 {
		t.Errorf("Diameter = %d, want 1", n.Diameter)
	}
	// Degree-1 nodes are excluded from clustering; none has degree >= 2.
	nearly(t, "Clustering", n.ClusteringCoefficient, 0)
}

// TestNavigability_TwoNodes pins the smallest graph past the n<=1 guard — a
// single directed edge A->B (N=2) — exercising the division terms nn=N^2-N=2 and
// (N-1)=1 just above the early return.
//
//	Directed: d(A,B)=1, B->A unreachable -> sumCD = 1 + K(=N=2) = 3; nn = 2.
//	Cp = (nn*N - sumCD)/(nn*(N-1)) = (4-3)/2 = 0.5.
//	Stratum: statusOut[A]=1, statusIn[B]=1 -> S(A)=-1,S(B)=1 -> AP=2; N even ->
//	LAP=N^2/2=2; Stratum = min(2/2,1) = 1.
//	Undirected A-B: 2 finite ordered pairs at d1 -> CPL=1, median=1, diameter=1.
//	Both nodes have degree 1 -> excluded from clustering -> 0.
func TestNavigability_TwoNodes(t *testing.T) {
	c := buildCorpus(t, doc("A.md", "a", nil), doc("B.md", "b", nil))
	g := BuildReferenceGraph(c, []reference.Reference{validRef("A.md", "B.md")}, BuildOptions{})
	n := g.ComputeNavigability()

	if n.Documents != 2 {
		t.Errorf("Documents = %d, want 2", n.Documents)
	}
	nearly(t, "Compactness", n.Compactness, 0.5)
	nearly(t, "Stratum", n.Stratum, 1)
	nearly(t, "CPL", n.CharacteristicPathLength, 1)
	nearly(t, "Median", n.MedianPathLength, 1)
	if n.Diameter != 1 {
		t.Errorf("Diameter = %d, want 1", n.Diameter)
	}
	if n.ReachablePairs != 2 {
		t.Errorf("ReachablePairs = %d, want 2", n.ReachablePairs)
	}
	nearly(t, "Clustering", n.ClusteringCoefficient, 0)
}

// TestNavigability_EmptyAndSingle: a 0- or 1-document corpus has no ordered
// pairs, so every metric is zero with no panic / div-by-zero.
func TestNavigability_EmptyAndSingle(t *testing.T) {
	empty := BuildReferenceGraph(buildCorpus(t), nil, BuildOptions{})
	en := empty.ComputeNavigability()
	if en != (Navigability{Documents: 0}) {
		t.Errorf("empty navigability = %+v, want zero with Documents=0", en)
	}

	single := BuildReferenceGraph(buildCorpus(t, doc("only.md", "o", nil)), nil, BuildOptions{})
	sn := single.ComputeNavigability()
	if sn != (Navigability{Documents: 1}) {
		t.Errorf("single navigability = %+v, want zero with Documents=1", sn)
	}
}

// TestNavigability_Deterministic: shuffling the reference insertion order must
// not change any scalar (the projection is rebuilt sorted, and all sums are in
// sorted order).
func TestNavigability_Deterministic(t *testing.T) {
	mk := func(refs []reference.Reference) Navigability {
		c := buildCorpus(t,
			doc("A.md", "a", nil), doc("B.md", "b", nil),
			doc("C.md", "c", nil), doc("D.md", "d", nil), doc("E.md", "e", nil),
		)
		return BuildReferenceGraph(c, refs, BuildOptions{}).ComputeNavigability()
	}
	forward := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"),
		validRef("C.md", "D.md"), validRef("D.md", "E.md"),
		validRef("A.md", "C.md"), validRef("E.md", "A.md"),
	}
	shuffled := []reference.Reference{
		validRef("E.md", "A.md"), validRef("A.md", "C.md"),
		validRef("C.md", "D.md"), validRef("A.md", "B.md"),
		validRef("D.md", "E.md"), validRef("B.md", "C.md"),
	}
	if mk(forward) != mk(shuffled) {
		t.Errorf("navigability differs under shuffled input:\n forward=%+v\nshuffled=%+v",
			mk(forward), mk(shuffled))
	}
}

// TestForEachSourceDistances_KnownDistances pins the BFS distances on the
// directed path A->B->C->D and confirms self-distance 0 plus unreachable absence.
func TestForEachSourceDistances_KnownDistances(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"), validRef("C.md", "D.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})

	got := map[identity.DocumentID]map[string]int{}
	g.ForEachSourceDistances(g.projAdj, func(src identity.DocumentID, dist map[identity.DocumentID]int) {
		m := map[string]int{}
		for k, v := range dist { // copy: the dist map is reused across sources
			m[k.String()] = v
		}
		got[src] = m
	})

	want := map[string]map[string]int{
		"A.md": {"A.md": 0, "B.md": 1, "C.md": 2, "D.md": 3},
		"B.md": {"B.md": 0, "C.md": 1, "D.md": 2},
		"C.md": {"C.md": 0, "D.md": 1},
		"D.md": {"D.md": 0},
	}
	for src, w := range want {
		g := got[identity.DocumentID(src)]
		if len(g) != len(w) {
			t.Errorf("source %s: got %v, want %v", src, g, w)
			continue
		}
		for dst, d := range w {
			if g[dst] != d {
				t.Errorf("source %s dist[%s] = %d, want %d", src, dst, g[dst], d)
			}
		}
	}
}

// TestForEachSourceDistances_Deterministic: shuffling reference insertion order
// yields identical visited distances (sorted source + neighbour expansion).
func TestForEachSourceDistances_Deterministic(t *testing.T) {
	collect := func(refs []reference.Reference) map[string]map[string]int {
		c := buildCorpus(t,
			doc("A.md", "a", nil), doc("B.md", "b", nil),
			doc("C.md", "c", nil), doc("D.md", "d", nil),
		)
		g := BuildReferenceGraph(c, refs, BuildOptions{})
		out := map[string]map[string]int{}
		g.ForEachSourceDistances(g.projAdj, func(src identity.DocumentID, dist map[identity.DocumentID]int) {
			m := map[string]int{}
			for k, v := range dist {
				m[k.String()] = v
			}
			out[src.String()] = m
		})
		return out
	}
	a := collect([]reference.Reference{
		validRef("A.md", "B.md"), validRef("A.md", "C.md"),
		validRef("B.md", "D.md"), validRef("C.md", "D.md"),
	})
	b := collect([]reference.Reference{
		validRef("C.md", "D.md"), validRef("A.md", "C.md"),
		validRef("B.md", "D.md"), validRef("A.md", "B.md"),
	})
	if len(a) != len(b) {
		t.Fatalf("source counts differ: %d vs %d", len(a), len(b))
	}
	for src, am := range a {
		bm := b[src]
		if len(am) != len(bm) {
			t.Errorf("source %s distances differ: %v vs %v", src, am, bm)
			continue
		}
		for dst, d := range am {
			if bm[dst] != d {
				t.Errorf("source %s dist[%s] differs: %d vs %d", src, dst, d, bm[dst])
			}
		}
	}
}
