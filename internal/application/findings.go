package application

import (
	"fmt"
	"strings"

	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
	"github.com/stacklok/matlatl/internal/platform"
)

// CheckExitCode maps a run Result to the ADR 0005 exit code for `matlatl
// check`. Broken links and broken anchors always fail (exit 1). Ambiguous links,
// orphans and unreachable documents are warnings that fail only under --strict.
// KnowledgeGap and SuggestedLink (both Info) never affect the exit code. A clean
// repo or an empty corpus returns ExitOK (0).
//
// ArticulationPoint and Bridge (ADR 0015) are likewise Info and DELIBERATELY
// never gate the exit code, even under --strict: they are structural-resilience
// hints (single points of failure in the link graph), not defects, so a corpus
// with a cut vertex is not a failed build. They are reported (findings.json /
// the human report / graph.json data) but never read here, mirroring
// SuggestedLink and KnowledgeGap.
//
// DeadLinkCount is DELIBERATELY excluded from the exit contract, even under
// --strict (ADR 0005): external link checking is opt-in (--check-external) and
// non-deterministic (network state, transient timeouts, rate limits), so gating
// CI on it would make a green build flaky. DeadLink findings are reported (in
// findings.json/JUnit and the human report) but never change the exit code; CI
// that wants to fail on dead external links should consume findings.json
// explicitly. This is why r.DeadLinkCount is intentionally not read here.
func (r Result) CheckExitCode(strict bool) platform.ExitCode {
	if r.BrokenLinkCount > 0 || r.BrokenAnchorCount > 0 {
		return platform.ExitFindings
	}
	if strict {
		if r.AmbiguousCount > 0 || r.OrphanCount > 0 || r.UnreachableCount > 0 {
			return platform.ExitFindings
		}
		// Graduated structure findings (under-linked, dead-end) fail --strict only
		// when the configurable severity promotes them to Warning (ADR 0012). At
		// the default Info severity they never affect the exit code.
		if r.StructureFindingsSeverity == StructureFindingsWarning &&
			(r.UnderLinkedCount > 0 || r.DeadEndCount > 0) {
			return platform.ExitFindings
		}
	}
	return platform.ExitOK
}

// Stable structured-detail keys attached to a Finding.Details map. They are the
// machine-actionable context an agent needs to act on a finding without parsing
// the prose Message, and are surfaced verbatim in findings.json (schema v2). A
// key here is part of the findings.json contract: renaming one is a breaking
// change that bumps the findings schema version.
const (
	// DetailTarget is the raw, human-facing link target (path plus #fragment).
	DetailTarget = "target"
	// DetailLinkType is the syntactic link type (e.g. "relative-link", "wikilink").
	DetailLinkType = "linkType"
	// DetailExpectedSlug is the anchor slug a broken-anchor reference expected.
	DetailExpectedSlug = "expectedSlug"
	// DetailTargetDocument is the resolved document a broken anchor lives in.
	DetailTargetDocument = "targetDocument"
	// DetailCandidates is the newline-joined candidate DocumentIDs of an ambiguous
	// reference (the alternatives an agent can pick a unique path from).
	DetailCandidates = "candidates"
	// DetailComponentA / DetailComponentB are the two cluster IDs of a gap, and
	// DetailRepresentativeA / DetailRepresentativeB a concrete bridge endpoint each.
	DetailComponentA      = "componentA"
	DetailComponentB      = "componentB"
	DetailRepresentativeA = "representativeA"
	DetailRepresentativeB = "representativeB"
	// DetailStatusCode is the final HTTP status of a failed external link
	// (DeadLink). DetailBlocked is "true" when the SSRF guard refused the URL.
	// Present only on --check-external runs (kept out of the default output).
	DetailStatusCode = "statusCode"
	DetailBlocked    = "blocked"
	// DetailInboundCount is the actual inbound-link count of an under-linked
	// document (the data behind the discoverability-threshold comparison).
	DetailInboundCount = "inboundCount"
	// DetailBowtieBucket is a node's bow-tie bucket
	// (core/in/out/tendril/disconnected); surfaced in graph.json node data.
	DetailBowtieBucket = "bowtieBucket"
	// Suggested-link detail keys (ADR 0013). DetailTargetDocument carries DocA (the
	// finding's anchor document); these carry the rest of the pair and its scores.
	DetailSuggestedTarget  = "suggestedTarget"
	DetailSharedNeighbours = "sharedNeighbours"
	DetailCoupling         = "coupling"
	DetailCoCitation       = "coCitation"
	DetailAdamicAdar       = "adamicAdar"
	// Critical-structure detail keys (ADR 0015). DetailBetweenness is the
	// load-bearing connector's betweenness score (the data behind the
	// articulation-point finding). DetailBridgeEndpoint is the OTHER endpoint of a
	// bridge edge (the finding is anchored at the canonical-min endpoint).
	DetailBetweenness    = "betweenness"
	DetailBridgeEndpoint = "bridgeEndpoint"
	// Low-scent-anchor detail keys (ADR 0016). DetailAnchorText is the link label
	// as written; DetailScentScore the Jaccard similarity to the target title
	// (fixed precision); DetailSuggestedAnchor the recommended replacement (the
	// target's title); DetailSourceDocument / DetailTargetDocument the endpoints.
	DetailAnchorText      = "anchorText"
	DetailScentScore      = "scentScore"
	DetailSuggestedAnchor = "suggestedAnchor"
	DetailSourceDocument  = "sourceDocument"
)

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
		Details: map[string]string{
			DetailTarget:   target,
			DetailLinkType: r.Type.String(),
		},
	}
}

func brokenAnchorFinding(r reference.Reference) analysis.Finding {
	// The resolver always sets Target.DocumentID on a BrokenAnchor (the document
	// resolved; only the anchor was missing), so it is non-empty here. For a
	// same-document anchor it is the origin itself (resolver.go).
	doc := r.Target.DocumentID
	return analysis.Finding{
		ID:       findingID(analysis.BrokenAnchor, r.Origin, r.Line, rawTargetText(r)),
		Kind:     analysis.BrokenAnchor,
		Severity: analysis.Error,
		Location: analysis.Location{Document: r.Origin, Line: r.Line},
		Message:  fmt.Sprintf("anchor #%s does not exist in %q", r.Fragment, doc),
		SuggestedFix: fmt.Sprintf(
			"Add a heading that slugifies to %q in %q, or update the fragment to match an existing heading (slugs are GitHub-style: lowercase, spaces to dashes).",
			r.Fragment, doc),
		Details: map[string]string{
			DetailTarget:         rawTargetText(r),
			DetailLinkType:       r.Type.String(),
			DetailExpectedSlug:   r.Fragment,
			DetailTargetDocument: doc.String(),
		},
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
		Details: map[string]string{
			DetailTarget:     rawTargetText(r),
			DetailLinkType:   r.Type.String(),
			DetailCandidates: strings.Join(cands, "\n"),
		},
	}
}
