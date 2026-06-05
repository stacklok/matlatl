// Package application orchestrates the matlatl pipeline. It depends on the
// domain and platform layers and defines the small set of interfaces (ports)
// that mark genuine test seams. It is wired by cmd/matlatl and must not import
// cobra. See ADR 0004.
package application

import "github.com/stacklok/matlatl/internal/domain/reference"

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
	}
}
