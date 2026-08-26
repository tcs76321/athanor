package llm

// Phase names (§8.1 job states; §13.1 Dialectical phases). The Dialectical
// Engine phase names double as job state names.
const (
	PhasePlanning     = "planning"
	PhaseDiverging    = "diverging"
	PhaseEvaluating   = "evaluating"
	PhaseReflecting   = "reflecting"
	PhaseSynthesizing = "synthesizing"
	PhaseComparing    = "comparing"
)

// PhaseSpec states a phase's temperature policy (§13.1). Min == Max means
// the phase pins its temperature outright; otherwise the spec is a range
// and a persona default inside the range is honored, while a default
// outside it is clamped to the nearest bound ("the phase value wins").
type PhaseSpec struct {
	Min float64
	Max float64
}

// phaseSpecs is the §13.1 table.
var phaseSpecs = map[string]PhaseSpec{
	PhasePlanning:     {Min: 0.2, Max: 0.2}, // low — deterministic planning
	PhaseDiverging:    {Min: 0.7, Max: 1.1}, // high — explore, avoid convergence
	PhaseEvaluating:   {Min: 0.0, Max: 0.0}, // zero — maximally deterministic
	PhaseReflecting:   {Min: 0.6, Max: 0.8}, // moderate-to-high
	PhaseSynthesizing: {Min: 0.2, Max: 0.2}, // low
	PhaseComparing:    {Min: 0.0, Max: 0.0}, // zero — deterministic judgment
}

// PhaseSpec returns the temperature policy for a phase. The second return
// is false for phases with no policy (the persona default then applies
// untouched) — e.g. context_building, which §13.1 does not define.
func PhaseSpecFor(phase string) (PhaseSpec, bool) {
	s, ok := phaseSpecs[phase]
	return s, ok
}

// ResolveTemperature applies §13.1 precedence: an attached ExplorationPath
// stage (M3) would override both inputs; next the phase policy wins over
// the persona default; a phase with no policy leaves the default alone.
//
// Precedence implemented here (the ExplorationPath hook arrives with M3
// and will sit above this function):
//
//	phase policy > persona default
func ResolveTemperature(phase string, personaDefault float64) float64 {
	spec, ok := phaseSpecs[phase]
	if !ok {
		return personaDefault
	}
	if personaDefault < spec.Min {
		return spec.Min
	}
	if personaDefault > spec.Max {
		return spec.Max
	}
	return personaDefault
}
