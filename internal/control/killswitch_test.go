package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/tcs76321/athanor/internal/store"
	"github.com/tcs76321/athanor/migrations"
)

func openKillSwitch(t *testing.T) (*KillSwitch, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := store.Migrate(s.DB(), migrations.FS, ""); err != nil {
		t.Fatal(err)
	}
	k, err := NewKillSwitch(s)
	if err != nil {
		t.Fatal(err)
	}
	return k, s, dir
}

func TestStartsUnfrozen(t *testing.T) {
	k, _, _ := openKillSwitch(t)
	if k.Frozen() {
		t.Error("fresh daemon reports frozen, want running")
	}
}

func TestFreezeUnfreezeRoundTrip(t *testing.T) {
	k, s, _ := openKillSwitch(t)
	ctx := context.Background()

	if err := k.Freeze(ctx); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if !k.Frozen() {
		t.Fatal("daemon not frozen after Freeze")
	}

	// Unfreeze requires an explicit reason (§22.2).
	if err := k.Unfreeze(ctx, ""); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("Unfreeze(empty reason) = %v, want ErrReasonRequired", err)
	}
	if !k.Frozen() {
		t.Error("daemon unfroze without a reason — a rejected unfreeze must not change state")
	}

	if err := k.Unfreeze(ctx, "investigated the alarm; resuming"); err != nil {
		t.Fatalf("Unfreeze: %v", err)
	}
	if k.Frozen() {
		t.Error("daemon still frozen after Unfreeze")
	}

	// Both transitions are audited with the reason recorded.
	events, err := s.QueryEvents(ctx, store.EventFilter{Category: "jobs"})
	if err != nil {
		t.Fatal(err)
	}
	var sawFreeze, sawUnfreezeReason bool
	for _, e := range events {
		if e.DataJSON == `{"event":"freeze"}` {
			sawFreeze = true
		}
		if e.DataJSON == `{"event":"unfreeze","reason":"investigated the alarm; resuming"}` {
			sawUnfreezeReason = true
		}
	}
	if !sawFreeze || !sawUnfreezeReason {
		t.Errorf("audit trail incomplete (freeze=%v, unfreeze-with-reason=%v): %+v", sawFreeze, sawUnfreezeReason, events)
	}
}

func TestFreezeIdempotent(t *testing.T) {
	k, _, _ := openKillSwitch(t)
	ctx := context.Background()
	if err := k.Freeze(ctx); err != nil {
		t.Fatal(err)
	}
	if err := k.Freeze(ctx); err != nil {
		t.Fatalf("second Freeze: %v", err)
	}
	if err := k.Unfreeze(ctx, "r"); err != nil {
		t.Fatal(err)
	}
	// Unfreezing a running daemon is a no-op, not an error.
	if err := k.Unfreeze(ctx, "already running"); err != nil {
		t.Fatalf("Unfreeze(running) = %v, want nil", err)
	}
}

// TestFrozenSurvivesRestart is the M1-T6 acceptance criterion: frozen
// state persists across a daemon restart (§22.2 — frozen mode never lifts
// automatically, and a crash while frozen must not silently unfreeze).
func TestFrozenSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	ctx := context.Background()

	s1, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(s1.DB(), migrations.FS, ""); err != nil {
		t.Fatal(err)
	}
	k1, err := NewKillSwitch(s1)
	if err != nil {
		t.Fatal(err)
	}
	if err := k1.Freeze(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	// Restart: frozen state is inherited.
	s2, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	k2, err := NewKillSwitch(s2)
	if err != nil {
		t.Fatal(err)
	}
	if !k2.Frozen() {
		t.Fatal("frozen state lost across restart, want inherited (§22.2)")
	}

	// And it still unfreezes with a reason afterwards.
	if err := k2.Unfreeze(ctx, "post-restart review done"); err != nil {
		t.Fatalf("Unfreeze after restart: %v", err)
	}
	if k2.Frozen() {
		t.Error("still frozen after unfreeze")
	}
}

func TestCorruptFrozenValueFailsLoudly(t *testing.T) {
	_, s, _ := openKillSwitch(t)
	if _, err := s.DB().Exec(`INSERT INTO system_state (key, value) VALUES ('frozen', 'maybe')`); err != nil {
		t.Fatal(err)
	}
	if _, err := NewKillSwitch(s); err == nil {
		t.Fatal("corrupt frozen value accepted, want loud failure")
	}
}
