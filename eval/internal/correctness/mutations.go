package correctness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
	"github.com/stacklok/matlatl/eval/internal/harness"
)

func runMutations(ctx context.Context, file *mutationFile, snapshots map[string]*fixtureSnapshot) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if file.SchemaVersion != SchemaVersion || file.Family != "reversible-mutation" || !safe(file.Fixture) || len(file.Cases) == 0 || len(file.Cases) > maxCases {
		return 0, errorsf("reversible-mutation: unsupported or unsafe contract")
	}
	last := ""
	for _, tc := range file.Cases {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		validReplacement := func(value string) bool { return len(value) <= maxStringBytes && !strings.ContainsRune(value, '\x00') }
		if !safe(tc.ID) || tc.ID <= last || !safe(tc.Directory) || !safe(tc.Path) || tc.Old == tc.New || tc.Old == "" || !validReplacement(tc.Old) || !validReplacement(tc.New) || !validSHA256(tc.BaseHash) || !validSHA256(tc.FixtureHash) || !validMutationObservation(tc.Base) || !validMutationObservation(tc.Mutated) {
			return 0, errorsf("reversible-mutation: cases must have safe sorted IDs, valid hashes, and reversible bounded replacements")
		}
		last = tc.ID
		rel := filepath.ToSlash(filepath.Join(file.Fixture, tc.Directory))
		snapshot, ok := snapshots[rel]
		if !ok {
			return 0, fmt.Errorf("reversible-mutation/%s: fixture %q was not snapshotted", tc.ID, rel)
		}
		if err := runMutationCase(ctx, snapshot, tc); err != nil {
			return 0, fmt.Errorf("reversible-mutation/%s: %w", tc.ID, err)
		}
	}
	return len(file.Cases), nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func runMutationCase(ctx context.Context, snapshot *fixtureSnapshot, tc mutationCase) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if snapshot.hash != tc.FixtureHash {
		return fmt.Errorf("fixture tree hash mismatch: got %s want %s", snapshot.hash, tc.FixtureHash)
	}
	baseHash, err := snapshot.fileHash(tc.Path)
	if err != nil {
		return err
	}
	if baseHash != tc.BaseHash {
		return fmt.Errorf("base hash mismatch for %s: got %s want %s", tc.Path, baseHash, tc.BaseHash)
	}
	temp, err := snapshot.materialize(ctx, "matlatl-mutation-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(temp) }()
	copiedTreeHash, err := evalfs.TreeHash(temp)
	if err != nil {
		return err
	}
	if copiedTreeHash != tc.FixtureHash {
		return fmt.Errorf("copied fixture tree hash mismatch: got %s want %s", copiedTreeHash, tc.FixtureHash)
	}
	materializedHash, err := evalfs.FileHash(temp, tc.Path)
	if err != nil {
		return err
	}
	if materializedHash != tc.BaseHash {
		return fmt.Errorf("base hash mismatch for %s: got %s want %s", tc.Path, materializedHash, tc.BaseHash)
	}
	baseObservation, baseArtifact, err := mutationSnapshot(ctx, temp)
	if err != nil {
		return err
	}
	if err := compareRegisteredMutation(tc.Base, baseObservation); err != nil {
		return fmt.Errorf("base observation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := replaceExactlyOnce(temp, tc.Path, tc.Old, tc.New, tc.BaseHash); err != nil {
		return fmt.Errorf("mutation: %w", err)
	}
	mutatedObservation, _, err := mutationSnapshot(ctx, temp)
	if err != nil {
		return err
	}
	if err := compareRegisteredMutation(tc.Mutated, mutatedObservation); err != nil {
		return fmt.Errorf("mutated observation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	mutatedHash, err := evalfs.FileHash(temp, tc.Path)
	if err != nil {
		return err
	}
	if err := replaceExactlyOnce(temp, tc.Path, tc.New, tc.Old, mutatedHash); err != nil {
		return fmt.Errorf("inverse: %w", err)
	}
	restoredHash, err := evalfs.FileHash(temp, tc.Path)
	if err != nil {
		return err
	}
	if restoredHash != tc.BaseHash {
		return fmt.Errorf("inverse hash got %s want %s", restoredHash, tc.BaseHash)
	}
	restoredTreeHash, err := evalfs.TreeHash(temp)
	if err != nil {
		return err
	}
	if restoredTreeHash != tc.FixtureHash {
		return fmt.Errorf("inverse fixture tree hash got %s want %s", restoredTreeHash, tc.FixtureHash)
	}
	restoredObservation, restoredArtifact, err := mutationSnapshot(ctx, temp)
	if err != nil {
		return err
	}
	if !slices.Equal(baseArtifact, restoredArtifact) {
		return errorsf("inverse did not restore graph artifact bytes")
	}
	baseJSON, _ := json.Marshal(baseObservation)
	restoredJSON, _ := json.Marshal(restoredObservation)
	if !slices.Equal(baseJSON, restoredJSON) {
		return errorsf("inverse did not restore normalized observations")
	}
	return nil
}

func replaceExactlyOnce(root, rel, old, replacement, expectedHash string) error {
	if !safe(rel) {
		return fmt.Errorf("unsafe path %q", rel)
	}
	path, err := evalfs.Path(root, rel)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("replacement target is not a regular non-symlink file")
	}
	content, err := evalfs.Read(root, rel)
	if err != nil {
		return err
	}
	actualHash := sha256.Sum256(content)
	if hex.EncodeToString(actualHash[:]) != expectedHash {
		return errorsf("hash mismatch before replacement")
	}
	if count := bytesCount(content, []byte(old)); count != 1 {
		return fmt.Errorf("replacement must match exactly once, matched %d", count)
	}
	updated := []byte(strings.Replace(string(content), old, replacement, 1))
	return os.WriteFile(path, updated, info.Mode().Perm())
}

func bytesCount(content, needle []byte) int {
	return strings.Count(string(content), string(needle))
}

func mutationSnapshot(ctx context.Context, root string) (mutationObservation, []byte, error) {
	if err := ctx.Err(); err != nil {
		return mutationObservation{}, nil, err
	}
	run, err := harness.AnalyzePipeline(ctx, root, false)
	if err != nil {
		return mutationObservation{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return mutationObservation{}, nil, err
	}
	artifact, err := harness.EmitGraph(ctx, root)
	if err != nil {
		return mutationObservation{}, nil, err
	}
	observation := mutationObservation{}
	for _, source := range run.Result.Metrics.Graph.Documents() {
		if err := ctx.Err(); err != nil {
			return mutationObservation{}, nil, err
		}
		for _, target := range run.Result.Metrics.Graph.ProjectionOut(source) {
			observation.Edges = append(observation.Edges, source.String()+"->"+target.String())
		}
	}
	for _, finding := range run.Result.Report.Findings() {
		observation.Findings = append(observation.Findings, fmt.Sprintf("%s|%s|%d", finding.Kind, finding.Location.Document, finding.Location.Line))
	}
	for _, component := range run.Result.Metrics.WCC {
		members := make([]string, len(component.Members))
		for i, member := range component.Members {
			members[i] = member.String()
		}
		observation.WCC = append(observation.WCC, strings.Join(members, ","))
	}
	observation.FarFromRoot = idStrings(run.Result.Metrics.Hops.FarFromRoot)
	observation.Articulations = idStrings(run.Result.Metrics.Critical.ArticulationPoints)
	for _, bridge := range run.Result.Metrics.Critical.Bridges {
		observation.Bridges = append(observation.Bridges, bridge.A.String()+"|"+bridge.B.String())
	}
	return observation, artifact, nil
}

func validMutationObservation(observation mutationObservation) bool {
	sortedGroups := [][]string{
		observation.Edges, observation.WCC, observation.FarFromRoot,
		observation.Articulations, observation.Bridges,
	}
	for _, group := range sortedGroups {
		if group == nil || len(group) > maxItems || !sortedUnique(group) || !shortStrings(group...) {
			return false
		}
	}
	return observation.Findings != nil && len(observation.Findings) <= maxItems && shortStrings(observation.Findings...)
}

func compareRegisteredMutation(want, got mutationObservation) error {
	checks := []struct {
		name string
		want []string
		got  []string
	}{
		{"edges", want.Edges, got.Edges}, {"findings", want.Findings, got.Findings}, {"wcc", want.WCC, got.WCC},
		{"farFromRoot", want.FarFromRoot, got.FarFromRoot}, {"articulations", want.Articulations, got.Articulations}, {"bridges", want.Bridges, got.Bridges},
	}
	for _, check := range checks {
		if !slices.Equal(check.want, check.got) {
			return fmt.Errorf("%s got=%v want=%v", check.name, check.got, check.want)
		}
	}
	return nil
}
