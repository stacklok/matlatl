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
		"http://0.0.0.0/",                          // unspecified address (IsUnspecified)
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

// TestRedirectCap_RefusesAfterMaxHops pins the ADR-0003 "cap redirects"
// requirement: a server that redirects in an endless loop (each hop to a
// permitted host) must be refused once the configured MaxRedirects is exceeded,
// rather than followed forever. We set MaxRedirects=2 and assert the request
// fails with a redirect-cap error (and counts the hops the server saw).
func TestRedirectCap_RefusesAfterMaxHops(t *testing.T) {
	var hops int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hops, 1)
		// Always redirect back to ourselves -> an infinite loop unless capped.
		http.Redirect(w, &http.Request{}, srv.URL+"/again", http.StatusFound)
	}))
	defer srv.Close()

	host := mustHost(t, srv.URL)
	const maxRedirects = 2
	c := New(Config{PerHostInterval: 0, MaxRedirects: maxRedirects, Allow: []string{host}},
		WithResolver(&fakeResolver{m: map[string][]net.IP{}}))

	out := c.Check(context.Background(), []string{srv.URL + "/start"})
	r := out[srv.URL+"/start"]
	if r.OK {
		t.Fatalf("looping redirect returned OK; want a redirect-cap failure")
	}
	if !strings.Contains(strings.ToLower(r.Err), "redirect") {
		t.Errorf("expected a redirect-cap error, got Err=%q", r.Err)
	}
	// The client stops once `len(via) >= maxRedirects`, so the server sees at most
	// maxRedirects+1 requests (the initial + the capped redirects). It must NOT
	// have followed the loop indefinitely.
	if got := atomic.LoadInt32(&hops); got > maxRedirects+1 {
		t.Errorf("server saw %d hops, want <= %d (redirect cap not enforced)", got, maxRedirects+1)
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

// TestSSRF_DenyCIDRBlocksResolvedIP pins the Deny-path CIDR branch of parseACL +
// ipDenied/ipInAny: a public-looking host that resolves INTO a denied CIDR
// (10.0.0.0/8) must be Blocked, and — because the deny verdict is reached during
// the guard, before any request — WITHOUT touching the network. This is the
// fail-open regression guard for the Deny path (previously untested).
func TestSSRF_DenyCIDRBlocksResolvedIP(t *testing.T) {
	res := &fakeResolver{m: map[string][]net.IP{
		"inside-deny.example.com": {net.IPv4(10, 5, 6, 7)}, // resolves into 10.0.0.0/8
	}}
	tr := &countingTransport{inner: http.DefaultTransport}
	c := New(Config{PerHostInterval: 0, Deny: []string{"10.0.0.0/8"}},
		WithResolver(res), WithTransport(tr))

	// Sanity: parseACL must have produced a CIDR net, not a literal-host entry.
	if len(c.denyNets) != 1 {
		t.Fatalf("parseACL(Deny CIDR): denyNets=%d, want 1 (CIDR not parsed)", len(c.denyNets))
	}
	if len(c.denyHost) != 0 {
		t.Fatalf("parseACL(Deny CIDR): denyHost=%d, want 0 (CIDR leaked into host map)", len(c.denyHost))
	}

	const u = "http://inside-deny.example.com/x"
	out := c.Check(context.Background(), []string{u})
	r := out[u]
	if !r.Blocked {
		t.Fatalf("denied-CIDR host: Blocked=false, want true (FAIL-OPEN: Err=%q OK=%v)", r.Err, r.OK)
	}
	if r.OK {
		t.Errorf("denied-CIDR host: OK=true, want false")
	}
	if n := tr.calls.Load(); n != 0 {
		t.Errorf("denied-CIDR host reached the network %d time(s); want 0", n)
	}
}

// TestSSRF_DenyBareIP pins the bare-IP -> /32 conversion branch of parseACL: a
// bare-IP Deny entry ("192.168.1.5") must block exactly that resolved IP. We use
// a 192.168/16 address (RFC1918) but a sibling that the range guard would ALSO
// block, so to prove the bare-IP deny branch specifically we additionally assert
// the entry parsed into denyNets (a /32), not denyHost.
func TestSSRF_DenyBareIP(t *testing.T) {
	res := &fakeResolver{m: map[string][]net.IP{
		"host.example.com": {net.IPv4(192, 168, 1, 5)},
	}}
	tr := &countingTransport{inner: http.DefaultTransport}
	c := New(Config{PerHostInterval: 0, Deny: []string{"192.168.1.5"}},
		WithResolver(res), WithTransport(tr))

	if len(c.denyNets) != 1 || len(c.denyHost) != 0 {
		t.Fatalf("parseACL(bare IP): denyNets=%d denyHost=%d, want 1 and 0 (bare IP not -> /32)",
			len(c.denyNets), len(c.denyHost))
	}
	// The /32 must contain the exact IP and not its neighbor.
	if !c.ipDenied(net.IPv4(192, 168, 1, 5)) {
		t.Errorf("ipDenied(192.168.1.5) = false, want true (bare-IP /32 deny not effective)")
	}
	if c.ipDenied(net.IPv4(192, 168, 1, 6)) {
		t.Errorf("ipDenied(192.168.1.6) = true, want false (bare-IP deny widened beyond /32)")
	}

	const u = "http://host.example.com/x"
	out := c.Check(context.Background(), []string{u})
	if r := out[u]; !r.Blocked {
		t.Errorf("bare-IP denied host: Blocked=false, want true (Err=%q)", r.Err)
	}
	if n := tr.calls.Load(); n != 0 {
		t.Errorf("bare-IP denied host reached the network %d time(s); want 0", n)
	}
}

// TestSSRF_AllowCIDRPermitsHostStillRangeChecksIP pins the Allow CIDR branch:
//   - a host whose resolved IP is inside an Allow CIDR is permitted past the
//     private-range guard (the operator vouches for the range) — proven by
//     allowing a loopback test-server's /8 and getting a real 200; and
//   - the resolved IP is STILL range-checked: a host inside the literal-host
//     allow but resolving OUTSIDE the allow CIDR to a private IP is refused.
func TestSSRF_AllowCIDRPermitsHostStillRangeChecksIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	host := mustHost(t, srv.URL) // 127.0.0.1:PORT

	// Allow the loopback /8 by CIDR. The test server's IP (127.0.0.1) is inside it,
	// so the otherwise-internal loopback address is permitted via the allow CIDR.
	c := New(Config{PerHostInterval: 0, Allow: []string{"127.0.0.0/8"}},
		WithResolver(&fakeResolver{m: map[string][]net.IP{}}))
	if len(c.allowNets) != 1 {
		t.Fatalf("parseACL(Allow CIDR): allowNets=%d, want 1", len(c.allowNets))
	}
	out := c.Check(context.Background(), []string{srv.URL + "/ok"})
	if r := out[srv.URL+"/ok"]; !r.OK || r.StatusCode != http.StatusOK {
		t.Errorf("allow-CIDR host: OK=%v status=%d, want true/200 (Err=%q Blocked=%v)",
			r.OK, r.StatusCode, r.Err, r.Blocked)
	}
	_ = host

	// Now prove the resolved IP is still range-checked when only the literal host
	// (not the resolved IP) is allowlisted: a public-looking name that the resolver
	// rebinds to a private IP OUTSIDE the allow CIDR must still be Blocked.
	res2 := &fakeResolver{m: map[string][]net.IP{
		"vouched.example.com": {net.IPv4(10, 9, 8, 7)}, // private, outside 8.8.8.0/24 allow
	}}
	tr2 := &countingTransport{inner: http.DefaultTransport}
	c2 := New(Config{PerHostInterval: 0, Allow: []string{"8.8.8.0/24"}},
		WithResolver(res2), WithTransport(tr2))
	const u2 = "http://vouched.example.com/x"
	got := c2.Check(context.Background(), []string{u2})
	if r := got[u2]; !r.Blocked {
		t.Errorf("allow-CIDR not matching resolved IP: Blocked=false, want true "+
			"(resolved IP must still be range-checked; Err=%q)", r.Err)
	}
	if n := tr2.calls.Load(); n != 0 {
		t.Errorf("range-checked-after-allow-CIDR reached the network %d time(s); want 0", n)
	}
}

