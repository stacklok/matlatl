package linkcheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeResolver maps hostnames to fixed IPs so the SSRF guard is exercised
// deterministically and WITHOUT real DNS. A host that resolves to a private IP
// models a DNS-rebinding-to-internal attack (public-looking name, internal IP).
// It is read-only after construction (the map is not mutated concurrently), so
// LookupIP is goroutine-safe; the call counter is atomic.
type fakeResolver struct {
	m       map[string][]net.IP
	calls   atomic.Int64
	lastErr error
}

func (f *fakeResolver) LookupIP(_ context.Context, host string) ([]net.IP, error) {
	f.calls.Add(1)
	if ips, ok := f.m[host]; ok {
		return ips, nil
	}
	if f.lastErr != nil {
		return nil, f.lastErr
	}
	// Default: pretend everything resolves to a public IP.
	return []net.IP{net.IPv4(93, 184, 216, 34)}, nil
}

// countingTransport asserts whether the HTTP layer was ever touched. The SSRF
// tests require ZERO network for refused targets.
type countingTransport struct {
	inner http.RoundTripper
	calls atomic.Int64
}

func (t *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return t.inner.RoundTrip(r)
}

// TestSSRF_RefusesInternalWithoutNetwork is the mandated proof (ADR 0003): every
// internal/metadata/loopback/private/disallowed-scheme target is refused with
// Blocked=true and the HTTP transport is NEVER invoked.
func TestSSRF_RefusesInternalWithoutNetwork(t *testing.T) {
	res := &fakeResolver{m: map[string][]net.IP{
		// DNS-rebinding: a public-looking host that resolves to a private IP.
		"rebind.example.com": {net.IPv4(10, 0, 0, 5)},
		"metadata.evil.test": {net.IPv4(169, 254, 169, 254)},
	}}
	tr := &countingTransport{inner: http.DefaultTransport}
	c := New(Config{PerHostInterval: 0}, WithResolver(res), WithTransport(tr))

	blocked := []string{
		"http://169.254.169.254/latest/meta-data/", // cloud metadata (link-local)
		"http://127.0.0.1:8080/admin",              // loopback
		"http://localhost/secret",                  // loopback via name (fake resolver default? no -> add)
		"http://10.1.2.3/",                         // RFC1918
		"http://192.168.0.1/",                      // RFC1918
		"http://172.16.5.9/",                       // RFC1918
		"http://[::1]/",                            // IPv6 loopback
		"http://rebind.example.com/",               // DNS-rebinding to 10.x
		"http://metadata.evil.test/",               // name -> metadata IP
		"file:///etc/passwd",                       // disallowed scheme
		"gopher://internal/",                       // disallowed scheme
		"data:text/plain,hi",                       // disallowed scheme
	}
	// localhost must resolve to loopback for the guard; add it to the fake.
	res.m["localhost"] = []net.IP{net.IPv4(127, 0, 0, 1)}

	out := c.Check(context.Background(), blocked)
	for _, u := range blocked {
		r, ok := out[u]
		if !ok {
			t.Fatalf("%s: missing result", u)
		}
		if !r.Blocked {
			t.Errorf("%s: Blocked=false, want true (Err=%q OK=%v)", u, r.Err, r.OK)
		}
		if r.OK {
			t.Errorf("%s: OK=true, want false", u)
		}
	}
	if n := tr.calls.Load(); n != 0 {
		t.Fatalf("SSRF guard let %d request(s) reach the network; want 0", n)
	}
}

