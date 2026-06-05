// Package linkcheck implements application.ExternalLinkChecker: an opt-in
// (--check-external) HTTP liveness checker for external http(s) links, with a
// mandatory SSRF guard (ADR 0003). It is the ONLY package that performs outbound
// network requests, and it lives in infrastructure so the domain stays free of
// net/http (ADR 0004).
//
// Security model (ADR 0003 invariant 6). Before any request, and again on every
// redirect hop, the checker:
//   - rejects non-http(s) schemes (file://, data:, gopher:// … are never fetched);
//   - resolves the host to IPs and rejects the request if ANY resolved IP is
//     loopback, link-local, the cloud metadata address (169.254.169.254),
//     unique-local, or in a private RFC1918 range — checking the RESOLVED IP
//     (not just the literal host) defeats DNS-rebinding-to-internal;
//   - honors an explicit allow/deny list of host:port or IP/CIDR before the
//     range checks.
//
// It bounds concurrency, applies a per-host minimum interval (rate limit) and a
// per-request timeout, caps redirects, and de-duplicates URLs so the same URL is
// never fetched twice. It is fully injectable: the HTTP transport and the DNS
// resolver are interfaces so tests drive it with an httptest server and a fake
// resolver and assert that internal targets are refused WITHOUT a network call.
package linkcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/stacklok/doctopus/internal/application"
)

// Resolver looks up the IP addresses for a host. The standard implementation
// wraps net.Resolver; tests inject a fake to exercise the guard deterministically
// (including DNS-rebinding: a public-looking host that resolves to a private IP).
type Resolver interface {
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
}

// netResolver is the production Resolver backed by the stdlib.
type netResolver struct{ r *net.Resolver }

func (n netResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := n.r.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP)
	}
	return out, nil
}

// Config tunes a Checker. The zero value is safe; New fills defaults.
type Config struct {
	// Concurrency bounds in-flight requests (default 8, min 1).
	Concurrency int
	// Timeout is the per-request timeout (default 5s).
	Timeout time.Duration
	// MaxRedirects caps redirect hops; each hop is re-checked by the SSRF guard.
	// Zero-value semantics: 0 means "use the default of 5" (so the safe-by-default
	// zero Config still follows a sensible number of hops); a negative value
	// (e.g. -1) means "follow NO redirects" (the cap becomes 0 hops, so any
	// redirect is refused). See New, which normalizes these.
	MaxRedirects int
	// PerHostInterval is the minimum spacing between requests to the same host
	// (rate limit; default 200ms). Zero disables rate limiting.
	PerHostInterval time.Duration
	// Allow is an explicit allowlist of host[:port] or IP/CIDR entries. A URL
	// whose host/IP matches an Allow entry skips the private-range guard (the
	// operator vouches for it). Allow is checked before Deny and before ranges.
	Allow []string
	// Deny is an explicit denylist of host[:port] or IP/CIDR entries, checked
	// before the range guard; a match is always refused.
	Deny []string
}

// Checker validates external URLs. Construct with New. It implements
// application.ExternalLinkChecker.
type Checker struct {
	cfg      Config
	client   *http.Client
	resolver Resolver

	mu        sync.Mutex
	lastHost  map[string]time.Time // last request time per host (rate limit)
	allowNets []*net.IPNet
	allowHost map[string]struct{}
	denyNets  []*net.IPNet
	denyHost  map[string]struct{}
}

var _ application.ExternalLinkChecker = (*Checker)(nil)

// errBlocked tags an SSRF-guard refusal so Check can mark the result Blocked.
var errBlocked = errors.New("ssrf guard")

// Option customizes a Checker (mainly for tests: inject a transport/resolver).
type Option func(*Checker)

// WithResolver injects a custom DNS resolver (tests use a fake).
func WithResolver(r Resolver) Option { return func(c *Checker) { c.resolver = r } }

// WithTransport injects a custom http.RoundTripper (tests point it at an
// httptest server). The checker still installs its own redirect guard.
func WithTransport(rt http.RoundTripper) Option {
	return func(c *Checker) { c.client.Transport = rt }
}

