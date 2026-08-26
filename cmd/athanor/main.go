// Command athanor is the daemon entry point.
//
// Boot sequence (M0-T7): load config → init logging → open store → run
// migrations → serve /healthz on loopback → graceful shutdown on
// SIGINT/SIGTERM. Later milestones wire subsystems into this order.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tcs76321/athanor/internal/config"
	"github.com/tcs76321/athanor/internal/logging"
	"github.com/tcs76321/athanor/internal/server"
	"github.com/tcs76321/athanor/internal/store"
	"github.com/tcs76321/athanor/migrations"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.0.0-dev"

const shutdownTimeout = 10 * time.Second

func main() {
	configPath := flag.String("config", "config.yaml", "path to config.yaml")
	addr := flag.String("addr", "127.0.0.1:7420", "HTTP listen address (loopback only, §21.8)")
	stateDir := flag.String("state-dir", "state", "state directory (database, logs, backups)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("athanor", version)
		return
	}

	if err := run(*configPath, *addr, *stateDir); err != nil {
		fmt.Fprintln(os.Stderr, "athanor:", err)
		os.Exit(1)
	}
}

// run boots the daemon and blocks until an OS signal or server failure.
func run(configPath, addr, stateDir string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logMgr, err := logging.New(logging.Options{
		Dir:        filepath.Join(stateDir, "logs"),
		Level:      cfg.Logging.Level,
		Categories: cfg.Logging.Categories,
	})
	if err != nil {
		return fmt.Errorf("initialising logging: %w", err)
	}
	defer func() { _ = logMgr.Close() }()

	st, err := store.Open(filepath.Join(stateDir, "athanor.db"))
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer func() { _ = st.Close() }()

	if err := store.Migrate(st.DB(), migrations.FS, filepath.Join(stateDir, "backups")); err != nil {
		return fmt.Errorf("migrating database: %w", err)
	}

	loopAddr, err := server.LocalhostAddr(addr)
	if err != nil {
		return err
	}
	srv := server.New(version)
	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ln, err := net.Listen("tcp", loopAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", loopAddr, err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	// Dogfood the EventLog: record startup in the append-only audit trail.
	if _, err := st.AppendEvent(context.Background(), store.Event{
		Category: "jobs",
		Data:     map[string]any{"event": "startup", "version": version},
	}); err != nil {
		return fmt.Errorf("recording startup event: %w", err)
	}

	fmt.Printf("athanor %s listening on http://%s\n", version, loopAddr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case sig := <-stop:
		fmt.Printf("received %s — shutting down\n", sig)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	}

	if _, err := st.AppendEvent(context.Background(), store.Event{
		Category: "jobs",
		Data:     map[string]any{"event": "shutdown"},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "athanor: recording shutdown event: %v\n", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down http server: %w", err)
	}
	return nil
}

