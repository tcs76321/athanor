package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tcs76321/athanor/internal/artifact"
	"github.com/tcs76321/athanor/internal/evaluation"
	"github.com/tcs76321/athanor/internal/job"
	"github.com/tcs76321/athanor/internal/llm"
	"github.com/tcs76321/athanor/internal/project"
	"github.com/tcs76321/athanor/internal/toolenvelope"
)

// evalVerdict is the §13.1 Phase 6 JSON the security persona produces.
// The schema is the LLM's output shape; the engine enforces the
// §19.3 deterministic guard against it, not the LLM's text.
type evalVerdict struct {
	Passed             bool     `json:"passed"`
	Score              float64  `json:"score"`
	FailedTests        []string `json:"failed_tests"`
	MissingCriteria    []string `json:"missing_criteria"`
	SecurityIssues     []string `json:"security_issues"`
	StyleIssues        []string `json:"style_issues"`
	BetterThanPrevious bool     `json:"better_than_previous"`
	Confidence         float64  `json:"confidence"`
	Summary            string   `json:"summary"`
}

// phaseEvaluate (§13.1 Phase 3, M3-T1): for every candidate artifact
// produced by the diverging phase, run the §19 acceptance-criteria
// checks, exercise the test command in a Job Pod (code archetype
// only), and persist one EvaluationRecord per candidate.
//
// Temperature: 0.0 (pinned, "maximally deterministic"). Persona:
// `security` (the LLM is told to judge, not generate).
//
// Sub-state: for the code archetype, `running_tests` is recorded as
// an event row (`substate_entered` / `substate_exited`) so the
// timeline is queryable without a jobs.state column (per §8.1's
// "tracked sub-state" note).
//
// Branch (§8.2):
//   - ≥1 candidate passed (passed == true) → `evaluating → synthesizing`
//   - 0 candidates passed                  → `evaluating → reflecting`
func (e *Engine) phaseEvaluate(ctx context.Context, j job.Job) error {
	if e.eval == nil {
		return errors.New("engine: evaluation repo is nil (M3-T1; tests must pass it)")
	}
	p, t, err := e.contexts(ctx, j)
	if err != nil {
		return err
	}

	candidates, err := e.listCandidateArtifacts(ctx, j)
	if err != nil {
		return fmt.Errorf("listing candidates: %w", err)
	}
	if len(candidates) == 0 {
		// No candidates landed: divergence produced nothing. This is
		// a hard failure (the engine cannot evaluate a void).
		e.audit(ctx, j.ID, map[string]any{
			"event": "evaluating_no_candidates",
		})
		_, err = e.jobs.Transition(ctx, j.ID, job.StateFailed)
		return err
	}

	// The previous accepted artifact (§19.2 `compared_against`) is
	// the same for every candidate in this job.
	previousID := ""
	if prev, err := e.artifacts.LatestAcceptedByProject(ctx, p.ID); err == nil {
		previousID = prev.ID
	} else if !errors.Is(err, artifact.ErrNotFound) {
		return fmt.Errorf("loading previous accepted artifact: %w", err)
	}

	passCount := 0
	for i, cand := range candidates {
		rec, err := e.evaluateCandidate(ctx, j, p, t, cand, previousID, i+1, len(candidates))
		if err != nil {
			return fmt.Errorf("evaluating candidate %d: %w", i+1, err)
		}
		if rec.PassedTests && len(rec.MissingCriteria) == 0 && len(rec.SecurityIssues) == 0 {
			passCount++
		}
	}

	e.audit(ctx, j.ID, map[string]any{
		"event":      "evaluating_done",
		"candidates": len(candidates),
		"passed":     passCount,
	})

	if passCount > 0 {
		_, err = e.jobs.Transition(ctx, j.ID, job.StateSynthesizing)
	} else {
		_, err = e.jobs.Transition(ctx, j.ID, job.StateReflecting)
	}
	return err
}

