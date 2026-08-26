// Package control implements daemon-wide control surfaces, starting with
// the kill switch (ARCHITECTURE §22).
//
// Semantics (§22.1–§22.2):
//   - Freezing stops all new work. (The M1 job executor consults Frozen()
//     before starting jobs; M2 extends this to Job Pods and fetches.)
//   - Frozen state is persisted in SQLite (system_state table) and
//     therefore survives restarts.
//   - Frozen mode never lifts automatically. Unfreezing requires an
//     explicit command with a reason, recorded in the EventLog.
package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/tcs76321/athanor/internal/store"
)

// SystemState keys.
const (
	keyFrozen = "frozen"
)

// Frozen state values.
const (
	ValueRunning = "running"
	ValueFrozen  = "frozen"
)

// ErrReasonRequired is returned by Unfreeze without an explicit
// acknowledgment reason (§22.2: unfreezing must never be accidental).
var ErrReasonRequired = errors.New("unfreeze requires an explicit reason (recorded in the event log)")

// KillSwitch is the persistent freeze control.
type KillSwitch struct {
	store *store.Store

	mu     sync.RWMutex
	frozen bool
}

// NewKillSwitch loads the persisted frozen state. A daemon restart must
// inherit freeze status, never reset it (§22.2).
func NewKillSwitch(s *store.Store) (*KillSwitch, error) {
	k := &KillSwitch{store: s}
	var value string
	err := s.DB().QueryRow(`SELECT value FROM system_state WHERE key = ?`, keyFrozen).Scan(&value)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Never frozen: nothing to record.
	case err != nil:
		return nil, fmt.Errorf("loading frozen state: %w", err)
	case value == ValueFrozen:
		k.frozen = true
	case value == ValueRunning:
	default:
		return nil, fmt.Errorf("system_state.%s has unknown value %q (want %q or %q)",
			keyFrozen, value, ValueRunning, ValueFrozen)
	}
	return k, nil
}

// Frozen reports whether the daemon is frozen (no new work may start).
func (k *KillSwitch) Frozen() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.frozen
}

// Freeze enters frozen mode. Idempotent: freezing an already-frozen
// daemon is a no-op. The transition is persisted and audited.
func (k *KillSwitch) Freeze(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.frozen {
		return nil
	}
	if err := k.setFrozen(ctx, ValueFrozen); err != nil {
		return err
	}
	k.frozen = true
	return k.audit(ctx, map[string]any{"event": "freeze"})
}

// Unfreeze exits frozen mode. Requires a non-empty reason that is
// recorded in the EventLog (§22.2: explicit user acknowledgment).
// Unfreezing a running daemon is a no-op.
func (k *KillSwitch) Unfreeze(ctx context.Context, reason string) error {
	if reason == "" {
		return ErrReasonRequired
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.frozen {
		return nil
	}
	if err := k.setFrozen(ctx, ValueRunning); err != nil {
		return err
	}
	k.frozen = false
	return k.audit(ctx, map[string]any{"event": "unfreeze", "reason": reason})
}

// setFrozen persists the frozen value; caller holds the write lock.
func (k *KillSwitch) setFrozen(ctx context.Context, value string) error {
	_, err := k.store.DB().ExecContext(ctx, `
		INSERT INTO system_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		keyFrozen, value)
	if err != nil {
		return fmt.Errorf("persisting frozen state: %w", err)
	}
	return nil
}

// audit records freeze/unfreeze in the append-only event log under the
// jobs category (control is job lifecycle).
func (k *KillSwitch) audit(ctx context.Context, data map[string]any) error {
	_, err := k.store.AppendEvent(ctx, store.Event{Category: "jobs", Data: data})
	if err != nil {
		return fmt.Errorf("auditing kill switch event: %w", err)
	}
	return nil
}
