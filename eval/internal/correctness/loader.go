// Package correctness loads and executes the independent correctness-oracle v1 contract.
package correctness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// SchemaVersion is the current correctness-oracle contract version.
	SchemaVersion  = 1
	maxOracleBytes = 1 << 20
	maxCases       = 256
	maxItems       = 1024
	maxStringBytes = 16 << 10
)

type graphFile struct {
	SchemaVersion        int         `json:"schemaVersion"`
	Family               string      `json:"family"`
	NumericTolerance     float64     `json:"numericTolerance"`
	FarFromRootThreshold int         `json:"farFromRootThreshold"`
	Cases                []graphCase `json:"cases"`
}

type graphCase struct {
	ID        string      `json:"id"`
	Documents []string    `json:"documents"`
	Edges     [][2]string `json:"edges"`
	Roots     []string    `json:"roots"`
}

type trailFile struct {
	SchemaVersion int         `json:"schemaVersion"`
	Family        string      `json:"family"`
	Cases         []trailCase `json:"cases"`
}

type trailCase struct {
	ID    string         `json:"id"`
	Graph mechanismGraph `json:"graph"`
	Want  []trailWant    `json:"want"`
}

type trailWant struct {
	Root  string   `json:"root"`
	Order []string `json:"order"`
}

type backlinkFile struct {
	SchemaVersion      int            `json:"schemaVersion"`
	Family             string         `json:"family"`
	Fixture            string         `json:"fixture"`
	AuthoredReferences int            `json:"authoredReferences"`
	Documents          []backlinkWant `json:"documents"`
}

type backlinkWant struct {
	Path      string   `json:"path"`
	Backlinks []string `json:"backlinks"`
}

type determinismFile struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	Family         string               `json:"family"`
	Fixture        string               `json:"fixture"`
	Artifacts      []string             `json:"artifacts"`
	Sentinels      determinismSentinels `json:"sentinels"`
	CreationOrders []string             `json:"creationOrders"`
	RunsPerOrder   int                  `json:"runsPerOrder"`
}

type determinismSentinels struct {
	GraphDocument string `json:"graphDocument"`
	FindingID     string `json:"findingId"`
	TrailRoot     string `json:"trailRoot"`
	IndexText     string `json:"indexText"`
	LLMSText      string `json:"llmsText"`
}

type resolverFile struct {
	SchemaVersion int             `json:"schemaVersion"`
	Family        string          `json:"family"`
	Catalog       resolverCatalog `json:"catalog"`
	Cases         []resolverCase  `json:"cases"`
}

type resolverCatalog struct {
	Documents []string            `json:"documents"`
	Headings  map[string][]string `json:"headings"`
	Aliases   map[string][]string `json:"aliases"`
	Assets    []string            `json:"assets"`
}

type resolverCase struct {
	ID       string       `json:"id"`
	Origin   string       `json:"origin"`
	Target   string       `json:"target"`
	Fragment string       `json:"fragment"`
	Type     string       `json:"type"`
	Policy   string       `json:"policy"`
	Want     resolverWant `json:"want"`
}

type pipelineFile struct {
	SchemaVersion int            `json:"schemaVersion"`
	Family        string         `json:"family"`
	Fixture       string         `json:"fixture"`
	Cases         []pipelineCase `json:"cases"`
}

type mechanismGraph struct {
	Documents []string    `json:"documents"`
	Edges     [][2]string `json:"edges"`
}

type gapFile struct {
	SchemaVersion int       `json:"schemaVersion"`
	Family        string    `json:"family"`
	Cases         []gapCase `json:"cases"`
}

type gapCase struct {
	ID               string         `json:"id"`
	Graph            mechanismGraph `json:"graph"`
	MinComponentSize int            `json:"minComponentSize"`
	Want             [][2]string    `json:"want"`
	Truncated        bool           `json:"truncated"`
}

type suggestionFile struct {
	SchemaVersion    int              `json:"schemaVersion"`
	Family           string           `json:"family"`
	NumericTolerance float64          `json:"numericTolerance"`
	DefaultMinShared int              `json:"defaultMinShared"`
	DefaultMaxFanout int              `json:"defaultMaxFanout"`
	MaxResults       int              `json:"maxResults"`
	Cases            []suggestionCase `json:"cases"`
}

type suggestionCase struct {
	ID          string           `json:"id"`
	Graph       mechanismGraph   `json:"graph"`
	MinShared   int              `json:"minShared"`
	MaxFanout   int              `json:"maxFanout"`
	Want        []suggestionWant `json:"want"`
	Truncated   bool             `json:"truncated"`
	HubsSkipped bool             `json:"hubsSkipped"`
}

type suggestionWant struct {
	A          string  `json:"a"`
	B          string  `json:"b"`
	Shared     int     `json:"shared"`
	Coupling   int     `json:"coupling"`
	CoCitation int     `json:"coCitation"`
	AdamicAdar float64 `json:"adamicAdar"`
}

type scentFile struct {
	SchemaVersion    int         `json:"schemaVersion"`
	Family           string      `json:"family"`
	NumericTolerance float64     `json:"numericTolerance"`
	Cases            []scentCase `json:"cases"`
}

type scentCase struct {
	ID         string       `json:"id"`
	Documents  []scentDoc   `json:"documents"`
	Links      []scentLink  `json:"links"`
	Want       []scentWant  `json:"want"`
	TokenProof []tokenProof `json:"tokenProof"`
}

