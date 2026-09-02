// M3-T3 commit 3.2: SupersedeAndAccept is the §9.3 atomic
// supersede+accept transition. Tests cover the happy path
// and the two CAS failure modes (stale previous, stale
// new).
package artifact

import (
	"context"
	"strings"
	"testing"

	"github.com/tcs76321/athanor/internal/store"
)

// TestSupersedeAndAccept_HappyPath: previous is `accepted`,
// new is `candidate`. After the call: previous is
// `superseded`, new is `accepted`, and a single audit row
// is appended for each.
func TestSupersedeAndAccept_HappyPath(t *testing.T) {
	a, s, _ := openStore(t)
	ctx := context.Background()

	// Two drafts → candidate → previous becomes accepted
	// (the first), new becomes candidate.
	prev, err := a.CreateDraft(ctx, "p1", KindDocument, []byte("previous content"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetStatus(ctx, prev.ID, StatusCandidate); err != nil {
		t.Fatal(err)
	}
	if err := a.SetStatus(ctx, prev.ID, StatusAccepted); err != nil {
		t.Fatal(err)
	}
	new, err := a.CreateDraft(ctx, "p1", KindDocument, []byte("new content"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetStatus(ctx, new.ID, StatusCandidate); err != nil {
		t.Fatal(err)
	}

	// Atomic swap.
	if err := a.SupersedeAndAccept(ctx, prev.ID, new.ID); err != nil {
		t.Fatalf("SupersedeAndAccept: %v", err)
	}

	// Post-state: previous = superseded, new = accepted.
	gotPrev, err := a.Get(ctx, prev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotPrev.Status != StatusSuperseded {
		t.Errorf("prev status = %s, want superseded", gotPrev.Status)
	}
	gotNew, err := a.Get(ctx, new.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotNew.Status != StatusAccepted {
		t.Errorf("new status = %s, want accepted", gotNew.Status)
	}

	// Two audit rows from the swap itself: prev accepted →
	// superseded, and new candidate → accepted. The test
	// setup also produces status events, so we filter for
	// the swap's unique from/to value (`accepted` →
	// `superseded`): the setup never produces this
	// transition, so the count is exact.
	events, err := s.QueryEvents(ctx, store.EventFilter{ProjectID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	supersedeEvents := 0
	acceptEvents := 0
	for _, e := range events {
		if !strings.Contains(e.DataJSON, `"event":"status"`) {
			continue
		}
		if strings.Contains(e.DataJSON, `"from":"accepted","to":"superseded"`) {
			supersedeEvents++
		}
		if strings.Contains(e.DataJSON, `"from":"candidate","to":"accepted"`) {
			acceptEvents++
		}
	}
	if supersedeEvents != 1 {
		t.Errorf("audit accepted→superseded events = %d, want 1", supersedeEvents)
	}
	// Setup produces 1 candidate→accepted (the previous
	// becoming accepted). The swap produces 1 (the new
	// becoming accepted). Total: 2.
	if acceptEvents != 2 {
		t.Errorf("audit candidate→accepted events = %d, want 2 (one from setup, one from swap)", acceptEvents)
	}
}

// TestSupersedeAndAccept_StalePreviousErrors: a second
// call with an already-superseded previous returns an
// error and does not double-accept the new artifact.
func TestSupersedeAndAccept_StalePreviousErrors(t *testing.T) {
	a, _, _ := openStore(t)
	ctx := context.Background()

	prev, _ := a.CreateDraft(ctx, "p1", KindDocument, []byte("p"))
	_ = a.SetStatus(ctx, prev.ID, StatusCandidate)
	_ = a.SetStatus(ctx, prev.ID, StatusAccepted)

	new, _ := a.CreateDraft(ctx, "p1", KindDocument, []byte("n"))
	_ = a.SetStatus(ctx, new.ID, StatusCandidate)

	// First call succeeds.
	if err := a.SupersedeAndAccept(ctx, prev.ID, new.ID); err != nil {
		t.Fatal(err)
	}
	// Second call: previous is no longer `accepted`; the CAS
	// fails and the function returns an error.
	err := a.SupersedeAndAccept(ctx, prev.ID, new.ID)
	if err == nil {
		t.Fatal("second SupersedeAndAccept returned nil error; want stale-previous error")
	}
	if !strings.Contains(err.Error(), "no longer `accepted`") {
		t.Errorf("error = %q, want stale-previous message", err)
	}
}

// TestSupersedeAndAccept_StaleNewErrors: the new
// artifact is no longer `candidate` (e.g. already
// `rejected`). The function returns an error. The
// transaction is rolled back so the previous stays
// `accepted`.
func TestSupersedeAndAccept_StaleNewErrors(t *testing.T) {
	a, _, _ := openStore(t)
	ctx := context.Background()

	prev, _ := a.CreateDraft(ctx, "p1", KindDocument, []byte("p"))
	_ = a.SetStatus(ctx, prev.ID, StatusCandidate)
	_ = a.SetStatus(ctx, prev.ID, StatusAccepted)

	new, _ := a.CreateDraft(ctx, "p1", KindDocument, []byte("n"))
	_ = a.SetStatus(ctx, new.ID, StatusCandidate)
	_ = a.SetStatus(ctx, new.ID, StatusRejected)

	err := a.SupersedeAndAccept(ctx, prev.ID, new.ID)
	if err == nil {
		t.Fatal("SupersedeAndAccept returned nil error; want stale-new error")
	}
	if !strings.Contains(err.Error(), "no longer `candidate`") {
		t.Errorf("error = %q, want stale-new message", err)
	}
	// Transaction rolled back: prev stays accepted.
	gotPrev, _ := a.Get(ctx, prev.ID)
	if gotPrev.Status != StatusAccepted {
		t.Errorf("prev status = %s after failed swap, want accepted (rollback)", gotPrev.Status)
	}
}

// TestSupersedeAndAccept_EmptyIDs: both error paths
// reject empty IDs.
func TestSupersedeAndAccept_EmptyIDs(t *testing.T) {
	a, _, _ := openStore(t)
	ctx := context.Background()
	if err := a.SupersedeAndAccept(ctx, "", "x"); err == nil {
		t.Error("empty previousID accepted, want error")
	}
	if err := a.SupersedeAndAccept(ctx, "x", ""); err == nil {
		t.Error("empty newID accepted, want error")
	}
}
