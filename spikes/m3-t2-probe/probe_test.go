// Package main is the M3-T2 (commit 2.6) per-task probe.
// Pure aggregation functions are unit-tested here; the
// HTTP plumbing is exercised end-to-end when the probe runs.
package main

import (
	"strings"
	"testing"
)

// TestParseEvaluationRecords covers the audit-row extraction:
// only `evaluation_record_created` rows are kept; every
// other event is ignored.
func TestParseEvaluationRecords(t *testing.T) {
	events := []transitionEvent{
		{DataJSON: `{"event":"transition","to":"evaluating"}`},
		{DataJSON: `{"event":"evaluation_record_created","record_id":"r1","artifact_id":"a1","score":0.9,"passed_tests":true,"better_than_previous":true,"confidence":0.9,"missing_criteria":[],"security_issues":[],"style_issues":["trailing whitespace"]}`},
		{DataJSON: `{"event":"comparison","winner":"new"}`},
		{DataJSON: `{"event":"evaluation_record_created","record_id":"r2","artifact_id":"a2","score":0.5,"passed_tests":false,"better_than_previous":false,"confidence":0.6,"missing_criteria":["docstrings"],"security_issues":[],"style_issues":[]}`},
		{DataJSON: `not json`},
	}
	got := parseEvaluationRecords(events)
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	if got[0].RecordID != "r1" {
		t.Errorf("got[0].RecordID = %q, want r1", got[0].RecordID)
	}
	if len(got[0].StyleIssues) != 1 || got[0].StyleIssues[0] != "trailing whitespace" {
		t.Errorf("got[0].StyleIssues = %v, want [trailing whitespace]", got[0].StyleIssues)
	}
	if got[1].RecordID != "r2" {
		t.Errorf("got[1].RecordID = %q, want r2", got[1].RecordID)
	}
	if len(got[1].MissingCriteria) != 1 || got[1].MissingCriteria[0] != "docstrings" {
		t.Errorf("got[1].MissingCriteria = %v, want [docstrings]", got[1].MissingCriteria)
	}
}

// TestParseEvaluationRecords_EmptyOnNoEvents asserts the
// function is well-behaved on the empty input and on inputs
// with no `evaluation_record_created` rows.
func TestParseEvaluationRecords_EmptyOnNoEvents(t *testing.T) {
	if got := parseEvaluationRecords(nil); len(got) != 0 {
		t.Errorf("nil events: got %d records, want 0", len(got))
	}
	events := []transitionEvent{
		{DataJSON: `{"event":"transition","to":"evaluating"}`},
		{DataJSON: `{"event":"comparison","winner":"new"}`},
	}
	if got := parseEvaluationRecords(events); len(got) != 0 {
		t.Errorf("non-matching events: got %d records, want 0", len(got))
	}
}

// TestComputeRubricCoverage covers the union / dedup /
// sort + the Has* flags.
func TestComputeRubricCoverage(t *testing.T) {
	records := []evaluationRecordAudit{
		{
			PassedTests:        true,
			BetterThanPrevious: true,
			MissingCriteria:    []string{"docstrings", "no TODO"},
			StyleIssues:        []string{"trailing whitespace"},
		},
		{
			PassedTests:        false,
			BetterThanPrevious: false,
			MissingCriteria:    []string{"docstrings", "tests pass"},
			SecurityIssues:     []string{"insecure deserialization"},
		},
	}
	cov := computeRubricCoverage(records)
	if cov.Total != 2 {
		t.Errorf("Total = %d, want 2", cov.Total)
	}
	if cov.Passed != 1 {
		t.Errorf("Passed = %d, want 1", cov.Passed)
	}
	if cov.Better != 1 {
		t.Errorf("Better = %d, want 1", cov.Better)
	}
	if !cov.HasMissing || !cov.HasSecurity || !cov.HasStyle {
		t.Errorf("Has* flags = %+v, want all true", cov)
	}
	wantMissing := []string{"docstrings", "no TODO", "tests pass"}
	if strings.Join(cov.MissingCriteria, ",") != strings.Join(wantMissing, ",") {
		t.Errorf("MissingCriteria = %v, want %v", cov.MissingCriteria, wantMissing)
	}
	wantSecurity := []string{"insecure deserialization"}
	if strings.Join(cov.SecurityIssues, ",") != strings.Join(wantSecurity, ",") {
		t.Errorf("SecurityIssues = %v, want %v", cov.SecurityIssues, wantSecurity)
	}
	wantStyle := []string{"trailing whitespace"}
	if strings.Join(cov.StyleIssues, ",") != strings.Join(wantStyle, ",") {
		t.Errorf("StyleIssues = %v, want %v", cov.StyleIssues, wantStyle)
	}
}