type scentDoc struct {
	Path     string      `json:"path"`
	Title    string      `json:"title"`
	Headings [][2]string `json:"headings"`
}

type scentLink struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Section string `json:"section"`
	Line    int    `json:"line"`
	Anchor  string `json:"anchor"`
}

type scentWant struct {
	Source     string  `json:"source"`
	Target     string  `json:"target"`
	Line       int     `json:"line"`
	Anchor     string  `json:"anchor"`
	Score      float64 `json:"score"`
	Suggestion string  `json:"suggestion"`
}

type tokenProof struct {
	Text   string   `json:"text"`
	Tokens []string `json:"tokens"`
}

type mutationFile struct {
	SchemaVersion int            `json:"schemaVersion"`
	Family        string         `json:"family"`
	Fixture       string         `json:"fixture"`
	Cases         []mutationCase `json:"cases"`
}

type mutationCase struct {
	ID          string              `json:"id"`
	Directory   string              `json:"directory"`
	FixtureHash string              `json:"fixtureHash"`
	Path        string              `json:"path"`
	BaseHash    string              `json:"baseHash"`
	Old         string              `json:"old"`
	New         string              `json:"new"`
	Base        mutationObservation `json:"base"`
	Mutated     mutationObservation `json:"mutated"`
}

type mutationObservation struct {
	Edges         []string `json:"edges"`
	Findings      []string `json:"findings"`
	WCC           []string `json:"wcc"`
	FarFromRoot   []string `json:"farFromRoot"`
	Articulations []string `json:"articulations"`
	Bridges       []string `json:"bridges"`
}

type pipelineCase struct {
	ID            string         `json:"id"`
	Source        sourceOracle   `json:"source"`
	Target        string         `json:"target"`
	Fragment      string         `json:"fragment"`
	Type          string         `json:"type"`
	AnchorText    string         `json:"anchorText"`
	CheckLowScent bool           `json:"checkLowScent,omitempty"`
	Want          resolverWant   `json:"want"`
	EdgesDefault  []string       `json:"edgesDefault"`
	EdgesStrict   *[]string      `json:"edgesStrict,omitempty"`
	Finding       *findingOracle `json:"finding,omitempty"`
}

type sourceOracle struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

type findingOracle struct {
	Kind     string            `json:"kind"`
	Severity string            `json:"severity,omitempty"`
	Path     string            `json:"path"`
	Line     int               `json:"line"`
	Details  map[string]string `json:"details,omitempty"`
}

type resolverWant struct {
	Health     string   `json:"health"`
	Kind       string   `json:"kind"`
	Document   string   `json:"document"`
	Anchor     string   `json:"anchor"`
	Directory  string   `json:"directory"`
	Children   []string `json:"children"`
	Candidates []string `json:"candidates"`
	AssetCalls []string `json:"assetCalls"`
}

func load[T any](filename string) (*T, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	limited := io.LimitReader(f, maxOracleBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	return decode[T](body)
}

func decode[T any](body []byte) (*T, error) {
	if len(body) > maxOracleBytes {
		return nil, fmt.Errorf("oracle file exceeds %d bytes", maxOracleBytes)
	}
	if err := rejectDuplicateKeys(body); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var value T
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing JSON data")
	}
	return &value, nil
}

func rejectDuplicateKeys(body []byte) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("non-string object key")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		default:
			return nil
		}
	}
	if err := walk(); err != nil {
		return err
	}
	return nil
}

func shortString(value string) bool {
	return len(value) <= maxStringBytes && !strings.ContainsAny(value, "\x00\r\n")
}

func shortStrings(values ...string) bool {
	for _, value := range values {
		if !shortString(value) {
			return false
		}
	}
	return true
}

func safe(value string) bool {
	return value != "" && shortString(value) && path.Clean(value) == value && value != "." && !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "../")
}

func sortedUnique(values []string) bool {
	return slices.IsSorted(values) && len(slices.Compact(slices.Clone(values))) == len(values)
}

func oracleDir(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(abs, "oracles", "correctness", "v1")
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("correctness oracle path is not a directory")
	}
	return dir, nil
}

// Counts is the deterministic number of checked cases by family.
type Counts struct {
	CanonicalNavigation, Graph, Resolver, Pipeline int
	Scent, Gaps, Suggestions, Mutations            int
	Backlinks, Trails, Determinism                 int
}

// Summary renders stable family and total counts.
func (c Counts) Summary() string {
	total := c.CanonicalNavigation + c.Graph + c.Resolver + c.Pipeline + c.Scent + c.Gaps + c.Suggestions + c.Mutations + c.Backlinks + c.Trails + c.Determinism
	return fmt.Sprintf("correctness-oracle/v1: families=11 cases=%d\n  canonical-navigation cases=%d\n  canonical-graph cases=%d\n  direct-resolver cases=%d\n  pipeline-resolver cases=%d\n  information-scent cases=%d\n  knowledge-gap cases=%d\n  suggested-link cases=%d\n  reversible-mutation cases=%d\n  emitted-backlinks cases=%d\n  emitted-trails cases=%d\n  artifact-determinism cases=%d\n", total, c.CanonicalNavigation, c.Graph, c.Resolver, c.Pipeline, c.Scent, c.Gaps, c.Suggestions, c.Mutations, c.Backlinks, c.Trails, c.Determinism)
}
