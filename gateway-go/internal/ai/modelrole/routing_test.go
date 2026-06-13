package modelrole

import (
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/router"
)

func intPtr(i int) *int       { return &i }
func strPtr(s string) *string { return &s }

// TestRoutingProfileForModel_Layering covers the three resolution layers: a
// dual-mode model resolves to the builtin default + its capability toggle (the
// current main model's behavior), a model with no toggle stays inert, and a
// deneb.json routing block overrides per-model knobs.
func TestRoutingProfileForModel_Layering(t *testing.T) {
	def := router.DefaultProfile()
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel: "vllm/deepseek-v4-flash",
		Providers: map[string]ProviderResolved{
			// vllm: no routing override — must resolve to the builtin default
			// plus the deepseek capability toggle (today's behavior, unchanged).
			"vllm": {BaseURL: "https://vllm.example/v1"},
			// acme: a non-toggle provider that an operator turns ON and points
			// at a custom kwarg, with a tuned length gate.
			"acme": {
				BaseURL: "https://acme.example/v1",
				Routing: &RoutingOverride{
					Enabled:        boolPtr(true),
					ToggleKwarg:    strPtr("enable_thinking"),
					MaxSimpleRunes: intPtr(80),
				},
			},
			// muff: an operator explicitly DISABLES routing for a model that
			// would otherwise be eligible by its toggle.
			"vllm-muffled": {
				BaseURL: "https://vllm2.example/v1",
				Routing: &RoutingOverride{Enabled: boolPtr(false)},
			},
		},
	})

	t.Run("dual-mode model = builtin default + capability toggle", func(t *testing.T) {
		p := reg.RoutingProfileForModel("vllm", "deepseek-v4-flash")
		if !p.Enabled || p.ToggleKwarg != "thinking" {
			t.Fatalf("deepseek-v4 must route with the 'thinking' toggle, got %+v", p)
		}
		if p.MaxSimpleRunes != def.MaxSimpleRunes || p.StepCeilingTurn != def.StepCeilingTurn ||
			p.ObservationRunes != def.ObservationRunes || p.CumulativeRunes != def.CumulativeRunes ||
			p.HeavyHistoryRunes != def.HeavyHistoryRunes {
			t.Errorf("unconfigured model must keep the default thresholds, got %+v", p)
		}
	})

	t.Run("no toggle, no override = inert", func(t *testing.T) {
		if p := reg.RoutingProfileForModel("openrouter", "some-remote-model"); p.Enabled {
			t.Errorf("a model with no toggle must resolve inert, got %+v", p)
		}
	})

	t.Run("override enables a model and retunes a knob", func(t *testing.T) {
		p := reg.RoutingProfileForModel("acme", "acme-thinker")
		if !p.Enabled || p.ToggleKwarg != "enable_thinking" {
			t.Fatalf("override must enable routing with the custom toggle, got %+v", p)
		}
		if p.MaxSimpleRunes != 80 {
			t.Errorf("override MaxSimpleRunes = %d, want 80", p.MaxSimpleRunes)
		}
		// Unspecified knobs stay at the default.
		if p.CumulativeRunes != def.CumulativeRunes {
			t.Errorf("unspecified knob drifted: CumulativeRunes = %d, want %d", p.CumulativeRunes, def.CumulativeRunes)
		}
	})

	t.Run("override can disable an otherwise-eligible model", func(t *testing.T) {
		// The toggle resolution would set Enabled (vllm-prefixed + deepseek
		// name), but the explicit Enabled=false override wins.
		if p := reg.RoutingProfileForModel("vllm-muffled", "deepseek-v4-flash"); p.Enabled {
			t.Errorf("explicit Enabled=false must win, got %+v", p)
		}
	})
}
