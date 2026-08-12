// Package report renders deterministic Markdown from validated results.
package report

import (
	"fmt"
	"html"
	"slices"
	"strings"

	"github.com/stacklok/matlatl/eval/internal/manifest"
)

// Markdown renders results after sorting by task, arm, and attempt identity.
func Markdown(results []*manifest.Result) string {
	sorted := slices.Clone(results)
	slices.SortFunc(sorted, func(a, b *manifest.Result) int {
		if n := strings.Compare(a.TaskID, b.TaskID); n != 0 {
			return n
		}
		if n := strings.Compare(a.Arm, b.Arm); n != 0 {
			return n
		}
		return strings.Compare(a.AttemptID, b.AttemptID)
	})
	var out strings.Builder
	out.WriteString("# Eval report\n\n> Offline v1 scaffold; validated append-only records.\n\n")
	if len(sorted) == 0 {
		out.WriteString("## Results\n\nNo results recorded.\n")
		return out.String()
	}
	passed, failed, unscored := 0, 0, 0
	for _, result := range sorted {
		switch result.Score {
		case 1:
			passed++
		case 0:
			failed++
		default:
			unscored++
		}
	}
	fmt.Fprintf(&out, "## Summary\n\n- total: %d\n- pass: %d\n- fail: %d\n- unscored: %d\n\n", len(sorted), passed, failed, unscored)
	out.WriteString("## Results\n\n| task | arm | attempt | status | score | answer |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, result := range sorted {
		fmt.Fprintf(&out, "| %s | %s | %s | %s | %s | %s |\n",
			escape(result.TaskID), escape(result.Arm), escape(short(result.AttemptID)), escape(string(result.Status)),
			score(result.Score), escape(result.Answer))
	}
	return out.String()
}

func score(value int) string {
	if value == 1 {
		return "pass"
	}
	if value == 0 {
		return "fail"
	}
	return "unscored"
}

func escape(value string) string {
	if value == "" {
		return "—"
	}
	value = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(value)
	value = html.EscapeString(value)
	return strings.NewReplacer(
		"\\", "\\\\", "|", "\\|", "[", "\\[", "]", "\\]",
		"(", "\\(", ")", "\\)", "*", "\\*", "_", "\\_",
		"!", "\\!", "`", "\\`", ":", "\\:",
	).Replace(value)
}

func short(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
