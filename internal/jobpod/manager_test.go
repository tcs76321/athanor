package jobpod

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeClient records every `podman` invocation and returns canned
// output. Tests use the recording slice to assert the right argv
// was passed at the right moment.
type fakeClient struct {
	mu        sync.Mutex
	calls     [][]string
	responder func(args []string) (stdout, stderr []byte, err error)
}

func (f *fakeClient) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	f.mu.Lock()
	cp := make([]string, len(args))
	copy(cp, args)
	f.calls = append(f.calls, cp)
	responder := f.responder
	f.mu.Unlock()
	if responder == nil {
		return nil, nil, nil
	}
	return responder(cp)
}

func (f *fakeClient) Calls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = append([]string(nil), c...)
	}
	return out
}

// stubFreezer satisfies the Freezer interface for tests.
type stubFreezer struct{ frozen bool }

func (s *stubFreezer) Frozen() bool { return s.frozen }

// goodID is a valid v4 UUID for use in tests.
const goodID = "3b241101-e2bb-4255-8caf-4136c566a962"

// validSpec returns a Spec that passes validation.
func validSpec() Spec {
	return Spec{
		ID:      goodID,
		Image:   "alpine:3.20",
		Command: []string{"sleep", "10"},
	}
}

// TestStart_ValidatesSpec asserts every invalid Spec shape returns
// ErrInvalidSpec without making a client call.
func TestStart_ValidatesSpec(t *testing.T) {
	client := &fakeClient{}
	m := New(client, &stubFreezer{})

	cases := []struct {
		name string
		mut  func(*Spec)
	}{
		{"empty id", func(s *Spec) { s.ID = "" }},
		{"non-uuid id", func(s *Spec) { s.ID = "not-a-uuid" }},
		{"v1 uuid", func(s *Spec) { s.ID = "3b241101-e2bb-1255-8caf-4136c566a962" }},
		{"empty image", func(s *Spec) { s.Image = "" }},
		{"empty command", func(s *Spec) { s.Command = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validSpec()
			tc.mut(&s)
			_, err := m.Start(context.Background(), s)
			if !errors.Is(err, ErrInvalidSpec) {
				t.Errorf("err = %v, want ErrInvalidSpec", err)
			}
			if len(client.Calls()) != 0 {
				t.Errorf("client was called %d times, want 0 (validation must fail fast)", len(client.Calls()))
			}
		})
	}
}

// TestStart_RespectsFreezer asserts a frozen freezer blocks new pods
// without making a client call.
func TestStart_RespectsFreezer(t *testing.T) {
	client := &fakeClient{}
	m := New(client, &stubFreezer{frozen: true})
	_, err := m.Start(context.Background(), validSpec())
	if !errors.Is(err, ErrFrozen) {
		t.Errorf("err = %v, want ErrFrozen", err)
	}
	if len(client.Calls()) != 0 {
		t.Errorf("client called %d times while frozen, want 0", len(client.Calls()))
	}
}

// TestStart_HappyPath asserts the pod is registered with StatePending
// after a successful Start, and the first client call is `podman run`
// with the §21.2 flag block in argv.
func TestStart_HappyPath(t *testing.T) {
	client := &fakeClient{}
	m := New(client, &stubFreezer{})

	pod, err := m.Start(context.Background(), validSpec())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if pod.State != StatePending {
		t.Errorf("pod.State = %s, want pending", pod.State)
	}
	if pod.ID != goodID {
		t.Errorf("pod.ID = %q, want %q", pod.ID, goodID)
	}

	calls := client.Calls()
	if len(calls) < 1 {
		t.Fatal("client was not called")
	}
	first := calls[0]
	if first[0] != "run" {
		t.Errorf("first call[0] = %q, want \"run\"", first[0])
	}
	joined := joinArgs(first)
	for _, want := range []string{"--read-only", "--cap-drop", "ALL", "--network", "none"} {
		if !contains(joined, want) {
			t.Errorf("argv missing %q\ngot: %s", want, joined)
		}
	}

	got, err := m.Get(goodID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != goodID {
		t.Errorf("Get returned %q, want %q", got.ID, goodID)
	}
}

// TestStart_DuplicateID asserts a second Start with the same ID
// returns ErrAlreadyExists.
func TestStart_DuplicateID(t *testing.T) {
	client := &fakeClient{}
	m := New(client, &stubFreezer{})
	if _, err := m.Start(context.Background(), validSpec()); err != nil {
		t.Fatal(err)
	}
	_, err := m.Start(context.Background(), validSpec())
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}
}

