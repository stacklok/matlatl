package nimbus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
	"github.com/stacklok/matlatl/eval/internal/harness"
	"github.com/stacklok/matlatl/internal/infrastructure/emit/graphjson"
	"github.com/stacklok/matlatl/internal/infrastructure/mcpserver"
)

// ToolNames is the sorted allowlist of MCP tools exercised by Nimbus.
var ToolNames = []string{"corpus-summary", "critical-docs", "get-section", "list-orphans", "path-between", "suggest-links", "what-links-to"}

// FilesystemEvent records one treatment-surface open.
type FilesystemEvent struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
}

// ToolEvent records one MCP request and response.
type ToolEvent struct {
	Tool      string `json:"tool"`
	Arguments string `json:"arguments"`
	Response  string `json:"response"`
}

// ProbeManifest contains the event-derived treatment access evidence.
type ProbeManifest struct {
	SchemaVersion    int               `json:"schemaVersion"`
	FilesystemEvents []FilesystemEvent `json:"filesystemEvents"`
	ToolEvents       []ToolEvent       `json:"toolEvents"`
	ClaimBoundary    string            `json:"claimBoundary"`
}

// AccessSummary is the deterministic aggregation of probe events.
type AccessSummary struct {
	LLMS, Trails, TotalTools int
	PerTool                  []ToolCount
	Calls                    []ToolCall
}

// ToolCount records calls to one allowlisted MCP tool.
type ToolCount struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
}

// ToolCall records the stable hashes of one MCP exchange.
type ToolCall struct{ Tool, ArgumentsSHA256, ResponseSHA256 string }

// SummarizeAccess validates and aggregates a probe manifest.
func SummarizeAccess(p ProbeManifest) (AccessSummary, error) {
	if p.SchemaVersion != 1 || !strings.Contains(p.ClaimBoundary, "not kernel-level") {
		return AccessSummary{}, errors.New("invalid access claim boundary")
	}
	s := AccessSummary{}
	counts := map[string]int{}
	for _, e := range p.FilesystemEvents {
		if e.Operation != "open" {
			return s, fmt.Errorf("unsupported filesystem operation %q", e.Operation)
		}
		switch e.Path {
		case "llms.txt":
			s.LLMS++
		case "trails.json":
			s.Trails++
		default:
			return s, fmt.Errorf("untracked treatment surface %q", e.Path)
		}
	}
	for _, e := range p.ToolEvents {
		if !slices.Contains(ToolNames, e.Tool) {
			return s, fmt.Errorf("unknown MCP tool %q", e.Tool)
		}
		counts[e.Tool]++
		s.TotalTools++
		s.Calls = append(s.Calls, ToolCall{e.Tool, hashText(e.Arguments), hashText(e.Response)})
	}
	for _, name := range ToolNames {
		s.PerTool = append(s.PerTool, ToolCount{name, counts[name]})
	}
	return s, nil
}

// Telemetry is the required calibration-only usage record.
type Telemetry struct {
	SchemaVersion   int     `json:"schemaVersion"`
	CalibrationOnly bool    `json:"calibrationOnly"`
	CacheRead       *uint64 `json:"cacheRead"`
	CacheWrite      *uint64 `json:"cacheWrite"`
	InputTokens     uint64  `json:"inputTokens"`
	OutputTokens    uint64  `json:"outputTokens"`
	Turns           uint64  `json:"turns"`
	ToolCalls       uint64  `json:"toolCalls"`
	BilledMicros    uint64  `json:"billedMicros"`
	WallMillis      uint64  `json:"wallMillis"`
}

// QualificationCandidate is one synthetic baseline-only model candidate.
type QualificationCandidate struct {
	Name                   string          `json:"name"`
	BaselineOnly           bool            `json:"baselineOnly"`
	CompetencePassed       bool            `json:"competencePassed"`
	ProtocolReliabilityPPM uint64          `json:"protocolReliabilityPpm"`
	TelemetryComplete      bool            `json:"telemetryComplete"`
	ProjectedCostMicros    uint64          `json:"projectedCostMicros"`
	BudgetMicros           uint64          `json:"budgetMicros"`
	DeterministicTieBreak  string          `json:"deterministicTieBreak"`
	CalibrationOnly        bool            `json:"calibrationOnly"`
	TreatmentGenerated     bool            `json:"treatmentGenerated"`
	Telemetry              json.RawMessage `json:"telemetry"`
}

