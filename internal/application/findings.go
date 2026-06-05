package application

import (
	"fmt"
	"strings"

	"github.com/stacklok/doctopus/internal/domain/analysis"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/domain/reference"
	"github.com/stacklok/doctopus/internal/platform"
)

// CheckExitCode maps a run Result to the ADR 0005 exit code for `doctopus
// check`. Broken links and broken anchors always fail (exit 1). Ambiguous links,
// orphans and unreachable documents are warnings that fail only under --strict.
// KnowledgeGap (Info) never affects the exit code. A clean repo or an empty
// corpus returns ExitOK (0).
func (r Result) CheckExitCode(strict bool) platform.ExitCode {
	if r.BrokenLinkCount > 0 || r.BrokenAnchorCount > 0 {
		return platform.ExitFindings
	}
	if strict && (r.AmbiguousCount > 0 || r.OrphanCount > 0 || r.UnreachableCount > 0) {
		return platform.ExitFindings
	}
	return platform.ExitOK
}

// findingsFromReferences turns resolved references into analysis Findings. Only
// unhealthy references that map to a P2 finding kind (broken link, broken
// anchor, ambiguous) produce findings; healthy/external/ignored references do
// not. Orphan/unreachable findings are a P3 concern and are not produced here.
//
// Severity follows ADR 0005: broken links/anchors are Error (fail the build),
// ambiguous links are Warning (fail only under --strict). The CLI maps severity
// to the exit code via the configured threshold.
func findingsFromReferences(refs []reference.Reference) []analysis.Finding {
	var out []analysis.Finding
	for _, r := range refs {
		switch r.Health {
		case reference.Broken:
			out = append(out, brokenLinkFinding(r))
		case reference.BrokenAnchor:
			out = append(out, brokenAnchorFinding(r))
		case reference.Ambiguous:
			out = append(out, ambiguousFinding(r))
		default:
			// Valid, NonNote, HealthExternal, Ignored, Unresolved: no finding.
		}
	}
	return out
}

// findingID builds a stable, deterministic finding identifier from its kind,
// location and target so the same defect yields the same ID across runs.
func findingID(kind analysis.FindingKind, origin identity.DocumentID, line int, target string) string {
	return fmt.Sprintf("%s:%s:%d:%s", kind, origin, line, target)
}

// rawTargetText reconstructs the human-facing target (path plus #fragment) for
// messages.
func rawTargetText(r reference.Reference) string {
	if r.Fragment == "" {
		return r.RawTarget
	}
	if r.RawTarget == "" {
		return "#" + r.Fragment
	}
	return r.RawTarget + "#" + r.Fragment
}

func brokenLinkFinding(r reference.Reference) analysis.Finding {
	target := rawTargetText(r)
	return analysis.Finding{
		ID:       findingID(analysis.BrokenLink, r.Origin, r.Line, target),
		Kind:     analysis.BrokenLink,
		Severity: analysis.Error,
		Location: analysis.Location{Document: r.Origin, Line: r.Line},
		Message:  fmt.Sprintf("%s link target %q does not resolve to a document in the corpus", r.Type, target),
		SuggestedFix: fmt.Sprintf(
			"Check that %q exists and is spelled correctly relative to %q; if it lives elsewhere, fix the path or move the file.",
			target, r.Origin),
	}
}

func brokenAnchorFinding(r reference.Reference) analysis.Finding {
	doc := r.Target.DocumentID
	if doc == "" {
		doc = r.Origin
	}
	return analysis.Finding{
		ID:       findingID(analysis.BrokenAnchor, r.Origin, r.Line, rawTargetText(r)),
		Kind:     analysis.BrokenAnchor,
		Severity: analysis.Error,
		Location: analysis.Location{Document: r.Origin, Line: r.Line},
		Message:  fmt.Sprintf("anchor #%s does not exist in %q", r.Fragment, doc),
		SuggestedFix: fmt.Sprintf(
			"Add a heading that slugifies to %q in %q, or update the fragment to match an existing heading (slugs are GitHub-style: lowercase, spaces to dashes).",
			r.Fragment, doc),
	}
}

func ambiguousFinding(r reference.Reference) analysis.Finding {
	cands := make([]string, 0, len(r.Candidates))
	for _, c := range r.Candidates {
		cands = append(cands, c.String())
	}
	list := strings.Join(cands, ", ")
	return analysis.Finding{
		ID:       findingID(analysis.Ambiguous, r.Origin, r.Line, rawTargetText(r)),
		Kind:     analysis.Ambiguous,
		Severity: analysis.Warning,
		Location: analysis.Location{Document: r.Origin, Line: r.Line},
		Message:  fmt.Sprintf("link target %q is ambiguous; it matches %d documents: %s", rawTargetText(r), len(r.Candidates), list),
		SuggestedFix: fmt.Sprintf(
			"Disambiguate %q by using a longer, unique path (e.g. one of: %s).",
			rawTargetText(r), list),
	}
}
