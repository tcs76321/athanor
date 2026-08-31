package artifact

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

// Get loads one artifact by ID.
func (a *Store) Get(ctx context.Context, id string) (Artifact, error) {
	row := a.db.DB().QueryRowContext(ctx, artifactSelect+` WHERE id = ?`, id)
	art, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return art, err
}

// ListByProject returns a project's artifacts, newest first.
func (a *Store) ListByProject(ctx context.Context, projectID string) ([]Artifact, error) {
	rows, err := a.db.DB().QueryContext(ctx,
		artifactSelect+` WHERE project_id = ? ORDER BY created_at DESC, id ASC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("listing artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Artifact
	for rows.Next() {
		art, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, art)
	}
	return out, rows.Err()
}

// LatestAcceptedByProject returns the project's most recently accepted
// artifact, or ErrNotFound when the project has no accepted artifact
// yet. Used by §13.1 Phase 6 (Comparing) to find the "previous" side
// of the §19.3 comparison rule; the comparison phase picks the
// project's current best, not just any non-rejected artifact.
//
// M3-T1 owns this; M3-T5 (git tool) reuses it to pick the base commit
// for a new accepted artifact. There can be at most one accepted
// artifact per project at a time (§9.3: accepting a new one
// supersedes the old), but the query orders by version DESC then
// created_at DESC to stay correct if that invariant ever relaxes.
func (a *Store) LatestAcceptedByProject(ctx context.Context, projectID string) (Artifact, error) {
	row := a.db.DB().QueryRowContext(ctx,
		artifactSelect+` WHERE project_id = ? AND status = ? ORDER BY version DESC, created_at DESC, id ASC LIMIT 1`,
		projectID, string(StatusAccepted))
	art, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, fmt.Errorf("%w: no accepted artifact for project %s", ErrNotFound, projectID)
	}
	return art, err
}

// SetStatus applies a §9.3 status transition.
func (a *Store) SetStatus(ctx context.Context, id string, to Status) error {
	art, err := a.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := ValidateTransition(art.Status, to); err != nil {
		return err
	}
	res, err := a.db.DB().ExecContext(ctx,
		`UPDATE artifacts SET status = ? WHERE id = ? AND status = ?`,
		string(to), id, string(art.Status))
	if err != nil {
		return fmt.Errorf("updating artifact status: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("artifact %s changed status concurrently; retry", id)
	}
	return a.audit(ctx, nil, art, map[string]any{
		"event": "status", "from": string(art.Status), "to": string(to),
	})
}

// ReadContent returns the artifact's file content, verifying the recorded
// SHA-256 hash first: silent bitrot is never acceptable (fail loudly).
func (a *Store) ReadContent(ctx context.Context, id string) ([]byte, error) {
	art, err := a.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if art.StoragePath == "" {
		return nil, fmt.Errorf("artifact %s has no stored content", id)
	}
	data, err := os.ReadFile(art.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("reading artifact %s content: %w", id, err)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != art.ContentHash {
		return nil, &ContentMismatchError{ID: id, WantHash: art.ContentHash, GotHash: got}
	}
	return data, nil
}
