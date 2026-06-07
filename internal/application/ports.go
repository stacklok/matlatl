package application

import (
	"context"
	"time"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// The interfaces in this file are the pipeline's real test seams (ADR 0004).
// They are the only interfaces introduced for collaborators: filesystem
// scanning, document parsing, and artifact writing all have a single production
// implementation in internal/infrastructure but are abstracted here so the
// pipeline can be driven with fakes in tests. We deliberately do NOT introduce
// interfaces for collaborators that lack a real seam.

// ScannedFile is a candidate file discovered by a FileScanner, carrying the
// information the parser needs without performing any parsing itself.
type ScannedFile struct {
	// Path is the absolute filesystem path of the file.
	Path string
	// ID is the canonical document identity derived from the scan root.
	ID identity.DocumentID
	// ModTime is the file's last-modified time.
	ModTime time.Time
	// Size is the file size in bytes.
	Size int64
}

// NoticeKind classifies a non-fatal scanner observation surfaced to the user.
type NoticeKind int

const (
	// NoticeSkippedSymlink reports a symlink that was not followed (ADR 0003).
	NoticeSkippedSymlink NoticeKind = iota
	// NoticeEscapesRoot reports a path that, after canonicalization, resolves
	// outside the scan root (the genuine root-escape boundary, ADR 0003).
	NoticeEscapesRoot
	// NoticeOversized reports a file skipped for exceeding the size cap.
	NoticeOversized
	// NoticeTruncated reports that discovery stopped at the file-count cap.
	NoticeTruncated
	// NoticeWalkError reports a directory-walk error on an entry (the entry was
	// skipped; the walk continued).
	NoticeWalkError
	// NoticeIOError reports a stat/info or identity-derivation failure on an
	// otherwise-eligible file (the file was skipped).
	NoticeIOError
	// NoticeConfig reports a tolerated observation from loading the per-repo
	// `.matlatl.yml` (a missing version field assumed as 1, an unknown/typo key
	// ignored, an oversized config skipped). It never by itself fails the run;
	// hard config errors are surfaced as a real error mapped to ExitUsage (ADR
	// 0011).
	NoticeConfig
	// NoticeSkippedNestedRepo reports a directory pruned because it is a nested
	// git repository — a submodule, linked worktree, or nested clone (detected by
	// the presence of a `.git` entry inside it; ADR 0017). The scan root's own
	// `.git` is exempt, so this fires only for nested working trees below the root.
	NoticeSkippedNestedRepo
)

// String returns a short identifier for the notice kind.
func (k NoticeKind) String() string {
	switch k {
	case NoticeSkippedSymlink:
		return "skipped-symlink"
	case NoticeEscapesRoot:
		return "escapes-root"
	case NoticeOversized:
		return "oversized"
	case NoticeTruncated:
		return "truncated"
	case NoticeWalkError:
		return "walk-error"
	case NoticeIOError:
		return "io-error"
	case NoticeConfig:
		return "config"
	case NoticeSkippedNestedRepo:
		return "skipped-nested-repo"
	default:
		return "unknown"
	}
}

// Notice is a non-fatal observation from the scan stage (a skipped symlink, an
// oversized file, a root-escaping path, or a truncated discovery). Notices are
// reported to the user but do not by themselves fail the run.
type Notice struct {
	Kind NoticeKind
	// Path is the offending filesystem path (best-effort; may be relative).
	Path string
	// Detail is a human-readable explanation.
	Detail string
}

// ScanResult is the outcome of a scan: the deterministically sorted files to
// parse plus any notices.
type ScanResult struct {
	Files   []ScannedFile
	Notices []Notice
}

// FileScanner walks a root and returns the markdown files to parse, enforcing
// the security boundary and ignore rules (ADR 0003). Production implementation:
// internal/infrastructure/fsscanner.
type FileScanner interface {
	Scan(ctx context.Context, root string) (ScanResult, error)
}

// DocumentParser turns a scanned file into a pure-domain Document (front
// matter, section tree, raw references). Production implementation:
// internal/infrastructure/mdparser (the only package allowed to import
// goldmark, ADR 0002).
type DocumentParser interface {
	Parse(ctx context.Context, file ScannedFile) (*corpus.Document, error)
}

// DocumentParserFactory mints DocumentParsers. A single DocumentParser is not
// guaranteed goroutine-safe (its underlying goldmark parser carries per-call
// mutable state), so the parse stage obtains parsers through this factory: the
// single-worker fast path uses one parser, and the fan-out path calls Clone per
// worker for an independent parser. Production implementation:
// internal/infrastructure/mdparser.
type DocumentParserFactory interface {
	// New returns a freshly configured DocumentParser.
	New() DocumentParser
	// Clone returns an independent DocumentParser safe to use on its own
	// goroutine. It is equivalent to New but names the per-worker intent.
	Clone() DocumentParser
}

// Artifact is a single named output to be written to the output directory.
type Artifact struct {
	// Name is the artifact filename relative to the output directory.
	Name string
	// Content is the rendered artifact bytes.
	Content []byte
}

// ArtifactWriter persists rendered artifacts, sanitizing every path to stay
// under the output directory (reverse zip-slip, ADR 0003). Production
// implementation: internal/infrastructure/emit.
type ArtifactWriter interface {
	Write(ctx context.Context, artifacts []Artifact) error
}

// ExternalLinkChecker validates a batch of external (http/https) URLs for
// liveness, applying the SSRF guard, bounded concurrency, per-host rate
// limiting, redirect caps and result de-duplication (ADR 0003). It is the
// opt-in --check-external seam: OFF by default so the deterministic pipeline
// output is unchanged. The domain never imports net/http; this interface keeps
// the checker in infrastructure (internal/infrastructure/linkcheck). The result
// map is keyed by the exact input URL string.
type ExternalLinkChecker interface {
	Check(ctx context.Context, urls []string) map[string]ExternalResult
}

// ExternalResult is the outcome of checking one external URL.
type ExternalResult struct {
	// URL is the checked URL (the map key, repeated for convenience).
	URL string
	// OK is true when the URL responded with a non-error status and passed the
	// SSRF guard.
	OK bool
	// StatusCode is the final HTTP status (0 when no request was made, e.g. a
	// guard refusal or a transport error).
	StatusCode int
	// Blocked is true when the SSRF guard refused the URL (internal/metadata
	// host, disallowed scheme, redirect to an internal host). No network request
	// was made for a blocked URL.
	Blocked bool
	// Err is a human-readable failure reason (empty when OK).
	Err string
}
