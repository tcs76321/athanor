package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/tcs76321/athanor/internal/evaluation"
	"github.com/tcs76321/athanor/internal/job"
	"github.com/tcs76321/athanor/internal/llm"
)

// M3-T1 deliberate simplification: the reflection-loop budget is
// hard-coded to 2 iterations. M3-T4 owns the config field
// (`execution.max_reflection_loops`) and the `system_state` counter
// for budget-exhausted escalation; the constant below is the M3-T1
// placeholder that keeps the engine's branch logic in place while
// the configurability is built out.
const maxReflectionIterations = 2

// phaseReflect (§13.1 Phase 4, M3-T1): entered only when the
// evaluating phase found no passing candidates. Asks the LLM
// (main/tall/alternative persona, moderate-to-high temperature) what
// went wrong, persists the proposed improvements, and either loops
// back to diverging (within the budget) or transitions the job to
// `failed` (budget exhausted).
//
// §8.2 branch: `reflecting → diverging` (retry) or
// `reflecting → failed` (budget).
func (e *Engine) phaseReflect(ctx context.Context, j job.Job) error {
	if e.eval == nil {
		return errors.New("engine: evaluation repo is nil (M3-T1; tests must pass it)")
	}
	p, t, err := e.contexts(ctx, j)
	if err != nil {
		return err
	}

	// The reflection counter lives on the Job's recovery_flag
	// column (co-opted for M3-T1; M3-T4 will move this to a
	// typed counter in system_state). We re-fetch the job here
	// because the `j` parameter carries the snapshot taken at
	// the top of the Run loop, which is stale: the previous
	// reflection iteration set the flag *after* the most recent
	// `Get`, so the in-memory `j.RecoveryFlag` is one iteration
	// behind the DB's value.
	fresh, fetchErr := e.jobs.Get(ctx, j.ID)
	if fetchErr != nil {
		return fmt.Errorf("re-fetching job for reflection counter: %w", fetchErr)
	}
	iter := currentReflectionIteration(fresh)
	if iter >= maxReflectionIterations {
		e.audit(ctx, j.ID, map[string]any{
			"event":     "reflection_budget_exhausted",
			"iter":      iter,
			"max":       maxReflectionIterations,
			"archetype": p.Archetype,
		})
		_, err = e.jobs.Transition(ctx, j.ID, job.StateFailed)
		return err
	}

	// Aggregate the failure context for the LLM: the most recent
	// EvaluationRecord set tells the reflector what failed.
	records, err := e.eval.ListByJob(ctx, j.ID)
	if err != nil {
		return fmt.Errorf("listing evaluation records: %w", err)
	}
	instructions := buildReflectionInstructions(records)

	resp, err := e.call(ctx, j, p, t, llm.PhaseReflecting, llm.RoleMain, instructions)
	if err != nil {
		return err
	}

	e.audit(ctx, j.ID, map[string]any{
		"event":      "reflection",
		"iter":       iter + 1,
		"candidates": len(records),
		"proposal":   truncateForEvent(resp.Content, 1024),
	})

	// Bump the iteration counter via the recovery flag channel.
	// (Same simplification as the read side.) The check below
	// enforces the budget BEFORE re-entering divergence: if the
	// upcoming iteration would exceed `maxReflectionIterations`,
	// we transition to `failed` now rather than waste another
	// divergence + evaluation cycle that we already know will not
	// count toward a passing job.
	nextIter := iter + 1
	if err := e.jobs.SetRecoveryFlag(ctx, j.ID, fmt.Sprintf("reflect-%d", nextIter)); err != nil {
		slog.Warn("engine: bumping reflection counter", "err", err)
	}
	if nextIter >= maxReflectionIterations {
		e.audit(ctx, j.ID, map[string]any{
			"event":     "reflection_budget_exhausted",
			"iter":      nextIter,
			"max":       maxReflectionIterations,
			"archetype": p.Archetype,
		})
		_, err = e.jobs.Transition(ctx, j.ID, job.StateFailed)
		return err
	}

	_, err = e.jobs.Transition(ctx, j.ID, job.StateDiverging)
	return err
}

// currentReflectionIteration extracts the iteration count from the
// Job's recovery_flag column, which we co-opt for M3-T1 in lieu of
// a typed counter. The format is "reflect-N" where N is 0-based; the
// returned value is the number of *completed* iterations (so a fresh
// job starts at 0). A malformed or empty flag returns 0.
//
// M3-T4 replaces this with a real counter in system_state.
func currentReflectionIteration(j job.Job) int {
	if !strings.HasPrefix(j.RecoveryFlag, "reflect-") {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimPrefix(j.RecoveryFlag, "reflect-"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// buildReflectionInstructions composes the prompt extra for the
// reflection phase. The reflector sees the most recent
// EvaluationRecord set, the missing criteria, and a one-sentence
// summary of each candidate's failure.
func buildReflectionInstructions(records []evaluation.Record) string {
	if len(records) == 0 {
		return "No candidates landed; divergence produced nothing. Suggest a different approach."
	}
	var b strings.Builder
	b.WriteString("REFLECTION. All candidates failed acceptance. The EvaluationRecords:\n")
	for i, r := range records {
		fmt.Fprintf(&b, "  candidate %d (artifact %s): score=%.2f, passed=%v, missing=%v, security=%v, summary=%q\n",
			i+1, r.ArtifactID, r.Score, r.PassedTests, r.MissingCriteria, r.SecurityIssues, r.Summary)
	}
	b.WriteString("\nPropose ONE specific change (a different approach, a missing context, or a hybrid) that the next divergence pass should try. Output prose, not JSON.")
	return b.String()
}

// truncateForEvent bounds the size of an LLM response embedded in
// an audit event. A multi-KB proposal would inflate the events table
// without bound; 1 KB is enough for a post-mortem reader.
func truncateForEvent(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
