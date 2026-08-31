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

	// Previous-side context: the project's last accepted artifact.
	var (
		previousID   string
		previousMeta string
	)
	if prev, err := e.artifacts.LatestAcceptedByProject(ctx, p.ID); err == nil {
		previousID = prev.ID
		previousMeta = prev.ID
	} else if !errors.Is(err, artifact.ErrNotFound) {
		return fmt.Errorf("loading previous accepted artifact: %w", err)
	}

	instructions := buildComparisonInstructions(final, records, previousID)
	resp, err := e.call(ctx, j, p, t, llm.PhaseComparing, llm.RoleSecurity, instructions)
	if err != nil {
		return err
	}

	verdict, err := parseComparisonVerdict(resp.Content)
	if err != nil {
		return fmt.Errorf("parsing comparison verdict: %w", err)
	}

	// §19.3 deterministic guard.
	threshold := e.cfg.Execution.MinJudgeConfidence
	if threshold <= 0 {
		threshold = 0.7
	}
	strongNew := false
	for _, r := range records {
		if r.BetterThanPrevious && r.Confidence > threshold {
			strongNew = true
			break
		}
	}
	if verdict.Winner == "new" && !strongNew {
		// LLM said "new" but no EvaluationRecord backs it up.
		// Downgrade to "previous" if one exists, else "none".
		if previousID != "" {
			verdict.Winner = "previous"
			verdict.Reasons = append(verdict.Reasons,
				fmt.Sprintf("downgraded from 'new' to 'previous': no EvaluationRecord met better_than_previous + confidence > %.2f", threshold))
		} else {
			verdict.Winner = "none"
			verdict.Reasons = append(verdict.Reasons,
				fmt.Sprintf("downgraded from 'new' to 'none': no prior accepted artifact and no EvaluationRecord met confidence > %.2f", threshold))
		}
	}

	e.audit(ctx, j.ID, map[string]any{
		"event":                "comparison",
		"winner":               verdict.Winner,
		"confidence":           verdict.Confidence,
		"reasons":              verdict.Reasons,
		"missing_requirements": verdict.MissingRequirements,
		"new_artifact_id":      final.ID,
		"previous_id":          previousMeta,
		"records":              len(records),
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
// structured JSON.
func buildComparisonInstructions(final artifact.Artifact, records []evaluation.Record, previousID string) string {
	content, _ := osReadFileLimited(final.StoragePath, 4096)
	var b strings.Builder
	fmt.Fprintf(&b, "COMPARISON. Decide whether to accept the new artifact.\n"+
		"new artifact_id: %s\nprevious artifact_id: %s\n", final.ID, previousID)
	b.WriteString("\nEvaluationRecords for the new artifact:\n")
	for i, r := range records {
		fmt.Fprintf(&b, "  record %d: artifact=%s, passed_tests=%v, missing_criteria=%v, security_issues=%v, better_than_previous=%v, confidence=%.2f, summary=%q\n",
			i+1, r.ArtifactID, r.PassedTests, r.MissingCriteria, r.SecurityIssues, r.BetterThanPrevious, r.Confidence, r.Summary)
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
