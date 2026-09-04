package scanner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Registry is the per-pipeline scanner dispatcher. The three
// PipelineKinds map to independent lists of scanners; an
// operator who wants prompt-injection off at ingress sets
// `airlock.scanners.ingress: [size, zipbomb, clamav, yara]` and
// the registry honors it.
//
// The registry is the single source of truth for "which
// scanner names exist." The M4-T2 ingest pipeline calls
// `RunAll(ctx, PipelineIngress, in)`; the M4-T3 close-out
// adds the concrete instantiation (in-tree scanners wired by
// name; external scanners via `cmd/athanor/scanners`). The
// M4-T4 egress loop calls `RunAll(ctx, PipelineEgress, in)`.
type Registry struct {
	mu         sync.RWMutex
	byName     map[string]Scanner
	byPipeline map[PipelineKind][]string
}

// NewRegistry constructs a registry from the per-pipeline name
// lists. The full instantiation logic (in-tree scanners built
// here, external scanners built in `cmd/athanor/scanners` and
// passed in via `external`) lands in M4-T3. The signature here
// is the T2 placeholder: callers pass a `map[string]Scanner`
// of already-constructed scanners, and the registry validates
// that every named scanner is present.
//
// In M4-T3 this constructor will be replaced or extended; the
// `M4-T2` commit intentionally does not over-design the
// constructor. The tests for T2 build a registry with a
// hand-constructed map so they can exercise the worst-wins
// aggregation and the per-pipeline selection without
// committing to the M4-T3 dispatcher shape.
func NewRegistry(scanners map[string]Scanner, ingress, egress, userPrompt []string) (*Registry, error) {
	if scanners == nil {
		scanners = map[string]Scanner{}
	}
	for name, s := range scanners {
		if s == nil {
			return nil, fmt.Errorf("scanner %q is nil", name)
		}
		if s.Name() != name {
			return nil, fmt.Errorf("scanner registered as %q but Name()=%q", name, s.Name())
		}
	}
	check := func(kind PipelineKind, names []string) error {
		if !kind.Valid() {
			return fmt.Errorf("invalid pipeline kind %q", kind)
		}
		if len(names) == 0 {
			return fmt.Errorf("%s pipeline has no scanners (fail-closed: at least one scanner is required)", kind)
		}
		seen := map[string]bool{}
		for _, n := range names {
			if seen[n] {
				return fmt.Errorf("%s pipeline lists %q twice", kind, n)
			}
			seen[n] = true
			if _, ok := scanners[n]; !ok {
				return fmt.Errorf("%s pipeline references unknown scanner %q", kind, n)
			}
		}
		return nil
	}
	if err := check(PipelineIngress, ingress); err != nil {
		return nil, err
	}
	if err := check(PipelineEgress, egress); err != nil {
		return nil, err
	}
	if err := check(PipelineUserPrompt, userPrompt); err != nil {
		return nil, err
	}
	return &Registry{
		byName: scanners,
		byPipeline: map[PipelineKind][]string{
			PipelineIngress:    append([]string(nil), ingress...),
			PipelineEgress:     append([]string(nil), egress...),
			PipelineUserPrompt: append([]string(nil), userPrompt...),
		},
	}, nil
}

// ErrUnknownPipeline is returned by RunAll when called with a
// PipelineKind that has no scanners registered. This is
// configuration-level (a present-but-empty `airlock.scanners`
// list) and should fail loudly at construction; the error is
// returned here as a defensive check.
var ErrUnknownPipeline = errors.New("scanner: unknown pipeline kind")

// Scanners returns the named scanners for a pipeline, in the
// order they were registered. The returned slice is a copy;
// callers may not mutate the registry through it.
func (r *Registry) Scanners(kind PipelineKind) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src := r.byPipeline[kind]
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// PipelineResult is the aggregated outcome of RunAll. The
// per-scanner results are kept in the order the registry ran
// them (stable for testability); the aggregated Verdict is
// the worst-wins over all of them; Reason is the Reason of
// the *first* result that achieved the worst verdict (so the
// audit row names the actually-deciding scanner, not whichever
// scanner happened to finish last).
type PipelineResult struct {
	Kind    PipelineKind
	Verdict Verdict
	Reason  string
	Details map[string]any
	// PerScanner is one entry per registered scanner, in the
	// order the registry ran them. Used for the audit row's
	// per-scanner breakdown; tests assert the order to pin
	// the registry's deterministic behavior.
	PerScanner []PerScannerResult
	// TotalDuration is the sum of per-scanner durations
	// (not wall-clock from the caller's perspective; that
	// matters only for tests that want to assert sequential
	// execution).
	TotalDuration time.Duration
}

