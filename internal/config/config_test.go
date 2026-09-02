package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tcs76321/athanor/internal/toolenvelope"
	"gopkg.in/yaml.v3"
)

const validMinimal = `
version: 2
agent:
  idle_threshold: "10m"
personas:
  security:
    model: "llama3.2:3b"
    context_target: 4096
    temperature: 0.0
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadValidMinimalAppliesDefaults(t *testing.T) {
	cfg, err := Load(writeTemp(t, validMinimal))
	if err != nil {
		t.Fatalf("Load(valid) = %v, want nil", err)
	}
	if cfg.Version != 2 {
		t.Errorf("Version = %d, want 2", cfg.Version)
	}
	if cfg.Agent.IdleThreshold.D() != 10*time.Minute {
		t.Errorf("explicit idle_threshold not honored: %v", cfg.Agent.IdleThreshold)
	}
	if cfg.Agent.ActiveHours != "00:00-24:00" {
		t.Errorf("ActiveHours default = %q", cfg.Agent.ActiveHours)
	}
	if got := cfg.Personas.Security.Model; got != "llama3.2:3b" || cfg.Personas.Security.ContextTarget != 4096 {
		t.Errorf("explicit security persona overridden: %+v", cfg.Personas.Security)
	}
	if got := cfg.Personas.Wide; got.Model != "qwen2.5:7b" || got.ContextTarget != 65536 || got.Temp() != 0.7 {
		t.Errorf("wide persona defaults = %+v", got)
	}
	if cfg.Execution.DivergenceCandidates != 3 {
		t.Errorf("DivergenceCandidates default = %d", cfg.Execution.DivergenceCandidates)
	}
	if cfg.Execution.MaxReflectionLoops != 2 {
		t.Errorf("MaxReflectionLoops default = %d, want 2", cfg.Execution.MaxReflectionLoops)
	}
	if cfg.Execution.JudgePersona != "security" {
		t.Errorf("JudgePersona default = %q", cfg.Execution.JudgePersona)
	}
	if d, _ := cfg.Execution.PhaseBudget("evaluating"); d != 600*time.Second {
		t.Errorf("evaluating budget default = %v", d)
	}
	if d, _ := cfg.Execution.PhaseBudget("synthesizing"); d != 300*time.Second {
		t.Errorf("fallback budget for unknown phase = %v", d)
	}
	if !Val(cfg.Security.ScanIngressFiles, false) || !Val(cfg.Backup.Auto, false) {
		t.Error("default-on security/backup flags not applied")
	}
	if len(cfg.Logging.Categories) != len(Categories) {
		t.Errorf("logging categories default = %v", cfg.Logging.Categories)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("err = %v, want ErrFileNotFound", err)
	}
}

func TestParseEmptyConfig(t *testing.T) {
	for _, tc := range []string{"", "\n\n", "   "} {
		if _, err := Parse([]byte(tc)); !errors.Is(err, ErrEmptyConfig) {
			t.Fatalf("Parse(%q) err = %v, want ErrEmptyConfig", tc, err)
		}
	}
}

func TestParseMalformedYAML(t *testing.T) {
	_, err := Parse([]byte("version: 2\n  bad indent: ["))
	if err == nil || !strings.Contains(err.Error(), "malformed config") {
		t.Fatalf("err = %v, want malformed config error", err)
	}
}

func TestParseWrongType(t *testing.T) {
	_, err := Parse([]byte("version: \"two\""))
	if err == nil || !strings.Contains(err.Error(), "wrong-typed") {
		t.Fatalf("err = %v, want wrong-typed error", err)
	}
	// Duration given as bare number instead of quoted string.
	_, err = Parse([]byte("agent:\n  idle_threshold: 5\n"))
	if err == nil || !strings.Contains(err.Error(), "quoted string") {
		t.Fatalf("duration-as-int err = %v, want specific quoting guidance", err)
	}
}

func TestParseUnknownFieldRejected(t *testing.T) {
	_, err := Parse([]byte("version: 2\ndefinitely_not_a_field: true\n"))
	if err == nil {
		t.Fatal("unknown field accepted, want strict rejection")
	}
}

func TestValidateRejectsInvalidSemantics(t *testing.T) {
	cases := []struct {
		name   string
		yaml   string
		errSub string
	}{
		{
			name:   "bad version",
			yaml:   "version: 1",
			errSub: "unsupported config version",
		},
		{
			name:   "bad active hours",
			yaml:   "version: 2\nagent:\n  active_hours: \"25:00-99:99\"\n",
			errSub: "active_hours",
		},
		{
			name:   "battery threshold out of range",
			yaml:   "version: 2\npower:\n  battery_pause_threshold_percent: 150\n",
			errSub: "battery_pause_threshold_percent",
		},
		{
			name:   "unknown judge persona",
			yaml:   "version: 2\nexecution:\n  judge_persona: oracle\n",
			errSub: "judge_persona",
		},
		{
			name:   "nonzero compaction temperature violates invariant",
			yaml:   "version: 2\ncontext_engine:\n  compaction_temperature: 0.3\n",
			errSub: "compaction_temperature",
		},
		{
			name:   "kv thresholds inverted",
			yaml:   "version: 2\ncontext_engine:\n  kv_cache_warning_threshold: 0.95\n  kv_cache_critical_threshold: 0.85\n",
			errSub: "kv_cache_warning_threshold",
		},
		{
			name:   "temperature out of range",
			yaml:   "version: 2\npersonas:\n  main:\n    model: m\n    context_target: 1024\n    temperature: 3.0\n",
			errSub: "main.temperature",
		},
		{
			name:   "bad network policy",
			yaml:   "version: 2\nnetwork:\n  default_policy: permit-all\n",
			errSub: "default_policy",
		},
		{
			name:   "unknown log category",
			yaml:   "version: 2\nlogging:\n  categories: [jobs, gossip]\n",
			errSub: "gossip",
		},
		{
			name:   "negative divergence candidates",
			yaml:   "version: 2\nexecution:\n  divergence_candidates: -2\n",
			errSub: "divergence_candidates",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.errSub) {
				t.Fatalf("err = %v, want containing %q", err, tc.errSub)
			}
		})
	}
}

func TestExplicitFalseHonoredForDefaultTrueFlags(t *testing.T) {
	cfg, err := Parse([]byte("version: 2\npower:\n  daydream_on_idle: false\nsecurity:\n  scan_ingress_files: false\nbackup:\n  auto: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if Val(cfg.Power.DaydreamOnIdle, true) {
		t.Error("explicit daydream_on_idle=false was overridden by default")
	}
	if Val(cfg.Security.ScanIngressFiles, true) {
		t.Error("explicit scan_ingress_files=false was overridden by default")
	}
	if Val(cfg.Backup.Auto, true) {
		t.Error("explicit backup.auto=false was overridden by default")
	}
}

func TestDefaultIsValid(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default() err = %v, want nil", err)
	}
	if cfg.Version != CurrentVersion {
		t.Errorf("Version = %d, want %d", cfg.Version, CurrentVersion)
	}
	if cfg.Inference.DefaultBackend != "ollama" {
		t.Errorf("DefaultBackend = %q, want ollama", cfg.Inference.DefaultBackend)
	}
	if cfg.SourcePath != "" {
		t.Errorf("SourcePath = %q, want empty (no file involved)", cfg.SourcePath)
	}
}

// TestDefaultEqualsEmptySpecifiedConfig proves Default() is exactly what
// Load produces for a config file that specifies nothing but a valid
// version — the fallback and the file path converge on one configuration.
func TestDefaultEqualsEmptySpecifiedConfig(t *testing.T) {
	fromFile, err := Parse([]byte("version: 2\n"))
	if err != nil {
		t.Fatalf("Parse(minimal): %v", err)
	}
	def, err := Default()
	if err != nil {
		t.Fatalf("Default(): %v", err)
	}
	if !reflect.DeepEqual(def, fromFile) {
		t.Errorf("Default() differs from Parse(minimal):\ndef:   %+v\nfile:  %+v", def, fromFile)
	}
}

// TestDefaultYAMLRoundTrip proves `athanor init` output is loadable: the
// marshaled default config parses back into exactly the same values
// (durations render as quoted strings via Duration.MarshalYAML).
func TestDefaultYAMLRoundTrip(t *testing.T) {
	def, err := Default()
	if err != nil {
		t.Fatalf("Default(): %v", err)
	}
	raw, err := yaml.Marshal(def)
	if err != nil {
		t.Fatalf("marshalling default: %v", err)
	}
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("parsing marshaled default: %v\nyaml:\n%s", err, raw)
	}
	// Normalize nil vs empty slices (nil marshals as [] and parses back
	// as empty-but-non-nil; semantically identical).
	for _, c := range []*Config{def, parsed} {
		if len(c.Network.AllowList) == 0 {
			c.Network.AllowList = nil
		}
		if len(c.Logging.Categories) == 0 {
			c.Logging.Categories = nil
		}
	}
	if !reflect.DeepEqual(def, parsed) {
		t.Errorf("marshaled default does not round-trip to the same config:\ndef:  %+v\nwant: %+v", parsed, def)
	}
}

// TestExampleConfigMatchesDefaults keeps config.example.yaml honest: the
// shipped example must always parse and equal the built-in defaults, so
// copying it to config.yaml is a documented no-op.
func TestExampleConfigMatchesDefaults(t *testing.T) {
	data, err := os.ReadFile("../../config.example.yaml")
	if err != nil {
		t.Fatalf("reading config.example.yaml: %v (file must ship with the repo)", err)
	}
	example, err := Parse(data)
	if err != nil {
		t.Fatalf("config.example.yaml does not parse: %v", err)
	}
	def, err := Default()
	if err != nil {
		t.Fatalf("Default(): %v", err)
	}
	// Normalize nil vs empty slices: `allow_list: []` in YAML decodes to an
	// empty slice while an omitted field stays nil — semantically identical.
	for _, c := range []*Config{example, def} {
		if len(c.Network.AllowList) == 0 {
			c.Network.AllowList = nil
		}
		if len(c.Logging.Categories) == 0 {
			c.Logging.Categories = nil
		}
		if len(c.JobPod.DefaultTools) == 0 {
			c.JobPod.DefaultTools = nil
		}
	}
	if !reflect.DeepEqual(example, def) {
		exJSON, _ := json.MarshalIndent(example, "", "  ")
		defJSON, _ := json.MarshalIndent(def, "", "  ")
		t.Errorf("config.example.yaml drifted from built-in defaults — update the example (or defaults) so they match\nexample:\n%s\ndefaults:\n%s", exJSON, defJSON)
	}
}

// TestJobPod_DefaultEnvelopeEmpty (M2-T4) proves the shipped default
// has an empty tool envelope: no tools are granted to a job unless
// the operator explicitly opts in. This is the safe default for a
// tool-execution surface.
func TestJobPod_DefaultEnvelopeEmpty(t *testing.T) {
	def, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	env, err := def.JobPodEnvelope()
	if err != nil {
		t.Fatalf("JobPodEnvelope(): %v", err)
	}
	if !env.IsEmpty() {
		t.Errorf("default JobPod envelope = %v, want empty", env.Tools())
	}
}

// TestJobPod_EnvelopeRejectsUnknown (M2-T4) proves the closed-set
// check runs at config-load time, not just at runtime: a typo in
// the config is an actionable error, not a silent no-op.
func TestJobPod_EnvelopeRejectsUnknown(t *testing.T) {
	bad := []byte("job_pod:\n  default_tools: [execute_code, run_remote_shell]\n")
	_, err := Parse(bad)
	if err == nil {
		t.Fatal("Parse with unknown tool returned nil error; want a job_pod.default_tools error")
	}
	if !strings.Contains(err.Error(), "job_pod.default_tools") {
		t.Errorf("error %q does not mention job_pod.default_tools (the failing field)", err)
	}
	if !strings.Contains(err.Error(), "run_remote_shell") {
		t.Errorf("error %q does not name the offending tool", err)
	}
}

// TestJobPod_EnvelopeAcceptsKnown (M2-T4) proves the validation
// accepts the closed set. A config that explicitly opts in to
// execute_code + run_tests must parse and yield the matching
// envelope.
func TestJobPod_EnvelopeAcceptsKnown(t *testing.T) {
	good := []byte("job_pod:\n  default_tools: [execute_code, run_tests]\n  image: python:3.12-alpine\n")
	cfg, err := Parse(good)
	if err != nil {
		t.Fatalf("Parse with known tools: %v", err)
	}
	env, err := cfg.JobPodEnvelope()
	if err != nil {
		t.Fatalf("JobPodEnvelope(): %v", err)
	}
	if !env.Allows(toolenvelope.ToolExecuteCode) || !env.Allows(toolenvelope.ToolRunTests) {
		t.Errorf("envelope = %v, want both tools allowed", env.Tools())
	}
}
