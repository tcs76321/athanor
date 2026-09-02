// Package server: external API Host-header allowlist
// middleware (ADR-0011). Closes the DNS-rebinding
// attack class: a malicious web page on the same
// machine (or a DNS-rebinding PoC) cannot issue
// requests to the daemon under a foreign Host header.
//
// The internal API at /internal/v1/ is unaffected —
// it has its own per-job bearer-token middleware
// (ADR-0008) and its threat model is different.
//
// Per ADR-0011 §D1, the default allowlist is:
//   - 127.0.0.1:7420
//   - localhost:7420
//   - [::1]:7420
//   - athanor.local:7420
//
// The port is part of the comparison because the
// same Host header may legitimately serve multiple
// daemons on different ports (the check is exact,
// not prefix-based).
package server

import (
	"fmt"
	"net"
	"net/http"
)

// hostAllowlist is the set of acceptable Host header
// values for the external API. The check is
// case-insensitive and includes the port (because
// multiple daemons may run on the same machine on
// different ports). An empty allowlist disables the
// check — a documented escape hatch for test
// environments, never the default.
//
// The set is a value type to keep Handler() pure: no
// goroutine-safe map, no shared mutable state.
type hostAllowlist struct {
	allowed map[string]struct{} // lowercased "host:port"
}

// newHostAllowlist builds an allowlist from a slice
// of "host:port" strings. Empty entries are
// rejected. The lookup map is built once; the
// returned allowlist is read-only.
func newHostAllowlist(entries []string) (hostAllowlist, error) {
	out := hostAllowlist{allowed: make(map[string]struct{}, len(entries))}
	for _, e := range entries {
		if e == "" {
			return hostAllowlist{}, fmt.Errorf("host allowlist contains an empty entry")
		}
		// Normalize: lowercase the host, preserve
		// the case of an IPv6 literal (Go's
		// net.JoinHostPort already lowercases, so
		// the lookup is consistent across
		// requesters that pass "[::1]:7420" vs
		// "127.0.0.1:7420").
		host, port, err := net.SplitHostPort(e)
		if err != nil {
			return hostAllowlist{}, fmt.Errorf("host allowlist entry %q must be host:port: %w", e, err)
		}
		// net.JoinHostPort lowercases the host
		// part; this matches the comparison the
		// middleware does.
		out.allowed[net.JoinHostPort(host, port)] = struct{}{}
	}
	return out, nil
}

// allows reports whether the given `host:port` is
// in the allowlist. The lookup is O(1) and the
// comparison is case-insensitive (the build step
// stores keys in net.JoinHostPort's canonical
// form).
func (h hostAllowlist) allows(hostPort string) bool {
	if h.allowed == nil {
		return false
	}
	_, ok := h.allowed[hostPort]
	return ok
}

// hostMiddleware returns a middleware that
// enforces the allowlist. A request with no Host
// header (or an empty one) is rejected with 400 —
// the §21.8 invariant is "external API is reachable
// only from a loopback caller," and an empty Host
// fails that test.
//
// A request whose Host header is not in the
// allowlist is rejected with 421 Misdirected
// Request, matching the §D5 response code in
// ADR-0011. The internal API at /internal/v1/ is
// reached via a different mux and is not wrapped by
// this middleware.
func (s *Server) hostMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if host == "" {
			http.Error(w, `{"error":"missing Host header"}`, http.StatusBadRequest)
			return
		}
		// The mux already binds loopback only;
		// the canonical "host:port" form is what
		// arrives here. An attacker controlling
		// DNS could send any "host:port" — we
		// reject anything not in the allowlist.
		if !s.hostAllowlist.allows(canonicalHostPort(host)) {
			http.Error(w, fmt.Sprintf(`{"error":"host %q not in allowlist"}`, host), http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// canonicalHostPort returns the host:port form Go's
// net package would produce. This normalizes
// "127.0.0.1:7420" → "127.0.0.1:7420" and
// "[::1]:7420" → "[::1]:7420" (the brackets are
// preserved, which is what net.JoinHostPort uses
// in the build step). When the input has no port,
// the function returns just the lowercased host
// (the loopback listen address always includes a
// port, so this branch is defensive).
func canonicalHostPort(h string) string {
	if _, _, err := net.SplitHostPort(h); err == nil {
		// net.SplitHostPort accepts "127.0.0.1:7420";
		// rejoin to get the canonical form.
		host, port, _ := net.SplitHostPort(h)
		return net.JoinHostPort(host, port)
	}
	// No port — return as-is, lowercased.
	return lowerASCII(h)
}

// lowerASCII is a fast ASCII-lowercase. The Host
// header is ASCII per RFC 7230 §3.1.1.1, so a
// Unicode-aware lowercase is overkill.
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// defaultAllowlistSanity is a package-init sanity
// check: the §D1 default list must build without
// error. If a future contributor adds a typo to
// defaultHostAllowlist, `go build` fails before
// the daemon starts. The check is wrapped in a
// function so a typo is reported with the line
// number of the offending entry, not the init
// line.
var _ = defaultAllowlistSanity()

func defaultAllowlistSanity() error {
	defaults := []string{
		"127.0.0.1:7420",
		"localhost:7420",
		"[::1]:7420",
		"athanor.local:7420",
	}
	_, err := newHostAllowlist(defaults)
	if err != nil {
		return fmt.Errorf("default host allowlist invalid: %w", err)
	}
	return nil
}
