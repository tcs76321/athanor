package toolenvelope

// ExecuteRequest is the wire shape the engine sends to the Job Pod
// for an execute_code or run_tests call. The shape is shared
// between the engine (which constructs it) and the internal API
// runner (which serializes it to JSON) so the two stay in lockstep
// without an import cycle on internal/internalapi.
//
// The fields cover the M2-T4 surface. M3-T2 may extend with
// environment variables, working-directory overrides, or a richer
// language enum; new fields must remain backward-compatible (the
// runner ignores unknown fields today).
type ExecuteRequest struct {
	// Tool names the closed-set tool ("execute_code" or "run_tests").
	// The internal API also checks EnvelopeFor(jobID) before
	// dispatching; the Tool field is a defense-in-depth double-check.
	Tool Tool `json:"tool"`
	// Language is "python" for execute_code in M2-T4; ignored by
	// run_tests. The closed set is enforced in the handler.
	Language string `json:"language,omitempty"`
	// Code is the source to execute. Ignored by run_tests.
	Code string `json:"code,omitempty"`
	// Command is the test command line. Ignored by execute_code.
	// Example: "pytest -q".
	Command string `json:"command,omitempty"`
	// TimeoutSeconds caps the wall time inside the pod. The runner
	// enforces it via context.WithTimeout. Zero means "use the
	// per-phase budget default."
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// ExecuteResult is the wire shape the Job Pod returns. ExitCode is
// the program's exit status; -1 means the pod itself failed before
// the program ran (image pull error, language missing, etc.).
//
// DurationMS is wall-clock milliseconds from the runner's
// perspective; it includes pod-internal startup that the program's
// own timing would not.
type ExecuteResult struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMS int64  `json:"duration_ms"`
}
