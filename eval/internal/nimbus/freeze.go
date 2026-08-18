package nimbus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
	"github.com/stacklok/matlatl/eval/internal/harness"
)

// VerifierImage is the immutable toolchain image used by the private verifier.
const VerifierImage = "docker.io/library/golang:1.26.4@sha256:f96cc555eb8db430159a3aa6797cd5bae561945b7b0fe7d0e284c63a3b291609"
const verifierRecipe = "three isolated stages: output-less public checks; separately mounted trusted adapter exact-build to byte+inode-bounded labeled named tmpfs volume without candidate execution; source-free challenge-sequenced execution from read-only output; ownership-verified unique labels+cidfiles; verified cleanup"

// FrozenFile records one byte- and mode-frozen suite file.
type FrozenFile struct {
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// NamedHash associates a stable name with a SHA-256 digest.
type NamedHash struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// RuntimeImage records a runtime's immutable verifier image identity.
type RuntimeImage struct {
	Runtime  string `json:"runtime"`
	ImageID  string `json:"imageId"`
	Platform string `json:"platform"`
}

// Toolchain records all frozen verifier build inputs and outputs.
type Toolchain struct {
	GoVersion            string         `json:"goVersion"`
	StaticBinaryPlatform string         `json:"staticBinaryPlatform"`
	VerifierImage        string         `json:"verifierImage"`
	VerifierRecipeSHA256 string         `json:"verifierRecipeSha256"`
	StaticBinaryRecipes  []NamedHash    `json:"staticBinaryRecipes"`
	StaticBinaryInputs   []NamedHash    `json:"staticBinaryInputs"`
	StaticBinaries       []NamedHash    `json:"staticBinaries"`
	RuntimeImages        []RuntimeImage `json:"runtimeImages"`
}

// Freeze is the canonical Nimbus suite and toolchain inventory.
type Freeze struct {
	SchemaVersion      int          `json:"schemaVersion"`
	CalibrationOnly    bool         `json:"calibrationOnly"`
	ReviewStatus       string       `json:"reviewStatus"`
	Files              []FrozenFile `json:"files"`
	CorpusTreeSHA256   string       `json:"corpusTreeSha256"`
	Tasks              []NamedHash  `json:"tasks"`
	Mutations          []NamedHash  `json:"mutations"`
	Verifiers          []NamedHash  `json:"verifiers"`
	Patches            []NamedHash  `json:"patches"`
	GeneratedArtifacts []NamedHash  `json:"generatedArtifacts"`
	TopologySHA256     string       `json:"topologySha256"`
	ProbesSHA256       string       `json:"probesSha256,omitempty"`
	Toolchain          Toolchain    `json:"toolchain"`
}

// BuildFreeze computes the canonical inventory for a validated suite.
func BuildFreeze(s *Suite, runtimeImages []RuntimeImage) (*Freeze, error) {
	files, err := evalfs.Files(s.Root)
	if err != nil {
		return nil, err
	}
	f := &Freeze{SchemaVersion: 1, CalibrationOnly: true, ReviewStatus: "pending"}
	for _, rel := range files {
		if rel == "freeze.json" {
			continue
		}
		b, err := evalfs.Read(s.Root, rel)
		if err != nil {
			return nil, err
		}
		path, _ := evalfs.Path(s.Root, rel)
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		f.Files = append(f.Files, FrozenFile{rel, uint32(info.Mode().Perm()), int64(len(b)), hex.EncodeToString(sum[:])})
	}
	f.CorpusTreeSHA256, err = evalfs.TreeHash(s.Repository)
	if err != nil {
		return nil, err
	}
	for _, t := range s.Tasks {
		f.Tasks = append(f.Tasks, namedFileHash(s, t.ID, "tasks/"+t.ID+"/task.json"))
		f.Mutations = append(f.Mutations, namedFileHash(s, t.ID, "tasks/"+t.ID+"/mutation.json"))
		f.Verifiers = append(f.Verifiers, namedFileHash(s, t.ID, "private/"+t.ID+"/cases.json"))
		f.Patches = append(f.Patches, namedFileHash(s, t.ID, "private/"+t.ID+"/patches.json"))
	}
	f.Verifiers = append(f.Verifiers, namedFileHash(s, "adapter", "private/adapter.go.txt"))
	if hasFile(files, "private/topology.json") {
		f.TopologySHA256, _ = evalfs.FileHash(s.Root, "private/topology.json")
	}
	artifacts, err := harness.EmitArtifacts(context.Background(), s.Repository)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"llms.txt", "trails.json"} {
		f.GeneratedArtifacts = append(f.GeneratedArtifacts, NamedHash{name, SHA256(artifacts[name])})
	}
	slices.SortFunc(runtimeImages, func(a, b RuntimeImage) int { return strings.Compare(a.Runtime, b.Runtime) })
	staticBinaries, err := staticBinaryHashes(s)
	if err != nil {
		return nil, err
	}
	staticInputs, err := staticBinaryInputs(s)
	if err != nil {
		return nil, err
	}
	f.Toolchain = Toolchain{
		GoVersion: "go1.26.4", StaticBinaryPlatform: "linux/amd64", VerifierImage: VerifierImage,
		VerifierRecipeSHA256: hashText(verifierRecipe),
		StaticBinaryRecipes: []NamedHash{
			{"eval-supervisor", hashText("CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -mod=readonly -trimpath -ldflags=-s -w -buildid= ./eval/cmd/eval-supervisor")},
			{"fake-opencode", hashText("CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -mod=readonly -trimpath -ldflags=-s -w -buildid= ./eval/cmd/fake-opencode")},
		},
		StaticBinaryInputs: staticInputs, StaticBinaries: staticBinaries, RuntimeImages: runtimeImages,
	}
	return f, nil
}

