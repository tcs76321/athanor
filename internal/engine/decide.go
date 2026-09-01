// M3-T2 commit 2.5: extract the §19.3 deterministic guard
// as a pure function.
//
// The function is a pure transformation: same inputs produce
// the same outputs, no I/O, no time, no random, no global state.
// This makes the §19.3 rule exhaustively testable in
// `decide_test.go` (table-driven, ≤ 9 rows per ADR-0014 §3.4)
// and gives M3-T3 a clean seam to extend the rule (e.g., for
// ties or boundary conditions) without touching `phaseCompare`.
//
// §19.3 (paraphrased): the LLM may say "winner: new", but the
// engine only honors that verdict when at least one
// EvaluationRecord in the current job's set has
// `better_than_previous == true` AND `confidence >
// min_judge_confidence`. Otherwise the verdict is downgraded:
//   - no prior accepted artifact   → "new" → "none"
//   - prior accepted artifact exists → "new" → "previous"
//
// The function does NOT flip the other direction: an LLM that
// says "previous" or "none" is left alone even when a record
// would back "new". The §19.3 rule is a *floor on acceptance*,
// not a recommendation engine. (A future M3-T3 may add a
// "LLM says previous but a strong record exists → reconsider"
// rule, but it is not part of the M3-T1 contract.)
package engine

import (
	"fmt"

	"github.com/tcs76321/athanor/internal/evaluation"
)

// DecideWinner applies the §19.3 rule to the LLM's verdict
// and returns the verdict the engine should act on. The
// input verdict is not mutated; a new value is returned. If
// a downgrade happened, the new `Reasons` slice contains the
// original reasons plus a single appended line explaining the
// downgrade.
//
// `threshold` is the resolved `min_judge_confidence` (the
// caller resolves config defaults before passing; the
// function does not consult config). A threshold <= 0
// disables the guard (every record meets it), which is
// useful for tests and matches the §19.3 "fail open"
// fallback when the operator leaves the field unset.
//
// `hasPrevious` is true when the project has a prior
// accepted artifact; false otherwise. The function does
// not query the DB — `phaseCompare` is the only caller and
// already has this information.
func DecideWinner(verdict comparisonVerdict, records []evaluation.Record, threshold float64, hasPrevious bool) comparisonVerdict {
	if threshold <= 0 {
		// Disabled: every record (if any) meets it. The
		// LLM's verdict stands. (The disabled case is
		// a single-record gate; the loop below is a
		// no-op when records is empty.)
		return verdict
	}
	// §19.3: new wins ⟺ ∃ EvaluationRecord with
	// better_than_previous AND confidence > threshold.
	strongNew := false
	for _, r := range records {
		if r.BetterThanPrevious && r.Confidence > threshold {
			strongNew = true
			break
		}
	}
	if verdict.Winner != "new" {
		// The guard only blocks an unsupported "new"; it
		// never promotes an unsupported "previous" or
		// "none". (See package doc comment.)
		return verdict
	}
	if strongNew {
		// The LLM's verdict is backed by a record. Honor it.
		return verdict
	}
	// Downgrade.
	out := verdict
	if hasPrevious {
		out.Winner = "previous"
		out.Reasons = append(out.Reasons,
			fmt.Sprintf("downgraded from 'new' to 'previous': no EvaluationRecord met better_than_previous + confidence > %.2f", threshold))
	} else {
		out.Winner = "none"
		out.Reasons = append(out.Reasons,
			fmt.Sprintf("downgraded from 'new' to 'none': no prior accepted artifact and no EvaluationRecord met confidence > %.2f", threshold))
	}
	return out
}
