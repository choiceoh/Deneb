package server

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverauto"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverchat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/servermail"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverwire/early"
)

// stubServer builds a partial Server with feature managers wired the same way
// New() does, for unit tests that should not boot the full gateway.
func stubServer() *Server {
	s := &Server{
		ServerRPC:     &ServerRPC{},
		ServerRuntime: &ServerRuntime{},
	}
	s.Mail = servermail.New(s)
	s.Chat = serverchat.New(s, s.Mail)
	s.Auto = serverauto.New(s)
	return s
}

func TestRegisterEarlyMethodsReturnsValidationErrorBeforeStoresInitialize(t *testing.T) {
	srv := stubServer()
	hub := rpcutil.NewGatewayHub(rpcutil.HubConfig{})

	err := srv.registerEarlyMethods(hub, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "hub validation") {
		t.Fatalf("registerEarlyMethods() error = %v, want hub validation error", err)
	}
	if hub.Phase() != rpcutil.PhaseEarly {
		t.Fatalf("hub phase = %d, want PhaseEarly", hub.Phase())
	}
	if srv.Mail.NativeSyncStore != nil || srv.Mail.WorkFeedStore != nil || srv.Mail.ContactsStore != nil {
		t.Fatal("capability stores initialized before hub validation")
	}
}

func TestEarlyCapabilityHelpers_PreserveMethodNames(t *testing.T) {
	srv := stubServer()
	hub := rpcutil.NewGatewayHub(rpcutil.HubConfig{})
	ports := srv.wirePorts()

	tests := []struct {
		name string
		got  map[string]rpcutil.HandlerFunc
		want []string
	}{
		{
			name: "native gateway",
			got:  early.EarlyMiniappGatewayMethods(ports, hub),
			want: []string{"miniapp.client.hello", "miniapp.ping", "miniapp.whoami"},
		},
		{
			name: "projects",
			got:  early.EarlyProjectMethods(ports, hub),
			want: []string{"miniapp.project.digests", "miniapp.project.linked"},
		},
		{
			name: "skills",
			got:  early.EarlySkillMethods(ports),
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
			got:  early.EarlySelfImprovementMethods(ports),
			want: []string{
				"miniapp.self_improvement_coding.dispatch",
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
	srv := stubServer()
	ports := srv.wirePorts()
	if got := early.EarlyProviderMethods(ports); got != nil {
		t.Fatalf("provider groups without registry = %v, want nil", got)
	}

	srv.providers = provider.NewRegistry()
	ports = srv.wirePorts()
	var names []string
	for _, methods := range early.EarlyProviderMethods(ports) {
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
