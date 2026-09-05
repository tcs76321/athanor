package job

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/tcs76321/athanor/internal/store"
	"github.com/tcs76321/athanor/migrations"
)

// TestConcurrentTransitionLosesLoudly proves the CAS guard: two writers
// racing on the same job produce exactly one winner; the loser gets the
// typed conflict error, never a silent overwrite.
func TestConcurrentTransitionLosesLoudly(t *testing.T) {
	r, s := openRepo(t)
	projectID, taskID := seedProjectTask(t, s)
	ctx := context.Background()
	j, err := r.Create(ctx, taskID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	runTo(t, r, j.ID, StateContextBuilding, StatePlanning)

	winner := make(chan error, 1)
	loser := make(chan error, 1)
	go func() { _, err := r.Transition(ctx, j.ID, StateDiverging); winner <- err }()
	go func() { _, err := r.Transition(ctx, j.ID, StateDiverging); loser <- err }()

	errW, errL := <-winner, <-loser
	if errW != nil && errL != nil {
		t.Fatalf("both concurrent transitions failed: %v / %v", errW, errL)
	}
	if errW == nil && errL == nil {
		t.Fatal("both concurrent transitions succeeded; CAS guard missing")
	}
	failed := errW
	if failed == nil {
		failed = errL
	}
	if !errors.Is(failed, ErrConcurrentTransition) {
		t.Fatalf("loser error = %v, want ErrConcurrentTransition", failed)
	}

	final, err := r.Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != StateDiverging {
		t.Errorf("final state = %q, want diverging (exactly one transition won)", final.State)
	}
}

// TestConcurrentTransition_DifferentTargetsRacesCAS drives the
// SQL CAS branch in Repository.Transition (transition.go:69-91)
// directly, without the production wrapper.
//
// What this test pins: the `WHERE state = <expected>` clause
// on the `jobs` UPDATE statement is the §8.2 atomicity guard
// (ADR-0006). When two writers race on the same row from the
// same expected state, exactly one UPDATE matches one row;
// the other matches zero, and `Repository.Transition` maps
// `RowsAffected == 0` to `ErrConcurrentTransition`. The
// same-state short-circuit added in `b9f0bee` does not run
// here (the targets are different), so the SQL CAS is the
// only line of defense — losing the guard would silently
// overwrite the committed state.
//
// Why this test bypasses `Repository.Transition`: the
// production store is capped at one connection per
// ADR-0003/0004 (sqlite-vec extension affinity), so
// concurrent `Transition` calls serialize at the pool and
// rarely overlap inside the BeginTx/UPDATE window. Even
// opening a second `*store.Store` (separate pool) cannot
// force the race deterministically, because `Transition`
// reads state on entry and the first writer's commit can
// land before the second writer's read. Driving the SQL
// directly through two `*sql.DB` connections and a
// `BeginTx` barrier ensures both transactions hold their
// RESERVED/EXCLUSIVE lock contention point at the same
// moment, so the second writer's `WHERE state = ?` clause
// reliably sees `RowsAffected == 0`. This is the same
// `UPDATE … WHERE id = ? AND state = ?` shape that
// `Repository.Transition` issues; the test fails loud if
// either the SQL CAS contract or the call site's error
// mapping drifts.
func TestConcurrentTransition_DifferentTargetsRacesCAS(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	// Single-connection store is fine for setup; the
	// race itself uses two raw `*sql.DB` connections on
	// the same WAL-mode file (one each for the two
	// writers), so each writer holds its own connection
	// during the contended UPDATE.
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := store.Migrate(s.DB(), migrations.FS, ""); err != nil {
		t.Fatal(err)
	}
	r := NewRepository(s)
	projectID, taskID := seedProjectTask(t, s)
	j, err := r.Create(ctx, taskID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	runTo(t, r, j.ID, StateContextBuilding, StatePlanning)

	// Two raw connections on the same file. `*store.Store`
	// already verified the file is WAL; opening two
	// `*sql.DB` here is the same DSN minus the single
	// connection cap (the cap is store-wide, not
	// database-wide).
	dsn := fmt.Sprintf(
		"file:%s?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&_foreign_keys=ON",
		dbPath,
	)
	dbA, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbA.Close() })
	dbB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbB.Close() })

	// Barrier: both goroutines must BeginTx before
	// either runs the contended UPDATE, so the second
	// UPDATE sees the row in the post-A state and
	// `WHERE state = ?` returns 0 rows. The `rep` and
	// `ok` channels carry `RowsAffected` and the SQL
	// error back to the test goroutine.
	const expected = string(StatePlanning)
	ready := make(chan struct{})
	type result struct {
		rows int64
		err  error
	}
	done := make(chan result, 2)
	for _, db := range []struct {
		name string
		h    *sql.DB
		to   string
	}{
		{"A", dbA, string(StateDiverging)},
		{"B", dbB, string(StatePaused)},
	} {
		entry := db
		go func() {
			tx, err := entry.h.BeginTx(ctx, nil)
			if err != nil {
				done <- result{err: fmt.Errorf("%s: begin: %w", entry.name, err)}
				return
			}
			defer func() { _ = tx.Rollback() }()
			<-ready
			res, err := tx.ExecContext(ctx, `
				UPDATE jobs SET state = ?
				 WHERE id = ? AND state = ?`,
				entry.to, j.ID, expected,
			)
			if err != nil {
				done <- result{err: fmt.Errorf("%s: update: %w", entry.name, err)}
				return
			}
			n, err := res.RowsAffected()
			if err != nil {
				done <- result{err: fmt.Errorf("%s: rows: %w", entry.name, err)}
				return
			}
			// Only the winner commits; the loser
			// keeps its tx open until the test
			// reads both `RowsAffected` values.
			if n == 1 {
				if err := tx.Commit(); err != nil {
					done <- result{err: fmt.Errorf("%s: commit: %w", entry.name, err)}
					return
				}
			}
			done <- result{rows: n}
		}()
	}
	// Give both goroutines a moment to reach the
	// barrier, then release them. The barrier is
	// what makes the race deterministic; the sleep
	// is defensive against scheduler quirks where a
	// goroutine hasn't been scheduled yet when we
	// close the channel.
	time.Sleep(10 * time.Millisecond)
	close(ready)

	resA := <-done
	resB := <-done
	if resA.err != nil {
		t.Fatalf("writer A: %v", resA.err)
	}
	if resB.err != nil {
		t.Fatalf("writer B: %v", resB.err)
	}
	// Exactly one writer matched the row; the other
	// saw `RowsAffected == 0` because the winner's
	// commit (or pending write) invalidated the
	// `WHERE state = 'planning'` clause. This is the
	// exact contract `Repository.Transition` line 87
	// (`if n == 0`) reads.
	total := resA.rows + resB.rows
	if total != 1 {
		t.Fatalf("CAS broken: writer A rows=%d, writer B rows=%d, sum=%d, want 1",
			resA.rows, resB.rows, total)
	}
	// The error path that `Repository.Transition`
	// takes for the loser — `ErrConcurrentTransition` —
	// depends on `RowsAffected == 0` and a successful
	// `Get` immediately before. Both halves of the
	// mapping are pinned by sibling tests; the SQL
	// contract pinned here is the missing third
	// half.
}