// TestStop_HappyPath asserts Stop calls `podman rm -f <id>` and
// removes the pod from the in-memory map.
func TestStop_HappyPath(t *testing.T) {
	client := &fakeClient{}
	m := New(client, &stubFreezer{})
	if _, err := m.Start(context.Background(), validSpec()); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background(), goodID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	calls := client.Calls()
	last := calls[len(calls)-1]
	if last[0] != "rm" || last[1] != "-f" || last[2] != goodID {
		t.Errorf("last call = %v, want [rm -f %s]", last, goodID)
	}
	if _, err := m.Get(goodID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Stop err = %v, want ErrNotFound", err)
	}
}

// TestStop_NotFound asserts Stop on an unknown ID returns ErrNotFound
// without calling the client.
func TestStop_NotFound(t *testing.T) {
	client := &fakeClient{}
	m := New(client, &stubFreezer{})
	callsBefore := len(client.Calls())
	if err := m.Stop(context.Background(), goodID); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if len(client.Calls()) != callsBefore {
		t.Errorf("client was called for a non-existent pod")
	}
}

// TestStop_Idempotent asserts the second Stop on a stopped pod is a
// no-op (ErrNotFound because the pod is no longer in the map).
func TestStop_Idempotent(t *testing.T) {
	client := &fakeClient{}
	m := New(client, &stubFreezer{})
	if _, err := m.Start(context.Background(), validSpec()); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background(), goodID); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	callsBefore := len(client.Calls())
	if err := m.Stop(context.Background(), goodID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Stop err = %v, want ErrNotFound", err)
	}
	if len(client.Calls()) != callsBefore {
		t.Errorf("second Stop called the client; should be a no-op")
	}
}

// TestSupervise_TransitionsToStopped asserts the supervisor updates
// the in-memory Pod to StateStopped when `podman inspect` reports
// `exited 0`.
func TestSupervise_TransitionsToStopped(t *testing.T) {
	runCount := 0
	inspectCount := 0
	client := &fakeClient{
		responder: func(args []string) ([]byte, []byte, error) {
			switch args[0] {
			case "run":
				runCount++
				return nil, nil, nil
			case "inspect":
				inspectCount++
				return []byte("exited 0"), nil, nil
			}
			return nil, nil, nil
		},
	}
	m := New(client, &stubFreezer{}).(*manager)
	if _, err := m.Start(context.Background(), validSpec()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := m.Get(goodID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == StateStopped {
			if runCount != 1 {
				t.Errorf("run called %d times, want 1", runCount)
			}
			if inspectCount < 1 {
				t.Errorf("inspect called %d times, want >= 1", inspectCount)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	got, _ := m.Get(goodID)
	t.Fatalf("pod state never reached stopped; final state = %s", got.State)
}

// TestSupervise_TransitionsToFailed asserts the supervisor records
// StateFailed and a non-nil ExitErr when the pod exits non-zero.
func TestSupervise_TransitionsToFailed(t *testing.T) {
	client := &fakeClient{
		responder: func(args []string) ([]byte, []byte, error) {
			switch args[0] {
			case "run":
				return nil, nil, nil
			case "inspect":
				return []byte("exited 1"), nil, nil
			}
			return nil, nil, nil
		},
	}
	m := New(client, &stubFreezer{}).(*manager)
	if _, err := m.Start(context.Background(), validSpec()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := m.Get(goodID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == StateFailed {
			if got.ExitErr == nil {
				t.Error("ExitErr is nil on failed pod")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	got, _ := m.Get(goodID)
	t.Fatalf("pod state never reached failed; final state = %s", got.State)
}

// TestParseInspect covers the inspect output parser directly so the
// supervisor's behavior is not just tested through the manager.
func TestParseInspect(t *testing.T) {
	cases := []struct {
		in           string
		wantStatus   string
		wantExitCode int
	}{
		{"running 0", "running", 0},
		{"exited 0", "exited", 0},
		{"exited 1", "exited", 1},
		{"stopped 137", "stopped", 137},
		{"  running 0  \n", "running", 0},
		{"unknown", "unknown", 0},
		{"", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			s, c := parseInspect(tc.in)
			if s != tc.wantStatus || c != tc.wantExitCode {
				t.Errorf("parseInspect(%q) = (%q, %d), want (%q, %d)",
					tc.in, s, c, tc.wantStatus, tc.wantExitCode)
			}
		})
	}
}

// contains is a tiny helper for substring assertions, used to keep
// the test file free of an extra strings import.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
