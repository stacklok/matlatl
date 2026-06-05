package application

import (
	"cmp"
	"context"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/stacklok/doctopus/internal/domain/analysis"
	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/domain/reference"
)

// maxParseWorkers caps the autodetected worker pool. Parsing is CPU + a little
// I/O; beyond a modest fan-out the single-threaded deterministic merge and the
// per-file allocation dominate, so we cap to keep goroutine/scheduler overhead
// bounded on many-core machines.
const maxParseWorkers = 16

// parseResult is one file's parse outcome, tagged with its DocumentID so the
// merge can run in a deterministic (sorted) order independent of which worker
// finished first.
type parseResult struct {
	id  identity.DocumentID
	doc *corpus.Document
	err error
}

// resolveWorkerCount turns the configured ParseWorkers into an effective worker
// count: 0 → min(GOMAXPROCS, maxParseWorkers); a positive value is honored but
// capped; it never exceeds the number of files (no idle workers).
func resolveWorkerCount(configured, files int) int {
	n := configured
	if n <= 0 {
		n = runtime.GOMAXPROCS(0)
	}
	if n > maxParseWorkers {
		n = maxParseWorkers
	}
	if n > files {
		n = files
	}
	if n < 1 {
		n = 1
	}
	return n
}

// parseFiles parses every scanned file and returns the results sorted by
// DocumentID. It fans out parsing across a bounded worker pool (each worker owns
// an independent parser obtained via Factory.Clone, since goldmark parsers are
// not safe to share — ADR 0002), then sorts the per-file results so the
// single-threaded merge in Run is deterministic and byte-identical to the
// sequential path at any worker count.
//
// Per-file parse errors are non-fatal: they are carried on the parseResult and
// surfaced as notices by the caller (ADR 0003 threat model). Context
// cancellation is the only fatal condition — on ctx.Done all workers stop and
// the function returns the cancellation error.
func (p *Pipeline) parseFiles(ctx context.Context, files []ScannedFile) ([]parseResult, error) {
	if len(files) == 0 {
		return nil, nil
	}
	workers := resolveWorkerCount(p.cfg.ParseWorkers, len(files))

	// Single-worker fast path: parse sequentially with one parser. Output is
	// identical to the pool path; this avoids goroutine/channel overhead and is
	// the honest default when concurrency is not configured to help.
	if workers == 1 {
		parser := p.parserFac.New()
		out := make([]parseResult, 0, len(files))
		for _, f := range files {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("pipeline canceled during parse: %w", err)
			}
			doc, err := parser.Parse(ctx, f)
			out = append(out, parseResult{id: f.ID, doc: doc, err: err})
		}
		sortResults(out)
		return out, nil
	}

	results := make([]parseResult, len(files))
	jobs := make(chan int)

	// A cancelable context so the first ctx.Done stops every worker promptly.
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			parser := p.parserFac.Clone() // independent per-worker parser
			for idx := range jobs {
				if wctx.Err() != nil {
					return
				}
				f := files[idx]
				doc, err := parser.Parse(wctx, f)
				// Write into this index only — no shared mutable state, so no race.
				results[idx] = parseResult{id: f.ID, doc: doc, err: err}
			}
		}()
	}

	// Feed jobs, stopping early if the context is canceled.
	feedErr := func() error {
		for i := range files {
			select {
			case <-wctx.Done():
				return fmt.Errorf("pipeline canceled during parse: %w", wctx.Err())
			case jobs <- i:
			}
		}
		return nil
	}()
	close(jobs)
	wg.Wait()

	if feedErr != nil {
		return nil, feedErr
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("pipeline canceled during parse: %w", err)
	}

	sortResults(results)
	return results, nil
}

// sortResults orders parse results by DocumentID so the merge is deterministic
// regardless of worker completion order.
func sortResults(rs []parseResult) {
	slices.SortFunc(rs, func(a, b parseResult) int { return cmp.Compare(a.id, b.id) })
}

// checkExternalLinks runs the injected ExternalLinkChecker over the de-duplicated
// set of HealthExternal http(s) targets and turns failures into DeadLink
// findings. It is called only when --check-external is set; if no checker is
// wired it emits a single notice and returns no findings (so the flag is never a
// silent no-op). All network behavior + the SSRF guard live in the checker
// (infrastructure); this method only marshals references ↔ findings.
func (p *Pipeline) checkExternalLinks(ctx context.Context, refs []reference.Reference) []analysis.Finding {
	if p.cfg.ExternalChecker == nil {
		_, _ = fmt.Fprintln(p.log,
			"doctopus: notice [check-external-unwired] --check-external set but no checker configured; skipping")
		return nil
	}

	// Collect the unique external URLs to check (http/https only — the checker
	// re-validates the scheme, but we avoid handing it mailto:/file: noise).
	urlSet := make(map[string]struct{})
	for _, r := range refs {
		if r.Health != reference.HealthExternal {
			continue
		}
		u := rawTargetText(r)
		if isHTTPURL(u) {
			urlSet[u] = struct{}{}
		}
	}
	if len(urlSet) == 0 {
		return nil
	}
	urls := make([]string, 0, len(urlSet))
	for u := range urlSet {
		urls = append(urls, u)
	}
	slices.Sort(urls)

	results := p.cfg.ExternalChecker.Check(ctx, urls)

	// Emit one DeadLink finding per (origin, failing URL) so the finding is
	// located at the document that contains the bad link. Sorted iteration keeps
	// the output stable even though external findings are excluded from the
	// default run.
	var findings []analysis.Finding
	seen := make(map[string]struct{})
	for _, r := range refs {
		if r.Health != reference.HealthExternal {
			continue
		}
		u := rawTargetText(r)
		res, ok := results[u]
		if !ok || res.OK {
			continue
		}
		key := fmt.Sprintf("%s|%d|%s", r.Origin, r.Line, u)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		findings = append(findings, deadLinkFinding(r, res))
	}
	return findings
}

// isHTTPURL reports whether u has an http:// or https:// scheme (case-folded).
func isHTTPURL(u string) bool {
	lower := strings.ToLower(u)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// deadLinkFinding builds a DeadLink finding for a failed external check. It is a
// Warning severity for reporting (it sorts with the soft findings) but NEVER
// affects the exit code, even under --strict: external checks are opt-in and
// non-deterministic, so they are kept out of the CI gate (see Result.CheckExitCode
// and ADR 0005). It never appears in the default deterministic run.
func deadLinkFinding(r reference.Reference, res ExternalResult) analysis.Finding {
	target := rawTargetText(r)
	reason := res.Err
	if reason == "" {
		switch {
		case res.Blocked:
			reason = "refused by SSRF guard"
		case res.StatusCode != 0:
			reason = fmt.Sprintf("HTTP %d", res.StatusCode)
		default:
			reason = "unreachable"
		}
	}
	details := map[string]string{
		DetailTarget:   target,
		DetailLinkType: r.Type.String(),
	}
	if res.StatusCode != 0 {
		details[DetailStatusCode] = fmt.Sprintf("%d", res.StatusCode)
	}
	if res.Blocked {
		details[DetailBlocked] = "true"
	}
	return analysis.Finding{
		ID:       findingID(analysis.DeadLink, r.Origin, r.Line, target),
		Kind:     analysis.DeadLink,
		Severity: analysis.Warning,
		Location: analysis.Location{Document: r.Origin, Line: r.Line},
		Message:  fmt.Sprintf("external link %q is dead: %s", target, reason),
		SuggestedFix: fmt.Sprintf(
			"Verify %q is reachable; if it moved, update the link, otherwise remove or replace it.", target),
		Details: details,
	}
}