// TestComputeRubricCoverage_AllCleanBaseline is the §19
// contract: when every record is clean, the union arrays
// are empty and the Has* flags are false. This is the
// "rubric fired correctly on a clean candidate" assertion.
func TestComputeRubricCoverage_AllCleanBaseline(t *testing.T) {
	records := []evaluationRecordAudit{
		{PassedTests: true, BetterThanPrevious: true},
		{PassedTests: true, BetterThanPrevious: true},
	}
	cov := computeRubricCoverage(records)
	if cov.Total != 2 {
		t.Errorf("Total = %d, want 2", cov.Total)
	}
	if cov.HasMissing || cov.HasSecurity || cov.HasStyle {
		t.Errorf("Has* flags = %+v, want all false on clean baseline", cov)
	}
	if len(cov.MissingCriteria) != 0 || len(cov.SecurityIssues) != 0 || len(cov.StyleIssues) != 0 {
		t.Errorf("expected empty union arrays on clean baseline, got %+v", cov)
	}
}

// TestComputeRubricCoverage_EmptyNoRecords asserts the
// zero-case: no records, all zero fields, all flags false.
func TestComputeRubricCoverage_EmptyNoRecords(t *testing.T) {
	cov := computeRubricCoverage(nil)
	if cov.Total != 0 || cov.Passed != 0 || cov.Better != 0 {
		t.Errorf("expected zero counts, got %+v", cov)
	}
	if cov.HasMissing || cov.HasSecurity || cov.HasStyle {
		t.Errorf("expected no Has* flags, got %+v", cov)
	}
}

// TestRenderMarkdownRow covers the row layout. The em-dash
// placeholder for empty union arrays is part of the table
// contract; downstream markdown consumers should not see
// empty table cells.
func TestRenderMarkdownRow(t *testing.T) {
	r := Result{
		Number:          1,
		Goal:            "Write a Python function that returns the n-th Fibonacci number using recursion.",
		Criteria:        []string{"pure stdlib", "docstrings on every public function"},
		JobID:           "abc123def456",
		JobState:        "completed",
		RecordsTotal:    3,
		RecordsPassed:   3,
		RecordsFailed:   0,
		BetterCount:     3,
		MissingCriteria: []string{"docstrings"},
		SecurityIssues:  []string{},
		StyleIssues:     []string{},
		Notes:           "all clean baseline",
	}
	got := renderMarkdownRow(r)
	// em-dash substitution: empty arrays render as "—".
	if !strings.Contains(got, "—") {
		t.Errorf("renderMarkdownRow should render empty arrays as em-dash; got %q", got)
	}
	// non-empty arrays render verbatim.
	if !strings.Contains(got, "docstrings") {
		t.Errorf("renderMarkdownRow should include the missing_criteria; got %q", got)
	}
	// field order: number, goal, criteria, job_id, state, ...
	parts := strings.Split(got, " | ")
	if len(parts) != 13 {
		t.Errorf("renderMarkdownRow parts = %d, want 13", len(parts))
	}
	if parts[0] != "1" {
		t.Errorf("parts[0] = %q, want 1", parts[0])
	}
}

// TestTrunc / TestItoa are tiny string-helper unit tests.
func TestTrunc(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "he..."},
		{"hello", 2, "he"},
		{"", 5, ""},
	}
	for _, c := range cases {
		if got := trunc(c.in, c.n); got != c.want {
			t.Errorf("trunc(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestItoa(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{100, "100"},
	}
	for _, c := range cases {
		if got := itoa(c.in); got != c.want {
			t.Errorf("itoa(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
