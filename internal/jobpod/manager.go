package jobpod

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
)

// supervisionInterval is the cadence of the per-pod `podman inspect`
// poll. Two seconds matches internal/job.Recover's active-job scan
// cadence and is short enough that a crashed pod is noticed promptly
// without flooding the host with inspect calls.
const supervisionInterval = 2 * time.Second

// podmanInspectFormat is the `podman inspect` template the supervisor
// uses to read the container's state. Two fields, space-separated.
const podmanInspectFormat = "{{.State.Status}} {{.State.ExitCode}}"

// manager is the production Manager impl. Concurrency-safe.
type manager struct {
	client  Client
	freezer Freezer
	mu      sync.RWMutex
	pods    map[string]*podEntry
}

type podEntry struct {
	pod   *Pod
	stopC chan struct{} // closed by the supervisor when the pod terminates
}

// New returns a Manager ready for use. The supervisor goroutines
// start lazily on the first Start call and stop when the pod's
// state transitions to a terminal state (M2-T5 expands the test).
func New(client Client, freezer Freezer) Manager {
	return &manager{
		client:  client,
		freezer: freezer,
		pods:    map[string]*podEntry{},
	}
}

// validate enforces the documented Spec shape. Tight on the ID
// (UUID v4) so a future caller cannot smuggle in a non-UUID that
// podman would silently accept.
var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func (m *manager) validateSpec(spec Spec) error {
	if !uuidV4Pattern.MatchString(spec.ID) {
		return fmt.Errorf("%w: ID must be a v4 UUID, got %q", ErrInvalidSpec, spec.ID)
	}
	if spec.Image == "" {
		return fmt.Errorf("%w: image is required", ErrInvalidSpec)
	}
	if len(spec.Command) == 0 {
		return fmt.Errorf("%w: command is required", ErrInvalidSpec)
	}
	return nil
}

// Start brings up a new pod. Steps:
//  1. validate the spec (no client call yet)
//  2. check the freezer (§22.1)
//  3. check for duplicate ID
//  4. build the argv
//  5. invoke the client (podman run --detach)
//  6. register the pod in the in-memory map
//  7. start the supervisor goroutine
func (m *manager) Start(ctx context.Context, spec Spec) (*Pod, error) {
	if err := m.validateSpec(spec); err != nil {
		return nil, err
	}
	if m.freezer != nil && m.freezer.Frozen() {
		return nil, ErrFrozen
	}

	m.mu.Lock()
	if _, exists := m.pods[spec.ID]; exists {
		m.mu.Unlock()
		return nil, ErrAlreadyExists
	}
	m.mu.Unlock()

	args := buildArgs(spec)
	if _, _, err := m.client.Run(ctx, args...); err != nil {
		return nil, fmt.Errorf("podman run: %w", err)
	}

	pod := &Pod{ID: spec.ID, State: StatePending}
	entry := &podEntry{pod: pod, stopC: make(chan struct{})}

	m.mu.Lock()
	m.pods[spec.ID] = entry
	m.mu.Unlock()

	go m.supervise(ctx, spec.ID, entry)
	return pod, nil
}

// supervise polls `podman inspect` until the container reports
// `exited` or `stopped`, then updates the in-memory Pod. Polling
// is the simplest correct primitive; events are M2-T5 territory.
func (m *manager) supervise(ctx context.Context, id string, entry *podEntry) {
	defer close(entry.stopC)
	ticker := time.NewTicker(supervisionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		stdout, stderr, err := m.client.Run(ctx, "inspect", "--format", podmanInspectFormat, id)
		if err != nil {
			// Inspect may fail transiently (e.g. container is being
			// created). Treat as "still running" and try again.
			slog.Debug("jobpod: inspect failed, retrying", "pod", id, "stderr", string(stderr), "err", err)
			continue
		}
		status, exitCode := parseInspect(string(stdout))
		switch status {
		case "running":
			m.setState(id, StateRunning, nil)
		case "exited", "stopped":
			if exitCode == 0 {
				m.setState(id, StateStopped, nil)
			} else {
				m.setState(id, StateFailed, fmt.Errorf("podman exit code %d", exitCode))
			}
			return
		default:
			// Unknown status (paused, dead, etc.). Log and keep
			// polling; the next tick will resolve.
			slog.Debug("jobpod: unexpected pod status", "pod", id, "status", status, "exit", exitCode)
		}
	}
}

// setState updates the in-memory Pod under the write lock.
func (m *manager) setState(id string, state State, exitErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.pods[id]
	if !ok {
		return
	}
	entry.pod.State = state
	entry.pod.ExitErr = exitErr
}

// Stop force-removes a pod. Idempotent: stopping a stopped pod is
// a no-op. Returns ErrNotFound for an unknown ID. Stops do not
// consult the freezer — teardown must always be possible.
func (m *manager) Stop(ctx context.Context, id string) error {
	m.mu.RLock()
	_, exists := m.pods[id]
	m.mu.RUnlock()
	if !exists {
		return ErrNotFound
	}
	if _, _, err := m.client.Run(ctx, "rm", "-f", id); err != nil {
		// A failed stop is not necessarily fatal: the pod may have
		// already exited. We remove it from the in-memory map
		// regardless so future Stop calls are no-ops.
		slog.Warn("jobpod: stop failed; removing from in-memory map", "pod", id, "err", err)
	}
	m.mu.Lock()
	delete(m.pods, id)
	m.mu.Unlock()
	return nil
}

// Get returns the in-memory view of a pod. No I/O.
func (m *manager) Get(id string) (*Pod, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.pods[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &Pod{ID: entry.pod.ID, State: entry.pod.State, ExitErr: entry.pod.ExitErr}, nil
}

// parseInspect splits `podman inspect` output into (status, exitCode).
// The documented two-token format is "<status> <exitCode>"; we fall
// back to a whole-string status when there is no exit code.
func parseInspect(s string) (string, int) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "", 0
	}
	if len(fields) == 1 {
		return fields[0], 0
	}
	exitCode := 0
	if _, err := fmt.Sscanf(fields[len(fields)-1], "%d", &exitCode); err != nil {
		return strings.Join(fields, " "), 0
	}
	return strings.Join(fields[:len(fields)-1], " "), exitCode
}
