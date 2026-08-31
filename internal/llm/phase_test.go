package llm

import "testing"

// TestResolveTemperature encodes the full §13.1 precedence table:
// phase policy (pin or range-clamp) over persona defaults, and untouched
// defaults for phases without a policy. M3-T1's stage-override hook
// (ExplorationPath seam) is exercised separately in TestResolveTemperatureStageOverride.
func TestResolveTemperature(t *testing.T) {
	cases := []struct {
		name           string
		phase          string
		personaDefault float64
		want           float64
	}{
		// Planning is pinned at 0.2: tall's 0.2 default is honored
		// (equals the pin), main's 0.4 is overridden.
		{"planning tall default equals pin", PhasePlanning, 0.2, 0.2},
		{"planning main default above pin", PhasePlanning, 0.4, 0.2},
		{"planning zero default below pin", PhasePlanning, 0.0, 0.2},

		// Diverging is a range [0.7, 1.1]: alternative's 0.8 stays;
		// main's 0.4 and tall's 0.2 clamp up; >1.1 clamps down.
		{"diverging alternative in range", PhaseDiverging, 0.8, 0.8},
		{"diverging main below range clamps up", PhaseDiverging, 0.4, 0.7},
		{"diverging tall far below range", PhaseDiverging, 0.2, 0.7},
		{"diverging above range clamps down", PhaseDiverging, 1.4, 1.1},
		{"diverging at upper bound", PhaseDiverging, 1.1, 1.1},

		// Evaluation and comparison are pinned at 0.0 regardless of the
		// persona default — judgment is deterministic (invariant §4.2).
		{"evaluating security default", PhaseEvaluating, 0.0, 0.0},
		{"evaluating nonzero default pinned to zero", PhaseEvaluating, 0.4, 0.0},
		{"comparing nonzero default pinned to zero", PhaseComparing, 0.9, 0.0},

		// Reflection range [0.6, 0.8].
		{"reflecting main below range", PhaseReflecting, 0.4, 0.6},
		{"reflecting alternative in range", PhaseReflecting, 0.8, 0.8},

		// Synthesis pinned at 0.2.
		{"synthesizing main", PhaseSynthesizing, 0.4, 0.2},
		{"synthesizing tall", PhaseSynthesizing, 0.2, 0.2},

		// Phases without a policy leave the persona default untouched.
		{"context_building has no policy", "context_building", 0.4, 0.4},
		{"queued has no policy", "queued", 0.9, 0.9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveTemperature(tc.phase, tc.personaDefault, nil); got != tc.want {
				t.Errorf("ResolveTemperature(%q, %v, nil) = %v, want %v",
					tc.phase, tc.personaDefault, got, tc.want)
			}
		})
	}
}

// TestResolveTemperatureStageOverride covers the M3-T1 ExplorationPath
// hook: a non-nil stageOverride wins outright, even over a 0.0-pinned
// phase. This is the contract that makes future ExplorationPath
// support a thin pass-through on the call site: the engine passes
// nil today, and the resolver behaves identically to the M1 form.
func TestResolveTemperatureStageOverride(t *testing.T) {
	t.Run("override wins over 0.0 pin", func(t *testing.T) {
		one := 1.0
		if got := ResolveTemperature(PhaseEvaluating, 0.0, &one); got != 1.0 {
			t.Errorf("evaluating with stage=1.0 = %v, want 1.0 (override must beat the 0.0 pin)", got)
		}
	})
	t.Run("override wins over range clamp", func(t *testing.T) {
		zero := 0.0
		if got := ResolveTemperature(PhaseDiverging, 0.8, &zero); got != 0.0 {
			t.Errorf("diverging with stage=0.0 = %v, want 0.0 (override must beat the [0.7,1.1] range)", got)
		}
	})
	t.Run("nil preserves M1 behavior", func(t *testing.T) {
		if got := ResolveTemperature(PhaseEvaluating, 0.4, nil); got != 0.0 {
			t.Errorf("evaluating with nil stage = %v, want 0.0 (M1 form unchanged)", got)
		}
	})
}

func TestPhaseSpecFor(t *testing.T) {
	if _, ok := PhaseSpecFor("context_building"); ok {
		t.Error("PhaseSpecFor(context_building) reported a policy; §13.1 defines none")
	}
	for _, phase := range []string{PhasePlanning, PhaseDiverging, PhaseEvaluating, PhaseReflecting, PhaseSynthesizing, PhaseComparing} {
		if _, ok := PhaseSpecFor(phase); !ok {
			t.Errorf("PhaseSpecFor(%q) missing; every §13.1 phase must have a policy", phase)
		}
	}
}
