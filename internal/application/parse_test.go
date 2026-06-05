package application

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stacklok/doctopus/internal/domain/analysis"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/domain/reference"
)

// fakeExternalChecker is a deterministic stand-in for the HTTP link checker. It
// returns a pre-seeded result per URL (and OK for any URL it was not told
// about), so checkExternalLinks can be exercised without a network.
type fakeExternalChecker struct {
	results map[string]ExternalResult
	gotURLs []string
}

func (f *fakeExternalChecker) Check(_ context.Context, urls []string) map[string]ExternalResult {
	f.gotURLs = append(f.gotURLs, urls...)
	out := make(map[string]ExternalResult, len(urls))
	for _, u := range urls {
		if r, ok := f.results[u]; ok {
			out[u] = r
			continue
		}
		out[u] = ExternalResult{URL: u, OK: true, StatusCode: 200}
	}
	return out
}

var _ ExternalLinkChecker = (*fakeExternalChecker)(nil)

// extRef builds an external reference at a given origin/line/URL.
func extRef(origin, url string, line int) reference.Reference {
	return reference.Reference{
		RawReference: reference.RawReference{
			Origin:    identity.DocumentID(origin),
			RawTarget: url,
			Type:      reference.External,
			Line:      line,
		},
		Health: reference.HealthExternal,
	}
}

// TestCheckExternalLinks_UnwiredEmitsNoticeNoFindings asserts that with
// CheckExternal on but no checker configured, the method emits a single notice
// and returns NO findings (so the flag is never a silent no-op, but also never
// fabricates findings).
func TestCheckExternalLinks_UnwiredEmitsNoticeNoFindings(t *testing.T) {
	var log bytes.Buffer
	cfg := DefaultConfig()
	cfg.CheckExternal = true
	cfg.ExternalChecker = nil
	p := NewPipeline(cfg, &fakeScanner{}, newFakeFactory(&fakeParser{}), &log)

	refs := []reference.Reference{extRef("a.md", "https://example.com", 3)}
	got := p.checkExternalLinks(context.Background(), refs)
	if len(got) != 0 {
		t.Errorf("findings = %d, want 0 when no checker is wired", len(got))
	}
	if !strings.Contains(log.String(), "[check-external-unwired]") {
		t.Errorf("want a check-external-unwired notice, got:\n%s", log.String())
	}
}

// TestCheckExternalLinks_DeadLinkFinding asserts a fake checker that fails a URL
// produces exactly one DeadLink finding carrying the correct origin, line and
// target.
func TestCheckExternalLinks_DeadLinkFinding(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CheckExternal = true
	cfg.ExternalChecker = &fakeExternalChecker{results: map[string]ExternalResult{
		"https://dead.example/x": {URL: "https://dead.example/x", OK: false, StatusCode: 404, Err: "HTTP 404"},
	}}
	p := NewPipeline(cfg, &fakeScanner{}, newFakeFactory(&fakeParser{}), nil)

	refs := []reference.Reference{
		extRef("doc/a.md", "https://dead.example/x", 12),
		extRef("doc/a.md", "https://live.example/ok", 13), // not in results map -> OK
	}
	got := p.checkExternalLinks(context.Background(), refs)
	if len(got) != 1 {
		t.Fatalf("findings = %d, want 1 (only the dead URL)", len(got))
	}
	f := got[0]
	if f.Kind != analysis.DeadLink {
		t.Errorf("Kind = %v, want DeadLink", f.Kind)
	}
	if f.Location.Document != identity.DocumentID("doc/a.md") {
		t.Errorf("origin = %q, want doc/a.md", f.Location.Document)
	}
	if f.Location.Line != 12 {
		t.Errorf("line = %d, want 12", f.Location.Line)
	}
	if f.Details[DetailTarget] != "https://dead.example/x" {
		t.Errorf("target detail = %q, want the dead URL", f.Details[DetailTarget])
	}
	if f.Details[DetailStatusCode] != "404" {
		t.Errorf("statusCode detail = %q, want 404", f.Details[DetailStatusCode])
	}
}

// TestCheckExternalLinks_DeduplicatesURLs asserts the checker is handed each
// unique http(s) URL exactly once (non-http schemes are filtered out), even when
// the same URL appears in multiple references.
func TestCheckExternalLinks_DeduplicatesURLs(t *testing.T) {
	checker := &fakeExternalChecker{}
	cfg := DefaultConfig()
	cfg.CheckExternal = true
	cfg.ExternalChecker = checker
	p := NewPipeline(cfg, &fakeScanner{}, newFakeFactory(&fakeParser{}), nil)

	refs := []reference.Reference{
		extRef("a.md", "https://example.com", 1),
		extRef("b.md", "https://example.com", 2),       // duplicate URL
		extRef("c.md", "mailto:nobody@example.com", 3), // non-http: filtered
	}
	_ = p.checkExternalLinks(context.Background(), refs)
	if len(checker.gotURLs) != 1 || checker.gotURLs[0] != "https://example.com" {
		t.Errorf("checker received %v, want exactly [https://example.com]", checker.gotURLs)
	}
}

func TestIsHTTPURL(t *testing.T) {
	cases := map[string]bool{
		"http://x":    true,
		"https://x":   true,
		"HTTPS://X":   true,
		"HtTp://x":    true,
		"ftp://x":     false,
		"mailto:a@b":  false,
		"":            false,
		"http":        false,
		"/local/path": false,
	}
	for in, want := range cases {
		if got := isHTTPURL(in); got != want {
			t.Errorf("isHTTPURL(%q) = %v, want %v", in, got, want)
		}
	}
}
