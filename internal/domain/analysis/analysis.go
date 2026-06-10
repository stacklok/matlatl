// Package analysis holds the pure-domain result model: findings, their
// severity and kind, and the immutable AnalysisReport every emitter renders
// from. It depends only on the standard library and the leaf identity package
// (for DocumentID); it imports nothing from application or infrastructure
// (ADR 0004).
package analysis

import (
	"cmp"
	"slices"

	"github.com/stacklok/matlatl/internal/domain/identity"
)

// Severity classifies the weight of a finding. The ordering (Info < Warning <
// Error) is meaningful and used for threshold comparisons.
type Severity int

const (
	// Info is an informational finding.
	Info Severity = iota
	// Warning is a finding that does not fail the build by default.
	Warning
	// Error is a finding that fails the build (exit 1, ADR 0005).
	Error
)

// String returns the canonical name of the severity.
func (s Severity) String() string {
	switch s {
	case Info:
		return "info"
	case Warning:
		return "warning"
	case Error:
		return "error"
	default:
		return "unknown"
	}
}

// Valid reports whether s is a defined Severity.
func (s Severity) Valid() bool {
	return s >= Info && s <= Error
}

// FindingKind classifies what a finding is about.
type FindingKind int

const (
	// BrokenLink is a reference whose target document does not exist.
	BrokenLink FindingKind = iota
	// BrokenAnchor is a reference whose target document exists but anchor does not.
	BrokenAnchor
	// Orphan is a document nothing links to.
	Orphan
	// Unreachable is a document not reachable from the root set.
	Unreachable
	// Ambiguous is a reference that matches multiple candidate documents.
	Ambiguous
	// KnowledgeGap is a detected documentation gap.
	KnowledgeGap
	// UnderLinked is a document with fewer inbound links than the discoverability
	// threshold (but at least one outbound link). Below-default it is Info; a
	// config knob can promote it to Warning (ADR 0012).
	UnderLinked
	// DeadEnd is a document with inbound links but no outbound navigational links
	// (a terminal node). Below-default it is Info; a config knob can promote it to
	// Warning (ADR 0012).
	DeadEnd
	// SuggestedLink is a topology-based suggestion that two UNLINKED but
	// structurally-close documents may warrant a navigational link (ADR 0013). It
	// is always Info and NEVER gates the exit code (it is an experimental,
	// additive discoverability hint, not a defect).
	SuggestedLink
	// DeadLink is an external (http/https) link that failed an opt-in liveness
	// check (--check-external): unreachable, an error status, or refused by the
	// SSRF guard. It is produced only when external checking is enabled, so it is
	// kept OUT of the default deterministic output (ADR 0003).
	DeadLink
	// ArticulationPoint is a document that is a cut VERTEX of the undirected link
	// closure (ADR 0015): removing or unlinking it fragments the corpus into more
	// pieces. It is always Info and NEVER gates the exit code (even --strict) — it
	// is a structural-resilience hint, not a defect — mirroring SuggestedLink and
	// KnowledgeGap.
	ArticulationPoint
	// Bridge is a navigational link that is a cut EDGE of the undirected closure
	// (ADR 0015): it is the only connection between two parts of the corpus, so
	// losing it disconnects them. Like ArticulationPoint it is always Info and
	// never gates the exit code.
	Bridge
	// LowScentAnchor is a navigational link whose anchor text shares too few
	// meaningful tokens with its destination — the target's title or its section
	// headings (the fragment's own heading for an anchored link) — to preview
	// where it leads (ADR 0016): a generic "click here" / a label unrelated to
	// the destination gives a reader or agent weak "information scent" (Pirolli &
	// Card 1999). It is always Info and NEVER gates the exit code (even --strict)
	// — a discoverability hint, not a defect — mirroring SuggestedLink,
	// ArticulationPoint and Bridge.
	LowScentAnchor
)

// String returns the canonical name of the finding kind.
func (k FindingKind) String() string {
	switch k {
	case BrokenLink:
		return "broken-link"
	case BrokenAnchor:
		return "broken-anchor"
	case Orphan:
		return "orphan"
	case Unreachable:
		return "unreachable"
	case Ambiguous:
		return "ambiguous"
	case KnowledgeGap:
		return "knowledge-gap"
	case UnderLinked:
		return "under-linked"
	case DeadEnd:
		return "dead-end"
	case SuggestedLink:
		return "suggested-link"
	case DeadLink:
		return "dead-link"
	case ArticulationPoint:
		return "articulation-point"
	case Bridge:
		return "bridge"
	case LowScentAnchor:
		return "low-scent-anchor"
	default:
		return "unknown"
	}
}