// PerScannerResult is one row in PipelineResult.PerScanner.
// Scanner is the registered name; Result is the scanner's
// ScanResult. A non-nil Err is recorded alongside the result
// (the registry converts the error into VerdictUncertain for
// the aggregation; the original error is preserved here for
// the audit row's `error` field).
type PerScannerResult struct {
	Scanner string
	Result  ScanResult
	Err     error
}

// RunAll runs every registered scanner for kind against in and
// returns the worst-wins aggregation. The contract:
//
//   - Each scanner is invoked with the same ScanInput.
//   - A scanner error is recorded as VerdictUncertain with
//     reason `scanner:<name>:error:<err>`; the original error
//     is preserved in PerScanner.
//   - The aggregated Verdict is max(per-scanner verdicts).
//   - The aggregated Reason is the first per-scanner Reason
//     that achieved the worst verdict (the "deciding" scanner).
//   - PerScanner is populated in registration order.
//   - An empty registered list for kind is treated as
//     VerdictUncertain (fail-closed): running with no
//     scanners is a configuration error, not a clean
//     pass. The reason is `scanner:pipeline:empty`.
//
// RunAll runs the scanners **sequentially**. Parallel
// execution is a future optimization; the current ingress
// pipeline ingests one file at a time and the cost is
// dominated by external-binary subprocess startup, which
// parallelizing would not help.
func (r *Registry) RunAll(ctx context.Context, kind PipelineKind, in ScanInput) PipelineResult {
	r.mu.RLock()
	names := append([]string(nil), r.byPipeline[kind]...)
	r.mu.RUnlock()

	res := PipelineResult{Kind: kind, Verdict: VerdictClean}
	if !kind.Valid() {
		res.Verdict = VerdictUncertain
		res.Reason = "scanner:pipeline:invalid"
		return res
	}
	if len(names) == 0 {
		// Fail-closed: an empty scanner list is
		// either a misconfiguration (NewRegistry
		// validated each list is non-empty for the
		// closed set of in-tree scanners) or a
		// future operator who opted out of scanning.
		// In both cases, the pipeline should
		// quarantine, not pass.
		res.Verdict = VerdictUncertain
		res.Reason = "scanner:pipeline:empty"
		return res
	}
	for _, name := range names {
		r.mu.RLock()
		s, ok := r.byName[name]
		r.mu.RUnlock()
		if !ok {
			// Defensive: NewRegistry validates this; if
			// a name slipped through, treat as error.
			res.PerScanner = append(res.PerScanner, PerScannerResult{
				Scanner: name,
				Err:     fmt.Errorf("scanner %q not found in registry", name),
			})
			res.Verdict = VerdictUncertain
			continue
		}
		start := time.Now()
		sr, err := s.Scan(ctx, in)
		sr.Duration = time.Since(start)
		ps := PerScannerResult{Scanner: name, Result: sr}
		if err != nil {
			ps.Err = err
			// Convert the error to VerdictUncertain for
			// the aggregation; preserve the original
			// error in the per-scanner row. The
			// aggregation below reads ps.Result, not
			// the raw sr, so the override takes effect.
			ps.Result.Verdict = VerdictUncertain
			ps.Result.Reason = fmt.Sprintf("scanner:%s:error:%v", name, err)
		}
		res.PerScanner = append(res.PerScanner, ps)
		res.TotalDuration += sr.Duration
		// Worst-wins: only upgrade, never downgrade.
		// Read from ps.Result (not sr) so a scanner
		// error is reflected as VerdictUncertain.
		if ps.Result.Verdict > res.Verdict {
			res.Verdict = ps.Result.Verdict
		}
	}
	// Find the first per-scanner Reason matching the
	// aggregated Verdict (the "deciding" scanner).
	for _, ps := range res.PerScanner {
		if ps.Result.Verdict == res.Verdict && ps.Result.Reason != "" {
			res.Reason = ps.Result.Reason
			break
		}
	}
	// Build a flat Details map: scanner name → details.
	// Used by the audit-row writer to capture per-scanner
	// metadata. A nil Details on a scanner is recorded as
	// nil (so the audit row can distinguish "no details"
	// from "empty map").
	res.Details = map[string]any{}
	for _, ps := range res.PerScanner {
		res.Details[ps.Scanner] = ps.Result.Details
	}
	return res
}
