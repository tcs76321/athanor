package engine

import (
	"context"
	"fmt"

	"github.com/tcs76321/athanor/internal/artifact"
	"github.com/tcs76321/athanor/internal/job"
	"github.com/tcs76321/athanor/internal/llm"
)

// phaseDivergeN (§13.1 Phase 2): generates N candidate artifacts, each
// persisted as a draft `proposal` artifact (§9.1). The number of
// candidates is `cfg.Execution.DivergenceCandidates`, defaulting to 3
// per the M3-T1 simplification that "difficulty_hint" from the planner
// is not yet consumed; M3-T2 may feed it in.
//
// All candidates use the `main` persona at the phase's high temperature
// (0.7–1.1) so they actually differ; the LLM is told to "explore
// orthogonal options" via the prompt. The transition is to
// `evaluating` (§8.2: divergence always feeds evaluation).
//
// Crash safety: every candidate is persisted before the transition.
// A crash mid-divergence resumes into `diverging` (per §23.6) and the
// candidates that already landed in the artifacts table are picked up
// by `phaseEvaluate` via `LatestForJob(KindProposal)` (the table is
// sorted newest-first; the highest version is the one the
// divergence run actually completed last, but every candidate
// between the resume point and the truncation is still evaluable).
func (e *Engine) phaseDivergeN(ctx context.Context, j job.Job) error {
	p, t, err := e.contexts(ctx, j)
	if err != nil {
		return err
	}
	n := e.cfg.Execution.DivergenceCandidates
	if n < 1 {
		n = 1
	}
	if max := e.cfg.Execution.MaxHardTaskVariations; max > 0 && n > max {
		n = max
	}

	e.audit(ctx, j.ID, map[string]any{
		"event":      "divergence_start",
		"candidates": n,
		"archetype":  p.Archetype,
	})

	for i := 0; i < n; i++ {
		// The instruction appends a per-candidate seed so consecutive
		// calls with the same prompt still produce different outputs
		// at the same temperature. Without the seed the LLM has
		// nothing to anchor the variation; with the seed the
		// evaluator can tell candidates apart by their index.
		seed := fmt.Sprintf("CANDIDATE %d of %d. Produce a solution that differs from any other candidate you might generate for this task.", i+1, n)
		resp, err := e.call(ctx, j, p, t, llm.PhaseDiverging, llm.RoleMain, seed)
		if err != nil {
			return fmt.Errorf("divergence candidate %d/%d: %w", i+1, n, err)
		}
		if _, err := e.artifacts.CreateDraftFor(ctx, p.ID, t.ID, j.ID,
			artifact.KindProposal, []byte(resp.Content)); err != nil {
			return fmt.Errorf("persisting divergence candidate %d: %w", i+1, err)
		}
		e.audit(ctx, j.ID, map[string]any{
			"event": "divergence_candidate",
			"index": i + 1,
			"of":    n,
			"chars": len(resp.Content),
		})
	}

	_, err = e.jobs.Transition(ctx, j.ID, job.StateEvaluating)
	return err
}
