package main

import (
	"errors"

	"github.com/tcs76321/athanor/internal/internalapi"
	"github.com/tcs76321/athanor/internal/jobpod"
)

// tokenStoreAdapter bridges jobpod.Manager.TokenFor to the
// internalapi.TokenStore interface. The two interfaces agree on
// the happy-path signature (jobID → token string, error) but
// disagree on the not-found sentinel: jobpod returns
// ErrNotFound, internalapi wants ErrTokenNotFound. The adapter
// translates.
//
// Kept in cmd/ (not in either internal/ package) because it is
// the only place where the two surfaces meet. If a future caller
// in internal/ needs the same bridge, lift this into its own
// package; for M2-T3 the one-call-site is fine.
type tokenStoreAdapter struct {
	mgr jobpod.Manager
}

// Get satisfies internalapi.TokenStore.
func (a tokenStoreAdapter) Get(jobID string) (string, error) {
	tok, err := a.mgr.TokenFor(jobID)
	if err != nil {
		if errors.Is(err, jobpod.ErrNotFound) {
			return "", internalapi.ErrTokenNotFound
		}
		return "", err
	}
	return tok, nil
}
