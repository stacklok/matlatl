// This file implements the process exit-code contract defined in ADR 0005:
// 0=success, 1=findings at/above threshold, 2=usage error, 3=runtime error.
package platform

// ExitCode is the process exit status returned by doctopus. The mapping is a
// stable contract that CI pipelines depend on (see ADR 0005).
type ExitCode int

const (
	// ExitOK signals success: no findings at or above the failure threshold.
	// An empty corpus (no markdown found) is also success.
	ExitOK ExitCode = 0
	// ExitFindings signals that findings exist at or above the failure
	// threshold (broken links/anchors; orphans and ambiguous links only under
	// --strict).
	ExitFindings ExitCode = 1
	// ExitUsage signals a usage error: bad flags or arguments.
	ExitUsage ExitCode = 2
	// ExitRuntime signals a runtime error: unreadable path, I/O failure, or an
	// internal error.
	ExitRuntime ExitCode = 3
)

// String returns a short identifier for the exit code, useful in logs.
func (c ExitCode) String() string {
	switch c {
	case ExitOK:
		return "ok"
	case ExitFindings:
		return "findings"
	case ExitUsage:
		return "usage"
	case ExitRuntime:
		return "runtime"
	default:
		return "unknown"
	}
}