// listCandidateArtifacts returns the divergence proposals in version
// order (oldest first) so the evaluator's indices match the
// divergence indices and the audit trail is monotonic.
func (e *Engine) listCandidateArtifacts(ctx context.Context, j job.Job) ([]artifact.Artifact, error) {
	rows, err := e.artifacts.ListByProject(ctx, j.ProjectID)
	if err != nil {
		return nil, err
	}
	var out []artifact.Artifact
	for _, a := range rows {
		if a.JobID == j.ID && a.Kind == artifact.KindProposal {
			out = append(out, a)
		}
	}
	// ListByProject returns newest first; reverse for evaluation order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// evaluateCandidate runs the §19 checks for one candidate and
// persists the resulting EvaluationRecord. The candidate is scored by
// the security persona at temp 0.0; for the code archetype, the
// existing test command is run in the Job Pod first and its exit
// code is folded into the verdict (the LLM doesn't grade tests; the
// pod does).
func (e *Engine) evaluateCandidate(ctx context.Context, j job.Job, p project.Project, t project.Task,
	cand artifact.Artifact, previousID string, idx, total int) (evaluation.Record, error) {

	var (
		testsPassed  bool
		failedTests  []string
		testExitCode int
	)
	if p.Archetype == project.ArchetypeCode && e.runner != nil {
		// §8.1 "tracked sub-state": running_tests is an event, not a
		// column. We enter and exit the substate around the
		// `runner.RunTests` call so the audit log shows the duration.
		e.audit(ctx, j.ID, map[string]any{
			"event":     "substate_entered",
			"phase":     string(llm.PhaseEvaluating),
			"substate":  "running_tests",
			"candidate": idx,
		})
		req := toolenvelope.ExecuteRequest{Command: "pytest -q"}
		res, runErr := e.runner.RunTests(ctx, j.ID, req)
		if runErr == nil {
			testsPassed = res.ExitCode == 0
			testExitCode = res.ExitCode
			if !testsPassed {
				failedTests = []string{"pytest"} // TODO M3-T2: surface per-test names
			}
		} else if !errors.Is(runErr, toolenvelope.ErrToolDisallowed) {
			e.audit(ctx, j.ID, map[string]any{
				"event":     "tests_run_failed",
				"candidate": idx,
				"error":     runErr.Error(),
			})
		}
		e.audit(ctx, j.ID, map[string]any{
			"event":     "substate_exited",
			"phase":     string(llm.PhaseEvaluating),
			"substate":  "running_tests",
			"candidate": idx,
			"exit_code": testExitCode,
		})
	} else if p.Archetype == project.ArchetypeCode {
		// No runner wired (unit test mode): leave testsPassed = false
		// and let the LLM persona make the call via MissingCriteria.
		testsPassed = false
	}

	content, err := e.artifacts.ReadContent(ctx, cand.ID)
	if err != nil {
		return evaluation.Record{}, fmt.Errorf("reading candidate content: %w", err)
	}

	instructions := fmt.Sprintf(
		"EVALUATE CANDIDATE %d of %d (artifact_id=%s). "+
			"Tests already ran in the Job Pod: passed=%v, failed_tests=%v. "+
			"Apply the §19 acceptance-criteria check to the candidate content below. "+
			"Output JSON only: {passed, score, failed_tests, missing_criteria, "+
			"security_issues, style_issues, better_than_previous, confidence, summary}. "+
			"\n\nCANDIDATE CONTENT:\n%s",
		idx, total, cand.ID, testsPassed, failedTests, string(content))

	resp, err := e.call(ctx, j, p, t, llm.PhaseEvaluating, llm.RoleSecurity, instructions)
	if err != nil {
		return evaluation.Record{}, err
	}

	verdict, err := parseEvalVerdict(resp.Content)
	if err != nil {
		return evaluation.Record{}, fmt.Errorf("parsing security verdict: %w", err)
	}

	// Reconcile the LLM verdict with the deterministic test result:
	// the LLM's `passed` field is overridden by the pod's verdict
	// when the test command actually ran.
	if p.Archetype == project.ArchetypeCode && e.runner != nil {
		verdict.Passed = testsPassed && len(verdict.MissingCriteria) == 0 && len(verdict.SecurityIssues) == 0
		if testsPassed {
			// Clear test-only failures if the run was actually green.
			verdict.FailedTests = nil
		} else {
			verdict.FailedTests = failedTests
		}
	}

	rec := evaluation.NewRecord(j.ID, cand.ID)
	rec.ComparedAgainst = previousID
	rec.Score = verdict.Score
	rec.PassedTests = verdict.Passed
	rec.FailedTests = verdict.FailedTests
	rec.MissingCriteria = verdict.MissingCriteria
	rec.SecurityIssues = verdict.SecurityIssues
	rec.StyleIssues = verdict.StyleIssues
	rec.BetterThanPrevious = verdict.BetterThanPrevious
	rec.Confidence = verdict.Confidence
	rec.Summary = verdict.Summary
	return e.eval.Create(ctx, rec)
}

// parseEvalVerdict extracts the JSON the security persona produced.
// A non-JSON response is a hard error — the §13.1 contract is
// "structured JSON output," and a wandering verdict is not a verdict
// at all.
func parseEvalVerdict(content string) (evalVerdict, error) {
	var v evalVerdict
	// Be lenient about wrapping: the LLM may emit the JSON inside
	// a code fence or preceded by prose. Find the first '{' and
	// the matching '}'.
	start := -1
	for i, c := range content {
		if c == '{' {
			start = i
			break
		}
	}
	if start < 0 {
		return v, fmt.Errorf("no JSON object in verdict: %q", content)
	}
	// Track depth so an embedded '}' inside a string doesn't fool us.
	depth, end := 0, -1
	inStr, escape := false, false
	for i := start; i < len(content); i++ {
		c := content[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inStr {
			escape = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return v, fmt.Errorf("unterminated JSON object in verdict: %q", content)
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &v); err != nil {
		return v, fmt.Errorf("decoding verdict JSON: %w", err)
	}
	return v, nil
}
