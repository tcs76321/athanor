package llm

import (
	"testing"

	"github.com/tcs76321/athanor/internal/config"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Default()
	if err != nil {
		t.Fatalf("config.Default(): %v", err)
	}
	return cfg
}

func TestNewRegistryFromDefaults(t *testing.T) {
	cfg := testConfig(t)
	r, err := NewRegistry(cfg.Personas)
	if err != nil {
		t.Fatalf("NewRegistry(defaults) err = %v, want nil", err)
	}
	// Spot-check each §12.1 role resolved with its model and temperature.
	for role, wantModel := range map[string]string{
		RoleWide: "qwen2.5:7b", RoleTall: "qwen2.5-coder:32b", RoleMain: "mistral-nemo:12b",
		RoleSecurity: "phi3:3.8b", RoleAlternative: "llama3.1:8b",
	} {
		p, ok := r.Persona(role)
		if !ok {
			t.Errorf("role %q missing from registry", role)
			continue
		}
		if p.Model != wantModel {
			t.Errorf("persona %s model = %q, want %q", role, p.Model, wantModel)
		}
		if p.ContextTarget <= 0 {
			t.Errorf("persona %s context_target = %d, want > 0", role, p.ContextTarget)
		}
	}
	// Invariant §4.2-adjacent: security persona at Temp 0.0 in defaults.
	if p, _ := r.Persona(RoleSecurity); p.Temperature != 0.0 {
		t.Errorf("security persona temperature = %v, want 0.0", p.Temperature)
	}
}

func TestNewRegistryUnknownRole(t *testing.T) {
	cfg := testConfig(t)
	r, err := NewRegistry(cfg.Personas)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Persona("oracle"); ok {
		t.Error("Persona(unknown) reported ok, want false")
	}
}

func TestNewRegistryRejectsMissingModel(t *testing.T) {
	// A persona partially configured (context target but no model) must
	// fail registry construction loudly, not surface per-call later.
	cfg := testConfig(t)
	cfg.Personas.Main = config.PersonaConfig{ContextTarget: 8192}
	if _, err := NewRegistry(cfg.Personas); err == nil {
		t.Fatal("registry built with modelless main persona, want error")
	}

	// Entirely absent personas (zero config) must also fail — the registry
	// requires all five roles.
	empty := config.Personas{}
	if _, err := NewRegistry(empty); err == nil {
		t.Fatal("registry built from empty personas, want error")
	}
}

func TestRegistryAllContainsFiveRoles(t *testing.T) {
	cfg := testConfig(t)
	r, err := NewRegistry(cfg.Personas)
	if err != nil {
		t.Fatal(err)
	}
	all := r.All()
	if len(all) != 5 {
		t.Fatalf("All() has %d personas, want 5", len(all))
	}
	// All() must return a copy: mutating it must not corrupt the registry.
	p := all[RoleMain]
	p.Model = "tampered"
	all[RoleMain] = p
	if got, _ := r.Persona(RoleMain); got.Model == "tampered" {
		t.Error("All() leaked internal map state")
	}
}
