package chat

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
)

type providerPriorityRuntimePlugin struct {
	calls []provider.RuntimeAuthContext
}

func (*providerPriorityRuntimePlugin) ID() string                         { return "google" }
func (*providerPriorityRuntimePlugin) Label() string                      { return "provider priority probe" }
func (*providerPriorityRuntimePlugin) AuthMethods() []provider.AuthMethod { return nil }

func (p *providerPriorityRuntimePlugin) PrepareRuntimeAuth(
	_ context.Context,
	auth provider.RuntimeAuthContext,
) (*provider.PreparedAuth, error) {
	p.calls = append(p.calls, auth)
	if auth.APIKey == "runtime-error-key" {
		return nil, errors.New("runtime exchange failed")
	}
	baseURL := "https://runtime-default.invalid/v1"
	switch auth.APIKey {
	case "config-key":
		baseURL = "https://runtime-config.invalid/v1"
	case "managed-key":
		baseURL = "https://runtime-managed.invalid/v1"
	}
	return &provider.PreparedAuth{
		APIKey:  "prepared-" + auth.APIKey,
		BaseURL: baseURL,
	}, nil
}

func TestResolveClient_PreservesProviderSourcePriority(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pluginRegistry := provider.NewRegistry()
	plugin := &providerPriorityRuntimePlugin{}
	if err := pluginRegistry.Register(plugin); err != nil {
		t.Fatal(err)
	}
	runtimeResolver := provider.NewProviderRuntimeResolver(pluginRegistry, logger)

	authManager := provider.NewAuthManager(nil, logger)
	authManager.Store(&provider.ManagedCredential{
		ProviderID: "google",
		APIKey:     "managed-key",
		BaseURL:    "https://managed.invalid/v1",
	})
	authManager.Store(&provider.ManagedCredential{
		ProviderID: "custom",
		APIKey:     "custom-key",
		BaseURL:    "https://managed-custom.invalid/v1",
	})
	authManager.Store(&provider.ManagedCredential{
		ProviderID: "zai",
		APIKey:     "default-zai-key",
		BaseURL:    "https://managed-zai.invalid/v1",
	})

	registry := modelrole.NewRegistryWithOptions(logger, modelrole.RegistryOptions{
		MainModel:        "google/main-model",
		LightweightModel: "google/light-model",
		FallbackModel:    "google/fallback-model",
		Providers: map[string]modelrole.ProviderResolved{
			"google": {
				BaseURL: "https://registry.invalid/v1",
				APIKey:  "registry-key",
				APIMode: llm.APIModeOpenAI,
			},
		},
	})
	preconfigured := llm.NewClient("https://preconfigured.invalid/v1", "fallback-key")

	t.Run("provider config and runtime auth beat managed registry and fallback clients", func(t *testing.T) {
		plugin.calls = nil
		client := resolveClient(context.Background(), runDeps{
			providerConfigs: map[string]ProviderConfig{
				"google": {
					BaseURL: "https://config.invalid/v1",
					APIKey:  "config-key",
					API:     "anthropic-messages",
				},
			},
			authManager:     authManager,
			providerRuntime: runtimeResolver,
			registry:        registry,
			llmClient:       preconfigured,
		}, "google", logger)

		if client == nil {
			t.Fatal("resolveClient returned nil")
		}
		if got, want := client.BaseURL(), "https://runtime-config.invalid/v1"; got != want {
			t.Fatalf("BaseURL = %q, want config runtime override %q", got, want)
		}
		if got := client.APIMode(); got != llm.APIModeAnthropic {
			t.Fatalf("APIMode = %q, want explicit config mode %q", got, llm.APIModeAnthropic)
		}
		if len(plugin.calls) != 1 || plugin.calls[0].APIKey != "config-key" {
			t.Fatalf("runtime auth calls = %+v, want one call with config credentials", plugin.calls)
		}
	})

	t.Run("runtime auth failure keeps configured credentials and source priority", func(t *testing.T) {
		plugin.calls = nil
		client := resolveClient(context.Background(), runDeps{
			providerConfigs: map[string]ProviderConfig{
				"google": {
					BaseURL: "https://config-after-auth-error.invalid/v1",
					APIKey:  "runtime-error-key",
				},
			},
			authManager:     authManager,
			providerRuntime: runtimeResolver,
			registry:        registry,
			llmClient:       preconfigured,
		}, "google", logger)

		if got, want := client.BaseURL(), "https://config-after-auth-error.invalid/v1"; got != want {
			t.Fatalf("BaseURL = %q, want original configured URL %q", got, want)
		}
		if len(plugin.calls) != 1 || plugin.calls[0].APIKey != "runtime-error-key" {
			t.Fatalf("runtime auth calls = %+v, want one failed config exchange", plugin.calls)
		}
	})

	t.Run("managed credentials and runtime auth beat registry and fallback clients", func(t *testing.T) {
		plugin.calls = nil
		client := resolveClient(context.Background(), runDeps{
			authManager:     authManager,
			providerRuntime: runtimeResolver,
			registry:        registry,
			llmClient:       preconfigured,
		}, "google", logger)

		if got, want := client.BaseURL(), "https://runtime-managed.invalid/v1"; got != want {
			t.Fatalf("BaseURL = %q, want managed runtime override %q", got, want)
		}
		if len(plugin.calls) != 1 || plugin.calls[0].APIKey != "managed-key" {
			t.Fatalf("runtime auth calls = %+v, want one call with managed credentials", plugin.calls)
		}
	})

	t.Run("invalid configured endpoint falls through to managed credentials", func(t *testing.T) {
		client := resolveClient(context.Background(), runDeps{
			providerConfigs: map[string]ProviderConfig{"custom": {}},
			authManager:     authManager,
			registry:        registry,
			llmClient:       preconfigured,
		}, "custom", logger)
		if got, want := client.BaseURL(), "https://managed-custom.invalid/v1"; got != want {
			t.Fatalf("BaseURL = %q, want managed fallback %q", got, want)
		}
	})

	t.Run("registry role client is cached and beats preconfigured fallback", func(t *testing.T) {
		deps := runDeps{registry: registry, llmClient: preconfigured}
		first := resolveClient(context.Background(), deps, "google", logger)
		second := resolveClient(context.Background(), deps, "google", logger)
		if first == nil || first != second {
			t.Fatalf("registry clients = %p and %p, want the same cached client", first, second)
		}
		if first == preconfigured {
			t.Fatal("preconfigured fallback won over matching registry role")
		}
		if got, want := first.BaseURL(), "https://registry.invalid/v1"; got != want {
			t.Fatalf("BaseURL = %q, want registry URL %q", got, want)
		}
	})

	t.Run("unknown provider reaches preconfigured fallback", func(t *testing.T) {
		client := resolveClient(context.Background(), runDeps{
			registry:  registry,
			llmClient: preconfigured,
		}, "unknown-provider", logger)
		if client != preconfigured {
			t.Fatalf("client = %p, want preconfigured fallback %p", client, preconfigured)
		}
	})

	t.Run("empty provider defaults managed lookup to zai", func(t *testing.T) {
		client := resolveClient(context.Background(), runDeps{
			authManager: authManager,
			registry:    registry,
			llmClient:   preconfigured,
		}, "", logger)
		if got, want := client.BaseURL(), "https://managed-zai.invalid/v1"; got != want {
			t.Fatalf("BaseURL = %q, want default zai managed URL %q", got, want)
		}
		if got := client.APIMode(); got != llm.APIModeAnthropic {
			t.Fatalf("APIMode = %q, want zai default %q", got, llm.APIModeAnthropic)
		}
	})
}
