package modelrole

import (
	"log/slog"
	"testing"
	"time"
)

func healthTestRegistry() *Registry {
	// Non-vllm provider so construction performs no network probe.
	return NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:        "zai/glm-5-turbo",
		LightweightModel: "zai/glm-5-turbo",
		FallbackModel:    "zai/glm-5-turbo",
		TinyModel:        "zai/glm-5-turbo",
		AnalysisModel:    "zai/glm-5-turbo",
	})
}

func TestModelHealth_BreakerOpensAfterStreak(t *testing.T) {
	reg := healthTestRegistry()
	for i := range unhealthyStreak - 1 {
		reg.RecordModelFailure("m")
		if reg.ModelUnhealthy("m") {
			t.Fatalf("breaker open after %d failures, want >= %d", i+1, unhealthyStreak)
		}
	}
	reg.RecordModelFailure("m")
	if !reg.ModelUnhealthy("m") {
		t.Fatalf("breaker closed after %d failures", unhealthyStreak)
	}
}

func TestModelHealth_SuccessResetsStreak(t *testing.T) {
	reg := healthTestRegistry()
	for range unhealthyStreak {
		reg.RecordModelFailure("m")
	}
	reg.RecordModelSuccess("m")
	if reg.ModelUnhealthy("m") {
		t.Fatal("success must close the breaker")
	}
	// Streak restarts from zero, not from the prior count.
	reg.RecordModelFailure("m")
	if reg.ModelUnhealthy("m") {
		t.Fatal("single failure after a success must not reopen the breaker")
	}
}

func TestModelHealth_CooldownHalfOpens(t *testing.T) {
	reg := healthTestRegistry()
	for range unhealthyStreak {
		reg.RecordModelFailure("m")
	}
	// Age the last failure past the cooldown window.
	reg.health.mu.Lock()
	reg.health.models["m"].lastFailure = time.Now().Add(-unhealthyCooldown - time.Second)
	reg.health.mu.Unlock()
	if reg.ModelUnhealthy("m") {
		t.Fatal("breaker must half-open after the cooldown")
	}
	// A new failure inside the window re-arms it immediately (streak kept).
	reg.RecordModelFailure("m")
	if !reg.ModelUnhealthy("m") {
		t.Fatal("failure during half-open must re-open the breaker")
	}
}

func TestModelHealth_EmptyAndUnknownModels(t *testing.T) {
	reg := healthTestRegistry()
	reg.RecordModelFailure("")
	reg.RecordModelSuccess("") // must not panic
	if reg.ModelUnhealthy("") || reg.ModelUnhealthy("never-seen") {
		t.Fatal("empty/unknown models are always healthy")
	}
}