// New builds a Checker with the SSRF guard wired in. Defaults are filled for any
// zero Config field.
func New(cfg Config, opts ...Option) *Checker {
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 8
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.MaxRedirects < 0 {
		cfg.MaxRedirects = 0
	} else if cfg.MaxRedirects == 0 {
		cfg.MaxRedirects = 5
	}
	if cfg.PerHostInterval == 0 {
		cfg.PerHostInterval = 200 * time.Millisecond
	}
	c := &Checker{
		cfg:       cfg,
		resolver:  netResolver{r: net.DefaultResolver},
		lastHost:  make(map[string]time.Time),
		allowHost: make(map[string]struct{}),
		denyHost:  make(map[string]struct{}),
	}
	c.client = &http.Client{
		Timeout:   cfg.Timeout,
		Transport: http.DefaultTransport,
	}
	// Re-check every redirect target against the guard (no redirect to internal).
	c.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= cfg.MaxRedirects {
			return fmt.Errorf("stopped after %d redirects", cfg.MaxRedirects)
		}
		if err := c.guard(req.Context(), req.URL); err != nil {
			return fmt.Errorf("redirect blocked: %w", err)
		}
		return nil
	}
	c.allowNets, c.allowHost = parseACL(cfg.Allow)
	c.denyNets, c.denyHost = parseACL(cfg.Deny)
	for _, o := range opts {
		o(c)
	}
	return c
}

// parseACL splits ACL entries into CIDR networks and literal host[:port] keys.
func parseACL(entries []string) ([]*net.IPNet, map[string]struct{}) {
	var nets []*net.IPNet
	hosts := make(map[string]struct{})
	for _, e := range entries {
		if e == "" {
			continue
		}
		if _, ipnet, err := net.ParseCIDR(e); err == nil {
			nets = append(nets, ipnet)
			continue
		}
		if ip := net.ParseIP(e); ip != nil {
			// A bare IP becomes a /32 or /128.
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			_, ipnet, _ := net.ParseCIDR(fmt.Sprintf("%s/%d", e, bits))
			if ipnet != nil {
				nets = append(nets, ipnet)
				continue
			}
		}
		hosts[e] = struct{}{}
	}
	return nets, hosts
}

// Check validates each URL (de-duplicated) with bounded concurrency and returns
// a result keyed by the input URL string. Determinism of output is not required
// (external results are excluded from the default deterministic artifacts), but
// the map is complete: every input URL gets a result.
func (c *Checker) Check(ctx context.Context, urls []string) map[string]application.ExternalResult {
	// De-duplicate inputs (don't fetch the same URL twice).
	uniq := make([]string, 0, len(urls))
	seen := make(map[string]struct{}, len(urls))
	for _, u := range urls {
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		uniq = append(uniq, u)
	}
	slices.Sort(uniq)

	out := make(map[string]application.ExternalResult, len(uniq))
	var mu sync.Mutex
	sem := make(chan struct{}, c.cfg.Concurrency)
	var wg sync.WaitGroup

	for _, u := range uniq {
		select {
		case <-ctx.Done():
			mu.Lock()
			if _, ok := out[u]; !ok {
				out[u] = application.ExternalResult{URL: u, Err: ctx.Err().Error()}
			}
			mu.Unlock()
			continue
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			defer func() { <-sem }()
			res := c.checkOne(ctx, u)
			mu.Lock()
			out[u] = res
			mu.Unlock()
		}(u)
	}
	wg.Wait()
	return out
}

// checkOne validates a single URL: scheme + SSRF guard, rate limit, then a HEAD
// (falling back to GET when the server rejects HEAD).
func (c *Checker) checkOne(ctx context.Context, raw string) application.ExternalResult {
	res := application.ExternalResult{URL: raw}

	u, err := url.Parse(raw)
	if err != nil {
		res.Err = fmt.Sprintf("parse url: %v", err)
		return res
	}
	if gerr := c.guard(ctx, u); gerr != nil {
		res.Blocked = true
		res.Err = gerr.Error()
		return res
	}

	c.rateLimit(ctx, u.Hostname())

	status, err := c.do(ctx, http.MethodHead, raw)
	// Some servers reject HEAD (405/501) — retry with GET before declaring dead.
	if err == nil && (status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented) {
		status, err = c.do(ctx, http.MethodGet, raw)
	}
	if err != nil {
		if errors.Is(err, errBlocked) {
			res.Blocked = true
		}
		res.Err = err.Error()
		return res
	}
	res.StatusCode = status
	res.OK = status >= 200 && status < 400
	if !res.OK {
		res.Err = fmt.Sprintf("HTTP %d", status)
	}
	return res
}

