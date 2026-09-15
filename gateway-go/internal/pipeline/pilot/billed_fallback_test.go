package pilot

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// withTinyPaidRegistry points the helper at a registry whose tiny chain is
// tiny → free fallback → paid fallback → billed lightweight → fallback, all on
// the harness server.
func withTinyPaidRegistry(t *testing.T) {
	t.Helper()
	prev := pkgRegistry
	pkgRegistry = modelrole.NewRegistryWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), modelrole.RegistryOptions{
		MainModel:             "test/main",
		TinyModel:             "test/tiny",
		TinyFallbackModel:     "test/tinyfree",
		TinyFallbackPaidModel: "test/tinypaid",
		LightweightModel:      "test/light-billed",
		FallbackModel:         "test/fallback",
		Providers:             map[string]modelrole.ProviderResolved{"test": {BaseURL: pilotHarness.server.URL, APIKey: "test-key"}},
		// The paid rung bills like the lightweight one; only its role differs.
		MeteredModels: map[string]bool{"light-billed": true, "tinypaid": true},
	})
	t.Cleanup(func() { pkgRegistry = prev })
}

// The free fallback's rate limit is what the paid one is for.
func TestCallRoleLLMPaidTinyFallbackAnswersWhenFreeIsRateLimited(t *testing.T) {
	resetPilotHarness()
	withTinyPaidRegistry(t)
	setPilotMode("tiny", "http-error")
	setPilotMode("tinyfree", "rate-limited-inband")

	got, err := CallRoleLLM(context.Background(), modelrole.RoleTiny, "system", "user", 32)
	if err != nil || got != "reply:tinypaid" {
		t.Fatalf("CallRoleLLM = %q/%v, want the paid fallback's reply", got, err)
	}
	if models := requestedModels(); !reflect.DeepEqual(models, []string{"tiny", "tinyfree", "tinypaid"}) {
		t.Fatalf("requests = %v, want tiny, the free fallback once (429 moves on), then the paid one", models)
	}
}

// Past the paid rung, a billed rung nobody agreed to is skipped — walking the
// raw chain is how tiny calls ended up on a metered model.
func TestCallRoleLLMSkipsBilledRungNobodyAgreedTo(t *testing.T) {
	resetPilotHarness()
	withTinyPaidRegistry(t)
	setPilotMode("tiny", "http-error")
	setPilotMode("tinyfree", "http-error")
	setPilotMode("tinypaid", "http-error")

	got, err := CallRoleLLM(context.Background(), modelrole.RoleTiny, "system", "user", 32)
	if err != nil || got != "reply:fallback" {
		t.Fatalf("CallRoleLLM = %q/%v, want the unbilled fallback's reply", got, err)
	}
	if models := requestedModels(); !reflect.DeepEqual(models, []string{"tiny", "tinyfree", "tinypaid", "fallback"}) {
		t.Fatalf("requests = %v, want the billed lightweight never asked", models)
	}
}