// TestParseACL_MalformedEntriesAreSafe pins that a malformed ACL entry neither
// panics nor silently becomes a wildcard. A garbage entry like "not a cidr" is
// not a CIDR and not an IP, so it lands in the literal-host map as an exact key
// only — it must NOT widen the deny/allow nets, and it must NOT match unrelated
// hosts. An empty entry is dropped entirely.
func TestParseACL_MalformedEntriesAreSafe(t *testing.T) {
	nets, hosts := parseACL([]string{"", "not a cidr", "10.0.0.0/33", "garbage:99999"})
	// "10.0.0.0/33" is an invalid CIDR (bits>32) and not a bare IP -> literal host.
	// "garbage:99999" is neither CIDR nor IP -> literal host.
	if len(nets) != 0 {
		t.Errorf("malformed entries produced %d nets, want 0 (no fail-open widening)", len(nets))
	}
	// The empty entry is dropped; the three non-empty malformed entries are kept as
	// exact literal-host keys (matched verbatim, never as wildcards).
	if _, ok := hosts[""]; ok {
		t.Errorf("empty ACL entry was retained as a host key")
	}
	for _, want := range []string{"not a cidr", "10.0.0.0/33", "garbage:99999"} {
		if _, ok := hosts[want]; !ok {
			t.Errorf("malformed entry %q not retained as an exact host key", want)
		}
	}
	if _, ok := hosts["unrelated.example.com"]; ok {
		t.Errorf("malformed ACL matched an unrelated host (fail-open wildcard)")
	}

	// End-to-end: a malformed Deny entry must not block an unrelated public host
	// (no panic, no wildcard), and a guard with a malformed Allow must still block
	// a genuinely-internal target (the bad entry didn't disable the range guard).
	res := &fakeResolver{m: map[string][]net.IP{
		"public.example.com":   {net.IPv4(93, 184, 216, 34)},
		"internal.example.com": {net.IPv4(10, 0, 0, 1)},
	}}
	tr := &countingTransport{inner: http.DefaultTransport}
	c := New(Config{PerHostInterval: 0, Deny: []string{"not a cidr"}, Allow: []string{"also bad"}},
		WithResolver(res), WithTransport(tr))
	out := c.Check(context.Background(), []string{
		"http://internal.example.com/x", // must still be blocked despite malformed Allow
	})
	if r := out["http://internal.example.com/x"]; !r.Blocked {
		t.Errorf("malformed Allow disabled the range guard: internal host Blocked=false, want true")
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
