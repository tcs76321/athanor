package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/tcs76321/athanor/internal/artifact"
	"github.com/tcs76321/athanor/internal/evaluation"
	"github.com/tcs76321/athanor/internal/job"
	"github.com/tcs76321/athanor/internal/llm"
)

// comparisonVerdict is the §13.1 Phase 6 JSON the security persona
// produces. The schema mirrors the §13.1 example verbatim; the
// engine enforces the §19.3 rule against it.
type comparisonVerdict struct {
	Winner              string   `json:"winner"` // "new" | "previous" | "none"
	Confidence          float64  `json:"confidence"`
	Reasons             []string `json:"reasons"`
	MissingRequirements []string `json:"missing_requirements"`
}

// phaseCompare (§13.1 Phase 6, M3-T1): deterministically pick the
// winner of the comparison (§19.3). The LLM produces a structured
// JSON verdict, but the engine enforces the rule:
//
//	new wins ⟺ exists EvaluationRecord with
//	  better_than_previous == true AND confidence > min_judge_confidence
//
// The §19.3 guard is a safety belt: the LLM cannot flip a losing
// candidate into a winner by saying "winner: new" without backing
// it up. When the LLM's verdict conflicts with the guard, the
// engine downgrades (new → previous, or new → none if no previous
// exists) and audits the downgrade.
//
// Artifact status flow per §9.3:
//   - winner "new"      → candidate → accepted; previous → superseded
//   - winner "previous" → candidate → rejected; previous stays accepted
//   - winner "none"     → candidate → rejected; job → failed
func (e *Engine) phaseCompare(ctx context.Context, j job.Job) error {
	if e.eval == nil {
		return errors.New("engine: evaluation repo is nil (M3-T1; tests must pass it)")
	}
	p, t, err := e.contexts(ctx, j)
	if err != nil {
		return err
	}

	final, err := e.artifacts.LatestForJob(ctx, j.ID, finalKindFor(p.Archetype))
	if err != nil {
		return fmt.Errorf("loading final artifact for comparison: %w", err)
	}
	if err := e.artifacts.SetStatus(ctx, final.ID, artifact.StatusCandidate); err != nil {
		// A duplicate flip (e.g. a previous attempt left the
		// artifact as `candidate`) is not a hard error here.
		var ise *artifact.IllegalStatusError
		if !errors.As(err, &ise) {
			return fmt.Errorf("promoting final to candidate: %w", err)
		}
	}

	records, err := e.eval.ListByJob(ctx, j.ID)
	if err != nil {
		return fmt.Errorf("listing evaluation records: %w", err)
	}
	if len(records) == 0 {
		// Defensive: the engine only enters this phase with ≥1
		// evaluation record, but if a crash erased them we fail
		// loudly rather than silently accept.
		return errors.New("phaseCompare: no evaluation records persisted for this job")
	}

	// Previous-side context: the project's last accepted artifact,
	// plus its evaluation history. M3-T3 commit 3.1 adds the
	// "Previous-record summary" section to the comparison prompt
	// so the LLM judge can reason about how the previous scored
	// when it was a candidate, not just that it exists.
	var (
		previousID       string
		previousMeta     string
		previousRecords  []evaluation.Record
		previousAvgScore float64
		previousAvgConf  float64
	)
	if prev, err := e.artifacts.LatestAcceptedByProject(ctx, p.ID); err == nil {
		previousID = prev.ID
		previousMeta = prev.ID
		// Load the previous's own evaluation history (the
		// records that *describe* the previous when it was a
		// candidate). A previous with no records is fine —
		// the prompt just omits the "Previous-record summary"
		// section. The DB error is non-fatal: a corrupt or
		// partially-migrated DB should not fail the
		// comparison phase.
		if prs, perr := e.eval.ListByArtifact(ctx, prev.ID); perr == nil {
			previousRecords = prs
			if n := len(prs); n > 0 {
				var sumScore, sumConf float64
				for _, r := range prs {
					sumScore += r.Score
					sumConf += r.Confidence
				}
				previousAvgScore = sumScore / float64(n)
				previousAvgConf = sumConf / float64(n)
			}
		} else {
			// Best-effort: log and proceed with empty
			// previous-record history. The §19.3 guard
			// does not depend on the previous's records.
			e.audit(ctx, j.ID, map[string]any{
				"event":             "previous_records_load_failed",
				"previous_id":       prev.ID,
				"error":             perr.Error(),
			})
		}
	} else if !errors.Is(err, artifact.ErrNotFound) {
		return fmt.Errorf("loading previous accepted artifact: %w", err)
	}

	instructions := buildComparisonInstructions(final, records, previousID, previousRecords, previousAvgScore, previousAvgConf)
	resp, err := e.call(ctx, j, p, t, llm.PhaseComparing, llm.RoleSecurity, instructions)
	if err != nil {
		return err
	}

	verdict, err := parseComparisonVerdict(resp.Content)
	if err != nil {
		return fmt.Errorf("parsing comparison verdict: %w", err)
	}

	// M3-T2 commit 2.5: the §19.3 deterministic guard is now
	// a pure function in decide.go. The caller resolves the
	// threshold (config default 0.7 when zero) and
	// `hasPrevious` (whether the project has a prior
	// accepted artifact) before the call.
	threshold := e.cfg.Execution.MinJudgeConfidence
	if threshold <= 0 {
		threshold = 0.7
	}
	verdict = DecideWinner(verdict, records, threshold, previousID != "")

	e.audit(ctx, j.ID, map[string]any{
		"event":                "comparison",
		"winner":               verdict.Winner,
		"confidence":           verdict.Confidence,
		"reasons":              verdict.Reasons,
		"missing_requirements": verdict.MissingRequirements,
		"new_artifact_id":      final.ID,
		"previous_id":          previousMeta,
		"records":              len(records),
		// M3-T3 commit 3.1: previous-record context so
		// post-mortem readers can see how the previous
		// scored when it was a candidate. Used by the
		// M3-T7 quality probe for calibration analysis.
		"previous_records_count":   len(previousRecords),
		"previous_avg_score":       previousAvgScore,
		"previous_avg_confidence":  previousAvgConf,
	})

	// §9.3 status transitions.
	switch verdict.Winner {
	case "new":
		if previousID != "" {
			// Supersede the old accepted artifact.
			if err := e.artifacts.SetStatus(ctx, previousID, artifact.StatusSuperseded); err != nil {
				var ise *artifact.IllegalStatusError
				if !errors.As(err, &ise) {
					return fmt.Errorf("superseding previous: %w", err)
				}
			}
		}
		if err := e.artifacts.SetStatus(ctx, final.ID, artifact.StatusAccepted); err != nil {
			return fmt.Errorf("accepting new: %w", err)
		}
		_, err = e.jobs.Transition(ctx, j.ID, job.StateCompleted)
		return err
	case "previous":
		if err := e.artifacts.SetStatus(ctx, final.ID, artifact.StatusRejected); err != nil {
			return fmt.Errorf("rejecting new: %w", err)
		}
		_, err = e.jobs.Transition(ctx, j.ID, job.StateCompleted)
		return err
	default: // "none"
		if err := e.artifacts.SetStatus(ctx, final.ID, artifact.StatusRejected); err != nil {
			return fmt.Errorf("rejecting new (none winner): %w", err)
		}
		_, err = e.jobs.Transition(ctx, j.ID, job.StateFailed)
		return err
	}
}

