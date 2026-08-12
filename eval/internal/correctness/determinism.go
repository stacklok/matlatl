package correctness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
	"github.com/stacklok/matlatl/eval/internal/harness"
	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	trailsemit "github.com/stacklok/matlatl/internal/infrastructure/emit/trails"
)

var stableFixtureTime = time.Unix(946684800, 0).UTC()

func runDeterminism(ctx context.Context, file *determinismFile, snapshot *fixtureSnapshot) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	wantArtifacts := []string{"findings.json", "graph.json", "index.md", "llms.txt", "trails.json"}
	wantOrders := []string{"forward", "reverse"}
	if file.SchemaVersion != SchemaVersion || file.Family != "artifact-determinism" || !safe(file.Fixture) || !slices.Equal(file.Artifacts, wantArtifacts) || !validDeterminismSentinels(file.Sentinels) || !slices.Equal(file.CreationOrders, wantOrders) || file.RunsPerOrder < 2 || file.RunsPerOrder > 10 {
		return 0, errorsf("artifact-determinism: unsupported contract")
	}
	var baseline map[string][]byte
	for _, order := range file.CreationOrders {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		root, err := materializeFixtureInOrder(ctx, snapshot, order == "reverse")
		if err != nil {
			return 0, err
		}
		defer func() { _ = os.RemoveAll(root) }()
		for run := 0; run < file.RunsPerOrder; run++ {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			artifacts, err := harness.EmitArtifacts(ctx, root)
			if err != nil {
				return 0, fmt.Errorf("artifact-determinism/%s/run-%d: %w", order, run+1, err)
			}
			if err := validateDeterminismArtifacts(file, artifacts); err != nil {
				return 0, fmt.Errorf("artifact-determinism/%s/run-%d: %w", order, run+1, err)
			}
			if baseline == nil {
				baseline = cloneArtifacts(artifacts)
				continue
			}
			for _, name := range file.Artifacts {
				if !bytes.Equal(artifacts[name], baseline[name]) {
					return 0, fmt.Errorf("artifact-determinism/%s/run-%d: %s bytes differ", order, run+1, name)
				}
			}
		}
	}
	return 1, nil
}

func validDeterminismSentinels(s determinismSentinels) bool {
	return safe(s.GraphDocument) && identity.IsMarkdownPath(s.GraphDocument) && shortString(s.FindingID) && s.FindingID != "" && safe(s.TrailRoot) && identity.IsMarkdownPath(s.TrailRoot) && shortString(s.IndexText) && s.IndexText != "" && shortString(s.LLMSText) && s.LLMSText != ""
}

func validateDeterminismArtifacts(file *determinismFile, artifacts map[string][]byte) error {
	missing := make([]string, 0)
	for _, name := range file.Artifacts {
		if _, ok := artifacts[name]; !ok {
			missing = append(missing, name)
		}
	}
	extra := make([]string, 0)
	for name := range artifacts {
		if !slices.Contains(file.Artifacts, name) {
			extra = append(extra, name)
		}
	}
	slices.Sort(extra)
	if len(missing) != 0 || len(extra) != 0 {
		return fmt.Errorf("artifact keys differ: missing=%v extra=%v", missing, extra)
	}

	var graph graphjson.Document
	if err := json.Unmarshal(artifacts["graph.json"], &graph); err != nil {
		return fmt.Errorf("decode graph.json: %w", err)
	}
	if graph.SchemaVersion != 7 || !slices.ContainsFunc(graph.Nodes, func(node graphjson.Node) bool { return node.ID == file.Sentinels.GraphDocument }) {
		return fmt.Errorf("graph.json missing schema 7 or document %q", file.Sentinels.GraphDocument)
	}
	var findings findingsWire
	if err := json.Unmarshal(artifacts["findings.json"], &findings); err != nil {
		return fmt.Errorf("decode findings.json: %w", err)
	}
	if findings.SchemaVersion != 8 || !slices.ContainsFunc(findings.Findings, func(finding emittedFindingView) bool { return finding.ID == file.Sentinels.FindingID }) {
		return fmt.Errorf("findings.json missing schema 8 or finding %q", file.Sentinels.FindingID)
	}
	var trails trailsemit.Document
	if err := json.Unmarshal(artifacts["trails.json"], &trails); err != nil {
		return fmt.Errorf("decode trails.json: %w", err)
	}
	if trails.SchemaVersion != 1 || !slices.ContainsFunc(trails.Trails, func(trail trailsemit.Trail) bool { return trail.Root == file.Sentinels.TrailRoot }) {
		return fmt.Errorf("trails.json missing schema 1 or trail root %q", file.Sentinels.TrailRoot)
	}
	if !strings.Contains(string(artifacts["index.md"]), file.Sentinels.IndexText) {
		return fmt.Errorf("index.md missing sentinel %q", file.Sentinels.IndexText)
	}
	if !strings.Contains(string(artifacts["llms.txt"]), file.Sentinels.LLMSText) {
		return fmt.Errorf("llms.txt missing sentinel %q", file.Sentinels.LLMSText)
	}
	return nil
}

func materializeFixtureInOrder(ctx context.Context, snapshot *fixtureSnapshot, reverse bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	entries := slices.Clone(snapshot.entries)
	if reverse {
		slices.Reverse(entries)
	}
	root, err := os.MkdirTemp("", "matlatl-determinism-")
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			_ = os.RemoveAll(root)
			return "", err
		}
		if err := evalfs.WriteExclusive(root, entry.path, entry.content); err != nil {
			_ = os.RemoveAll(root)
			return "", err
		}
		dest, err := evalfs.Path(root, entry.path)
		if err != nil {
			_ = os.RemoveAll(root)
			return "", err
		}
		if err := os.Chtimes(dest, stableFixtureTime, stableFixtureTime); err != nil {
			_ = os.RemoveAll(root)
			return "", err
		}
	}
	return root, nil
}

func cloneArtifacts(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for name, body := range in {
		out[name] = bytes.Clone(body)
	}
	return out
}

// pureGraphArtifact exercises production machine emitters after document and
// edge insertion are shuffled. Phase C performs the same reversal for every
// suggested-link case; this reuses each Phase A graph rather than adding cases.
func pureGraphArtifact(ctx context.Context, tc graphCase, reverse bool, farFromRootThreshold int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	declaration := mechanismGraph{Documents: tc.Documents, Edges: tc.Edges}
	g, err := buildMechanismGraph(ctx, declaration, reverse)
	if err != nil {
		return nil, err
	}
	c := corpus.NewCorpus()
	documents := slices.Clone(tc.Documents)
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	metrics := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{RootGlobs: tc.Roots, Gaps: graphmodel.GapOptions{MinComponentSize: 2}, InboundThreshold: 3, FarFromRootThreshold: farFromRootThreshold})
	view := emit.BuildView(application.Result{DocumentCount: len(tc.Documents), Corpus: c, Metrics: metrics})
	graph, err := graphjson.JSON(view)
	if err != nil {
		return nil, err
	}
	trails, err := trailsemit.JSON(view)
	if err != nil {
		return nil, err
	}
	return append(graph, trails...), nil
}
