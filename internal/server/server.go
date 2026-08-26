// Package server provides the daemon's HTTP surface. Per ARCHITECTURE
// §21.8 it binds to loopback only; remote access is via SSH port
// forwarding. M0-T7 ships /healthz; later milestones attach UI and API
// routes to the same mux.
package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server holds daemon-wide HTTP state.
type Server struct {
	version string
	started time.Time
}

// New returns a Server whose uptime clock starts now.
func New(version string) *Server {
	return &Server{version: version, started: time.Now()}
}

// Handler returns the root http.Handler with all routes attached.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	return mux
}

type healthResponse struct {
	Status        string  `json:"status"`
	Version       string  `json:"version"`
	Uptime        string  `json:"uptime"`
	UptimeSeconds float64 `json:"uptime_seconds"`
}

// handleHealthz reports status, version, and uptime (M0-T7 acceptance).
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uptime := time.Since(s.started)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:        "ok",
		Version:       s.version,
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: uptime.Seconds(),
	})
}

// LocalhostAddr forces a listen address onto the loopback interface
// (§21.8). Bare ports and wildcard hosts become 127.0.0.1; explicit
// non-loopback hosts are rejected outright (fail closed).
func LocalhostAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("listen address %q must be host:port: %w", addr, err)
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	case "localhost", "127.0.0.1", "::1":
	default:
		return "", fmt.Errorf("listen address %q must bind localhost only (ARCHITECTURE §21.8)", addr)
	}
	return net.JoinHostPort(host, port), nil
}