// TestSSRF_AllowsPublicAndChecksLiveness drives a real httptest server through
// the checker: a public-looking host (resolved by the fake to the test server's
// loopback IP but explicitly ALLOWLISTED) returns a real status.
func TestSSRF_AllowsPublicAndChecksLiveness(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/nohead":
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	host := mustHost(t, srv.URL)
	// The test server listens on loopback; allowlist it so the guard permits it
	// (we are explicitly vouching for the test endpoint).
	c := New(Config{PerHostInterval: 0, Allow: []string{host}},
		WithResolver(&fakeResolver{m: map[string][]net.IP{}}))

	cases := []struct {
		path   string
		wantOK bool
		status int
	}{
		{"/ok", true, 200},
		{"/missing", false, 404},
		{"/nohead", true, 200}, // HEAD 405 -> GET 200
	}
	urls := make([]string, 0, len(cases))
	for _, tc := range cases {
		urls = append(urls, srv.URL+tc.path)
	}
	out := c.Check(context.Background(), urls)
	for i, tc := range cases {
		r := out[urls[i]]
		if r.OK != tc.wantOK {
			t.Errorf("%s: OK=%v, want %v (status=%d err=%q)", tc.path, r.OK, tc.wantOK, r.StatusCode, r.Err)
		}
		if r.StatusCode != tc.status {
			t.Errorf("%s: status=%d, want %d", tc.path, r.StatusCode, tc.status)
		}
	}
}

// TestSSRF_RedirectToInternalBlocked: a public endpoint that 302-redirects to an
// internal address must be refused at the redirect hop (the guard re-checks each
// target).
func TestSSRF_RedirectToInternalBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "http://10.0.0.1/internal", http.StatusFound)
	}))
	defer srv.Close()
	host := mustHost(t, srv.URL)
	c := New(Config{PerHostInterval: 0, Allow: []string{host}},
		WithResolver(&fakeResolver{m: map[string][]net.IP{}}))

	out := c.Check(context.Background(), []string{srv.URL + "/start"})
	r := out[srv.URL+"/start"]
	if r.OK {
		t.Fatalf("redirect to internal returned OK; want failure")
	}
	if !strings.Contains(strings.ToLower(r.Err), "redirect") && !r.Blocked {
		t.Errorf("expected a redirect/blocked failure, got Err=%q Blocked=%v", r.Err, r.Blocked)
	}
}

// TestCheck_Dedup: the same URL passed twice yields one entry; resolver is not
// hammered more than necessary.
func TestCheck_Dedup(t *testing.T) {
	res := &fakeResolver{m: map[string][]net.IP{"x": {net.IPv4(127, 0, 0, 1)}}}
	c := New(Config{PerHostInterval: 0}, WithResolver(res))
	out := c.Check(context.Background(), []string{"http://x/", "http://x/", "http://x/"})
	if len(out) != 1 {
		t.Fatalf("dedup: %d results, want 1", len(out))
	}
}

// TestGuard_IPClassification unit-checks isInternalIP for the key ranges.
func TestGuard_IPClassification(t *testing.T) {
	internal := []string{"127.0.0.1", "::1", "10.0.0.1", "172.16.0.1", "192.168.1.1",
		"169.254.169.254", "fd00::1", "0.0.0.0"}
	public := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"}
	for _, s := range internal {
		if !isInternalIP(net.ParseIP(s)) {
			t.Errorf("isInternalIP(%s) = false, want true", s)
		}
	}
	for _, s := range public {
		if isInternalIP(net.ParseIP(s)) {
			t.Errorf("isInternalIP(%s) = true, want false", s)
		}
	}
}

func mustHost(t *testing.T, raw string) string {
	t.Helper()
	// raw is like http://127.0.0.1:PORT
	h := strings.TrimPrefix(raw, "http://")
	if i := strings.IndexByte(h, '/'); i >= 0 {
		h = h[:i]
	}
	return h
}

func TestRateLimit_Spacing(t *testing.T) {
	res := &fakeResolver{m: map[string][]net.IP{"x": {net.IPv4(127, 0, 0, 1)}}}
	c := New(Config{PerHostInterval: 30 * time.Millisecond, Concurrency: 1}, WithResolver(res))
	// Allowlist the loopback so the guard does not block (we only test spacing).
	c.allowHost["x"] = struct{}{}
	start := time.Now()
	// Two requests to the same host should be spaced by at least one interval.
	c.rateLimit(context.Background(), "x")
	c.rateLimit(context.Background(), "x")
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("rate limit did not space requests: %v", elapsed)
	}
}
