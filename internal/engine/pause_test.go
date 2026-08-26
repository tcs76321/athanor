package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/tcs76321/athanor/internal/config"
	"github.com/tcs76321/athanor/internal/job"
	"github.com/tcs76321/athanor/internal/store"
)

// TestContextFloorPausesJobWithRecommendation proves the M1-T2
// recommend-or-escalate path end-to-end: a persona below the floor for a
// full-fidelity phase pauses the job (never silent truncation), with the
// recommendation in the event log.
func TestContextFloorPausesJobWithRecommendation(t *testing.T) {
	// Shrink main below the document floor so diverging violates §12.6.
	// The mutation must happen before the engine builds its persona
	// registry (which snapshots the config).
	e := newEnvWithCfg(t, func(cfg *config.Config) {
		cfg.Personas.Main.ContextTarget = 4096
		cfg.ContextEngine.DocumentFloor = 16384
	})

	jobID := e.submit(t)
	e.eng.Run(context.Background(), jobID)

	j, err := e.jobs.Get(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != job.StatePaused {
		t.Fatalf("job state = %s, want paused (floor violation)", j.State)
	}
	events, err := e.db.QueryEvents(context.Background(), store.EventFilter{JobID: jobID})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range events {
		var d struct {
			Event          string `json:"event"`
			Recommendation string `json:"recommendation"`
		}
		_ = json.Unmarshal([]byte(ev.DataJSON), &d)
		if d.Event == "context_floor_violation" && d.Recommendation != "" {
			found = true
		}
	}
	if !found {
		t.Error("no context_floor_violation event with a recommendation in the event log")
	}
	// Planning ran first (tall, floor-exempt); diverging paused before
	// calling the model.
	if e.ollama.calls != 1 {
		t.Errorf("llm calls = %d, want 1 (planning only; diverging paused)", e.ollama.calls)
	}
}

// TestFreezePausesAndUnfreezeResumes proves the kill switch mid-run:
// frozen engines pause active jobs; Recover after unfreeze resumes them
// to completion (§22.1–§22.2).
func TestFreezePausesAndUnfreezeResumes(t *testing.T) {
	e := newEnv(t)
	// Sanity: an unfrozen run completes.
	first := e.submit(t)
	e.eng.Run(context.Background(), first)
	j, err := e.jobs.Get(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != job.StateCompleted {
		t.Fatalf("sanity: job state = %s, want completed", j.State)
	}

	// Freeze, then run a second job synchronously: it must not start —
	// queued work simply stays queued (nothing has begun).
	if err := e.freezer.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := e.submit(t)
	e.eng.Run(context.Background(), second)
	held, err := e.jobs.Get(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if held.State != job.StateQueued {
		t.Fatalf("frozen queued run state = %s, want queued (no work starts)", held.State)
	}
	if e.ollama.calls != 3 {
		t.Errorf("llm calls = %d, want 3 (frozen job made no calls)", e.ollama.calls)
	}

	// Unfreeze + Recover: the held job runs to completion.
	if err := e.freezer.Unfreeze(context.Background(), "test resume"); err != nil {
		t.Fatal(err)
	}
	e.eng.Recover(context.Background())
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		final, err := e.jobs.Get(context.Background(), second)
		if err != nil {
			t.Fatal(err)
		}
		if final.State == job.StateCompleted {
			return
		}
		if final.State.Terminal() {
			t.Fatalf("resumed job reached %s, want completed", final.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("paused job never completed after unfreeze+recover")
}
