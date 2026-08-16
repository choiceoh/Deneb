package server

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

func TestRegisterEarlyMethodsReturnsValidationErrorBeforeStoresInitialize(t *testing.T) {
	srv := &Server{MemorySubsystem: &MemorySubsystem{}}
	hub := rpcutil.NewGatewayHub(rpcutil.HubConfig{})

	err := srv.registerEarlyMethods(hub, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "hub validation") {
		t.Fatalf("registerEarlyMethods() error = %v, want hub validation error", err)
	}
	if hub.Phase() != rpcutil.PhaseEarly {
		t.Fatalf("hub phase = %d, want PhaseEarly", hub.Phase())
	}
	if srv.nativeSyncStore != nil || srv.workFeedStore != nil || srv.contactsStore != nil {
		t.Fatal("capability stores initialized before hub validation")
	}
}

func TestEarlyCapabilityHelpers_PreserveMethodNames(t *testing.T) {
	srv := &Server{
		ServerRPC:        &ServerRPC{},
		MemorySubsystem:  &MemorySubsystem{},
		ChatManager:      &ChatManager{},
		GenesisSubsystem: &GenesisSubsystem{},
	}
	hub := rpcutil.NewGatewayHub(rpcutil.HubConfig{})

	tests := []struct {
		name string
		got  map[string]rpcutil.HandlerFunc
		want []string
	}{
		{
			name: "native gateway",
			got:  srv.earlyMiniappGatewayMethods(hub),
			want: []string{"miniapp.client.hello", "miniapp.ping", "miniapp.whoami"},
		},
		{
			name: "projects",
			got:  srv.earlyProjectMethods(hub),
			want: []string{
				"miniapp.project.digests",
				"miniapp.project.linked",
			},
		},
		{
			name: "skills",
			got:  srv.earlySkillMethods(),
			want: []string{
				"miniapp.skills.delete",
				"miniapp.skills.detail",
				"miniapp.skills.lifecycle",
				"miniapp.skills.list",
				"miniapp.skills.update",
			},
		},
		{
			name: "self improvement",
			got:  srv.earlySelfImprovementMethods(),
			want: []string{
				"miniapp.self_improvement_coding.dispatch",
				"miniapp.self_improvement_coding.impact",
				"miniapp.self_improvement_coding.list",
				"miniapp.self_improvement_coding.record",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sortedMethodNames(tt.got); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("method names = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEarlyProviderMethodsReturnsMethodsOnlyWithRegistry(t *testing.T) {
	srv := &Server{ServerRPC: &ServerRPC{}}
	if got := srv.earlyProviderMethods(); got != nil {
		t.Fatalf("provider groups without registry = %v, want nil", got)
	}

	srv.providers = provider.NewRegistry()
	var names []string
	for _, methods := range srv.earlyProviderMethods() {
		names = append(names, sortedMethodNames(methods)...)
	}
	sort.Strings(names)
	want := []string{
		"models.list",
		"providers.auth.prepare",
		"providers.catalog",
		"providers.get",
		"providers.list",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("provider method names = %v, want %v", names, want)
	}
}

func sortedMethodNames(methods map[string]rpcutil.HandlerFunc) []string {
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
