package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tcs76321/athanor/internal/artifact"
	"github.com/tcs76321/athanor/internal/config"
	"github.com/tcs76321/athanor/internal/job"
	"github.com/tcs76321/athanor/internal/store"
)

// M3 follow-up tests for the §19.3 deterministic guard
// when parameterized by config:
//
//   - `execution.min_judge_confidence = 0` (the documented
//     disabled sentinel) must disable the guard end-to-end
//     so the LLM's "new" verdict stands even with no
//     backing EvaluationRecord.
//
//   - The default threshold (0.7) must still guard:
//     LLM "new" with no backing record and a prior accepted
//     artifact is downgraded to "previous".
//
//   - A non-default threshold (0.5) is honored: a record
//     with confidence above 0.5 backs "new"; the pure
//     DecideWinner table tests cover the boundary.
//
// The three tests together pin the contract the operator
// sees in `config.example.yaml`. A regression in either
// direction — the engine overriding a deliberate 0 back
// to 0.7, or the engine ignoring a custom threshold — is
// caught here.

// TestCompare_DisabledThresholdHonorsLLMVerdict covers
// the "operator explicitly disabled the guard" path:
// cfg.Execution.MinJudgeConfidence = 0. The LLM says
// "winner: new" with no EvaluationRecord that has
// `better_than_previous == true`. The §19.3 guard is
// supposed to be off, so the verdict stands. The new
// artifact must be accepted, the job must complete, and
// the comparison audit row must show the LLM's
// `confidence` field unchanged (no synthetic downgrade
// reason was added).
func TestCompare_DisabledThresholdHonorsLLMVerdict(t *testing.T) {
	e := newEnvWithCfg(t, func(cfg *config.Config) {
		zero := 0.0
		cfg.Execution.MinJudgeConfidence = &zero
	})
	_, _, jobID := e.createProjectTask(t, "text", "disabled-threshold goal")

	// Three candidates whose EvaluationRecords say
	// "passed but not better than previous" — the LLM
	// has no record to back a "new" verdict, so the
	// §19.3 guard would normally downgrade.
	for i := 0; i < 3; i++ {
		e.ollama.evalVerdicts = append(e.ollama.evalVerdicts,
			`{"passed":true,"score":0.9,"failed_tests":[],"missing_criteria":[],"security_issues":[],"style_issues":[],"better_than_previous":false,"confidence":0.9,"summary":"passes but no improvement"}`)
	}
	e.ollama.comparisonVerdicts = []string{
		`{"winner":"new","confidence":0.99,"reasons":["LLM prefers new"],"missing_requirements":[]}`,
	}
	e.eng.Run(context.Background(), jobID)

	j, err := e.jobs.Get(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != job.StateCompleted {
		t.Fatalf("state = %s, want completed (disabled guard must not block accept)", j.State)
	}
	final, err := e.artifacts.LatestForJob(context.Background(), jobID, artifact.KindDocument)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != artifact.StatusAccepted {
		t.Errorf("final status = %s, want accepted (guard off, LLM 'new' stands)", final.Status)
	}

	// The comparison audit row must report the LLM's
	// original confidence — no synthetic downgrade
	// reason was added (DecideWinner's no-downgrade
	// path returns the verdict unchanged).
	rows, err := e.db.QueryEvents(context.Background(), store.EventFilter{JobID: jobID})
	if err != nil {
		t.Fatal(err)
	}
	var comparisonData struct {
		Event      string  `json:"event"`
		Winner     string  `json:"winner"`
		Confidence float64 `json:"confidence"`
	}
	found := false
	for _, r := range rows {
		if err := json.Unmarshal([]byte(r.DataJSON), &comparisonData); err != nil {
			continue
		}
		if comparisonData.Event == "comparison" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no comparison event in audit log")
	}
	if comparisonData.Winner != "new" {
		t.Errorf("comparison.winner = %q, want new (guard disabled)", comparisonData.Winner)
	}
	if comparisonData.Confidence != 0.99 {
		t.Errorf("comparison.confidence = %f, want 0.99 (LLM's reported value, unchanged)", comparisonData.Confidence)
	}
}

// TestCompare_DefaultThresholdStillGuards is the
// regression guard for the rescue removal: with the
// default threshold (0.7) — explicitly set, not
// inherited from defaults — and a previous accepted
// artifact, an LLM "new" verdict with no backing
// EvaluationRecord must still be downgraded to
// "previous". The new artifact must be rejected; the
// previous must stay accepted.
func TestCompare_DefaultThresholdStillGuards(t *testing.T) {
	e := newEnvWithCfg(t, func(cfg *config.Config) {
		v := 0.7
		cfg.Execution.MinJudgeConfidence = &v
	})
	projectID, taskID, _ := e.createProjectTask(t, "text", "default-threshold still guards")
	prev, err := e.artifacts.CreateDraftFor(context.Background(), projectID, taskID, "",
		artifact.KindDocument, []byte("previously accepted"))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.artifacts.SetStatus(context.Background(), prev.ID, artifact.StatusCandidate); err != nil {
		t.Fatal(err)
	}
	if err := e.artifacts.SetStatus(context.Background(), prev.ID, artifact.StatusAccepted); err != nil {
		t.Fatal(err)
	}

	newTask, err := e.projects.SubmitGoal(context.Background(), projectID,
		"new goal that competes with prior", []string{"must improve on previous"})
	if err != nil {
		t.Fatal(err)
	}
	newJob, err := e.jobs.Create(context.Background(), newTask.ID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	jobID := newJob.ID

	for i := 0; i < 3; i++ {
		e.ollama.evalVerdicts = append(e.ollama.evalVerdicts,
			`{"passed":true,"score":0.9,"failed_tests":[],"missing_criteria":[],"security_issues":[],"style_issues":[],"better_than_previous":false,"confidence":0.9,"summary":"passes but no improvement"}`)
	}
	e.ollama.comparisonVerdicts = []string{
		`{"winner":"new","confidence":0.99,"reasons":["LLM prefers new"],"missing_requirements":[]}`,
	}
	e.eng.Run(context.Background(), jobID)

	gotPrev, err := e.artifacts.Get(context.Background(), prev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotPrev.Status != artifact.StatusAccepted {
		t.Errorf("previous status = %s, want accepted (must stay accepted)", gotPrev.Status)
	}
	newFinal, err := e.artifacts.LatestForJob(context.Background(), jobID, artifact.KindDocument)
	if err != nil {
		t.Fatal(err)
	}
	if newFinal.Status != artifact.StatusRejected {
		t.Errorf("new final status = %s, want rejected (default threshold still downgrades)", newFinal.Status)
	}
}

// TestCompare_CustomThreshold proves a non-default
// threshold is honored end-to-end. With threshold = 0.5
// and an LLM "new" verdict backed by a record with
// confidence = 0.6, the verdict stands. (The pure
// DecideWinner table tests cover the boundary cases;
// this test pins the engine call site.)
func TestCompare_CustomThreshold(t *testing.T) {
	e := newEnvWithCfg(t, func(cfg *config.Config) {
		v := 0.5
		cfg.Execution.MinJudgeConfidence = &v
	})
	projectID, taskID, _ := e.createProjectTask(t, "text", "custom-threshold goal")
	prev, err := e.artifacts.CreateDraftFor(context.Background(), projectID, taskID, "",
		artifact.KindDocument, []byte("previously accepted"))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.artifacts.SetStatus(context.Background(), prev.ID, artifact.StatusCandidate); err != nil {
		t.Fatal(err)
	}
	if err := e.artifacts.SetStatus(context.Background(), prev.ID, artifact.StatusAccepted); err != nil {
		t.Fatal(err)
	}

	newTask, err := e.projects.SubmitGoal(context.Background(), projectID,
		"new goal competes with prior", []string{"must improve on previous"})
	if err != nil {
		t.Fatal(err)
	}
	newJob, err := e.jobs.Create(context.Background(), newTask.ID, projectID)
	if err != nil {
		t.Fatal(err)
	}
	jobID := newJob.ID

	// One of the three EvaluationRecords has
	// confidence=0.6 (above 0.5) and better_than_previous
	// = true, so DecideWinner's guard is satisfied.
	for i := 0; i < 3; i++ {
		if i == 0 {
			e.ollama.evalVerdicts = append(e.ollama.evalVerdicts,
				`{"passed":true,"score":0.9,"failed_tests":[],"missing_criteria":[],"security_issues":[],"style_issues":[],"better_than_previous":true,"confidence":0.6,"summary":"strong record"}`)
		} else {
			e.ollama.evalVerdicts = append(e.ollama.evalVerdicts,
				`{"passed":true,"score":0.9,"failed_tests":[],"missing_criteria":[],"security_issues":[],"style_issues":[],"better_than_previous":false,"confidence":0.9,"summary":"passes but no improvement"}`)
		}
	}
	e.ollama.comparisonVerdicts = []string{
		`{"winner":"new","confidence":0.99,"reasons":["LLM prefers new"],"missing_requirements":[]}`,
	}
	e.eng.Run(context.Background(), jobID)

	newFinal, err := e.artifacts.LatestForJob(context.Background(), jobID, artifact.KindDocument)
	if err != nil {
		t.Fatal(err)
	}
	if newFinal.Status != artifact.StatusAccepted {
		t.Errorf("new final status = %s, want accepted (record confidence 0.6 > custom threshold 0.5)", newFinal.Status)
	}
}
