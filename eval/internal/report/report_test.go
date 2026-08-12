package report

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/eval/internal/manifest"
)

func TestMarkdownGoldenAndPermutation(t *testing.T) {
	results := []*manifest.Result{
		{TaskID: "z-task", Arm: "baseline", AttemptID: "bbbbbbbbbbbb9", Status: manifest.StatusCompleted, Score: 0, Answer: "wrong|path"},
		{TaskID: "a-task", Arm: "baseline", AttemptID: "aaaaaaaaaaaa9", Status: manifest.StatusCompleted, Score: 1, Answer: "docs/operate.md"},
		{TaskID: "a-task", Arm: "plus-all", AttemptID: "cccccccccccc9", Status: manifest.StatusProviderFailure, Score: -1},
	}
	got := Markdown(results)
	want, err := os.ReadFile("testdata/report.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("report differs from golden:\n%s", got)
	}
	permuted := slices.Clone(results)
	permuted[0], permuted[2] = permuted[2], permuted[0]
	if other := Markdown(permuted); other != got {
		t.Fatal("report changed when input order changed")
	}
}

func TestMarkdownRejectsCellInjection(t *testing.T) {
	result := &manifest.Result{
		TaskID: "task\r\n| injected |", Arm: "<script>alert(1)</script>",
		AttemptID: "[click](https://example.test)", Status: manifest.StatusCompleted, Score: 1,
		Answer: "[run](javascript:alert(1)) <img src=x onerror=alert(1)>\n| second row | *bold*",
	}
	got := Markdown([]*manifest.Result{result})
	if strings.Count(got, "\n| ") != 3 {
		t.Fatalf("malicious content changed table row count:\n%s", got)
	}
	for _, active := range []string{"<script", "<img", "](", "\n| injected", "\n| second row"} {
		if strings.Contains(got, active) {
			t.Fatalf("report contains active markup %q:\n%s", active, got)
		}
	}
}

func TestEmptyMarkdown(t *testing.T) {
	const want = "# Eval report\n\n> Offline v1 scaffold; validated append-only records.\n\n## Results\n\nNo results recorded.\n"
	if got := Markdown(nil); got != want {
		t.Fatalf("empty report = %q", got)
	}
}
