package server

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestCollectBaseHealthPreservesRequiredContract(t *testing.T) {
	srv := testutil.Must(New(":0"))
	srv.ChatManager = &ChatManager{}
	srv.GenesisSubsystem = &GenesisSubsystem{}
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
	if subsystems["core"] != "go" || subsystems["local_ai"] != "off" || subsystems["embedding"] != "off" {
		t.Fatalf("unexpected subsystem contract: %v", subsystems)
	}

	workers, ok := health["workers"].(map[string]int)
	if !ok || workers["processes"] != 0 || workers["cron"] != 0 {
		t.Fatalf("unexpected workers contract: %T %v", health["workers"], health["workers"])
	}
	if _, ok := health["rpc"].(map[string]any); !ok {
		t.Fatalf("rpc type = %T, want map[string]any", health["rpc"])
	}
}

func TestPropusHealthCompatibilityAliasSharesSnapshot(t *testing.T) {
	srv := testutil.Must(New(":0"))
	srv.GenesisSubsystem = &GenesisSubsystem{}
	if section, ok := srv.collectPropusHealth(); ok || section != nil {
		t.Fatalf("unwired Propus tracker returned section=%v, ok=%v", section, ok)
	}

	health := map[string]any{}
	section := map[string]any{"state": "healthy"}
	attachPropusHealth(health, section)

	propus, ok := health["propus"].(map[string]any)
	if !ok {
		t.Fatalf("propus type = %T, want map[string]any", health["propus"])
	}
	legacy, ok := health["self_evolution"].(map[string]any)
	if !ok {
		t.Fatalf("self_evolution type = %T, want map[string]any", health["self_evolution"])
	}
	propus["alias_probe"] = true
	if legacy["alias_probe"] != true {
		t.Fatalf("propus aliases do not share one snapshot: propus=%v self_evolution=%v", propus, legacy)
	}
}
