//go:build ociintegration

package nimbus

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBlackBoxVerifierOCI(t *testing.T) {
	runtime := os.Getenv("MATLATL_OCI_RUNTIME")
	if runtime != "docker" && runtime != "podman" {
		t.Fatal("MATLATL_OCI_RUNTIME must be docker or podman")
	}
	s := testSuite(t)
	if _, err := PrepareVerifier(context.Background(), runtime); err != nil {
		t.Fatal(err)
	}
	results, err := Verify(context.Background(), s, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 41 {
		t.Fatalf("verified %d cases, want 41", len(results))
	}
	for _, result := range results {
		if result.Status != "completed" {
			t.Fatalf("incomplete verifier result: %+v", result)
		}
	}
	assertNoVerifierResources(t, runtime)

	patches, err := patchesFor(s, "batch-ceiling")
	if err != nil {
		t.Fatal(err)
	}
	correct := patches.Cases[0]
	for _, patch := range patches.Cases {
		if patch.Name == "correct" {
			correct = patch
		}
	}
	t.Run("public-check-cannot-write-output", func(t *testing.T) {
		base := t.TempDir()
		workspace := filepath.Join(base, "workspace")
		if _, err := Materialize(s, "batch-ceiling", workspace); err != nil {
			t.Fatal(err)
		}
		if err := ApplyCase(workspace, correct); err != nil {
			t.Fatal(err)
		}
		testSource := `package relay
import (
 "os"
 "testing"
)
func TestVerifierOutputIsNotWritable(t *testing.T) {
 if err := os.WriteFile("/output/candidate-owned", []byte("owned"), 0600); err == nil { t.Fatal("wrote verifier output") }
}`
		if err := os.WriteFile(filepath.Join(workspace, "relay", "output_isolation_test.go"), []byte(testSource), 0o600); err != nil {
			t.Fatal(err)
		}
		if output, err := verifyCase(context.Background(), runtime, s, "batch-ceiling", workspace, base); err != nil {
			t.Fatalf("isolated public check failed: %v: %s", err, output)
		}
	})
	t.Run("reserved-adapter-collision", func(t *testing.T) {
		base := t.TempDir()
		workspace := filepath.Join(base, "workspace")
		if _, err := Materialize(s, "batch-ceiling", workspace); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(workspace, "cmd", "nimbus-adapter"), 0o700); err != nil {
			t.Fatal(err)
		}
		if output, err := verifyCase(context.Background(), runtime, s, "batch-ceiling", workspace, base); err == nil || !exitError(err) {
			t.Fatalf("reserved collision accepted: %v: %s", err, output)
		}
	})
	t.Run("output-volume-inode-limit", func(t *testing.T) {
		assertOutputVolumeExhaustion(t, runtime)
	})
	t.Run("canceled-cleanup", func(t *testing.T) {
		base := t.TempDir()
		workspace := filepath.Join(base, "workspace")
		if _, err := Materialize(s, "batch-ceiling", workspace); err != nil {
			t.Fatal(err)
		}
		if err := ApplyCase(workspace, correct); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := verifyCase(ctx, runtime, s, "batch-ceiling", workspace, base); err == nil {
			t.Fatal("canceled verifier setup succeeded")
		}
	})
	assertNoVerifierResources(t, runtime)
}

func assertOutputVolumeExhaustion(t *testing.T, runtime string) {
	t.Helper()
	nonce := fmt.Sprintf("integration-%d-%d", os.Getpid(), time.Now().UnixNano())
	label := verifierLabelKey + "=" + nonce
	base := t.TempDir()
	container := ownedContainer{name: "matlatl-nimbus-fill-" + nonce, cidfile: filepath.Join(base, "fill.cid"), label: label}
	volume := ownedVolume{name: "matlatl-nimbus-output-" + nonce, label: label}
	if err := ensureContainerAbsent(runtime, container.name); err != nil {
		t.Fatal(err)
	}
	if err := createOwnedVolume(runtime, volume); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanupVerifierResources(runtime, []ownedContainer{container}, volume); err != nil {
			t.Error(err)
		}
	})
	args := append([]string{"run", "--name", container.name, "--cidfile", container.cidfile}, lockedContainerArgs(label, "128m", "1", "32", false)...)
	args = append(args, "--volume", volume.name+":/output:rw", VerifierImage, "/bin/sh", "-c", `set -e; i=0; while [ "$i" -lt 256 ]; do : > "/output/f$i"; i=$((i+1)); done`)
	_, _, err := runOwnedContainer(context.Background(), runtime, container, args, nil, 64<<10)
	if err == nil {
		t.Fatal("output tmpfs accepted files beyond its inode limit")
	}
}

func assertNoVerifierResources(t *testing.T, runtime string) {
	t.Helper()
	cmd := exec.Command(runtime, "ps", "-aq", "--filter", "label="+verifierLabelKey)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list verifier containers: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) != "" {
		t.Fatalf("verifier containers remained: %s", output)
	}
	cmd = exec.Command(runtime, "volume", "ls", "-q", "--filter", "label="+verifierLabelKey)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list verifier volumes: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) != "" {
		t.Fatalf("verifier volumes remained: %s", output)
	}
}