// Valid reports whether k is a defined FindingKind.
func (k FindingKind) Valid() bool {
	return k >= BrokenLink && k <= LowScentAnchor
}

// ParseFindingKind maps a canonical kind name (the String form, e.g.
// "broken-link") back to its FindingKind — the pure inverse of String. The
// second result is false for any string that is not a defined kind name
// (including "" and "unknown").
func ParseFindingKind(s string) (FindingKind, bool) {
	for k := BrokenLink; k.Valid(); k++ {
		if k.String() == s {
			return k, true
		}
	}
	return 0, false
}

// Location pins a finding to a source position.
type Location struct {
	Document identity.DocumentID
	// Line is the 1-based source line, or 0 if not line-specific.
	Line int
}

// Finding is a single diagnostic produced by analysis.
type Finding struct {
	// ID is a stable identifier for the finding (e.g. a rule code).
	ID string
	// Kind classifies the finding.
	Kind FindingKind
	// Severity is the weight of the finding.
	Severity Severity
	// Location pins the finding to a document and line.
	Location Location
	// Message is the human-readable description.
	Message string
	// SuggestedFix is an optional remediation hint.
	SuggestedFix string
	// Details carries optional structured, machine-actionable context for a
	// finding — the data an agent needs to act WITHOUT re-deriving it from the
	// prose Message (e.g. the candidate documents for an ambiguous link, the
	// expected slug for a broken anchor, the raw target for a broken link). Keys
	// are stable, documented per kind (see the application finding builders and
	// the findings.json schema). nil when the finding has no structured detail.
	//
	// It is a pure-data map of stable string→string pairs; the domain attaches no
	// behavior to it. Emitters render it verbatim. Multi-valued detail (e.g. the
	// ambiguous candidate list) is encoded as a "\n"-joined string under a single
	// key so the type stays a flat, deterministic map.
	Details map[string]string
}

// AnalysisReport is the frozen result of the analysis stage: a deterministically
// sorted list of findings plus summary counts. It is immutable after
// construction — its fields are unexported and exposed through accessors.
//
// The name intentionally matches the ubiquitous language fixed by ADR 0004 and
// architecture.md ("a frozen AnalysisReport"); the revive stutter warning is
// suppressed to keep that vocabulary stable across the codebase and docs.
//
//nolint:revive // ubiquitous-language name fixed by ADR 0004
type AnalysisReport struct {
	findings        []Finding
	countBySeverity map[Severity]int
	countByKind     map[FindingKind]int
}

// NewAnalysisReport builds an immutable report from findings. The findings are
// copied and sorted deterministically by (Document, Line, Kind, Severity, ID),
// a total order, so every emitter renders byte-stable output regardless of
// discovery order. A nil or empty input yields an empty report.
func NewAnalysisReport(findings []Finding) *AnalysisReport {
	sorted := make([]Finding, len(findings))
	copy(sorted, findings)
	slices.SortStableFunc(sorted, func(a, b Finding) int {
		if c := cmp.Compare(a.Location.Document, b.Location.Document); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Location.Line, b.Location.Line); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Severity, b.Severity); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})

	bySev := make(map[Severity]int)
	byKind := make(map[FindingKind]int)
	for _, f := range sorted {
		bySev[f.Severity]++
		byKind[f.Kind]++
	}

	return &AnalysisReport{
		findings:        sorted,
		countBySeverity: bySev,
		countByKind:     byKind,
	}
}

// Findings returns a copy of the sorted findings, preserving the report's
// immutability.
func (r *AnalysisReport) Findings() []Finding {
	out := make([]Finding, len(r.findings))
	copy(out, r.findings)
	return out
}

// Len returns the number of findings.
func (r *AnalysisReport) Len() int { return len(r.findings) }

// CountBySeverity returns the number of findings with the given severity.
func (r *AnalysisReport) CountBySeverity(s Severity) int { return r.countBySeverity[s] }

// CountByKind returns the number of findings of the given kind.
func (r *AnalysisReport) CountByKind(k FindingKind) int { return r.countByKind[k] }
