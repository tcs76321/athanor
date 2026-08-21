// Package power provides resource management for the Athanor daemon,
// throttling resource usage based on user activity state.
package power

import (
	"log/slog"
	"sync"
)

// Profile represents the power management profile.
type Profile string

const (
	// ProfileAutonomous - High resource utilization.
	// Multiple concurrent VMs allowed. Background "dreaming" (memory consolidation/indexing) is ACTIVE.
	ProfileAutonomous Profile = "autonomous"

	// ProfileInteractive - Deferential resource utilization.
	// Max 1 concurrent VM. Background "dreaming" is PAUSED. CPU quotas are reduced.
	ProfileInteractive Profile = "interactive"
)

// Limits defines the resource limits for a given profile.
type Limits struct {
	MaxConcurrentVMs int     // Maximum number of concurrent VMs allowed
	AllowDreaming    bool    // Whether background "dreaming" (memory consolidation/indexing) is enabled
	CPUQuota         float64 // CPU quota as fraction of total (0.0-1.0)
}

// OSWatcher defines the interface for OS-level power assertions.
// This is a stub for future CGO/D-Bus integration.
type OSWatcher interface {
	// AcquirePowerAssertion requests the OS to prevent sleep/idle.
	AcquirePowerAssertion() error
	// ReleasePowerAssertion releases the OS power assertion.
	ReleasePowerAssertion() error
}

// NoopWatcher is a no-op implementation of OSWatcher for testing
// and environments where OS integration is not available.
type NoopWatcher struct{}

// AcquirePowerAssertion logs the request and returns nil.
func (n *NoopWatcher) AcquirePowerAssertion() error {
	slog.Debug("NoopWatcher: AcquirePowerAssertion called")
	return nil
}

// ReleasePowerAssertion logs the request and returns nil.
func (n *NoopWatcher) ReleasePowerAssertion() error {
	slog.Debug("NoopWatcher: ReleasePowerAssertion called")
	return nil
}

// PowerManager manages power profiles and resource limits.
type PowerManager struct {
	mu            sync.RWMutex
	watcher       OSWatcher
	currentProfile Profile
	limits        Limits
}

// NewPowerManager creates a new PowerManager with the given OSWatcher.
// If watcher is nil, a NoopWatcher is used.
func NewPowerManager(watcher OSWatcher) *PowerManager {
	if watcher == nil {
		watcher = &NoopWatcher{}
	}
	pm := &PowerManager{
		watcher: watcher,
	}
	// Default to interactive profile (safe default)
	pm.setProfileInternal(ProfileInteractive)
	return pm
}

// profileLimits returns the Limits for a given profile.
func profileLimits(p Profile) Limits {
	switch p {
	case ProfileAutonomous:
		return Limits{
			MaxConcurrentVMs: 4,
			AllowDreaming:    true,
			CPUQuota:         1.0,
		}
	case ProfileInteractive:
		return Limits{
			MaxConcurrentVMs: 1,
			AllowDreaming:    false,
			CPUQuota:         0.3,
		}
	default:
		// Safe fallback
		return Limits{
			MaxConcurrentVMs: 1,
			AllowDreaming:    false,
			CPUQuota:         0.3,
		}
	}
}

// setProfileInternal updates the profile and limits without locking.
// Caller must hold the write lock.
func (pm *PowerManager) setProfileInternal(p Profile) {
	pm.currentProfile = p
	pm.limits = profileLimits(p)
	slog.Info("PowerManager profile changed", "profile", p, "limits", pm.limits)
}

// GetLimits returns the current resource limits.
// Safe for concurrent use.
func (pm *PowerManager) GetLimits() Limits {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.limits
}

// SetProfile changes the power profile.
// Safe for concurrent use.
func (pm *PowerManager) SetProfile(p Profile) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.currentProfile == p {
		slog.Debug("PowerManager profile unchanged", "profile", p)
		return
	}
	pm.setProfileInternal(p)
}

// ToggleAutonomousMode enables or disables autonomous mode.
// This is a convenience method for the Web UI to manually trigger state changes.
// Safe for concurrent use.
func (pm *PowerManager) ToggleAutonomousMode(enabled bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	var newProfile Profile
	if enabled {
		newProfile = ProfileAutonomous
	} else {
		newProfile = ProfileInteractive
	}

	if pm.currentProfile == newProfile {
		slog.Debug("PowerManager autonomous mode unchanged", "enabled", enabled)
		return
	}

	pm.setProfileInternal(newProfile)
}

// CurrentProfile returns the current power profile.
// Safe for concurrent use.
func (pm *PowerManager) CurrentProfile() Profile {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.currentProfile
}