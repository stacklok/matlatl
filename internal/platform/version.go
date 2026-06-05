// Package platform holds cross-cutting concerns that are neither domain logic
// nor application orchestration: version metadata and the process exit-code
// contract. It depends only on the standard library.
package platform

import "fmt"

// Build metadata. These are overridden at link time via -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/stacklok/matlatl/internal/platform.Version=1.2.3 ..."
//
// They default to "dev" for local/unstamped builds.
var (
	// Version is the semantic version of the build (e.g. "1.2.3").
	Version = "dev"
	// Commit is the git commit hash the build was produced from.
	Commit = "dev"
	// Date is the build timestamp.
	Date = "dev"
)

// BuildInfo returns a human-readable, single-line description of the build.
// It is deliberately not named String to avoid visual collision with the
// fmt.Stringer convention on a package-level function.
func BuildInfo() string {
	return fmt.Sprintf("matlatl %s (commit %s, built %s)", Version, Commit, Date)
}