// do performs one request and returns the status code. Redirects are guarded by
// the client's CheckRedirect.
func (c *Checker) do(ctx context.Context, method, raw string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, raw, http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "doctopus-linkcheck/1")
	resp, err := c.client.Do(req)
	if err != nil {
		// Unwrap a guarded redirect refusal so the caller can mark it Blocked.
		if errors.Is(err, errBlocked) {
			return 0, fmt.Errorf("%w: %v", errBlocked, err)
		}
		return 0, fmt.Errorf("request: %w", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// rateLimit enforces a minimum interval between requests to the same host.
func (c *Checker) rateLimit(ctx context.Context, host string) {
	if c.cfg.PerHostInterval <= 0 {
		return
	}
	c.mu.Lock()
	last, ok := c.lastHost[host]
	now := time.Now()
	var wait time.Duration
	if ok {
		if d := c.cfg.PerHostInterval - now.Sub(last); d > 0 {
			wait = d
		}
	}
	c.lastHost[host] = now.Add(wait)
	c.mu.Unlock()
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-ctx.Done():
		case <-t.C:
		}
	}
}

// guard enforces the SSRF policy on a URL: scheme allowlist, ACL, and a
// resolved-IP private-range check. It performs DNS resolution (defeating
// DNS-rebinding by checking the resolved IP, not the literal host). It returns
// errBlocked-wrapped errors so callers can flag a refusal.
func (c *Checker) guard(ctx context.Context, u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q not allowed", errBlocked, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: empty host", errBlocked)
	}
	hostPort := u.Host // host[:port]

	// Explicit allow wins (operator vouches for it) — but only for the literal
	// host/IP; resolved IPs are still range-checked below unless the host or a
	// resolved IP is explicitly allowlisted.
	allowed := c.hostAllowed(host) || c.hostAllowed(hostPort)

	// Explicit deny is absolute.
	if c.hostDenied(host) || c.hostDenied(hostPort) {
		return fmt.Errorf("%w: host %q is denylisted", errBlocked, host)
	}

	// Resolve and check each IP. A literal-IP host short-circuits resolution.
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		resolved, err := c.resolver.LookupIP(ctx, host)
		if err != nil {
			return fmt.Errorf("resolve %q: %w", host, err)
		}
		if len(resolved) == 0 {
			return fmt.Errorf("%w: host %q resolved to no addresses", errBlocked, host)
		}
		ips = resolved
	}

	for _, ip := range ips {
		if c.ipDenied(ip) {
			return fmt.Errorf("%w: resolved IP %s is denylisted", errBlocked, ip)
		}
		if c.ipAllowed(ip) || allowed {
			continue
		}
		if isInternalIP(ip) {
			return fmt.Errorf("%w: %q resolves to internal address %s", errBlocked, host, ip)
		}
	}
	return nil
}

func (c *Checker) hostAllowed(h string) bool { _, ok := c.allowHost[h]; return ok }
func (c *Checker) hostDenied(h string) bool  { _, ok := c.denyHost[h]; return ok }

func (c *Checker) ipAllowed(ip net.IP) bool { return ipInAny(ip, c.allowNets) }
func (c *Checker) ipDenied(ip net.IP) bool  { return ipInAny(ip, c.denyNets) }

func ipInAny(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// isInternalIP reports whether ip is one we must never fetch from a hostile
// repo's link: loopback, link-local (incl. the 169.254.169.254 cloud metadata
// endpoint), unique-local, unspecified, multicast, or a private RFC1918 range.
func isInternalIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if ip.IsPrivate() { // RFC1918 + RFC4193 unique-local (Go's definition)
		return true
	}
	// Belt-and-suspenders: the cloud metadata address is link-local, already
	// caught above, but assert it explicitly so the intent is unmistakable.
	if ip.Equal(net.IPv4(169, 254, 169, 254)) {
		return true
	}
	return false
}
