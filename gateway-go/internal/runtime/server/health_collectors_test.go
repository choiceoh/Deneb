package server

import (
	"testing"

	runtimehealth "github.com/choiceoh/deneb/gateway-go/internal/runtime/health"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverauto"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestCollectBaseHealthPreservesRequiredContract(t *testing.T) {
	// Isolate from the host's real cron store: New() opens the cron service at
	// DefaultCronStorePath(homeDir), so without this the workers contract
	// (cron==0) reads the operator's live task count and fails on any machine
	// with real crons (package convention — see server_test.go).
	t.Setenv("HOME", t.TempDir())
	srv := testutil.Must(New(":0"))
	// Isolate from the host's live local-AI/embedding daemons so the subsystem
	// contract stays "off" on every machine (New() otherwise probes 127.0.0.1).
	srv.Chat.LocalAIHub = nil
	srv.Chat.EmbeddingClient = nil
	health := srv.collectBaseHealth()

	wantKeys := map[string]struct{}{
		"status": {}, "version": {}, "model": {}, "uptime": {},
		"uptime_ms": {}, "subsystems": {}, "sessions": {}, "workers": {},
		"providers": {}, "rpc": {},
	}
	if len(health) != len(wantKeys) {
		t.Fatalf("base health keys = %v, want exactly %v", health, wantKeys)
	}
	for key := range wantKeys {
		if _, ok := health[key]; !ok {
			t.Errorf("base health missing required key %q: %v", key, health)
		}
	}
	if health["status"] != "ok" {
		t.Fatalf("base status = %v, want ok", health["status"])
	}

	subsystems, ok := health["subsystems"].(map[string]any)
	if !ok {
		t.Fatalf("subsystems type = %T, want map[string]any", health["subsystems"])
	}
	if subsystems["core"] != "go" {
		t.Fatalf("unexpected subsystem contract: %v", subsystems)
	}
	for _, key := range []string{"local_ai", "embedding"} {
		switch subsystems[key] {
		case "off", "ok", "unhealthy":
		default:
			t.Fatalf("unexpected %s status: %v", key, subsystems[key])
		}
	}

	workers, ok := health["workers"].(map[string]int)
	if !ok || workers["processes"] != 0 || workers["cron"] != 0 {
		t.Fatalf("unexpected workers contract: %T %v", health["workers"], health["workers"])
	}
	if _, ok := health["rpc"].(map[string]any); !ok {
		t.Fatalf("rpc type = %T, want map[string]any", health["rpc"])
	}
}

func TestPropusHealthAliasesReturnSameSnapshot(t *testing.T) {
	srv := &Server{Auto: &serverauto.Manager{}}
	if section, ok := runtimehealth.Propus(srv.GenesisTracker()); ok || section != nil {
		t.Fatalf("unwired Propus tracker returned section=%v, ok=%v", section, ok)
	}

	health := map[string]any{}
	section := &runtimehealth.PropusSection{}
	attachPropus(health, section)

	if health["propus"] != section || health["self_evolution"] != section {
		t.Fatalf("propus aliases do not share one snapshot: propus=%v self_evolution=%v", health["propus"], health["self_evolution"])
	}
}
