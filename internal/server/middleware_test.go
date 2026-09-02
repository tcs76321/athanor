// ADR-0011 Host-header allowlist middleware tests
// (the §D5 test corpus).
package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHostAllowlist_RejectsUnknownHost covers §D5
// row 1: a request whose Host is not in the
// allowlist is rejected with 421 Misdirected
// Request.
func TestHostAllowlist_RejectsUnknownHost(t *testing.T) {
	srv := New("test")
	// Keep the production allowlist (port 7420);
	// send a request to a port that's not in it.
	// We use httptest.NewRecorder + NewRequest to
	// bypass the loopback bind.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Host = "127.0.0.1:9999" // not in default allowlist (7420)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("status = %d, want 421 Misdirected", rec.Code)
	}
}

// TestHostAllowlist_RejectsDNSRebindingHost: an
// attacker-controlled hostname that DNS-resolves to
// 127.0.0.1 must be rejected. §D5 row 2.
func TestHostAllowlist_RejectsDNSRebindingHost(t *testing.T) {
	srv := New("test")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Host = "attacker.example.com:7420" // port matches, host does not
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("status = %d, want 421 Misdirected", rec.Code)
	}
}

// TestHostAllowlist_RejectsMissingHost: a request
// without a Host header is rejected with 400.
// §D5 row 3.
func TestHostAllowlist_RejectsMissingHost(t *testing.T) {
	srv := New("test")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Host = ""
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 Bad Request", rec.Code)
	}
}

// TestHostAllowlist_AllowsCanonicalHosts: every entry
// in the §D1 default list is accepted.
func TestHostAllowlist_AllowsCanonicalHosts(t *testing.T) {
	srv := New("test")
	for _, host := range []string{
		"127.0.0.1:7420",
		"localhost:7420",
		"[::1]:7420",
		"athanor.local:7420",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Host = host
		srv.Handler().ServeHTTP(rec, req)
		// healthz returns 200 on GET; 404 on /nope is
		// also "middleware allowed" (i.e. the request
		// reached the handler). Either is a pass.
		if rec.Code == http.StatusMisdirectedRequest || rec.Code == http.StatusBadRequest {
			t.Errorf("host %q: status = %d, want middleware-pass (200/404)", host, rec.Code)
		}
	}
}

// TestHostAllowlist_AllowsAllWhenEmpty: the empty
// allowlist is a documented escape hatch (tests +
// the no-control route test). The middleware must
// be a no-op when the allowlist is empty.
func TestHostAllowlist_AllowsAllWhenEmpty(t *testing.T) {
	srv := New("test")
	if err := srv.SetHostAllowlist(nil); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{
		"127.0.0.1:9999",
		"attacker.example.com:7420",
		"0.0.0.0:0",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Host = host
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code == http.StatusMisdirectedRequest {
			t.Errorf("empty allowlist rejected %q (status %d); want middleware-bypass", host, rec.Code)
		}
	}
}

// TestHostAllowlist_RejectsInvalidEntry: malformed
// entries (no port) are rejected at config time.
func TestHostAllowlist_RejectsInvalidEntry(t *testing.T) {
	srv := New("test")
	err := srv.SetHostAllowlist([]string{"127.0.0.1"})
	if err == nil {
		t.Fatal("SetHostAllowlist with no-port entry returned nil error; want failure")
	}
	if !strings.Contains(err.Error(), "host:port") {
		t.Errorf("error = %v, want 'host:port' guidance", err)
	}
}

// TestHostAllowlist_RejectsEmptyEntry: an empty
// string in the list is rejected.
func TestHostAllowlist_RejectsEmptyEntry(t *testing.T) {
	srv := New("test")
	if err := srv.SetHostAllowlist([]string{""}); err == nil {
		t.Error("SetHostAllowlist with empty-string entry returned nil error; want failure")
	}
}
