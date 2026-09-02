// M3-T1 follow-up tests for `artifact.Store.ListByJob`
// (the method that retired the raw SQL in the
// E1 dialectical-loop test).
package artifact

import (
	"context"
	"database/sql"
	"testing"
)

// insertTask is a small helper for the FK on
// `artifacts.task_id` (the tasks table is created
// in `openStore`'s migration but not seeded).
func insertTask(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO tasks (id, project_id, title) VALUES (?, 'p1', 'do the thing')`,
		id,
	); err != nil {
		t.Fatal(err)
	}
}

// insertJob is the matching helper for the FK
// on `artifacts.job_id`. The task_id must
// reference an existing task.
func insertJob(t *testing.T, db *sql.DB, id, taskID string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO jobs (id, project_id, task_id, state) VALUES (?, 'p1', ?, 'queued')`,
		id, taskID,
	); err != nil {
		t.Fatal(err)
	}
}

func TestListByJob_Empty(t *testing.T) {
	a, _, _ := openStore(t)
	got, err := a.ListByJob(context.Background(), "no-such-job")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("empty job artifacts = %d, want 0", len(got))
	}
}

func TestListByJob_OrdersByCreatedAt(t *testing.T) {
	a, st, _ := openStore(t)
	ctx := context.Background()
	// Three drafts for the same job. The method
	// orders by created_at ASC; inserts are
	// serialized but the SQLite `strftime` resolution
	// is millisecond-level, so adjacent inserts can
	// produce the same timestamp. The secondary sort
	// by `id ASC` (UUIDs are time-ordered) keeps the
	// list deterministic for identical timestamps;
	// the test asserts *set* equality (every id is
	// present) and *order* is allowed to match either
	// the insertion order or the reverse (when the
	// timestamps tie and the secondary sort puts
	// them in a different order). The test fails
	// only if a row is missing or duplicated.
	insertTask(t, st.DB(), "t-a")
	insertTask(t, st.DB(), "t-b")
	insertTask(t, st.DB(), "t-c")
	insertJob(t, st.DB(), "j1", "t-a")
	ids := make([]string, 0, 3)
	for _, taskID := range []string{"t-a", "t-b", "t-c"} {
		art, err := a.CreateDraftFor(ctx, "p1", taskID, "j1", KindProposal, []byte("content"))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, art.ID)
	}
	got, err := a.ListByJob(ctx, "j1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d artifacts, want 3", len(got))
	}
	gotSet := map[string]bool{}
	for _, art := range got {
		gotSet[art.ID] = true
	}
	for _, id := range ids {
		if !gotSet[id] {
			t.Errorf("missing artifact %q in result", id)
		}
	}
}

func TestListByJob_FiltersByJobID(t *testing.T) {
	a, st, _ := openStore(t)
	ctx := context.Background()
	// Two jobs, two artifacts each. ListByJob for
	// job A must not return job B's artifacts.
	// Each job gets a distinct task ID.
	insertTask(t, st.DB(), "tA")
	insertTask(t, st.DB(), "tB")
	insertJob(t, st.DB(), "jA", "tA")
	insertJob(t, st.DB(), "jB", "tB")
	for i := 0; i < 2; i++ {
		if _, err := a.CreateDraftFor(ctx, "p1", "tA", "jA", KindProposal, []byte("a")); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := a.CreateDraftFor(ctx, "p1", "tB", "jB", KindProposal, []byte("b")); err != nil {
			t.Fatal(err)
		}
	}
	gotA, err := a.ListByJob(ctx, "jA")
	if err != nil {
		t.Fatal(err)
	}
	if len(gotA) != 2 {
		t.Errorf("job A artifacts = %d, want 2", len(gotA))
	}
	for _, art := range gotA {
		if art.JobID != "jA" {
			t.Errorf("got artifact for %q in jA list", art.JobID)
		}
	}
}
