// Package scanner tests (M4-T2). The M4-T3 commit adds the
// concrete in-tree scanners (heuristic, size, zipbomb) and the
// external-binary adapters; this file covers only the types
// the M4-T2 ingress pipeline needs:
//
//   - Verdict ordering (worst-wins aggregation's foundation)
//   - PipelineKind.Valid (the closed-set enforcement)
//   - Registry.NewRegistry validation (typos caught at boot)
//   - Registry.RunAll worst-wins aggregation
//   - Scanner error → VerdictUncertain conversion
//
// The table-driven structure mirrors internal/airlock/paths'
// tests: every rejection class is a row, every row exercises
// the contract once.
package scanner

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fixedScanner is a test fake that returns a fixed result.
type fixedScanner struct {
	name   string
	result ScanResult
	err    error
}

func (f *fixedScanner) Name() string { return f.name }

func (f *fixedScanner) Scan(ctx context.Context, in ScanInput) (ScanResult, error) {
	return f.result, f.err
}

func TestVerdict_Ordering(t *testing.T) {
	if VerdictClean >= VerdictUncertain {
		t.Error("VerdictClean must sort below VerdictUncertain (worst-wins aggregation)")
	}
	if VerdictUncertain >= VerdictRejected {
		t.Error("VerdictUncertain must sort below VerdictRejected (worst-wins aggregation)")
	}
	if VerdictRejected < VerdictClean {
		t.Error("VerdictRejected must sort above VerdictClean (worst-wins aggregation)")
	}
}

func TestVerdict_String(t *testing.T) {
	cases := map[Verdict]string{
		VerdictClean:     "clean",
		VerdictUncertain: "uncertain",
		VerdictRejected:  "rejected",
		Verdict(99):      "unknown",
	}
	for v, want := range cases {
		if got := v.String(); got != want {
			t.Errorf("Verdict(%d).String() = %q, want %q", v, got, want)
		}
	}
}

func TestPipelineKind_Valid(t *testing.T) {
	for _, k := range []PipelineKind{PipelineIngress, PipelineEgress, PipelineUserPrompt} {
		if !k.Valid() {
			t.Errorf("PipelineKind(%q).Valid() = false, want true", k)
		}
	}
	for _, k := range []PipelineKind{"", "skill", "INGRESS", "ingress "} {
		if k.Valid() {
			t.Errorf("PipelineKind(%q).Valid() = true, want false (closed set)", k)
		}
	}
}

func TestNewRegistry_RejectsUnknownName(t *testing.T) {
	// "size" is registered but "yara" is not. The egress
	// pipeline lists "yara", so NewRegistry must reject
	// the configuration rather than silently dropping the
	// scanner at runtime.
	_, err := NewRegistry(
		map[string]Scanner{"size": &fixedScanner{name: "size"}},
		[]string{"size"},
		[]string{"yara"}, // not in scanners
		[]string{"size"},
	)
	if err == nil {
		t.Fatal("NewRegistry with unknown scanner name returned nil error; want failure")
	}
	if !strings.Contains(err.Error(), "unknown scanner") {
		t.Errorf("error %q does not mention 'unknown scanner'", err.Error())
	}
	if !strings.Contains(err.Error(), "yara") {
		t.Errorf("error %q does not name the unknown scanner", err.Error())
	}
}

func TestNewRegistry_RejectsDuplicateNames(t *testing.T) {
	_, err := NewRegistry(
		map[string]Scanner{"size": &fixedScanner{name: "size"}},
		[]string{"size", "size"}, // duplicate
		[]string{"size"},
		[]string{"size"},
	)
	if err == nil {
		t.Fatal("NewRegistry with duplicate name returned nil error; want failure")
	}
	if !strings.Contains(err.Error(), "twice") {
		t.Errorf("error %q does not mention the duplicate", err.Error())
	}
}

func TestNewRegistry_RejectsNilScanner(t *testing.T) {
	_, err := NewRegistry(
		map[string]Scanner{"size": nil},
		[]string{"size"},
		[]string{"size"},
		[]string{"size"},
	)
	if err == nil {
		t.Fatal("NewRegistry with nil scanner returned nil error; want failure")
	}
}

func TestNewRegistry_RejectsNameMismatch(t *testing.T) {
	_, err := NewRegistry(
		map[string]Scanner{
			"size": &fixedScanner{name: "sizee"}, // registered as "size" but Name() is "sizee"
		},
		[]string{"size"},
		[]string{"size"},
		[]string{"size"},
	)
	if err == nil {
		t.Fatal("NewRegistry with Name() mismatch returned nil error; want failure")
	}
	if !strings.Contains(err.Error(), "Name()") {
		t.Errorf("error %q does not mention Name() mismatch", err.Error())
	}
}

func TestNewRegistry_RejectsEmptyPipeline(t *testing.T) {
	// Fail-closed: a pipeline with no scanners is a
	// configuration error (a present-but-empty list means
	// "scan with nothing", which is silent-pass territory).
	_, err := NewRegistry(
		map[string]Scanner{"size": &fixedScanner{name: "size"}},
		[]string{"size"},
		[]string{}, // empty egress — rejected
		[]string{"size"},
	)
	if err == nil {
		t.Fatal("NewRegistry with empty pipeline list returned nil error; want failure")
	}
	if !strings.Contains(err.Error(), "egress") {
		t.Errorf("error %q does not name the empty pipeline", err.Error())
	}
}

