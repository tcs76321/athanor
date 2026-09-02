package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeControl is an in-memory Control for HTTP-level tests; the real
// KillSwitch has its own persistence tests in internal/control.
type fakeControl struct {
	frozen   bool
	unfrozen string // last reason passed to Unfreeze
}

func (f *fakeControl) Frozen() bool { return f.frozen }
func (f *fakeControl) Freeze(context.Context) error {
	f.frozen = true
	return nil
}
func (f *fakeControl) Unfreeze(_ context.Context, reason string) error {
	if reason == "" {
		return errReasonRequiredStub
	}
	f.frozen = false
	f.unfrozen = reason
	return nil
}

var errReasonRequiredStub = &stubError{"unfreeze requires an explicit reason"}

type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }

func newTestServer(t *testing.T) (*httptest.Server, *fakeControl) {
	t.Helper()
	ctrl := &fakeControl{}
	srv := New("test-version")
	srv.SetControl(ctrl)
	// Disable the ADR-0011 Host-header allowlist for
	// tests that are not about the middleware. The
	// httptest server binds a random port, which is
	// not in the default 7420 allowlist.
	if err := srv.SetHostAllowlist(nil); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, ctrl
}

func TestFreezeStatusEndpoint(t *testing.T) {
	ts, ctrl := newTestServer(t)
	resp, err := http.Get(ts.URL + "/freeze")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Frozen bool `json:"frozen"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Frozen {
		t.Error("fresh daemon reports frozen via GET /freeze")
	}
	ctrl.frozen = true
	resp2, err := http.Get(ts.URL + "/freeze")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp2.Body.Close() }()
	_ = json.NewDecoder(resp2.Body).Decode(&body)
	if !body.Frozen {
		t.Error("frozen daemon reports running via GET /freeze")
	}
}

func TestFreezePostFreezes(t *testing.T) {
	ts, ctrl := newTestServer(t)
	resp, err := http.Post(ts.URL+"/freeze", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /freeze status = %d, want 200", resp.StatusCode)
	}
	if !ctrl.Frozen() {
		t.Error("POST /freeze did not freeze the control")
	}
}

func TestUnfreezeRequiresReason(t *testing.T) {
	ts, ctrl := newTestServer(t)
	ctrl.frozen = true

	// Missing reason → 400, still frozen.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/freeze", bytes.NewReader([]byte(`{}`)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("DELETE /freeze without reason status = %d, want 400", resp.StatusCode)
	}
	if !ctrl.Frozen() {
		t.Error("unfreeze without reason succeeded at the control layer")
	}

	// With reason → 200 and unfrozen.
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/freeze",
		bytes.NewReader([]byte(`{"reason":"alarm investigated"}`)))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /freeze with reason status = %d, want 200", resp.StatusCode)
	}
	if ctrl.Frozen() || ctrl.unfrozen != "alarm investigated" {
		t.Errorf("unfreeze did not reach control with reason: %+v", ctrl)
	}
}

func TestFreezeMethodNotAllowed(t *testing.T) {
	ts, _ := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/freeze", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("PUT /freeze status = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != "GET, POST, DELETE" {
		t.Errorf("Allow header = %q, want GET, POST, DELETE", allow)
	}
}

// TestNoControlMeansNoFreezeRoute proves the control routes are only
// registered when a control surface is attached: a daemon that cannot be
// frozen also cannot be driven through it.
func TestNoControlMeansNoFreezeRoute(t *testing.T) {
	srv := New("test-version")
	// Disable the ADR-0011 allowlist for this test (the
	// no-control case is about route registration, not
	// the Host header).
	if err := srv.SetHostAllowlist(nil); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	resp, err := http.Get(ts.URL + "/freeze")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /freeze without control = %d, want 404", resp.StatusCode)
	}
}