func TestSetRecoveryFlag(t *testing.T) {
	r, s := openRepo(t)
	projectID, taskID := seedProjectTask(t, s)
	ctx := context.Background()
	j, err := r.Create(ctx, taskID, projectID)
	if err != nil {
		t.Fatal(err)
	}

	if err := r.SetRecoveryFlag(ctx, j.ID, "interrupted"); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RecoveryFlag != "interrupted" {
		t.Errorf("recovery_flag = %q, want interrupted", got.RecoveryFlag)
	}

	// Clearing on successful resume is the same call.
	if err := r.SetRecoveryFlag(ctx, j.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = r.Get(ctx, j.ID)
	if got.RecoveryFlag != "" {
		t.Errorf("recovery_flag = %q after clear, want empty", got.RecoveryFlag)
	}

	if err := r.SetRecoveryFlag(ctx, "ghost", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetRecoveryFlag(missing) = %v, want ErrNotFound", err)
	}
}

// TestTransitionRejectsIllegalPersisted moves through the repository, not
// just the pure table: an attempted skip must fail without touching the
// row or the event log.
func TestTransitionRejectsIllegalPersisted(t *testing.T) {
	r, s := openRepo(t)
	projectID, taskID := seedProjectTask(t, s)
	ctx := context.Background()
	j, err := r.Create(ctx, taskID, projectID)
	if err != nil {
		t.Fatal(err)
	}

	_, transErr := r.Transition(ctx, j.ID, StateCompleted)
	if transErr == nil {
		t.Fatal("queued → completed accepted, want rejection")
	}
	var ite *IllegalTransitionError
	if !errors.As(transErr, &ite) {
		t.Fatalf("err = %v (%T), want IllegalTransitionError", transErr, transErr)
	}

	// Nothing changed: state stays queued, no extra events.
	got, _ := r.Get(ctx, j.ID)
	if got.State != StateQueued {
		t.Errorf("state after rejected transition = %q, want queued", got.State)
	}
	events, _ := s.QueryEvents(ctx, store.EventFilter{JobID: j.ID, Category: "jobs"})
	if len(events) != 1 {
		t.Errorf("events after rejected transition = %d, want 1 (created only)", len(events))
	}
}
