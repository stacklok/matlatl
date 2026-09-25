//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stacklok/matlatl/internal/platform"
)

func TestIntegration_ContentRootsDocusaurusMDX(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Four documents distinguish repository-root behavior from the configured
	// Docusaurus content root while the literal Heading ID validates the fragment.
	write("README.md", "# Home\n\n[Guide](user-docs/guide.mdx)\n")
	write("user-docs/guide.mdx", "# Guide\n\n[Widget](/api/widget.mdx#api-widget-class)\n")
	write("user-docs/api/widget.mdx", "import Heading from '@theme/Heading';\n\n<Heading\n  id=\"api-widget-class\"\n>\n<code>Widget</code>\n</Heading>\n")
	write("api/widget.mdx", "# Repository Widget\n")

	runGraph := func() []struct{ From, To string } {
		t.Helper()
		outDir := t.TempDir()
		var out, errOut bytes.Buffer
		if code := runArgs(context.Background(), []string{"graph", dir, "--format", "json", "--out", outDir}, &out, &errOut); code != platform.ExitOK {
			t.Fatalf("graph code = %v, stderr=%q", code, errOut.String())
		}
		data, err := os.ReadFile(filepath.Join(outDir, "graph.json"))
		if err != nil {
			t.Fatal(err)
		}
		var graph struct {
			Edges []struct{ From, To string } `json:"edges"`
		}
		if err := json.Unmarshal(data, &graph); err != nil {
			t.Fatal(err)
		}
		return graph.Edges
	}
	hasEdge := func(edges []struct{ From, To string }, from, to string) bool {
		for _, edge := range edges {
			if edge.From == from && edge.To == to {
				return true
			}
		}
		return false
	}

	var out, errOut bytes.Buffer
	runCheckFindings := func() []struct {
		Kind    string            `json:"kind"`
		Details map[string]string `json:"details"`
	} {
		t.Helper()
		outDir := t.TempDir()
		out.Reset()
		errOut.Reset()
		if code := runArgs(context.Background(), []string{"check", dir, "--out", outDir}, &out, &errOut); code != platform.ExitFindings {
			t.Fatalf("check code = %v, want findings; stderr=%q", code, errOut.String())
		}
		data, err := os.ReadFile(filepath.Join(outDir, "findings.json"))
		if err != nil {
			t.Fatal(err)
		}
		var findings struct {
			Findings []struct {
				Kind    string            `json:"kind"`
				Details map[string]string `json:"details"`
			} `json:"findings"`
		}
		if err := json.Unmarshal(data, &findings); err != nil {
			t.Fatal(err)
		}
		return findings.Findings
	}
	assertFinding := func(findings []struct {
		Kind    string            `json:"kind"`
		Details map[string]string `json:"details"`
	}, kind, target string) {
		t.Helper()
		for _, finding := range findings {
			if finding.Kind == kind && finding.Details["target"] == target {
				return
			}
		}
		t.Fatalf("finding %s target %q not found in %#v", kind, target, findings)
	}

	// The unconfigured site uses ADR 0022 repository-root semantics, so this
	// fragment is checked against api/widget.mdx and is broken. With contentRoots
	// it resolves against the literal Heading anchor in user-docs instead.
	findings := runCheckFindings()
	assertFinding(findings, "broken-anchor", "/api/widget.mdx#api-widget-class")
	write(".matlatl.yml", "version: 1\ncontentRoots: [user-docs]\n")
	out.Reset()
	errOut.Reset()
	if code := runArgs(context.Background(), []string{"check", dir}, &out, &errOut); code != platform.ExitOK {
		t.Fatalf("configured static Heading anchor code = %v, stderr=%q", code, errOut.String())
	}

	// Graph edges are document-projected, so use the same root-absolute path
	// without a fragment to make the selected document observable.
	write("user-docs/guide.mdx", "# Guide\n\n[Widget](/api/widget.mdx)\n")
	edges := runGraph()
	if !hasEdge(edges, "user-docs/guide.mdx", "user-docs/api/widget.mdx") {
		t.Fatalf("configured root-absolute link did not target content root: %v", edges)
	}
	if hasEdge(edges, "user-docs/guide.mdx", "api/widget.mdx") {
		t.Fatalf("configured content root retained repository-root edge: %v", edges)
	}

	if err := os.Remove(filepath.Join(dir, ".matlatl.yml")); err != nil {
		t.Fatal(err)
	}
	edges = runGraph()
	if !hasEdge(edges, "user-docs/guide.mdx", "api/widget.mdx") {
		t.Fatalf("unconfigured root-absolute link did not target repository root: %v", edges)
	}

	// Both missing-file and missing-ID links remain findings after content-root
	// resolution; the static Heading ID above is the positive control.
	write(".matlatl.yml", "version: 1\ncontentRoots: [user-docs]\n")
	write("user-docs/guide.mdx", "# Guide\n\n[Widget](/api/widget.mdx#api-widget-class)\n[Missing file](/api/missing.mdx)\n[Missing ID](/api/widget.mdx#absent)\n")
	findings = runCheckFindings()
	assertFinding(findings, "broken-link", "/api/missing.mdx")
	assertFinding(findings, "broken-anchor", "/api/widget.mdx#absent")
}
