// M4-T2: wire the §21.3 ingress pipeline into the daemon
// boot sequence. The watcher is constructed after the
// store is migrated and before the HTTP server starts; it
// runs in the background until the daemon shuts down.
//
// The scanner set is M4-T2's minimum: `size` and `zipbomb`.
// Both are deterministic, in-tree, and have no external
// dependencies. The prompt-injection heuristic, ClamAV, and
// YARA land in M4-T3; their addition is a one-line edit to
// the per-pipeline lists here.
//
// The watcher is started in a separate goroutine and is
// bound to the daemon's lifetime via the run() context.
// On shutdown, the watcher's Close drains the pending
// queue before the process exits, so a clean shutdown
// doesn't leave half-processed files.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/tcs76321/athanor/internal/airlock/ingress"
	"github.com/tcs76321/athanor/internal/airlock/scanner"
	"github.com/tcs76321/athanor/internal/control"
	"github.com/tcs76321/athanor/internal/store"
)

// startIngress constructs and starts the ingress Watcher.
// Returns the Watcher (whose Close must be called at
// shutdown) and the constructed registry (so tests can
// inspect it).
//
// The inbox directory is `<state-dir>/workspace/inbox`.
// The quarantine directory is `<state-dir>/workspace/quarantine`,
// the sibling of `inbox` (the layout matches §4 of
// ARCHITECTURE.md).
func startIngress(
	ctx context.Context,
	stateDir string,
	store_ *store.Store,
	killSwitch *control.KillSwitch,
	cfg ingressConfig,
	logger *slog.Logger,
) (*ingress.Watcher, *scanner.Registry, error) {
	if !cfg.AirlockEnabled {
		logger.Info("airlock disabled by config; ingress not started")
		return nil, nil, nil
	}
	root := filepath.Join(stateDir, "workspace", "inbox")
	// Build the in-tree scanner set for T2. T3 adds
	// prompt-injection-heuristic, clamav, yara to the
	// ingress list and re-orders per ADR-0015.
	scanners := map[string]scanner.Scanner{
		"size":    scanner.NewSize(cfg.MaxIngressBytes),
		"zipbomb": scanner.NewZipBomb(cfg.MaxUncompressedRatio, cfg.MaxZipEntries, 50),
	}
	ingressList := []string{"size", "zipbomb"}
	// The other two pipeline lists (egress, user_prompt)
	// are wired in T4 / via the long-prompt heuristic in
	// the API. T2 ships only ingress; the other two are
	// unused. The registry requires every pipeline to
	// have at least one scanner (fail-closed at
	// construction), so we register `size` for them too
	// — the sizes still apply, even on LLM-generated
	// text. M4-T3 / T4 will replace these with the
	// proper scanners.
	egressList := []string{"size", "zipbomb"}
	userPromptList := []string{"size"}
	reg, err := scanner.NewRegistry(scanners, ingressList, egressList, userPromptList)
	if err != nil {
		return nil, nil, fmt.Errorf("building airlock scanner registry: %w", err)
	}
	qr := store.NewQuarantineRepo(store_)
	w, err := ingress.New(ctx, ingress.Options{
		Root:       root,
		Registry:   reg,
		Store:      store_,
		Quarantine: qr,
		Freezer:    killSwitch,
		Logger:     logger,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("starting ingress watcher: %w", err)
	}
	logger.Info("airlock ingress started", "inbox", root)
	return w, reg, nil
}

// ingressConfig is the T2-subset of the Airlock config the
// cmd layer needs. Kept here as a local struct so the cmd
// package doesn't have to import `config` types in tests
// that don't need them. The daemon's `config.Airlock` field
// maps 1:1 to this struct.
type ingressConfig struct {
	AirlockEnabled       bool
	MaxIngressBytes      int64
	MaxUncompressedRatio int
	MaxZipEntries        int
}
