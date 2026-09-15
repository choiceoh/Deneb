package modelrole

import (
	"log/slog"
	"strings"
	"testing"
)

const (
	engineA = "http://10.0.0.5:8000/metrics"
	engineB = "http://10.0.0.6:8000/metrics"
)

func TestEngineDown_MarksUnhealthyWithoutAStreak(t *testing.T) {
	reg := healthTestRegistry()
	if reg.ModelUnhealthy("local") || reg.EngineDown("local") {
		t.Fatal("precondition: a model nothing has reported is healthy")
	}

	reg.SetEngineDown(engineA, []string{"local", " local-low ", ""})

	for _, m := range []string{"local", "local-low"} {
		if !reg.EngineDown(m) {
			t.Errorf("EngineDown(%q) = false after its engine was reported down", m)
		}
		// Unhealthy at once — the chat pipeline's skip-to-fallback reads this,
		// and it must not wait for three paid-for failures.
		if !reg.ModelUnhealthy(m) {
			t.Errorf("ModelUnhealthy(%q) = false; an engine reported down needs no streak", m)
		}
	}
	if reg.EngineDown("cloud") || reg.ModelUnhealthy("cloud") {
		t.Error("a model the engine does not serve was marked down")
	}
}

func TestEngineDown_RecoveryClearsStateAndDropsStaleStreak(t *testing.T) {
	reg := healthTestRegistry()
	reg.SetEngineDown(engineA, []string{"local"})
	// Failures accumulated against the dead engine (e.g. calls that raced the
	// probe) must not keep traffic on the fallback once it is back.
	for range unhealthyStreak + 2 {
		reg.RecordModelFailure("local")
	}

	reg.SetEngineDown(engineA, nil)

	if reg.EngineDown("local") {
		t.Error("EngineDown still true after the engine recovered")
	}
	if reg.ModelUnhealthy("local") {
		t.Error("ModelUnhealthy still true after recovery: the stale streak would hold the model on fallback for the whole cooldown")
	}
}

func TestEngineDown_RecoveryLeavesUnrelatedBreakersAlone(t *testing.T) {
	reg := healthTestRegistry()
	for range unhealthyStreak {
		reg.RecordModelFailure("cloud")
	}
	reg.SetEngineDown(engineA, []string{"local"})
	reg.SetEngineDown(engineA, nil)

	if !reg.ModelUnhealthy("cloud") {
		t.Error("an engine recovering reset the breaker of a model it never served")
	}
}

func TestEngineDown_ModelServedByTwoEnginesStaysDownUntilBothRecover(t *testing.T) {
	reg := healthTestRegistry()
	reg.SetEngineDown(engineA, []string{"shared"})
	reg.SetEngineDown(engineB, []string{"shared"})
	for range unhealthyStreak {
		reg.RecordModelFailure("shared")
	}

	reg.SetEngineDown(engineA, nil)
	if !reg.EngineDown("shared") {
		t.Fatal("EngineDown false while the other engine still reports it down")
	}
	if !reg.ModelUnhealthy("shared") {
		t.Fatal("streak dropped while the model is still down elsewhere")
	}

	reg.SetEngineDown(engineB, nil)
	if reg.EngineDown("shared") || reg.ModelUnhealthy("shared") {
		t.Fatal("model still unhealthy after every engine recovered")
	}
}

func TestEngineDown_ReplacingTheSetReleasesRemovedModels(t *testing.T) {
	reg := healthTestRegistry()
	reg.SetEngineDown(engineA, []string{"old-name", "kept"})
	// The router config renamed an entry during the outage.
	reg.SetEngineDown(engineA, []string{"new-name", "kept"})

	if reg.EngineDown("old-name") {
		t.Error("a model no longer listed is still reported down")
	}
	for _, m := range []string{"new-name", "kept"} {
		if !reg.EngineDown(m) {
			t.Errorf("EngineDown(%q) = false after the set was replaced", m)
		}
	}
}

func TestEngineDown_NilAndEmptyInputsAreNoOps(t *testing.T) {
	var nilReg *Registry
	nilReg.SetEngineDown(engineA, []string{"m"})
	if nilReg.EngineDown("m") {
		t.Error("nil registry reported a model down")
	}

	reg := healthTestRegistry()
	reg.SetEngineDown("", []string{"m"})
	if reg.EngineDown("m") {
		t.Error("an empty endpoint registered a down set")
	}
	if reg.EngineDown("") {
		t.Error("empty model reported down")
	}
}

func TestModelsAt_ListsRolesPointedStraightAtTheServer(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:        "direct/glm-local",
		LightweightModel: "direct/glm-local",
		TinyModel:        "direct/glm-local-low",
		FallbackModel:    "cloud/glm-cloud",
		Providers: map[string]ProviderResolved{
			"direct": {BaseURL: "http://10.0.0.5:8000/v1", APIKey: "k"},
			"cloud":  {BaseURL: "https://api.example.com/v1", APIKey: "k"},
		},
	})

	got := strings.Join(reg.ModelsAt("10.0.0.5:8000"), ",")
	if got != "glm-local,glm-local-low" {
		t.Fatalf("ModelsAt = %q, want the two direct roles sorted and de-duplicated", got)
	}
	if other := reg.ModelsAt("10.0.0.9:8000"); len(other) != 0 {
		t.Fatalf("ModelsAt(other server) = %v, want none", other)
	}
	if none := reg.ModelsAt(""); none != nil {
		t.Fatalf("ModelsAt(\"\") = %v, want nil", none)
	}
}