func staticBinaryHashes(_ *Suite) (hashes []NamedHash, retErr error) {
	repositoryRoot, err := projectRoot()
	if err != nil {
		return nil, err
	}
	temp, err := os.MkdirTemp("", "nimbus-static-hash-*")
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, os.RemoveAll(temp)) }()
	for _, item := range []struct{ name, pkg string }{{"eval-supervisor", "./eval/cmd/eval-supervisor"}, {"fake-opencode", "./eval/cmd/fake-opencode"}} {
		output := filepath.Join(temp, item.name)
		// The package argument comes from the fixed two-entry allowlist above.
		cmd := exec.Command("go", "build", "-buildvcs=false", "-mod=readonly", "-trimpath", "-ldflags=-s -w -buildid=", "-o", output, item.pkg) //nolint:gosec // Fixed executable and package allowlist.
		cmd.Dir = repositoryRoot
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOTOOLCHAIN=local", "GOPROXY=off")
		if data, buildErr := cmd.CombinedOutput(); buildErr != nil {
			return nil, fmt.Errorf("freeze static binary %s: %w: %s", item.name, buildErr, string(data))
		}
		data, err := os.ReadFile(output)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, NamedHash{item.name, hashText(string(data))})
	}
	return hashes, nil
}

func staticBinaryInputs(_ *Suite) ([]NamedHash, error) {
	repositoryRoot, err := projectRoot()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("go", "list", "-deps", "-json", "./eval/cmd/eval-supervisor", "./eval/cmd/fake-opencode")
	cmd.Dir = repositoryRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOTOOLCHAIN=local", "GOPROXY=off")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(io.LimitReader(stdout, 64<<20))
	byImport := map[string]string{}
	for {
		var pkg struct {
			ImportPath                    string
			Dir                           string
			GoFiles, CgoFiles, EmbedFiles []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, err
		}
		files := append(append(slices.Clone(pkg.GoFiles), pkg.CgoFiles...), pkg.EmbedFiles...)
		slices.Sort(files)
		h := sha256.New()
		for _, name := range files {
			data, err := os.ReadFile(filepath.Join(pkg.Dir, name))
			if err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return nil, err
			}
			_, _ = fmt.Fprintf(h, "%d:%s:%d:", len(name), name, len(data))
			_, _ = h.Write(data)
		}
		byImport[pkg.ImportPath] = hex.EncodeToString(h.Sum(nil))
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("list static binary inputs: %w: %s", err, stderr.String())
	}
	names := make([]string, 0, len(byImport))
	for name := range byImport {
		names = append(names, name)
	}
	slices.Sort(names)
	inputs := make([]NamedHash, 0, len(names)+2)
	for _, name := range names {
		inputs = append(inputs, NamedHash{name, byImport[name]})
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(repositoryRoot, name))
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, NamedHash{name, SHA256(data)})
	}
	return inputs, nil
}

func projectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		mod := filepath.Join(dir, "go.mod")
		data, readErr := os.ReadFile(mod)
		if readErr == nil && bytes.Contains(data, []byte("module github.com/stacklok/matlatl")) {
			return evalfs.Root(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("matlatl module root not found")
		}
		dir = parent
	}
}

func namedFileHash(s *Suite, name, rel string) NamedHash {
	h, _ := evalfs.FileHash(s.Root, rel)
	return NamedHash{name, h}
}
func hashText(v string) string                { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func hasFile(files []string, rel string) bool { return slices.Contains(files, rel) }

// CheckFreeze verifies that freeze.json exactly matches the current suite.
func CheckFreeze(s *Suite) error {
	b, err := evalfs.Read(s.Root, "freeze.json")
	if err != nil {
		return err
	}
	var want Freeze
	if err := decodeStrict(b, &want); err != nil {
		return err
	}
	got, err := BuildFreeze(s, want.Toolchain.RuntimeImages)
	if err != nil {
		return err
	}
	a, _ := json.Marshal(want)
	z, _ := json.Marshal(got)
	if !slices.Equal(a, z) {
		return fmt.Errorf("freeze.json does not match current Nimbus tree")
	}
	return validateRuntimeImages(want.Toolchain.RuntimeImages)
}

// WriteFreeze writes a reviewed runtime inventory and the current suite freeze.
func WriteFreeze(s *Suite, runtimeImages []RuntimeImage) error {
	if err := validateRuntimeImages(runtimeImages); err != nil {
		return err
	}
	f, err := BuildFreeze(s, runtimeImages)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(f, "", "  ")
	b = append(b, '\n')
	path := filepath.Join(s.Root, "freeze.json")
	return os.WriteFile(path, b, 0o600)
}
func validateRuntimeImages(images []RuntimeImage) error {
	if len(images) != 2 || images[0].Runtime != "docker" || images[1].Runtime != "podman" {
		return fmt.Errorf("freeze requires sorted docker and podman runtime image IDs")
	}
	for _, x := range images {
		if !strings.HasPrefix(x.ImageID, "sha256:") || len(x.ImageID) != 71 {
			return fmt.Errorf("invalid immutable %s image ID", x.Runtime)
		}
		if x.Platform != "linux/amd64" && x.Platform != "linux/arm64" {
			return fmt.Errorf("invalid %s image platform %q", x.Runtime, x.Platform)
		}
	}
	return nil
}
