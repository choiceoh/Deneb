package observe

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
)

func TestMethodsExposeNoMissingObservationEndpoints(t *testing.T) {
	local := Methods(Deps{})
	miniapp := MiniappMethods(Deps{})
	for _, suffix := range []string{"turn", "logs", "behavior", "health"} {
		if local["observe."+suffix] == nil {
			t.Errorf("missing observe.%s", suffix)
		}
		if miniapp["miniapp.observe."+suffix] == nil {
			t.Errorf("missing miniapp.observe.%s", suffix)
		}
	}
}

func TestNilDependenciesReturnExplicitEmptyState(t *testing.T) {
	methods := Methods(Deps{})

	rpctest.MustErr(t, rpctest.Call(methods, "observe.turn", nil))

	logs := rpctest.Call(methods, "observe.logs", nil)
	rpctest.MustOK(t, logs)
	logsResult := rpctest.Result[map[string]any](t, logs)
	if logsResult["captureDisabled"] != true || logsResult["count"] != float64(0) {
		t.Fatalf("logs result = %#v", logsResult)
	}

	behavior := rpctest.Call(methods, "observe.behavior", nil)
	rpctest.MustOK(t, behavior)
	behaviorResult := rpctest.Result[map[string]any](t, behavior)
	if behaviorResult["proactiveDecisions"] == nil || behaviorResult["backgroundErrors"] == nil {
		t.Fatalf("behavior result = %#v", behaviorResult)
	}

	health := rpctest.Call(methods, "observe.health", nil)
	rpctest.MustOK(t, health)
	healthResult := rpctest.Result[map[string]any](t, health)
	if healthResult["captureEnabled"] != false || healthResult["agentLogEnabled"] != false {
		t.Fatalf("health result = %#v", healthResult)
	}
}
