package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tcs76321/athanor/internal/artifact"
	"github.com/tcs76321/athanor/internal/job"
	"github.com/tcs76321/athanor/internal/store"
)

// fakeOllama answers every chat request with a canned completion.
func fakeOllama(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			_, _ = w.Write([]byte(`{"version":"fake"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message":           map[string]string{"role": "assistant", "content": "A thoughtful result."},
			"done":              true,
			"prompt_eval_count": 100,
			"eval_count":        20,
		})
	}))
	t.Cleanup(ts.Close)
	return ts
}

// createProject submits the canonical first project.
func (h *harness) createProject(t *testing.T) (projectID, taskID string) {
	t.Helper()
	resp, err := http.Post(h.ts.URL+"/projects", "application/json", strings.NewReader(`{
		"name": "my-notes",
		"archetype": "text",
		"goal": "Write a short essay about why local-first software matters.",
		"acceptance_criteria": ["at least three arguments", "a conclusion"]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /projects status = %d, want 201", resp.StatusCode)
	}
	var out projectResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.ID, out.TaskID
}

func (h *harness) submitGoal(t *testing.T, projectID string) string {
	t.Helper()
	resp, err := http.Post(h.ts.URL+"/projects/"+projectID+"/goals", "application/json",
		strings.NewReader(`{"goal": "Summarize the essay in exactly five bullet points."}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /goals status = %d, want 201", resp.StatusCode)
	}
	var out goalResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.JobID
}

// TestEndToEndWalkingSkeleton is the M1 acceptance: a goal goes in, a
// draft artifact comes out, and every phase is audited. This is the same
// path the Gate G1 demo script (docs/demo-m1.md) drives.
func TestEndToEndWalkingSkeleton(t *testing.T) {
	h := newHarness(t)
	projectID, _ := h.createProject(t)
	jobID := h.submitGoal(t, projectID)

	j := h.waitTerminal(t, jobID)
	if j.State != job.StateCompleted {
		t.Fatalf("final job state = %s, want completed", j.State)
	}
	if j.StartedAt == nil || j.FinishedAt == nil {
		t.Error("started_at/finished_at not recorded")
	}

	// The proposal (divergence candidate) and the final document both
	// exist as drafts with verified content.
	ctx := context.Background()
	proposal, err := h.artifacts.LatestForJob(ctx, jobID, artifact.KindProposal)
	if err != nil {
		t.Fatalf("no proposal artifact: %v", err)
	}
	if proposal.Status != artifact.StatusDraft {
		t.Errorf("proposal status = %s, want draft (M1 artifacts stay drafts)", proposal.Status)
	}
	final, err := h.artifacts.LatestForJob(ctx, jobID, artifact.KindDocument)
	if err != nil {
		t.Fatalf("no final artifact: %v", err)
	}
	content, err := h.artifacts.ReadContent(ctx, final.ID)
	if err != nil {
		t.Fatalf("reading final artifact: %v", err)
	}
	if !strings.Contains(string(content), "A thoughtful result.") {
		t.Errorf("final artifact content = %q, want the model's synthesis", string(content))
	}

	// The full phase chain is audited in order.
	events, err := h.st.QueryEvents(ctx, store.EventFilter{JobID: jobID, Category: "jobs"})
	if err != nil {
		t.Fatal(err)
	}
	var sequence []string
	for _, e := range events {
		var d struct {
			Event string `json:"event"`
			To    string `json:"to"`
		}
		_ = json.Unmarshal([]byte(e.DataJSON), &d)
		if d.Event == "transition" {
			sequence = append(sequence, d.To)
		}
	}
	want := []string{"context_building", "planning", "diverging", "evaluating", "synthesizing", "comparing", "completed"}
	if strings.Join(sequence, ",") != strings.Join(want, ",") {
		t.Errorf("transition sequence = %v, want %v", sequence, want)
	}

	// GET /jobs/{id} exposes the completed job with its artifact.
	resp, err := http.Get(h.ts.URL + "/jobs/" + jobID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["state"] != "completed" || body["artifact_id"] != final.ID {
		t.Errorf("GET /jobs = %v, want completed with artifact %s", body, final.ID)
	}
}

// TestFrozenDaemonRejectsNewWork proves M1-T6 enforcement end-to-end
// (§22.1: frozen means no new work).
func TestFrozenDaemonRejectsNewWork(t *testing.T) {
	h := newHarness(t)
	projectID, _ := h.createProject(t)

	if err := h.freezer.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(h.ts.URL+"/projects/"+projectID+"/goals", "application/json",
		strings.NewReader(`{"goal": "This goal should be rejected while frozen."}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("goal submission while frozen = %d, want 409", resp.StatusCode)
	}

	// No job was created.
	active, err := h.jobs.Active(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Errorf("frozen daemon created %d jobs, want 0", len(active))
	}

	// Unfreeze with a reason re-enables submission.
	if err := h.freezer.Unfreeze(context.Background(), "test complete"); err != nil {
		t.Fatal(err)
	}
	jobID := h.submitGoal(t, projectID)
	if j := h.waitTerminal(t, jobID); j.State != job.StateCompleted {
		t.Errorf("job after unfreeze = %s, want completed", j.State)
	}
}