// ValidateQualificationCandidate validates a synthetic baseline-only candidate.
func ValidateQualificationCandidate(data []byte) (QualificationCandidate, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return QualificationCandidate{}, err
	}
	required := []string{"name", "baselineOnly", "competencePassed", "protocolReliabilityPpm", "telemetryComplete", "projectedCostMicros", "budgetMicros", "deterministicTieBreak", "calibrationOnly", "treatmentGenerated", "telemetry"}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return QualificationCandidate{}, fmt.Errorf("candidate field %s is required", name)
		}
	}
	var c QualificationCandidate
	if err := decodeStrict(data, &c); err != nil {
		return c, err
	}
	if c.Name == "" || !c.BaselineOnly || !c.CompetencePassed || c.ProtocolReliabilityPPM > 1_000_000 || !c.TelemetryComplete || c.ProjectedCostMicros > c.BudgetMicros || c.DeterministicTieBreak == "" || !c.CalibrationOnly || c.TreatmentGenerated {
		return c, errors.New("candidate does not satisfy synthetic baseline-only qualification contract")
	}
	if _, err := ValidateTelemetry(c.Telemetry); err != nil {
		return c, fmt.Errorf("candidate telemetry: %w", err)
	}
	return c, nil
}

// ValidateTelemetry validates complete calibration-only telemetry.
func ValidateTelemetry(data []byte) (Telemetry, error) {
	var wire struct {
		SchemaVersion   *uint64 `json:"schemaVersion"`
		CalibrationOnly *bool   `json:"calibrationOnly"`
		CacheRead       *uint64 `json:"cacheRead"`
		CacheWrite      *uint64 `json:"cacheWrite"`
		InputTokens     *uint64 `json:"inputTokens"`
		OutputTokens    *uint64 `json:"outputTokens"`
		Turns           *uint64 `json:"turns"`
		ToolCalls       *uint64 `json:"toolCalls"`
		BilledMicros    *uint64 `json:"billedMicros"`
		WallMillis      *uint64 `json:"wallMillis"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return Telemetry{}, err
	}
	if wire.SchemaVersion == nil || wire.CalibrationOnly == nil || wire.CacheRead == nil || wire.CacheWrite == nil || wire.InputTokens == nil || wire.OutputTokens == nil || wire.Turns == nil || wire.ToolCalls == nil || wire.BilledMicros == nil || wire.WallMillis == nil {
		return Telemetry{}, errors.New("every telemetry field is required")
	}
	if *wire.SchemaVersion != 1 || !*wire.CalibrationOnly {
		return Telemetry{}, errors.New("telemetry is not calibration schema v1")
	}
	if *wire.CacheRead != 0 || *wire.CacheWrite != 0 {
		return Telemetry{}, errors.New("cache counters must be zero")
	}
	return Telemetry{SchemaVersion: 1, CalibrationOnly: true, CacheRead: wire.CacheRead, CacheWrite: wire.CacheWrite, InputTokens: *wire.InputTokens, OutputTokens: *wire.OutputTokens, Turns: *wire.Turns, ToolCalls: *wire.ToolCalls, BilledMicros: *wire.BilledMicros, WallMillis: *wire.WallMillis}, nil
}

// ValidateMCPTransport permits only loopback streamable HTTP MCP endpoints.
func ValidateMCPTransport(kind, endpoint string) error {
	if kind != "remote" {
		return errors.New("MCP transport must be remote HTTP; local and stdio are forbidden")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Path != "/mcp" || u.Port() == "" {
		return errors.New("MCP endpoint must be loopback streamable HTTP /mcp")
	}
	return nil
}

func calibrateFilesystemAccessCounts() error {
	for _, surface := range []string{"llms.txt", "trails.json"} {
		for _, want := range []int{0, 1, 3} {
			manifest := ProbeManifest{SchemaVersion: 1, ClaimBoundary: "event-derived application opens; not kernel-level access evidence"}
			for range want {
				manifest.FilesystemEvents = append(manifest.FilesystemEvents, FilesystemEvent{Operation: "open", Path: surface})
			}
			summary, err := SummarizeAccess(manifest)
			if err != nil {
				return err
			}
			got, other := summary.LLMS, summary.Trails
			if surface == "trails.json" {
				got, other = summary.Trails, summary.LLMS
			}
			if got != want || other != 0 {
				return fmt.Errorf("%s access calibration got=(%d,%d), want=(%d,0)", surface, got, other, want)
			}
		}
	}
	return nil
}

// Probe runs the OCI-free deterministic Nimbus calibration gates.
func Probe(ctx context.Context, s *Suite) (string, error) {
	if err := CheckFreeze(s); err != nil {
		return "", err
	}
	if err := calibrateFilesystemAccessCounts(); err != nil {
		return "", err
	}
	artifacts, err := harness.EmitArtifacts(ctx, s.Repository)
	if err != nil {
		return "", err
	}
	for _, name := range []string{"llms.txt", "trails.json"} {
		if len(artifacts[name]) == 0 {
			return "", fmt.Errorf("empty %s", name)
		}
	}
	if err := probeTopology(s, artifacts["graph.json"]); err != nil {
		return "", err
	}
	p, err := runMechanicalProbe(ctx, s, artifacts)
	if err != nil {
		return "", err
	}
	summary, err := SummarizeAccess(p)
	if err != nil {
		return "", err
	}
	if summary.LLMS != 2 || summary.Trails != 1 || summary.TotalTools != 8 {
		return "", fmt.Errorf("access totals do not match observed probe events")
	}
	for _, c := range summary.PerTool {
		want := 1
		if c.Tool == "path-between" {
			want = 2
		}
		if c.Count != want {
			return "", fmt.Errorf("tool %s count=%d, want %d", c.Tool, c.Count, want)
		}
	}
	if err := ValidateMCPTransport("remote", "http://127.0.0.1:8080/mcp"); err != nil {
		return "", err
	}
	for _, bad := range [][2]string{{"stdio", ""}, {"local", "/mcp"}, {"remote", "http://localhost:8080/mcp"}} {
		if ValidateMCPTransport(bad[0], bad[1]) == nil {
			return "", errors.New("unsafe MCP transport accepted")
		}
	}
	if err := probeQualification(s); err != nil {
		return "", err
	}
	if err := probeAttempts(s); err != nil {
		return "", err
	}
	return fmt.Sprintf("Nimbus calibration probes passed: %d tasks, %d documents, %d event-derived MCP calls; no directional arm statistic\n", len(s.Tasks), countMarkdown(s.Repository), summary.TotalTools), nil
}

func runMechanicalProbe(ctx context.Context, s *Suite, artifacts map[string][]byte) (result ProbeManifest, retErr error) {
	root, err := os.MkdirTemp("", "nimbus-probe-*")
	if err != nil {
		return ProbeManifest{}, err
	}
	defer func() { retErr = errors.Join(retErr, os.RemoveAll(root)) }()
	baseline := filepath.Join(root, "baseline")
	all := filepath.Join(root, "all")
	for _, dst := range []string{baseline, all} {
		if _, err := copyRepository(s.Repository, dst); err != nil {
			return ProbeManifest{}, err
		}
		if err := os.WriteFile(filepath.Join(dst, "llms.txt"), []byte("stale\n"), 0o600); err != nil {
			return ProbeManifest{}, err
		}
		if err := os.WriteFile(filepath.Join(dst, "trails.json"), []byte("{}\n"), 0o600); err != nil {
			return ProbeManifest{}, err
		}
		config := []byte(`{"theme":"native-preserved","mcp":{"native":{"type":"remote","url":"https://native.invalid/mcp"},"matlatl":{"type":"local","command":["stale"]}}}`)
		if err := os.WriteFile(filepath.Join(dst, "opencode.json"), config, 0o600); err != nil {
			return ProbeManifest{}, err
		}
	}
	if err := os.Remove(filepath.Join(baseline, "llms.txt")); err != nil {
		return ProbeManifest{}, err
	}
	if err := os.Remove(filepath.Join(baseline, "trails.json")); err != nil {
		return ProbeManifest{}, err
	}
	const nativeConfig = `{"theme":"native-preserved","mcp":{"native":{"type":"remote","url":"https://native.invalid/mcp"}}}`
	if err := os.WriteFile(filepath.Join(baseline, "opencode.json"), []byte(nativeConfig), 0o600); err != nil {
		return ProbeManifest{}, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return ProbeManifest{}, err
	}
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	serveErr := make(chan error, 1)
	endpoint := "http://" + listener.Addr().String() + "/mcp"
	for _, name := range []string{"llms.txt", "trails.json"} {
		if err := os.WriteFile(filepath.Join(all, name), artifacts[name], 0o600); err != nil {
			return ProbeManifest{}, err
		}
	}
	const notice = "matlatl availability is frozen for this calibration"
	config := fmt.Sprintf(`{"theme":"native-preserved","mcp":{"native":{"type":"remote","url":"https://native.invalid/mcp"},"matlatl":{"type":"remote","url":%q}},"notice":%q}`, endpoint, notice)
	if err := os.WriteFile(filepath.Join(all, "opencode.json"), []byte(config), 0o600); err != nil {
		return ProbeManifest{}, err
	}
	go func() { serveErr <- mcpserver.ServeListener(serveCtx, all, listener) }()
	for _, rel := range mustFiles(s.Repository) {
		want, _ := evalfs.Read(s.Repository, rel)
		for _, arm := range []string{baseline, all} {
			got, readErr := os.ReadFile(filepath.Join(arm, filepath.FromSlash(rel)))
			if readErr != nil || !slices.Equal(got, want) {
				return ProbeManifest{}, fmt.Errorf("common parity changed %s", rel)
			}
		}
	}
	for _, name := range []string{"llms.txt", "trails.json"} {
		if _, err := os.Stat(filepath.Join(baseline, name)); !errors.Is(err, os.ErrNotExist) {
			return ProbeManifest{}, fmt.Errorf("normalized baseline retained %s", name)
		}
	}
	baselineConfig, err := os.ReadFile(filepath.Join(baseline, "opencode.json"))
	if err != nil || string(baselineConfig) != nativeConfig || strings.Contains(string(baselineConfig), "matlatl") {
		return ProbeManifest{}, errors.New("normalized baseline changed native config or retained matlatl MCP")
	}
	allConfig, err := os.ReadFile(filepath.Join(all, "opencode.json"))
	if err != nil || string(allConfig) != config || strings.Count(string(allConfig), `"notice"`) != 1 || strings.Count(string(allConfig), `"matlatl"`) != 1 {
		return ProbeManifest{}, errors.New("all-arm config lacks exact native config, notice, or remote matlatl MCP")
	}
	if err := ValidateMCPTransport("remote", endpoint); err != nil {
		return ProbeManifest{}, err
	}
	for _, name := range []string{"llms.txt", "trails.json"} {
		b, err := os.ReadFile(filepath.Join(all, name))
		if err != nil || SHA256(b) != SHA256(artifacts[name]) {
			return ProbeManifest{}, fmt.Errorf("prepared %s does not match generated artifact", name)
		}
	}
	p := ProbeManifest{SchemaVersion: 1, ClaimBoundary: "event-derived application opens; not kernel-level access evidence"}
	for _, name := range []string{"llms.txt", "llms.txt", "trails.json"} {
		f, err := os.Open(filepath.Join(all, name))
		if err != nil {
			return p, err
		}
		_, err = io.Copy(io.Discard, f)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			return p, errors.Join(err, closeErr)
		}
		p.FilesystemEvents = append(p.FilesystemEvents, FilesystemEvent{Operation: "open", Path: name})
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "nimbus-probe", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{}, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return p, fmt.Errorf("start matlatl MCP with --prepare: %w", err)
	}
	sessionClosed := false
	defer func() {
		if !sessionClosed {
			retErr = errors.Join(retErr, session.Close())
		}
	}()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		return p, err
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, ToolNames) {
		return p, fmt.Errorf("MCP tool names changed or were suppressed: %v", names)
	}
	calls := []struct {
		name string
		args map[string]any
	}{
		{"corpus-summary", map[string]any{}}, {"critical-docs", map[string]any{}},
		{"get-section", map[string]any{"ref": "docs/draining.md#pod-lifecycle-contract"}},
		{"list-orphans", map[string]any{}},
		{"path-between", map[string]any{"from": "README.md", "to": "docs/placement-design.md"}},
		{"path-between", map[string]any{"from": "README.md", "to": "docs/internals/queue-incident.md"}},
		{"suggest-links", map[string]any{}}, {"what-links-to", map[string]any{"doc": "docs/placement-design.md"}},
	}
	for _, call := range calls {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		if err != nil || result.IsError {
			return p, fmt.Errorf("MCP %s: %w", call.name, err)
		}
		args, _ := json.Marshal(call.args)
		response, _ := json.Marshal(result)
		p.ToolEvents = append(p.ToolEvents, ToolEvent{Tool: call.name, Arguments: string(args), Response: string(response)})
	}
	if err := session.Close(); err != nil {
		return p, err
	}
	sessionClosed = true
	cancel()
	if err := <-serveErr; err != nil {
		return p, err
	}
	return p, nil
}

func mustFiles(root string) []string {
	files, _ := evalfs.Files(root)
	return files
}

func copyRepository(source, destination string) (string, error) {
	if err := os.Mkdir(destination, 0o750); err != nil {
		return "", err
	}
	dst, err := evalfs.Root(destination)
	if err != nil {
		return "", err
	}
	files, err := evalfs.Files(source)
	if err != nil {
		return "", err
	}
	for _, rel := range files {
		b, err := evalfs.Read(source, rel)
		if err != nil {
			return "", err
		}
		if err := evalfs.WriteExclusive(dst, rel, b); err != nil {
			return "", err
		}
	}
	return dst, nil
}

type topologyManifest struct {
	SchemaVersion                int        `json:"schemaVersion"`
	Roots                        []string   `json:"roots"`
	RequiredEdges                [][]string `json:"requiredEdges"`
	PlacementMinimumAuthoredHops int        `json:"placementMinimumAuthoredHops"`
	Unreachable                  []string   `json:"unreachable"`
	RequiredNonemptySurfaces     []string   `json:"requiredNonemptySurfaces"`
	Notes                        string     `json:"notes"`
}

func probeTopology(s *Suite, graphBytes []byte) error {
	b, err := evalfs.Read(s.Root, "private/topology.json")
	if err != nil {
		return err
	}
	var expected topologyManifest
	if err := decodeStrict(b, &expected); err != nil {
		return err
	}
	if expected.SchemaVersion != 1 || !strings.Contains(expected.Notes, "not copied") {
		return errors.New("topology expectation provenance missing")
	}
	var actual graphjson.Document
	if err := json.Unmarshal(graphBytes, &actual); err != nil {
		return err
	}
	if !slices.Equal(actual.RootSet, expected.Roots) || !slices.Equal(actual.Unreachable, expected.Unreachable) {
		return fmt.Errorf("topology roots/unreachable mismatch: roots=%v unreachable=%v", actual.RootSet, actual.Unreachable)
	}
	edges := map[[2]string]bool{}
	for _, edge := range actual.Edges {
		edges[[2]string{edge.From, edge.To}] = true
	}
	for _, edge := range expected.RequiredEdges {
		if len(edge) != 2 || !edges[[2]string{edge[0], edge[1]}] {
			return fmt.Errorf("required authored edge missing: %v", edge)
		}
	}
	for _, node := range actual.Nodes {
		if node.ID == "docs/placement-design.md" && node.HopsFromRoot < expected.PlacementMinimumAuthoredHops {
			return fmt.Errorf("placement document has only %d authored hops", node.HopsFromRoot)
		}
	}
	return nil
}

func probeAttempts(s *Suite) (retErr error) {
	root, err := os.MkdirTemp("", "nimbus-attempts-*")
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, os.RemoveAll(root)) }()
	recordRoot, err := evalfs.Root(root)
	if err != nil {
		return err
	}
	firstWorkspace, err := Materialize(s, "batch-ceiling", filepath.Join(root, "workspace-pre"))
	if err != nil {
		return err
	}
	secondWorkspace, err := Materialize(s, "batch-ceiling", filepath.Join(root, "workspace-retry"))
	if err != nil {
		return err
	}
	firstHash, _ := evalfs.TreeHash(firstWorkspace)
	secondHash, _ := evalfs.TreeHash(secondWorkspace)
	parent := AttemptRecord{SchemaVersion: 1, AttemptID: "attempt-pre", WorkspaceHash: firstHash, Status: "environment-failure"}
	child := AttemptRecord{SchemaVersion: 1, AttemptID: "attempt-retry", RetryParent: parent.AttemptID, WorkspaceHash: secondHash, Exposed: true, Status: "completed"}
	if err := ValidateRetry(parent, child); err != nil {
		return err
	}
	if err := WriteAttempt(recordRoot, parent); err != nil {
		return err
	}
	if err := WriteAttempt(recordRoot, child); err != nil {
		return err
	}
	if err := WriteAttempt(recordRoot, child); err == nil {
		return errors.New("attempt overwrite was not append-only")
	}
	post := AttemptRecord{SchemaVersion: 1, AttemptID: "attempt-post", WorkspaceHash: firstHash, Exposed: true, Status: "provider-failure"}
	forbiddenRetry := AttemptRecord{SchemaVersion: 1, AttemptID: "attempt-forbidden", RetryParent: post.AttemptID, WorkspaceHash: secondHash, Status: "completed"}
	if ValidateRetry(post, forbiddenRetry) == nil {
		return errors.New("post-exposure retry accepted")
	}
	return nil
}

func probeQualification(s *Suite) error {
	b, err := evalfs.Read(s.Root, "private/qualification-examples.json")
	if err != nil {
		return err
	}
	var doc struct {
		SchemaVersion       int    `json:"schemaVersion"`
		ThresholdsStatus    string `json:"thresholdsStatus"`
		CandidateListStatus string `json:"candidateListStatus"`
		Examples            []struct {
			Name        string          `json:"name"`
			ExpectValid bool            `json:"expectValid"`
			Candidate   json.RawMessage `json:"candidate"`
		} `json:"examples"`
	}
	if err := decodeStrict(b, &doc); err != nil {
		return err
	}
	if doc.SchemaVersion != 1 || doc.ThresholdsStatus != "unfrozen" || doc.CandidateListStatus != "unfrozen" {
		return errors.New("qualification freeze status invalid")
	}
	for _, x := range doc.Examples {
		_, e := ValidateQualificationCandidate(x.Candidate)
		if (e == nil) != x.ExpectValid {
			return fmt.Errorf("qualification example %s unexpected validity: %v", x.Name, e)
		}
	}
	overflow := []byte(`{"schemaVersion":1,"calibrationOnly":true,"cacheRead":0,"cacheWrite":0,"inputTokens":18446744073709551616,"outputTokens":0,"turns":0,"toolCalls":0,"billedMicros":0,"wallMillis":0}`)
	if _, err := ValidateTelemetry(overflow); err == nil {
		return errors.New("telemetry overflow accepted")
	}
	return nil
}
func countMarkdown(root string) int {
	files, _ := evalfs.Files(root)
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, ".md") {
			n++
		}
	}
	return n
}

// SHA256 returns the lowercase SHA-256 digest of data.
func SHA256(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