// buildComparisonInstructions is the prompt for the security persona.
// It includes the final artifact content (truncated to 4 KB) and
// the EvaluationRecord set; the persona must produce the §13.1
// structured JSON. M3-T3 commit 3.1 adds a "Previous-record
// summary" section that lists the previous accepted artifact's
// own evaluation history, so the judge can reason about how the
// previous scored when it was a candidate (not just that it
// exists).
func buildComparisonInstructions(
	final artifact.Artifact,
	records []evaluation.Record,
	previousID string,
	previousRecords []evaluation.Record,
	previousAvgScore float64,
	previousAvgConf float64,
) string {
	content, _ := osReadFileLimited(final.StoragePath, 4096)
	var b strings.Builder
	fmt.Fprintf(&b, "COMPARISON. Decide whether to accept the new artifact.\n"+
		"new artifact_id: %s\nprevious artifact_id: %s\n", final.ID, previousID)
	b.WriteString("\nEvaluationRecords for the new artifact:\n")
	for i, r := range records {
		fmt.Fprintf(&b, "  record %d: artifact=%s, passed_tests=%v, missing_criteria=%v, security_issues=%v, better_than_previous=%v, confidence=%.2f, summary=%q\n",
			i+1, r.ArtifactID, r.PassedTests, r.MissingCriteria, r.SecurityIssues, r.BetterThanPrevious, r.Confidence, r.Summary)
	}
	if len(previousRecords) > 0 {
		// §3.1: surface the previous's own evaluation
		// history. The judge uses this to calibrate
		// "better than previous" (a non-trivial claim
		// when the previous scored 0.95 vs 0.30).
		b.WriteString("\nPrevious-record summary (the previous's own evaluation history):\n")
		fmt.Fprintf(&b, "  count: %d\n", len(previousRecords))
		fmt.Fprintf(&b, "  avg_score: %.2f\n", previousAvgScore)
		fmt.Fprintf(&b, "  avg_confidence: %.2f\n", previousAvgConf)
		for i, r := range previousRecords {
			fmt.Fprintf(&b, "  record %d: artifact=%s, passed_tests=%v, better_than_previous=%v, confidence=%.2f, summary=%q\n",
				i+1, r.ArtifactID, r.PassedTests, r.BetterThanPrevious, r.Confidence, r.Summary)
		}
	}
	b.WriteString("\nNew artifact content (truncated to 4 KB):\n")
	b.WriteString(content)
	b.WriteString("\n\nOutput JSON only: {winner: \"new\"|\"previous\"|\"none\", confidence: 0.0-1.0, reasons: [...], missing_requirements: [...]}")
	return b.String()
}

// osReadFileLimited reads up to `limit` bytes from path. Failures
// are non-fatal: the comparison proceeds with an empty content
// section rather than aborting the whole job.
func osReadFileLimited(path string, limit int) (string, error) {
	if path == "" {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, limit)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", err
	}
	return string(buf[:n]), nil
}

// parseComparisonVerdict is the comparison-phase twin of
// parseEvalVerdict. Same lenient-wrapping tolerance.
func parseComparisonVerdict(content string) (comparisonVerdict, error) {
	var v comparisonVerdict
	start := -1
	for i, c := range content {
		if c == '{' {
			start = i
			break
		}
	}
	if start < 0 {
		return v, fmt.Errorf("no JSON object in comparison verdict: %q", content)
	}
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
		return v, fmt.Errorf("unterminated JSON object in comparison verdict: %q", content)
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &v); err != nil {
		return v, fmt.Errorf("decoding comparison verdict: %w", err)
	}
	// Normalize the winner to one of the three known values; an
	// unknown string is treated as "none" (the safest default).
	switch v.Winner {
	case "new", "previous", "none":
	default:
		v.Winner = "none"
	}
	return v, nil
}
