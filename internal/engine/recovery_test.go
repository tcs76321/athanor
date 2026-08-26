package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tcs76321/athanor/internal/artifact"
	"github.com/tcs76321/athanor/internal/config"
	"github.com/tcs76321/athanor/internal/control"
	"github.com/tcs76321/athanor/internal/job"
	"github.com/tcs76321/athanor/internal/llm"
	"github.com/tcs76321/athanor/internal/project"
	"github.com/tcs76321/athanor/internal/store"
	"github.com/tcs76321/athanor/migrations"
)

// stepOnce performs exactly one phase step (test helper for driving a job
// to a chosen crash point).
func stepOnce(e *Engine, jobID string) error {
	j, err := e.jobs.Get(context.Background(), jobID)
	if err != nil {
		return err
	}
	return e.step(context.Background(), j)
}

// TestRecoverResumesMidFlightJob simulates a daemon crash mid-job (the
// engine disappears; only the store survives) and proves the job resumes
// from its last committed phase and completes, re-reading the persisted
// proposal artifact instead of relying on memory.
func TestRecoverResumesMidFlightJob(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db.DB(), migrations.FS, ""); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Default()
	ollama := newCountingOllama(t)
	cfg.Inference.OllamaURL = ollama.URL
	freezer, _ := control.NewKillSwitch(db)
	projects := project.NewRepo(db)
	jobs := job.NewRepository(db)
	artifacts := artifact.NewStore(db, filepath.Join(dir, "artifacts"))
	registry, _ := llm.NewRegistry(cfg.Personas)

	_, task, err := projects.Create(context.Background(), "demo", "text",
		"Write a short essay about local-first software.", nil)
	if err != nil {
		t.Fatal(err)
	}
	j, err := jobs.Create(context.Background(), task.ID, task.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	// Run up to synthesizing, then "crash" (drop the engine, keep state).
	eng1 := New(cfg, db, jobs, projects, artifacts, llm.NewClient(cfg.Inference.OllamaURL, nil), registry, freezer)
	for {
		cur, err := jobs.Get(context.Background(), j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if cur.State == job.StateSynthesizing {
			break
		}
		if cur.State.Terminal() {
			t.Fatalf("job reached %s before crash point", cur.State)
		}
		if err := stepOnce(eng1, j.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Restart: recovery marks the job interrupted and resumes it.
	db2, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	jobs2 := job.NewRepository(db2)
	artifacts2 := artifact.NewStore(db2, filepath.Join(dir, "artifacts"))
	eng2 := New(cfg, db2, jobs2, project.NewRepo(db2), artifacts2,
		llm.NewClient(cfg.Inference.OllamaURL, nil), registry, freezer)
	eng2.Recover(context.Background())

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		final, err := jobs2.Get(context.Background(), j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if final.State == job.StateCompleted {
			if final.RecoveryFlag != "" {
				t.Errorf("recovery flag not cleared after successful resume: %q", final.RecoveryFlag)
			}
			if _, err := artifacts2.LatestForJob(context.Background(), j.ID, artifact.KindDocument); err != nil {
				t.Errorf("final artifact missing after resume: %v", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job never completed after recovery")
}
