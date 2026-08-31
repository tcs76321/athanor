package job

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tcs76321/athanor/internal/store"
	"github.com/tcs76321/athanor/migrations"
)

// openRepo opens a fully migrated store-backed repository in a temp dir.
func openRepo(t *testing.T) (*Repository, *store.Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := store.Migrate(s.DB(), migrations.FS, ""); err != nil {
		t.Fatal(err)
	}
	return NewRepository(s), s
}

// seedProjectTask creates the parent rows a job needs.
func seedProjectTask(t *testing.T, s *store.Store) (projectID, taskID string) {
	t.Helper()
	db := s.DB()
	if _, err := db.Exec(
		`INSERT INTO projects (id, name, archetype, goal) VALUES ('p1','demo','text','write something worth reading')`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tasks (id, project_id, title) VALUES ('t1','p1','do the thing')`); err != nil {
		t.Fatal(err)
	}
	return "p1", "t1"
}

// runTo drives a job through the given legal states.
func runTo(t *testing.T, r *Repository, id string, states ...State) Job {
	t.Helper()
	j, err := r.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		j, err = r.Transition(context.Background(), id, s)
		if err != nil {
			t.Fatalf("transition to %s: %v", s, err)
		}
	}
	return j
}

func TestCreateStartsQueued(t *testing.T) {
	r, s := openRepo(t)
	projectID, taskID := seedProjectTask(t, s)

	j, err := r.Create(context.Background(), taskID, projectID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if j.State != StateQueued {
		t.Errorf("new job state = %q, want queued", j.State)
	}
	if j.ID == "" || j.TaskID != taskID || j.ProjectID != projectID {
		t.Errorf("job fields not persisted: %+v", j)
	}
	if j.StartedAt != nil || j.FinishedAt != nil {
		t.Error("new job must have no start/finish timestamps")
	}

	// Creation is audited in the append-only event log.
	events, err := s.QueryEvents(context.Background(), store.EventFilter{JobID: j.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Category != "jobs" {
		t.Fatalf("creation events = %+v, want one jobs-category event", events)
	}
}

func TestFullLifecycle(t *testing.T) {
	r, s := openRepo(t)
	projectID, taskID := seedProjectTask(t, s)
	ctx := context.Background()

	j, err := r.Create(ctx, taskID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	j = runTo(t, r, j.ID,
		StateContextBuilding, StatePlanning, StateDiverging, StateEvaluating, StateSynthesizing, StateComparing, StateCompleted)

	if j.State != StateCompleted {
		t.Errorf("final state = %q, want completed", j.State)
	}
	if j.StartedAt == nil {
		t.Error("started_at not set on first context_building transition")
	}
	if j.FinishedAt == nil {
		t.Error("finished_at not set on completion")
	}

	// Every transition was audited, in order.
	events, err := s.QueryEvents(ctx, store.EventFilter{JobID: j.ID, Category: "jobs"})
	if err != nil {
		t.Fatal(err)
	}
	// created + 7 transitions (post-§8.2 evaluating is mandatory after diverging)
	if len(events) != 8 {
		t.Fatalf("got %d events, want 8 (created + 7 transitions)", len(events))
	}

	// Terminal states admit no further transitions.
	if _, err := r.Transition(ctx, j.ID, StateQueued); err == nil {
		t.Error("transition out of completed accepted, want rejection")
	}
}

func TestPauseResumeRoundTrip(t *testing.T) {
	r, s := openRepo(t)
	projectID, taskID := seedProjectTask(t, s)
	ctx := context.Background()

	j, err := r.Create(ctx, taskID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	j = runTo(t, r, j.ID, StateContextBuilding, StatePlanning)

	// Pause records where to resume from.
	paused, err := r.Transition(ctx, j.ID, StatePaused)
	if err != nil {
		t.Fatal(err)
	}
	if paused.PausedFrom != StatePlanning {
		t.Errorf("paused_from = %q, want planning", paused.PausedFrom)
	}

	// Resuming to anything but paused_from is rejected.
	if _, err := r.Transition(ctx, j.ID, StateSynthesizing); err == nil {
		t.Error("resume to non-paused_from state accepted, want rejection")
	}

	// Resuming to paused_from works and clears the flag.
	resumed, err := r.Transition(ctx, j.ID, StatePlanning)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.State != StatePlanning || resumed.PausedFrom != "" {
		t.Errorf("resumed job = %+v, want planning with empty paused_from", resumed)
	}

	// A paused job may still be cancelled (kill switch semantics).
	runTo(t, r, j.ID, StatePaused)
	if _, err := r.Transition(ctx, j.ID, StateCancelled); err != nil {
		t.Fatalf("cancel from paused: %v", err)
	}
}
