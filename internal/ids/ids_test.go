package ids

import (
	"regexp"
	"testing"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewFormat(t *testing.T) {
	id := New()
	if !uuidPattern.MatchString(id) {
		t.Errorf("New() = %q, want a version-4 UUID string", id)
	}
}

// TestNewUnique is a cheap collision check over a batch; cryptographic
// randomness makes true collisions ~impossible, so any repeat is a bug
// (e.g., a broken rand source).
func TestNewUnique(t *testing.T) {
	seen := make(map[string]bool, 10000)
	for i := 0; i < 10000; i++ {
		id := New()
		if seen[id] {
			t.Fatalf("duplicate id generated: %s", id)
		}
		seen[id] = true
	}
}
