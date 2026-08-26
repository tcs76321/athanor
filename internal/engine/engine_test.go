package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tcs76321/athanor/internal/artifact"
	"github.com/tcs76321/athanor/internal/config"
	"github.com/tcs76321/athanor/internal/control"
	"github.com/tcs76321/athanor/internal/job"
	"github.com/tcs76321/athanor/internal/llm"
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
	eng := New(cfg, db, jobs, projects, artifacts, llm.NewClient(cfg.Inference.OllamaURL, nil), registry, freezer)
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
