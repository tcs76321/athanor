package internalapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tcs76321/athanor/internal/store"
	"github.com/tcs76321/athanor/internal/toolenvelope"
)

// mustParseTools is a tiny test helper that fails the test if
// the named tool names are not in the closed set. Keeps the
// test bodies free of error-handling noise.
func mustParseTools(t *testing.T, names ...string) toolenvelope.Envelope {
	t.Helper()
	env, err := toolenvelope.Parse(names)
	if err != nil {
		t.Fatalf("toolenvelope.Parse(%v): %v", names, err)
	}
	return env
}

// TestExecuteCode_RejectsWithoutToken proves the auth middleware
// still wraps the new route (Gate G2's structural check has a
// matching behavioral test).
func TestExecuteCode_RejectsWithoutToken(t *testing.T) {
	env := newHandlerTestEnv(t)
	_, taskID := env.seedProject(t)

	req := httptest.NewRequest("POST", "/internal/v1/jobs/"+taskID+"/execute_code",
		bytes.NewReader([]byte(`{"code":"print(1)"}`)))
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (no bearer)", w.Code)
	}
}

// TestExecuteCode_RejectsWhenToolNotInEnvelope is the M2-T4
// behavioral proof that the per-job allowlist is enforced: a
// request for a tool the envelope does not include gets 403 and
// an EventLog entry of category "jobs" with event
// "tool_disallowed".
func TestExecuteCode_RejectsWhenToolNotInEnvelope(t *testing.T) {
	env := newHandlerTestEnv(t)
	_, taskID := env.seedProject(t)
	env.tokens.WithToken(taskID, goodToken)
	// Default envelope is empty, so execute_code is disallowed.

	body, _ := json.Marshal(executeCodeRequest{Language: "python", Code: "print(1)"})
	req := httptest.NewRequest("POST", "/internal/v1/jobs/"+taskID+"/execute_code", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+goodToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(ErrToolDisallowed.Error())) {
		t.Errorf("body = %q, want it to contain ErrToolDisallowed text", w.Body.String())
	}
	events, err := env.store.QueryEvents(context.Background(), store.EventFilter{JobID: taskID})
	if err != nil {
		t.Fatal(err)
	}
	rejections := 0
	for _, e := range events {
		var d struct {
			Event string `json:"event"`
			Tool  string `json:"tool"`
		}
		_ = json.Unmarshal([]byte(e.DataJSON), &d)
		if d.Event == "tool_disallowed" && d.Tool == string(toolenvelope.ToolExecuteCode) {
			rejections++
		}
	}
	if rejections != 1 {
		t.Errorf("tool_disallowed events = %d, want 1", rejections)
	}
}

// TestExecuteCode_PassesEnvelopeCheck_Returns501 proves the
// happy path's structural pieces: auth + envelope check both
// pass, and the handler reaches its 501 "not yet implemented"
// placeholder. Commit 4 replaces the 501 with the real runner
// dispatch; the structural pieces (auth, envelope, audit
// tool_call) stay.
func TestExecuteCode_PassesEnvelopeCheck_Returns501(t *testing.T) {
	env := newHandlerTestEnv(t)
	_, taskID := env.seedProject(t)
	env.tokens.WithToken(taskID, goodToken)
	env.tools.WithAllow(taskID, mustParseTools(t, "execute_code"))

	body, _ := json.Marshal(executeCodeRequest{Language: "python", Code: "print(1)"})
	req := httptest.NewRequest("POST", "/internal/v1/jobs/"+taskID+"/execute_code", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+goodToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body = %s", w.Code, w.Body.String())
	}
	events, _ := env.store.QueryEvents(context.Background(), store.EventFilter{JobID: taskID})
	calls := 0
	for _, e := range events {
		var d struct {
			Event string `json:"event"`
		}
		_ = json.Unmarshal([]byte(e.DataJSON), &d)
		if d.Event == "tool_call" {
			calls++
		}
	}
	if calls != 1 {
		t.Errorf("tool_call events = %d, want 1", calls)
	}
}

// TestExecuteCode_RejectsMissingCode covers the 400 path: the
// body is well-formed JSON but lacks the required Code field.
func TestExecuteCode_RejectsMissingCode(t *testing.T) {
	env := newHandlerTestEnv(t)
	_, taskID := env.seedProject(t)
	env.tokens.WithToken(taskID, goodToken)
	env.tools.WithAllow(taskID, mustParseTools(t, "execute_code"))

	req := httptest.NewRequest("POST", "/internal/v1/jobs/"+taskID+"/execute_code",
		bytes.NewReader([]byte(`{"language":"python"}`)))
	req.Header.Set("Authorization", "Bearer "+goodToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing code)", w.Code)
	}
}

// TestExecuteCode_RejectsUnknownLanguage covers the closed-set
// check on Language. M2-T4 accepts only "python".
func TestExecuteCode_RejectsUnknownLanguage(t *testing.T) {
	env := newHandlerTestEnv(t)
	_, taskID := env.seedProject(t)
	env.tokens.WithToken(taskID, goodToken)
	env.tools.WithAllow(taskID, mustParseTools(t, "execute_code"))

	body, _ := json.Marshal(executeCodeRequest{Language: "ruby", Code: "puts 1"})
	req := httptest.NewRequest("POST", "/internal/v1/jobs/"+taskID+"/execute_code", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+goodToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (language not in closed set)", w.Code)
	}
}

// TestRunTests_RejectsWhenToolNotInEnvelope is the run_tests
// counterpart of TestExecuteCode_RejectsWhenToolNotInEnvelope.
// execute_code in the envelope does not grant run_tests.
func TestRunTests_RejectsWhenToolNotInEnvelope(t *testing.T) {
	env := newHandlerTestEnv(t)
	_, taskID := env.seedProject(t)
	env.tokens.WithToken(taskID, goodToken)
	env.tools.WithAllow(taskID, mustParseTools(t, "execute_code"))

	body, _ := json.Marshal(runTestsRequest{Command: "pytest -q"})
	req := httptest.NewRequest("POST", "/internal/v1/jobs/"+taskID+"/run_tests", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+goodToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
}

// TestRunTests_PassesEnvelopeCheck_Returns501 mirrors
// TestExecuteCode_PassesEnvelopeCheck_Returns501 for run_tests.
func TestRunTests_PassesEnvelopeCheck_Returns501(t *testing.T) {
	env := newHandlerTestEnv(t)
	_, taskID := env.seedProject(t)
	env.tokens.WithToken(taskID, goodToken)
	env.tools.WithAllow(taskID, mustParseTools(t, "run_tests"))

	body, _ := json.Marshal(runTestsRequest{Command: "pytest -q"})
	req := httptest.NewRequest("POST", "/internal/v1/jobs/"+taskID+"/run_tests", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+goodToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body = %s", w.Code, w.Body.String())
	}
}

// TestRunTests_RejectsMissingCommand is the run_tests counterpart
// of TestExecuteCode_RejectsMissingCode.
func TestRunTests_RejectsMissingCommand(t *testing.T) {
	env := newHandlerTestEnv(t)
	_, taskID := env.seedProject(t)
	env.tokens.WithToken(taskID, goodToken)
	env.tools.WithAllow(taskID, mustParseTools(t, "run_tests"))

	req := httptest.NewRequest("POST", "/internal/v1/jobs/"+taskID+"/run_tests",
		bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+goodToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing command)", w.Code)
	}
}
