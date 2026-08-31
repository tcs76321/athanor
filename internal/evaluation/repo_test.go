// Package evaluation (repo_test.go): the package is a pure data layer,
// so the tests exercise only the repo. The test environment opens a
// temp-dir store, applies migrations, and inserts the minimum parent
// rows (a project, a task, a job, an artifact) that the FK constraints
// require.

package evaluation

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tcs76321/athanor/internal/store"
	"github.com/tcs76321/athanor/migrations"
)

// openTestDB returns a fully migrated temp-dir store. The caller must
// Close it (t.Cleanup handles that).
func openTestDB(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := store.Migrate(s.DB(), migrations.FS, ""); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

// seedProjectTaskJobArtifact inserts the parent rows an evaluation
// record needs (one project, one task, one job, one artifact). It
// returns the IDs so the test can build Records.
func seedProjectTaskJobArtifact(t *testing.T, s *store.Store) (projectID, taskID, jobID, artifactID string) {
	t.Helper()
	projectID = "p-eval-1"
	taskID = "t-eval-1"
	jobID = "j-eval-1"
	artifactID = "a-eval-1"
	if _, err := s.DB().Exec(
		`INSERT INTO projects (id, name, archetype, goal) VALUES (?, 'eval test', 'text', 'goal')`,
		projectID,
	); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO tasks (id, project_id, title) VALUES (?, ?, 't')`,
		taskID, projectID,
	); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO jobs (id, task_id, project_id, state) VALUES (?, ?, ?, 'queued')`,
		jobID, taskID, projectID,
	); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO artifacts (id, project_id, kind, version, status, content_hash, storage_path)
		 VALUES (?, ?, 'proposal', 1, 'draft', 'sha256:deadbeef', '/tmp/eval-test')`,
		artifactID, projectID,
	); err != nil {
		t.Fatalf("insert artifact: %v", err)
	}
	return
}

// TestCreateListRoundTrip is the headline test: a record inserted
// through Create is returned by ListByJob in the same order, with all
// fields preserved. The contract "what you put in round-trips
// byte-for-byte" is non-negotiable per the package doc.
func TestCreateListRoundTrip(t *testing.T) {
	s := openTestDB(t)
	_, _, jobID, artID := seedProjectTaskJobArtifact(t, s)
	r := NewRepo(s)

	rec := NewRecord(jobID, artID)
	rec.Score = 0.87
	rec.PassedTests = true
	rec.FailedTests = []string{"TestFoo", "TestBar"}
	rec.MissingCriteria = []string{"docs updated"}
	rec.SecurityIssues = []string{}
	rec.StyleIssues = []string{"line too long"}
	rec.BetterThanPrevious = true
	rec.Confidence = 0.92
	rec.Summary = "candidate outscores previous accepted artifact"

	if _, err := r.Create(context.Background(), rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := r.ListByJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("ListByJob: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(got))
	}
	g := got[0]
	if g.ID != rec.ID || g.JobID != rec.JobID || g.ArtifactID != rec.ArtifactID {
		t.Errorf("ID/JobID/ArtifactID mismatch: got %s/%s/%s, want %s/%s/%s",
			g.ID, g.JobID, g.ArtifactID, rec.ID, rec.JobID, rec.ArtifactID)
	}
	if g.Score != rec.Score || g.Confidence != rec.Confidence {
		t.Errorf("Score/Confidence: got %g/%g, want %g/%g", g.Score, g.Confidence, rec.Score, rec.Confidence)
	}
	if g.PassedTests != rec.PassedTests || g.BetterThanPrevious != rec.BetterThanPrevious {
		t.Errorf("bool fields: got %v/%v, want %v/%v",
			g.PassedTests, g.BetterThanPrevious, rec.PassedTests, rec.BetterThanPrevious)
	}
	if !equalStringSlice(g.FailedTests, rec.FailedTests) {
		t.Errorf("FailedTests = %v, want %v", g.FailedTests, rec.FailedTests)
	}
	if !equalStringSlice(g.MissingCriteria, rec.MissingCriteria) {
		t.Errorf("MissingCriteria = %v, want %v", g.MissingCriteria, rec.MissingCriteria)
	}
	if !equalStringSlice(g.SecurityIssues, rec.SecurityIssues) {
		t.Errorf("SecurityIssues = %v, want %v", g.SecurityIssues, rec.SecurityIssues)
	}
	if !equalStringSlice(g.StyleIssues, rec.StyleIssues) {
		t.Errorf("StyleIssues = %v, want %v", g.StyleIssues, rec.StyleIssues)
	}
	if g.Summary != rec.Summary {
		t.Errorf("Summary = %q, want %q", g.Summary, rec.Summary)
	}
	if !g.CreatedAt.Equal(rec.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v (round-trip must preserve instant)", g.CreatedAt, rec.CreatedAt)
	}
}

// TestListByJobOrdering covers §13.1's claim that the comparison
// phase can order records deterministically. Two records inserted in
// a known order come back in the same order.
func TestListByJobOrdering(t *testing.T) {
	s := openTestDB(t)
	_, _, jobID, artID := seedProjectTaskJobArtifact(t, s)
	r := NewRepo(s)

	first := NewRecord(jobID, artID)
	first.Summary = "first"
	if _, err := r.Create(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	// Force the second record's created_at to be strictly later so
	// the (created_at ASC, id ASC) tie-breaker isn't exercised by
	// accident.
	time.Sleep(2 * time.Millisecond)
	second := NewRecord(jobID, artID)
	second.Summary = "second"
	if _, err := r.Create(context.Background(), second); err != nil {
		t.Fatal(err)
	}

	got, err := r.ListByJob(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != first.ID || got[1].ID != second.ID {
		t.Fatalf("order = [%s %s], want [first second]", got[0].Summary, got[1].Summary)
	}
}

// TestCreateEmitsAuditEvent verifies the §28.1 invariant: every
// persisted record is paired with an event in the append-only log.
func TestCreateEmitsAuditEvent(t *testing.T) {
	s := openTestDB(t)
	_, _, jobID, artID := seedProjectTaskJobArtifact(t, s)
	r := NewRepo(s)

	rec := NewRecord(jobID, artID)
	rec.Summary = "audit-event test"
	if _, err := r.Create(context.Background(), rec); err != nil {
		t.Fatal(err)
	}

	row := s.DB().QueryRow(
		`SELECT category, level, data_json FROM events WHERE job_id = ?`, jobID)
	var cat, level, data string
	if err := row.Scan(&cat, &level, &data); err != nil {
		t.Fatalf("scanning event: %v", err)
	}
	if cat != "jobs" {
		t.Errorf("event category = %q, want %q", cat, "jobs")
	}
	if level != "info" {
		t.Errorf("event level = %q, want %q", level, "info")
	}
	if !contains(data, "evaluation_record_created") || !contains(data, rec.ID) {
		t.Errorf("event data = %q, want evaluation_record_created mentioning %q", data, rec.ID)
	}
}

// TestCreateEnforcesForeignKeys exercises the §23.1 / ADR-0006
// invariant: a record cannot reference a job or artifact that does
// not exist.
func TestCreateEnforcesForeignKeys(t *testing.T) {
	s := openTestDB(t)
	r := NewRepo(s)

	rec := NewRecord("j-does-not-exist", "a-does-not-exist")
	_, err := r.Create(context.Background(), rec)
	if err == nil {
		t.Fatal("Create with missing job/artifact accepted, want FK violation")
	}
	if !contains(err.Error(), "FOREIGN KEY") {
		t.Errorf("err = %v, want FOREIGN KEY violation", err)
	}
}

// TestCreateValidatesInput covers the required-field checks that
// exist so a misuse of the API is loud rather than silent.
func TestCreateValidatesInput(t *testing.T) {
	s := openTestDB(t)
	_, _, jobID, artID := seedProjectTaskJobArtifact(t, s)
	r := NewRepo(s)

	t.Run("missing ID", func(t *testing.T) {
		rec := NewRecord(jobID, artID)
		rec.ID = ""
		if _, err := r.Create(context.Background(), rec); err == nil {
			t.Error("Create with empty ID accepted, want error")
		}
	})
	t.Run("missing JobID", func(t *testing.T) {
		rec := NewRecord("", artID)
		if _, err := r.Create(context.Background(), rec); err == nil {
			t.Error("Create with empty JobID accepted, want error")
		}
	})
	t.Run("missing ArtifactID", func(t *testing.T) {
		rec := NewRecord(jobID, "")
		if _, err := r.Create(context.Background(), rec); err == nil {
			t.Error("Create with empty ArtifactID accepted, want error")
		}
	})
	t.Run("missing CreatedAt", func(t *testing.T) {
		rec := Record{ID: "x", JobID: jobID, ArtifactID: artID}
		if _, err := r.Create(context.Background(), rec); err == nil {
			t.Error("Create with zero CreatedAt accepted, want error")
		}
	})
}

// TestComparedAgainstNullable covers the §19.2 case where the project
// has no prior accepted artifact: the column is NULL on the wire and
// empty in Go.
func TestComparedAgainstNullable(t *testing.T) {
	s := openTestDB(t)
	_, _, jobID, artID := seedProjectTaskJobArtifact(t, s)
	r := NewRepo(s)

	rec := NewRecord(jobID, artID)
	if _, err := r.Create(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	got, err := r.ListByJob(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ComparedAgainst != "" {
		t.Errorf("ComparedAgainst = %q, want empty (no previous accepted artifact)", got[0].ComparedAgainst)
	}
}

// equalStringSlice is a nil-safe slice equality check (nil != []string{}).
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// contains is a tiny strings.Contains shim so the test file has no
// extra import.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
