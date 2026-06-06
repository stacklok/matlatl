// Package application orchestrates the matlatl pipeline. It depends on the
// domain and platform layers and defines the small set of interfaces (ports)
// that mark genuine test seams. It is wired by cmd/matlatl and must not import
// cobra. See ADR 0004.
package application

import (
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// StructureFindingsSeverity selects the severity assigned to the graduated
// structure findings (under-linked, dead-end). It is a small typed enum so the
// CLI/config can carry "info" | "warning" and the pipeline can plumb a single
// value through to the finding builders (ADR 0012).
type StructureFindingsSeverity string

const (
	// StructureFindingsInfo (the default) makes under-linked/dead-end Info: they
	// are reported but NEVER fail `check`, even under --strict.
	StructureFindingsInfo StructureFindingsSeverity = "info"
	// StructureFindingsWarning promotes under-linked/dead-end to Warning: they
	// then fail `check --strict` like orphans/unreachable.
	StructureFindingsWarning StructureFindingsSeverity = "warning"
)

// Valid reports whether s is a defined severity choice.
func (s StructureFindingsSeverity) Valid() bool {
	return s == StructureFindingsInfo || s == StructureFindingsWarning
}

// Config holds the resolved run configuration for the pipeline. It is built by
// the CLI layer from flags and arguments and treated as read-only by the
// pipeline.
type Config struct {
	// RootPath is the scan root (the repository to analyze).
	RootPath string
	// Roots is the explicit reachability root set (documents BFS starts from);
	// empty means autodetect (README.md/index.md/type:index).
	Roots []string
	// Ignore holds additional ignore patterns layered on .matlatlignore.
	Ignore []string
	// ResolutionPolicy selects how raw targets map to documents (ADR 0001).
	ResolutionPolicy reference.ResolutionPolicy
	// OutputDir is the artifact output directory; empty means no artifacts.
	OutputDir string
	// Formats selects the emitter formats to render.
	Formats []string
	// Strict promotes orphan/ambiguous warnings to build failures (ADR 0005).
	Strict bool
	// CheckExternal enables opt-in external link liveness checks (ADR 0003).
	CheckExternal bool
	// Quiet suppresses non-essential output.
	Quiet bool
	// Verbose enables detailed logging.
	Verbose bool
	// ParseWorkers bounds the parse-stage worker pool. 0 means autodetect
	// (GOMAXPROCS, capped). 1 forces the single-threaded path. The merge into
	// the Corpus is always single-threaded and deterministic regardless of this
	// value (P6 fan-out parsing).
	ParseWorkers int
	// ExternalChecker, when non-nil and CheckExternal is set, validates
	// HealthExternal http(s) links. It is an application port (interface) so the
	// domain stays free of net/http; the CLI injects the infrastructure
	// implementation. nil disables external checking even under CheckExternal.
	ExternalChecker ExternalLinkChecker
	// InboundThreshold is the under-linked discoverability floor (ADR 0012): a
	// non-exempt document with outbound links but fewer inbound links is reported
	// as under-linked. <=0 is normalized to graphmodel.DefaultInboundThreshold (3)
	// in the domain.
	InboundThreshold int
	// StructureFindingsSeverity selects the severity of the graduated structure
	// findings (under-linked, dead-end). Default "info" (never fails check);
	// "warning" promotes them so they fail `check --strict`.
	StructureFindingsSeverity StructureFindingsSeverity
	// LinkSuggestionMinShared is the minimum shared-neighbour count an unlinked
	// pair must have to be reported as a suggested-link (ADR 0013). Config-only
	// knob (no CLI flag). <=0 is normalized to the domain default (2) in
	// PredictLinks.
	LinkSuggestionMinShared int
}

// DefaultConfig returns a Config with sane defaults: scan the current
// directory, longest-suffix resolution, no artifacts, no external checks.
func DefaultConfig() Config {
	return Config{
		RootPath:         ".",
		Roots:            nil,
		Ignore:           nil,
		ResolutionPolicy: reference.DefaultResolutionPolicy,
		OutputDir:        "",
		Formats:          nil,
		Strict:           false,
		CheckExternal:    false,
		Quiet:            false,
		Verbose:          false,
		ParseWorkers:     0, // autodetect
		ExternalChecker:  nil,

		InboundThreshold:          graphmodel.DefaultInboundThreshold,
		StructureFindingsSeverity: StructureFindingsInfo,
	}
}
