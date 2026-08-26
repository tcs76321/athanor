package llm

import (
	"strings"
	"testing"

	"github.com/tcs76321/athanor/internal/config"
)

// testFloors returns the default context-floor configuration.
func testFloors() config.ContextEngine {
	cfg, err := config.Default()
	if err != nil {
		panic(err)
	}
	return cfg.ContextEngine
}

func TestFloorForArchetypes(t *testing.T) {
	ce := testFloors()
	cases := []struct {
		archetype string
		want      int
		wantOK    bool
	}{
		{ArchetypeCode, 32768, true},
		{ArchetypeDocument, 16384, true},
		{ArchetypeText, 8192, true},
		{ArchetypeData, 8192, true},
		{ArchetypeMedia, 8192, true},
		{"telepathy", 0, false},
	}
	for _, tc := range cases {
		got, ok := FloorFor(tc.archetype, ce)
		if ok != tc.wantOK || got != tc.want {
			t.Errorf("FloorFor(%q) = (%d, %t), want (%d, %t)", tc.archetype, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestCheckFeasibleWhenAvailableMeetsRequirement(t *testing.T) {
	ce := testFloors()
	// main (target 32768) diverging on a code project with full target
	// available: feasible, required exactly max(32768, 32768).
	p := Persona{Role: RoleMain, ContextTarget: 32768}
	v := Check(p, PhaseDiverging, ArchetypeCode, 32768, ce)
	if !v.Feasible {
		t.Fatalf("verdict = %+v, want feasible", v)
	}
	if v.Required != 32768 {
		t.Errorf("Required = %d, want 32768", v.Required)
	}
	if v.Recommendation != "" {
		t.Errorf("feasible verdict carries recommendation %q, want empty", v.Recommendation)
	}
}

func TestCheckPersonaTargetAboveFloorWins(t *testing.T) {
	ce := testFloors()
	// wide targets 65536 > coding floor 32768: requirement is the persona
	// target, and 40k available is still infeasible.
	p := Persona{Role: RoleWide, ContextTarget: 65536}
	v := Check(p, PhaseDiverging, ArchetypeCode, 40000, ce)
	if v.Feasible {
		t.Fatal("verdict feasible with 40000 < 65536 required, want infeasible")
	}
	if v.Required != 65536 {
		t.Errorf("Required = %d, want 65536 (persona target above floor)", v.Required)
	}
}

func TestCheckTallExemptOnlyInPlanning(t *testing.T) {
	ce := testFloors()
	tall := Persona{Role: RoleTall, ContextTarget: 16384}

	// ADR-0002: tall during planning is exempt from the coding floor, so
	// its 16384 target is enough when 16384 is available.
	if v := Check(tall, PhasePlanning, ArchetypeCode, 16384, ce); !v.Feasible {
		t.Errorf("tall planning verdict = %+v, want feasible (floor exempt)", v)
	}

	// The same persona in synthesis (a full-fidelity phase) is bound by
	// the floor: 16384 available < 32768 required → pause + recommend.
	v := Check(tall, PhaseSynthesizing, ArchetypeCode, 16384, ce)
	if v.Feasible {
		t.Fatal("tall synthesizing verdict feasible, want infeasible (floor applies)")
	}
	if v.Required != 32768 {
		t.Errorf("Required = %d, want 32768 (coding floor above tall target)", v.Required)
	}
	if !strings.Contains(v.Recommendation, "tall") || !strings.Contains(v.Recommendation, "never silently") {
		t.Errorf("recommendation lacks persona identity / no-silent-truncation note: %q", v.Recommendation)
	}
}

func TestCheckSecurityBoundByFloor(t *testing.T) {
	ce := testFloors()
	// §12.6 binds security during evaluation/comparison. With the default
	// security target (8192) below the coding floor (32768), the check
	// must fail loudly and recommend — never silently reduce.
	sec := Persona{Role: RoleSecurity, ContextTarget: 8192}
	v := Check(sec, PhaseComparing, ArchetypeCode, 8192, ce)
	if v.Feasible {
		t.Fatal("security comparing code verdict feasible at 8192, want infeasible (§12.6 floor)")
	}
	if v.Required != 32768 {
		t.Errorf("Required = %d, want 32768", v.Required)
	}
	// A text project's simple floor (8192) is within security's target.
	if v := Check(sec, PhaseComparing, ArchetypeText, 8192, ce); !v.Feasible {
		t.Errorf("security comparing text verdict = %+v, want feasible", v)
	}
}

func TestCheckUnknownArchetypeFailsClosed(t *testing.T) {
	ce := testFloors()
	p := Persona{Role: RoleMain, ContextTarget: 32768}
	v := Check(p, PhaseDiverging, "astral", 999999, ce)
	if v.Feasible {
		t.Fatal("unknown archetype verdict feasible, want infeasible (fail closed)")
	}
	if !strings.Contains(v.Recommendation, "unknown archetype") {
		t.Errorf("recommendation = %q, want unknown-archetype explanation", v.Recommendation)
	}
}
