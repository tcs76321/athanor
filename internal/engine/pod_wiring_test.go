package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tcs76321/athanor/internal/job"
	"github.com/tcs76321/athanor/internal/toolenvelope"
)

// fakeRunner is a ToolRunner impl for tests. It records every
// call and returns canned output. The default canned output
// represents a successful program run; tests that want
// different behavior override it via the WithExit /
// WithError / WithDisallow helpers.
//
// fakeRunner is safe for concurrent use: tests can run jobs
// in parallel and assert on the slice of calls.
type fakeRunner struct {
	mu       sync.Mutex
	calls    []fakeCall
	exitCode int
	duration time.Duration
	withErr  error
	disallow map[string]bool
}

type fakeCall struct {
	Tool  string
	JobID string
	Lang  string
	Code  string
	Cmd   string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		exitCode: 0,
		duration: 10 * time.Millisecond,
		disallow: map[string]bool{},
	}
}

func (f *fakeRunner) WithDisallow(tool toolenvelope.Tool) *fakeRunner {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disallow[string(tool)] = true
	return f
}

func (f *fakeRunner) WithExit(code int) *fakeRunner {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exitCode = code
	return f
}

func (f *fakeRunner) WithError(err error) *fakeRunner {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.withErr = err
	return f
}

func (f *fakeRunner) Calls() []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeRunner) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeRunner) RunCode(ctx context.Context, jobID string, req toolenvelope.ExecuteRequest) (toolenvelope.ExecuteResult, error) {
	return f.run(ctx, jobID, req, "RunCode")
}

func (f *fakeRunner) RunTests(ctx context.Context, jobID string, req toolenvelope.ExecuteRequest) (toolenvelope.ExecuteResult, error) {
	return f.run(ctx, jobID, req, "RunTests")
}

func (f *fakeRunner) run(_ context.Context, jobID string, req toolenvelope.ExecuteRequest, kind string) (toolenvelope.ExecuteResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{
		Tool:  kind,
		JobID: jobID,
		Lang:  req.Language,
		Code:  req.Code,
		Cmd:   req.Command,
	})
	if f.withErr != nil {
		return toolenvelope.ExecuteResult{}, f.withErr
	}
	if f.disallow[kind] {
		return toolenvelope.ExecuteResult{}, toolenvelope.ErrToolDisallowed
	}
	return toolenvelope.ExecuteResult{
		ExitCode:   f.exitCode,
		Stdout:     "fake stdout",
		Stderr:     "",
		DurationMS: f.duration.Milliseconds(),
	}, nil
}

// TestRun_CodeArchetypeCallsRunner is the M2-T4 behavioral
// proof that the engine calls the ToolRunner for code-archetype
// jobs. The text-archetype test (TestRunCompletesFullChain)
// asserts the inverse: 3 LLM calls, 0 runner calls.
//
// The fake runner is wired into the engine, so any HTTP-shaped
// call the engine makes lands in the runner's call log. After
// Run completes, the log has exactly one RunCode call (the
// divergence proposal is executed in the pod) and one RunTests
// call ("pytest -q"). Both are recorded as EventLog entries.
func TestRun_CodeArchetypeCallsRunner(t *testing.T) {
	env := newEnv(t)
	codeJobID := env.submitCode(t)

	env.eng.Run(context.Background(), codeJobID)

	if got := env.runner.CallCount(); got != 2 {
		t.Errorf("runner calls = %d, want 2 (RunCode + RunTests)", got)
	}
	calls := env.runner.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	byKind := map[string]fakeCall{}
	for _, c := range calls {
		byKind[c.Tool] = c
	}
	if _, ok := byKind["RunCode"]; !ok {
		t.Error("no RunCode call in runner log")
	}
	if _, ok := byKind["RunTests"]; !ok {
		t.Error("no RunTests call in runner log")
	}
	if rc := byKind["RunCode"]; rc.Lang != "python" {
		t.Errorf("RunCode language = %q, want python", rc.Lang)
	}
	if rt := byKind["RunTests"]; rt.Cmd != "pytest -q" {
		t.Errorf("RunTests command = %q, want pytest -q", rt.Cmd)
	}
}

// TestRun_TextArchetypeDoesNotCallRunner is the inverse: text
// archetype skips the runner sub-steps entirely. The M1 walking
// skeleton still produces 3 LLM calls and 0 runner calls.
func TestRun_TextArchetypeDoesNotCallRunner(t *testing.T) {
	env := newEnv(t)
	jobID := env.submit(t)
	env.eng.Run(context.Background(), jobID)
	if got := env.runner.CallCount(); got != 0 {
		t.Errorf("runner calls = %d, want 0 for text archetype", got)
	}
}

// TestRun_ToolDisallowedSoftFails asserts the soft-fail
// behavior: when the runner returns toolenvelope.ErrToolDisallowed,
// the engine continues to comparing and the job completes.
// M3-T2 will turn this into a HITL escalation.
func TestRun_ToolDisallowedSoftFails(t *testing.T) {
	env := newEnv(t)
	jobID := env.submitCode(t)
	env.runner.WithDisallow(toolenvelope.ToolExecuteCode)
	env.eng.Run(context.Background(), jobID)

	j, err := env.jobs.Get(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != job.StateCompleted {
		t.Errorf("state = %s, want completed (disallow is a soft-fail)", j.State)
	}
	if env.runner.CallCount() < 1 {
		t.Errorf("runner calls = %d, want >= 1 (at least RunCode before the disallow)", env.runner.CallCount())
	}
}
