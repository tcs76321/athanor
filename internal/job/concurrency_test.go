package job

import (
	"context"
	"errors"
	"testing"

	"github.com/tcs76321/athanor/internal/store"
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

// TestConcurrentTransition_DifferentTargetsRacesCAS is the
// complementary coverage for the same-target race in
// TestConcurrentTransitionLosesLoudly. Two writers request
// *different* legal transitions from the same starting state
// (`planning -> diverging` vs `planning -> paused`, both
// legal from planning per the §8.2 table). Exactly one wins
// the SQL CAS; the loser sees ErrConcurrentTransition. This
// exercises the CAS branch in Repository.Transition itself,
// not the same-state short-circuit in the sibling test, so
// future regressions in either layer are caught by their
// own test.
func TestConcurrentTransition_DifferentTargetsRacesCAS(t *testing.T) {
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
	go func() { _, err := r.Transition(ctx, j.ID, StatePaused); loser <- err }()

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
