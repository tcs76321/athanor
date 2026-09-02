// M3-T4 commit 4.1 tests: the typed reflection counter in
// `system_state` (`reflect:counter:<job-id>`) replaces the
// M3-T1 hack of co-opting `jobs.recovery_flag` with a
// `"reflect-N"` string. The tests below exercise the
// read/write helpers in isolation, with a real
// `system_state` table from the standard test fixture.
package engine

import (
	"context"
	"testing"
)

// TestReflectCounter_RoundTrip exercises the
// getReflectCounter / setReflectCounter pair: an unset
// key reads as 0, a written value reads back identically,
// and an upsert overwrites.
func TestReflectCounter_RoundTrip(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	jobID := "test-reflect-rt-1"

	// Unset key → 0.
	if got := e.eng.getReflectCounter(ctx, jobID); got != 0 {
		t.Errorf("unset counter = %d, want 0", got)
	}

	// Set 1, read 1.
	if err := e.eng.setReflectCounter(ctx, jobID, 1); err != nil {
		t.Fatalf("setReflectCounter(1): %v", err)
	}
	if got := e.eng.getReflectCounter(ctx, jobID); got != 1 {
		t.Errorf("counter after set 1 = %d, want 1", got)
	}

	// Set 2, read 2 (upsert overwrites).
	if err := e.eng.setReflectCounter(ctx, jobID, 2); err != nil {
		t.Fatalf("setReflectCounter(2): %v", err)
	}
	if got := e.eng.getReflectCounter(ctx, jobID); got != 2 {
		t.Errorf("counter after set 2 = %d, want 2", got)
	}
}

// TestReflectCounter_KeyIsolatedPerJob confirms that
// counters for different jobs are independent: bumping
// one does not bleed into the other.
func TestReflectCounter_KeyIsolatedPerJob(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	if err := e.eng.setReflectCounter(ctx, "job-A", 3); err != nil {
		t.Fatal(err)
	}
	if err := e.eng.setReflectCounter(ctx, "job-B", 1); err != nil {
		t.Fatal(err)
	}
	if got := e.eng.getReflectCounter(ctx, "job-A"); got != 3 {
		t.Errorf("job-A counter = %d, want 3", got)
	}
	if got := e.eng.getReflectCounter(ctx, "job-B"); got != 1 {
		t.Errorf("job-B counter = %d, want 1", got)
	}
}

// TestReflectCounter_MalformedValueReturnsZero: a corrupt
// or non-numeric value in `system_state` must not panic
// and must be treated as 0. A corrupt counter must not
// block the engine.
func TestReflectCounter_MalformedValueReturnsZero(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	jobID := "test-reflect-corrupt"

	// Inject a malformed value directly into system_state.
	if _, err := e.db.DB().ExecContext(ctx,
		`INSERT INTO system_state (key, value) VALUES (?, ?)`,
		reflectCounterPrefix+jobID, "not-a-number",
	); err != nil {
		t.Fatal(err)
	}

	if got := e.eng.getReflectCounter(ctx, jobID); got != 0 {
		t.Errorf("corrupt counter = %d, want 0 (default)", got)
	}
}
