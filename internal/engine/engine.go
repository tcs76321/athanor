// Package engine drives jobs through the M1 walking-skeleton phase chain
// (ROADMAP M1): queued → context_building → planning → diverging (single
// candidate) → synthesizing → comparing → completed.
//
// M1 simplifications, all deliberate (ADR-0001 and ROADMAP M1 scope):
//   - No tool execution exists at all (Gate G1). The agent can only
//     think (one LLM call per phase) and persist.
//   - `evaluating`/`reflecting` phases arrive with Job Pods in M3.
//   - `comparing` is deterministic and trivial in M1: with no previous
//     accepted artifact and no evaluation machinery, the single draft
//     wins by definition; the event log records that decision.
//   - Context is assembled naively (prompt v1) — the MCE arrives in M5.
//   - The divergence candidate is persisted as a draft `proposal`
//     artifact (§9.1), so a crash mid-synthesis still finds it.
//
// Crash safety comes from §8.2: state is committed after every
// transition, so a killed run resumes from its last committed phase via
// Recover.
package engine

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/tcs76321/athanor/internal/artifact"
	"github.com/tcs76321/athanor/internal/config"
	"github.com/tcs76321/athanor/internal/job"
	"github.com/tcs76321/athanor/internal/llm"
	"github.com/tcs76321/athanor/internal/project"
	"github.com/tcs76321/athanor/internal/store"
)

// Freezer is the kill-switch surface the engine consults (§22.1: frozen
// means no work proceeds).
type Freezer interface {
	Frozen() bool
}

// ErrPaused reports that the job was paused instead of failed — the
// context floor was violated (recommend-or-escalate, §12.3) or the kill
// switch froze the daemon mid-run.
var ErrPaused = errors.New("job paused")

// Engine executes jobs.
type Engine struct {
	cfg       *config.Config
	db        *store.Store
	jobs      *job.Repository
	projects  *project.Repo
	artifacts *artifact.Store
	client    *llm.Client
	registry  *llm.Registry
	freezer   Freezer
	sem       chan struct{}
	mu        sync.Mutex // guards running set to avoid double-running a job
	running   map[string]bool
}

// New wires an engine. Concurrency is bounded by cfg.Limits.MaxConcurrentJobs.
func New(cfg *config.Config, db *store.Store, jobs *job.Repository, projects *project.Repo,
	artifacts *artifact.Store, client *llm.Client, registry *llm.Registry, freezer Freezer) *Engine {
	max := cfg.Limits.MaxConcurrentJobs
	if max < 1 {
		max = 1
	}
	return &Engine{
		cfg: cfg, db: db, jobs: jobs, projects: projects, artifacts: artifacts,
		client: client, registry: registry, freezer: freezer,
		sem:     make(chan struct{}, max),
		running: map[string]bool{},
	}
}

// Enqueue starts asynchronous execution of a job. Returns immediately;
// callers watch progress through the job state and event log.
func (e *Engine) Enqueue(jobID string) {
	go func() {
		e.sem <- struct{}{}
		defer func() { <-e.sem }()
		e.Run(context.Background(), jobID)
	}()
}

// Recover resumes every active job after a daemon restart or an unfreeze
// (§23.6): each job continues from its last committed state, paused jobs
// resume to paused_from.
func (e *Engine) Recover(ctx context.Context) {
	active, err := e.jobs.Active(ctx)
	if err != nil {
		slog.Error("recovery: listing active jobs", "err", err)
		return
	}
	for _, j := range active {
		if j.State.Terminal() {
			continue
		}
		if err := e.jobs.SetRecoveryFlag(ctx, j.ID, "interrupted"); err != nil {
			slog.Error("recovery: marking interrupted", "job", j.ID, "err", err)
		}
		slog.Info("resuming job after restart", "job", j.ID, "state", j.State)
		e.Enqueue(j.ID)
	}
}

// audit appends an engine event to the append-only log.
func (e *Engine) audit(ctx context.Context, jobID string, data map[string]any) {
	if _, err := e.db.AppendEvent(ctx, store.Event{Category: "jobs", JobID: jobID, Data: data}); err != nil {
		slog.Error("engine: appending event", "job", jobID, "err", err)
	}
}

// Run drives one job from its current state to a terminal state (or
// paused). Safe to call on an already-terminal or already-running job.
func (e *Engine) Run(ctx context.Context, jobID string) {
	e.mu.Lock()
	if e.running[jobID] {
		e.mu.Unlock()
		return // another goroutine owns this job
	}
	e.running[jobID] = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.running, jobID)
		e.mu.Unlock()
	}()

	for {
		j, err := e.jobs.Get(ctx, jobID)
		if err != nil {
			slog.Error("engine: loading job", "job", jobID, "err", err)
			return
		}
		if j.State.Terminal() || j.State == job.StatePaused {
			return
		}

		// §22.1: frozen means no work proceeds. Active jobs pause; queued
		// jobs simply stay queued (nothing has started). Either way,
		// unfreeze resumes them via Recover.
		if e.freezer.Frozen() {
			if job.CanTransition(j.State, job.StatePaused) {
				if _, err := e.jobs.Transition(ctx, jobID, job.StatePaused); err != nil {
					slog.Error("engine: pausing for freeze", "job", jobID, "err", err)
				}
			}
			return
		}

		if err := e.step(ctx, j); err != nil {
			if errors.Is(err, ErrPaused) {
				return
			}
			slog.Error("engine: phase failed", "job", jobID, "state", j.State, "err", err)
			if _, terr := e.jobs.Transition(ctx, jobID, job.StateFailed); terr != nil {
				slog.Error("engine: marking job failed", "job", jobID, "err", terr)
			}
			e.audit(ctx, jobID, map[string]any{
				"event": "job_failed", "state": string(j.State), "error": err.Error(),
			})
			return
		}
		// A successful step is a successful resume (§8.3: the recovery
		// flag clears once the job makes progress again).
		if j.RecoveryFlag != "" {
			_ = e.jobs.SetRecoveryFlag(ctx, jobID, "")
		}
	}
}
