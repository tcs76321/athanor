package job

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/tcs76321/athanor/internal/store"
	"github.com/tcs76321/athanor/migrations"
)

func TestGetMissingJob(t *testing.T) {
	r, _ := openRepo(t)
	_, err := r.Get(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

func TestActiveFiltersTerminal(t *testing.T) {
	r, s := openRepo(t)
	projectID, taskID := seedProjectTask(t, s)
	ctx := context.Background()

	stuck, err := r.Create(ctx, taskID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	runTo(t, r, stuck.ID, StateContextBuilding) // mid-flight

	done, err := r.Create(ctx, taskID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	runTo(t, r, done.ID, StateCancelled)

	active, err := r.Active(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != stuck.ID {
		t.Fatalf("Active() = %+v, want exactly the mid-flight job", active)
	}
}

// TestCrashRecoveryResumesFromLastCommittedState is the M1-T4 acceptance
// criterion: process death (simulated by closing the store without
// cleanup — exactly what kill -9 leaves behind) at any point mid-job
// leaves the last committed state readable and resumable on restart.
func TestCrashRecoveryResumesFromLastCommittedState(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	ctx := context.Background()

	// Boot once, drive a job partway, then "crash".
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(s.DB(), migrations.FS, ""); err != nil {
		t.Fatal(err)
	}
	r := NewRepository(s)
	seedProjectTask(t, s)
	j, err := r.Create(ctx, "t1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	runTo(t, r, j.ID, StateContextBuilding, StatePlanning, StateDiverging)
	preCrash, err := r.Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Restart: the job is in its last committed state and can continue.
	s2, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	r2 := NewRepository(s2)
	after, err := r2.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("job lost across restart: %v", err)
	}
	if after.State != preCrash.State {
		t.Fatalf("state after restart = %q, want %q (last committed)", after.State, preCrash.State)
	}
	if after.StartedAt == nil {
		t.Error("started_at lost across restart")
	}
	// Resume: the remaining legal path completes.
	resumed := runTo(t, r2, j.ID, StateSynthesizing, StateComparing, StateCompleted)
	if resumed.State != StateCompleted {
		t.Errorf("resumed job state = %q, want completed", resumed.State)
	}

	// The audit trail survives too: created + 3 pre-crash + 3 post-crash
	// transitions.
	events, err := s2.QueryEvents(ctx, store.EventFilter{JobID: j.ID, Category: "jobs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 7 {
		t.Errorf("events across crash = %d, want 7", len(events))
	}
}
