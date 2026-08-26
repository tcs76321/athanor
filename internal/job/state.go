// Package job implements the job state machine (ARCHITECTURE §8) and its
// SQLite-backed repository.
//
// The M1 edge set follows ADR-0001: evaluating/reflecting edges arrive
// with Job Pods in M3, awaiting_approval with HITL in M6. The schema
// (migration 0004) accepts the full §8.1 state set; this package decides
// which transitions are legal *now*.
package job

import "fmt"

// State is one §8.1 job state.
type State string

// The §8.1 states.
const (
	StateQueued           State = "queued"
	StateContextBuilding  State = "context_building"
	StatePlanning         State = "planning"
	StateDiverging        State = "diverging"
	StateEvaluating       State = "evaluating" // reachable from M3 (ADR-0001)
	StateReflecting       State = "reflecting" // reachable from M3
	StateSynthesizing     State = "synthesizing"
	StateComparing        State = "comparing"
	StateAwaitingApproval State = "awaiting_approval" // reachable from M6
	StatePaused           State = "paused"
	StateCompleted        State = "completed"
	StateFailed           State = "failed"
	StateCancelled        State = "cancelled"
)

// Terminal reports whether no further transitions may leave the state.
func (s State) Terminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCancelled:
		return true
	default:
		return false
	}
}

// Valid reports whether s is one of the §8.1 states.
func (s State) Valid() bool {
	switch s {
	case StateQueued, StateContextBuilding, StatePlanning, StateDiverging,
		StateEvaluating, StateReflecting, StateSynthesizing, StateComparing,
		StateAwaitingApproval, StatePaused, StateCompleted, StateFailed, StateCancelled:
		return true
	default:
		return false
	}
}

// transitions is the M1 legal-edge table (§8.2 + ADR-0001). Pausing is
// legal from every active working state; a paused job resumes to the
// state recorded in paused_from (enforced by the repository, since the
// table cannot express data-dependent edges). queued jobs cannot pause —
// they simply never start.
var transitions = map[State][]State{
	StateQueued:          {StateContextBuilding, StateCancelled},
	StateContextBuilding: {StatePlanning, StatePaused, StateFailed, StateCancelled},
	StatePlanning:        {StateDiverging, StatePaused, StateFailed, StateCancelled},
	StateDiverging:       {StateSynthesizing, StatePaused, StateFailed, StateCancelled}, // → evaluating arrives in M3
	StateSynthesizing:    {StateComparing, StatePaused, StateFailed, StateCancelled},
	StateComparing:       {StateCompleted, StateFailed, StatePaused, StateCancelled},
	StatePaused:          {StateContextBuilding, StatePlanning, StateDiverging, StateSynthesizing, StateCancelled},
	// Terminal states have no outgoing edges by construction (absent map
	// entries). Evaluating/reflecting/awaiting_approval edges arrive with
	// M3/M6 per ADR-0001.
}

// CanTransition reports whether from → to is a legal M1 edge. The paused
// resume edge additionally requires to == paused_from; that rule is
// enforced by the repository because it depends on stored data.
func CanTransition(from, to State) bool {
	if !from.Valid() || !to.Valid() || from == to {
		return false
	}
	for _, candidate := range transitions[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

// IllegalTransitionError names both states (M1-T4 acceptance: illegal
// transitions rejected with a specific error).
type IllegalTransitionError struct {
	From, To State
}

func (e *IllegalTransitionError) Error() string {
	return fmt.Sprintf("illegal job transition %s → %s", e.From, e.To)
}

// ValidateTransition returns a typed error when from → to is illegal.
func ValidateTransition(from, to State) error {
	if !from.Valid() {
		return fmt.Errorf("unknown job state %q", string(from))
	}
	if !to.Valid() {
		return fmt.Errorf("unknown job state %q", string(to))
	}
	if !CanTransition(from, to) {
		return &IllegalTransitionError{From: from, To: to}
	}
	return nil
}
