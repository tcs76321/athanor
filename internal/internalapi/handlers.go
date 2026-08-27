package internalapi

import (
	"net/http"
)

// API wires the internal API to its dependencies. All routes are
// mounted behind the auth middleware at registration time.
type API struct {
	tokens TokenStore
}

// New returns an API bound to the given TokenStore. The store
// is the only dependency; the actual DB and event-log access
// for the handlers lands in M2-T3 commit 3.
func New(tokens TokenStore) *API {
	return &API{tokens: tokens}
}

// Register attaches every internal route to mux. Every route is
// wrapped in authMiddleware; the structural proof of "every route
// is wrapped" lives in Gate G2 (internal/gate/gate_test.go) and
// runs in make test-race.
func (a *API) Register(mux *http.ServeMux) {
	// M2-T3 commit 2: handler stubs. Each is a 501 "Not
	// Implemented" placeholder; the real handlers (GET job
	// context, POST heartbeat, POST log) land in commit 3.
	// The routes are registered now so the middleware is
	// exercised end-to-end in commit 2's tests; replacing
	// the stubs in commit 3 is a one-file change.
	handle := func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotImplemented, "internal API route arrives in M2-T3 commit 3")
	}

	mux.Handle("GET /internal/v1/jobs/{id}",
		authMiddleware(a.tokens, http.HandlerFunc(handle)))
	mux.Handle("POST /internal/v1/jobs/{id}/heartbeat",
		authMiddleware(a.tokens, http.HandlerFunc(handle)))
	mux.Handle("POST /internal/v1/jobs/{id}/log",
		authMiddleware(a.tokens, http.HandlerFunc(handle)))
}
