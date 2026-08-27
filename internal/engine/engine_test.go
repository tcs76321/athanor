package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tcs76321/athanor/internal/artifact"
	"github.com/tcs76321/athanor/internal/config"
	"github.com/tcs76321/athanor/internal/control"
	"github.com/tcs76321/athanor/internal/job"
	"github.com/tcs76321/athanor/internal/llm"
	"github.com/tcs76321/athanor/internal/power"
	"github.com/tcs76321/athanor/internal/project"
	"github.com/tcs76321/athanor/internal/store"
	"github.com/tcs76321/athanor/migrations"
)

// countingOllama counts chat calls so tests can assert how many LLM
// requests each path performs.
type countingOllama struct {
	*httptest.Server
	calls int
}

func newCountingOllama(t *testing.T) *countingOllama {
	t.Helper()
	o := &countingOllama{}
	o.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			_, _ = w.Write([]byte(`{"version":"fake"}`))
			return
		}
		o.calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message":           map[string]string{"role": "assistant", "content": "A thoughtful result."},
			"done":              true,
			"prompt_eval_count": 10,
			"eval_count":        5,
		})
	}))
	t.Cleanup(o.Close)
	return o
}

type testEnv struct {
	cfg       *config.Config
	db        *store.Store
	jobs      *job.Repository
	projects  *project.Repo
	artifacts *artifact.Store
	freezer   *control.KillSwitch
	ollama    *countingOllama
	eng       *Engine
}

func newEnv(t *testing.T) *testEnv {
	return newEnvWithCfg(t, nil)
}

// newEnvWithCfg lets a test mutate the config before the engine (and its
// persona registry snapshot) is built.
func newEnvWithCfg(t *testing.T, mutate func(*config.Config)) *testEnv {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(db.DB(), migrations.FS, ""); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	ollama := newCountingOllama(t)
	cfg.Inference.OllamaURL = ollama.URL
	if mutate != nil {
		mutate(cfg)
	}

	freezer, err := control.NewKillSwitch(db)
	if err != nil {
		t.Fatal(err)
	}
	projects := project.NewRepo(db)
	jobs := job.NewRepository(db)
	artifacts := artifact.NewStore(db, filepath.Join(dir, "artifacts"))
	registry, err := llm.NewRegistry(cfg.Personas)
	if err != nil {
		t.Fatal(err)
	}
	eng := New(cfg, db, jobs, projects, artifacts, llm.NewClient(cfg.Inference.OllamaURL, nil), registry, freezer, power.NewPowerManager(nil))
	return &testEnv{cfg: cfg, db: db, jobs: jobs, projects: projects, artifacts: artifacts,
		freezer: freezer, ollama: ollama, eng: eng}
}

// submit creates a project with a text goal and its queued job. Names are
// unique per call (the projects.name column is UNIQUE).
var submitSeq int

func (e *testEnv) submit(t *testing.T) (jobID string) {
	t.Helper()
	submitSeq++
	_, task, err := e.projects.Create(context.Background(),
		fmt.Sprintf("demo-%d", submitSeq), "text",
		"Write a short essay about local-first software.", nil)
	if err != nil {
		t.Fatal(err)
	}
	j, err := e.jobs.Create(context.Background(), task.ID, task.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	return j.ID
}

// TestRunCompletesFullChain drives a job synchronously to completion.
func TestRunCompletesFullChain(t *testing.T) {
	e := newEnv(t)
	jobID := e.submit(t)
	e.eng.Run(context.Background(), jobID)

	j, err := e.jobs.Get(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != job.StateCompleted {
		t.Fatalf("state = %s, want completed", j.State)
	}
	// One LLM call per phase: planning, diverging, synthesizing (comparing
	// is deterministic in M1).
	if e.ollama.calls != 3 {
		t.Errorf("llm calls = %d, want 3", e.ollama.calls)
	}
	// Both artifacts exist.
	if _, err := e.artifacts.LatestForJob(context.Background(), jobID, artifact.KindProposal); err != nil {
		t.Errorf("proposal artifact missing: %v", err)
	}
	if _, err := e.artifacts.LatestForJob(context.Background(), jobID, artifact.KindDocument); err != nil {
		t.Errorf("final artifact missing: %v", err)
	}
}

// fixedCap is a ConcurrencyCap that returns a fixed value, used by
// M1-T8.4 tests to drive the engine's concurrency behavior.
type fixedCap struct{ n int }

func (f fixedCap) MaxConcurrentJobs() int { return f.n }

// TestEnqueueRespectsConcurrencyCap (M1-T8.4) proves the engine reads
// its concurrency cap from the injected ConcurrencyCap on every
// enqueue, and that a cap of 1 limits in-flight job goroutines to 1.
// We use a fake Ollama that blocks on a channel, so we can hold two
// jobs in flight simultaneously and observe the cap block the second.
func TestEnqueueRespectsConcurrencyCap(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(db.DB(), migrations.FS, ""); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}

	// A blocking fake ollama: each call parks on a release channel
	// so we can hold N jobs in flight at once.
	released := make(chan struct{})
	hang := make(chan struct{})
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			_, _ = w.Write([]byte(`{"version":"fake"}`))
			return
		}
		<-hang
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message":           map[string]string{"role": "assistant", "content": "x"},
			"done":              true,
			"prompt_eval_count": 1, "eval_count": 1,
		})
	}))
	t.Cleanup(ollama.Close)
	cfg.Inference.OllamaURL = ollama.URL

	freezer, _ := control.NewKillSwitch(db)
	registry, _ := llm.NewRegistry(cfg.Personas)
	artifacts := artifact.NewStore(db, filepath.Join(dir, "artifacts"))

	// Cap = 1: the engine must hold at most one in-flight job goroutine.
	cap := fixedCap{n: 1}
	eng := New(cfg, db, job.NewRepository(db), project.NewRepo(db), artifacts,
		llm.NewClient(cfg.Inference.OllamaURL, nil), registry, freezer, cap)

	// Submit two jobs. The first enters the LLM call (blocked on `hang`).
	// The second must wait in Enqueue's poll loop.
	projects := project.NewRepo(db)
	_, task1, err := projects.Create(context.Background(), "cap-1", "text",
		"Write a short essay about local-first software.", nil)
	if err != nil {
		t.Fatal(err)
	}
	j1, err := job.NewRepository(db).Create(context.Background(), task1.ID, task1.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	_, task2, err := projects.Create(context.Background(), "cap-2", "text",
		"Write a short essay about local-first software.", nil)
	if err != nil {
		t.Fatal(err)
	}
	j2, err := job.NewRepository(db).Create(context.Background(), task2.ID, task2.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	eng.Enqueue(j1.ID)
	eng.Enqueue(j2.ID)

	// Give both goroutines a moment: j1's LLM call is hanging, j2's
	// Enqueue poll-loop is spinning. If the cap is honored, j2's
	// in-flight count is 0 and the LLM was never called for j2.
	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt64(&eng.inFlight); got != 1 {
		t.Errorf("inFlight = %d, want 1 (cap=1 must hold a second job out of the run loop)", got)
	}

	// Release the LLM and let j1 finish; j2 should then run.
	close(hang)
	close(released)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j1s, err := job.NewRepository(db).Get(context.Background(), j1.ID)
		if err != nil {
			t.Fatal(err)
		}
		j2s, err := job.NewRepository(db).Get(context.Background(), j2.ID)
		if err != nil {
			t.Fatal(err)
		}
		if j1s.State == job.StateCompleted && j2s.State == job.StateCompleted {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("both jobs did not complete within deadline")
}
