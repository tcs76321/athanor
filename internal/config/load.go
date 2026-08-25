// Loading and defaults for the Athanor configuration (ARCHITECTURE §29).
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Sentinel errors callers can match on for distinct failure modes.
var (
	ErrFileNotFound = errors.New("config file not found")
	ErrEmptyConfig  = errors.New("config file is empty")
)

// Categories is the closed set of event-log categories (ARCHITECTURE §28.1).
var Categories = []string{
	"jobs", "recovery", "alarms", "airlock", "network", "podman",
	"inference", "feedback", "strategy", "context", "daydream",
	"power", "backup",
}

var activeHoursRE = regexp.MustCompile(`^\d{2}:\d{2}-\d{2}:\d{2}$`)

// Load reads, parses, applies defaults to, and validates the configuration
// at path. Each failure mode produces a distinct error.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrFileNotFound, path)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, err
	}
	cfg.SourcePath = path
	return cfg, nil
}

// Parse decodes raw YAML bytes, applies defaults for omitted optional
// fields, and validates the result.
func Parse(data []byte) (*Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, ErrEmptyConfig
	}
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // reject unknown keys outright
	if err := dec.Decode(&cfg); err != nil {
		var terr *yaml.TypeError
		if errors.As(err, &terr) {
			return nil, fmt.Errorf("wrong-typed config: %w", err)
		}
		return nil, fmt.Errorf("malformed config: %w", err)
	}
	if err := validateRaw(&cfg); err != nil {
		return nil, err
	}
	applyDefaults(&cfg)
	if err := validateCross(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