func TestRunAll_WorstWinsAggregation(t *testing.T) {
	// Three scanners: clean, uncertain, rejected.
	// The aggregated Verdict must be Rejected (the worst).
	// The aggregated Reason must be the deciding scanner's
	// (rejected, not clean or uncertain).
	clean := &fixedScanner{name: "clean", result: ScanResult{Verdict: VerdictClean, Reason: "clean-ok"}}
	uncert := &fixedScanner{name: "uncertain", result: ScanResult{Verdict: VerdictUncertain, Reason: "uncert-thing"}}
	rej := &fixedScanner{name: "rejected", result: ScanResult{Verdict: VerdictRejected, Reason: "rej-bad"}}
	reg, err := NewRegistry(
		map[string]Scanner{"clean": clean, "uncertain": uncert, "rejected": rej},
		[]string{"clean", "uncertain", "rejected"},
		[]string{"clean"},
		[]string{"clean"},
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	res := reg.RunAll(context.Background(), PipelineIngress, ScanInput{Path: "x"})
	if res.Verdict != VerdictRejected {
		t.Errorf("Verdict = %v, want VerdictRejected (worst-wins)", res.Verdict)
	}
	if res.Reason != "rej-bad" {
		t.Errorf("Reason = %q, want %q (deciding scanner's reason)", res.Reason, "rej-bad")
	}
	if len(res.PerScanner) != 3 {
		t.Errorf("PerScanner len = %d, want 3", len(res.PerScanner))
	}
	// Per-scanner order is registration order.
	wantOrder := []string{"clean", "uncertain", "rejected"}
	for i, ps := range res.PerScanner {
		if ps.Scanner != wantOrder[i] {
			t.Errorf("PerScanner[%d].Scanner = %q, want %q", i, ps.Scanner, wantOrder[i])
		}
	}
}

func TestRunAll_ScannerErrorBecomesUncertain(t *testing.T) {
	boom := &fixedScanner{name: "boom", err: errors.New("subprocess hung")}
	clean := &fixedScanner{name: "clean", result: ScanResult{Verdict: VerdictClean}}
	reg, err := NewRegistry(
		map[string]Scanner{"boom": boom, "clean": clean},
		[]string{"clean", "boom"},
		[]string{"clean"},
		[]string{"clean"},
	)
	if err != nil {
		t.Fatal(err)
	}
	res := reg.RunAll(context.Background(), PipelineIngress, ScanInput{})
	if res.Verdict != VerdictUncertain {
		t.Errorf("Verdict = %v, want VerdictUncertain (scanner error fails closed)", res.Verdict)
	}
	if !strings.Contains(res.Reason, "error") || !strings.Contains(res.Reason, "subprocess hung") {
		t.Errorf("Reason = %q, want it to contain the underlying error", res.Reason)
	}
	// The per-scanner row preserves the original error.
	if res.PerScanner[1].Err == nil {
		t.Error("PerScanner[1].Err = nil; want the original error preserved")
	}
}

func TestRunAll_AllClean(t *testing.T) {
	a := &fixedScanner{name: "a", result: ScanResult{Verdict: VerdictClean}}
	b := &fixedScanner{name: "b", result: ScanResult{Verdict: VerdictClean}}
	reg, err := NewRegistry(
		map[string]Scanner{"a": a, "b": b},
		[]string{"a", "b"},
		[]string{"a"},
		[]string{"a"},
	)
	if err != nil {
		t.Fatal(err)
	}
	res := reg.RunAll(context.Background(), PipelineIngress, ScanInput{})
	if res.Verdict != VerdictClean {
		t.Errorf("Verdict = %v, want VerdictClean", res.Verdict)
	}
	if res.Reason != "" {
		t.Errorf("Reason = %q, want empty (no deciding scanner)", res.Reason)
	}
}

func TestRunAll_PipelineIsolation(t *testing.T) {
	// The three pipelines are independent. A scanner
	// registered for ingress must not run for egress.
	// We register distinct scanners per pipeline to
	// exercise the isolation.
	a := &fixedScanner{name: "a", result: ScanResult{Verdict: VerdictClean}}
	b := &fixedScanner{name: "b", result: ScanResult{Verdict: VerdictClean}}
	reg, err := NewRegistry(
		map[string]Scanner{"a": a, "b": b},
		[]string{"a"},
		[]string{"b"},
		[]string{"a"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if names := reg.Scanners(PipelineEgress); len(names) != 1 || names[0] != "b" {
		t.Errorf("egress scanners = %v, want [b] (pipeline isolation)", names)
	}
	// The egress pipeline's RunAll uses "b", not "a".
	res := reg.RunAll(context.Background(), PipelineEgress, ScanInput{})
	if res.Verdict != VerdictClean {
		t.Errorf("egress Verdict = %v, want VerdictClean", res.Verdict)
	}
	if len(res.PerScanner) != 1 || res.PerScanner[0].Scanner != "b" {
		t.Errorf("egress PerScanner = %v, want one row for b", res.PerScanner)
	}
}
