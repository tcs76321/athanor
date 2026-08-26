package llm

import "testing"

// TestResolveTemperature encodes the full §13.1 precedence table:
// phase policy (pin or range-clamp) over persona defaults, and untouched
// defaults for phases without a policy.
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
			if got := ResolveTemperature(tc.phase, tc.personaDefault); got != tc.want {
				t.Errorf("ResolveTemperature(%q, %v) = %v, want %v", tc.phase, tc.personaDefault, got, tc.want)
			}
		})
	}
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
