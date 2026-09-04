// Package scanner is the §21.3 / §21.4 scanning layer for the M4
// airlock (ROADMAP M4-T2/T3/T4; ADR-0015). The package owns the
// Scanner interface, the per-pipeline registry, and the in-tree
// scanner implementations (heuristic, size, zipbomb).
//
// External scanners (ClamAV, YARA) live in `cmd/athanor/scanners/`
// per the Gate G1 contract; the registry's NewRegistry is the
// single dispatch site that bridges the in-tree and external
// halves.
//
// # Pipeline kinds
//
// Three PipelineKinds exist today (ADR-0015 §"Trust boundaries,
// not all text"):
//
//   - PipelineIngress: inbox file → workspace. `.processed/`
//   - PipelineEgress:  accepted artifact → exports/
//   - PipelineUserPrompt: long goal text → first-class instruction
//
// A fourth kind is intentionally NOT shipped: skill manifests are
// out of scope by project policy (ADR-0016). Adding a kind is a
// one-line enum + a one-line registry branch + a CHECK-constraint
// update in the migrations.
package scanner

import (
	"context"
	"io"
	"os"
	"time"
)

// Verdict is a scanner's classification of an input.
type Verdict int

// The three verdicts. The numeric ordering is meaningful: a
// registry's worst-wins aggregation is `max(verdict)` over all
// scanners, so `VerdictRejected > VerdictUncertain > VerdictClean`.
// Do not reorder the constants.
const (
	// VerdictClean: every check passed. The pipeline may proceed.
	VerdictClean Verdict = iota
	// VerdictUncertain: a check could not decide. The pipeline
	// fails closed (quarantine) — the absence of certainty is
	// not evidence of safety.
	VerdictUncertain
	// VerdictRejected: a check found a violation. The pipeline
	// fails closed (quarantine or reject, depending on the
	// choke point).
	VerdictRejected
)

// String renders a Verdict for human-readable audit rows and test
// failure messages. The values are stable strings, not iota
// numbers, so log output is greppable across versions.
func (v Verdict) String() string {
	switch v {
	case VerdictClean:
		return "clean"
	case VerdictUncertain:
		return "uncertain"
	case VerdictRejected:
		return "rejected"
	default:
		return "unknown"
	}
}

// PipelineKind names the choke point a scan runs at. See the
// package doc for the three kinds and why a fourth is not shipped.
type PipelineKind string

const (
	PipelineIngress    PipelineKind = "ingress"
	PipelineEgress     PipelineKind = "egress"
	PipelineUserPrompt PipelineKind = "user-prompt"
)

// Valid reports whether k is one of the three closed-set kinds.
// The migrations' CHECK constraint on quarantined_files.pipeline
// uses the same set; the two are kept in sync by ADR-0015 §"Trust
// boundaries, not all text".
func (k PipelineKind) Valid() bool {
	switch k {
	case PipelineIngress, PipelineEgress, PipelineUserPrompt:
		return true
	default:
		return false
	}
}

// ScanInput is what a scanner receives. Two stream shapes are
// supported so a scanner that wants bytes can take them and one
// that wants a Reader (e.g. an external subprocess) can stream:
//
//   - Bytes non-nil: the scanner reads from the in-memory slice.
//   - Bytes nil and Reader non-nil: the scanner reads from the
//     stream. The pipeline guarantees the Reader is positioned
//     at byte 0 and that Size is the total length to be read.
//
// Size and Mode are always populated (post-Validate) so a scanner
// that does not need to read at all (e.g. the `size` scanner)
// can short-circuit on Size alone.
type ScanInput struct {
	Path   string
	Bytes  []byte
	Reader io.Reader
	Size   int64
	Mode   os.FileMode
}

// ScanResult is what a scanner returns. Reason is a stable,
// machine-readable string (e.g. "scanner:clamav:Win.Test.EICAR_HDB-1");
// the audit-log row carries the *first* Reason across all
// scanners in the worst-wins aggregation. Details is
// scanner-specific JSON-encoded metadata (per-scanner verdicts,
// stdout from a subprocess, decompression ratios seen, etc.).
//
// Duration is the wall-time the scanner took; the registry's
// `RunAll` aggregates the per-scanner results into the
// per-pipeline audit row.
type ScanResult struct {
	Verdict  Verdict
	Reason   string
	Details  map[string]any
	Duration time.Duration
}

// Scanner is the contract every scanner implementation satisfies.
// The interface is intentionally tiny: a scanner is a pure
// function (ctx, in) (result, err). The registry's worst-wins
// aggregation, the pipeline's audit-row construction, and the
// test fakes all sit on top of this contract.
//
// A non-nil error is treated as VerdictUncertain by the
// registry; the implementation does not need to map errors
// to verdicts itself.
type Scanner interface {
	// Name is the registry key (e.g. "size", "zipbomb",
	// "prompt-injection-heuristic", "clamav", "yara").
	// The name is stable across restarts; it appears in
	// audit rows and quarantine `reason` strings.
	Name() string

	// Scan classifies in and returns a result. The
	// implementation MUST honor ctx cancellation: a scanner
	// that ignores ctx can wedge the pipeline on a hung
	// subprocess.
	Scan(ctx context.Context, in ScanInput) (ScanResult, error)
}
