package modelrole

import (
	"log/slog"
	"testing"
)

// TestTierRoles_DefaultToLightweight verifies tiny/analysis fall back to the
// lightweight model when unconfigured — the prior single-tier behavior, so an
// existing deployment is unchanged until it opts in.
func TestTierRoles_DefaultToLightweight(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:        "zai/glm-5-turbo",
		LightweightModel: "vllm/light-model",
	})
	if got := reg.Model(RoleTiny); got != "light-model" {
		t.Errorf("RoleTiny model = %q, want lightweight default %q", got, "light-model")
	}
	if got := reg.Model(RoleAnalysis); got != "light-model" {
		t.Errorf("RoleAnalysis model = %q, want lightweight default %q", got, "light-model")
	}
}

// TestTierRoles_ExplicitOverride verifies tiny/analysis use their own model when
// configured, independently of lightweight.
func TestTierRoles_ExplicitOverride(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:        "zai/glm-5-turbo",
		LightweightModel: "vllm/light-model",
		TinyModel:        "vllm/tiny-model",
		AnalysisModel:    "vllm/analysis-model",
	})
	if got := reg.Model(RoleTiny); got != "tiny-model" {
		t.Errorf("RoleTiny model = %q, want %q", got, "tiny-model")
	}
	if got := reg.Model(RoleAnalysis); got != "analysis-model" {
		t.Errorf("RoleAnalysis model = %q, want %q", got, "analysis-model")
	}
	if got := reg.Model(RoleLightweight); got != "light-model" {
		t.Errorf("RoleLightweight model = %q, want %q", got, "light-model")
	}
}

// TestTierRoles_FallbackChains pins the tiny/analysis fallback ordering: each
// degrades to lightweight, then the shared fallback role.
func TestTierRoles_FallbackChains(t *testing.T) {
	reg := NewRegistry(slog.Default(), "zai/test", "gemma4")
	cases := []struct {
		role Role
		want []Role
	}{
		{RoleTiny, []Role{RoleTiny, RoleLightweight, RoleFallback}},
		{RoleAnalysis, []Role{RoleAnalysis, RoleLightweight, RoleFallback}},
	}
	for _, c := range cases {
		got := reg.FallbackChain(c.role)
		if len(got) != len(c.want) {
			t.Errorf("FallbackChain(%s) = %v, want %v", c.role, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("FallbackChain(%s)[%d] = %s, want %s", c.role, i, got[i], c.want[i])
			}
		}
	}
}
