package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	providercore "github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type basicPlugin struct {
	id    string
	label string
	auth  []providercore.AuthMethod
}

func (p basicPlugin) ID() string                             { return p.id }
func (p basicPlugin) Label() string                          { return p.label }
func (p basicPlugin) AuthMethods() []providercore.AuthMethod { return p.auth }

type aliasOnlyPlugin struct {
	basicPlugin
	aliases []string
}

func (p aliasOnlyPlugin) Aliases() []string { return p.aliases }

type runtimePlugin struct {
	catalogPlugin
	prepared *providercore.PreparedAuth
	authErr  error
	wait     bool
}

type waitingCatalogPlugin struct{ basicPlugin }

func (p waitingCatalogPlugin) Catalog(ctx context.Context, _ providercore.CatalogContext) (*providercore.CatalogResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (p runtimePlugin) PrepareRuntimeAuth(ctx context.Context, _ providercore.RuntimeAuthContext) (*providercore.PreparedAuth, error) {
	if p.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return p.prepared, p.authErr
}

func decodeProviderPayload[T any](t *testing.T, resp *protocol.ResponseFrame) T {
	t.Helper()
	rpctest.MustOK(t, resp)
	var got T
	if err := json.Unmarshal(resp.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v (raw=%s)", err, resp.Payload)
	}
	return got
}

func registerPlugins(t *testing.T, plugins ...providercore.Plugin) *providercore.Registry {
	t.Helper()
	r := providercore.NewRegistry()
	for _, p := range plugins {
		if err := r.Register(p); err != nil {
			t.Fatalf("register %s: %v", p.ID(), err)
		}
	}
	return r
}

func TestMethodSurfacesAndNilRegistrySemantics(t *testing.T) {
	if got := Methods(Deps{}); got != nil {
		t.Fatalf("Methods nil registry = %#v", got)
	}
	methods := Methods(Deps{Providers: providercore.NewRegistry()})
	want := []string{"providers.list", "providers.get", "providers.catalog", "providers.auth.prepare"}
	for _, name := range want {
		if methods[name] == nil {
			t.Errorf("missing %s", name)
		}
	}
	if len(methods) != len(want) {
		t.Fatalf("provider method count = %d", len(methods))
	}
	models := ModelsMethods(ModelsDeps{})
	if len(models) != 1 || models["models.list"] == nil {
		t.Fatalf("ModelsMethods = %#v", models)
	}
	got := decodeProviderPayload[struct {
		Models []providercore.CatalogEntry `json:"models"`
	}](t, rpctest.Call(models, "models.list", nil))
	if got.Models == nil || len(got.Models) != 0 {
		t.Fatalf("nil registry models = %#v", got.Models)
	}
}

func TestSerializePluginReturnsFreshMapsAndOmitsAbsentFields(t *testing.T) {
	plain := basicPlugin{id: "plain", label: "Plain", auth: []providercore.AuthMethod{{ID: "key", Kind: "api_key"}}}
	serialized := serializePlugin(plain)
	if serialized["id"] != "plain" || serialized["label"] != "Plain" || serialized["auth"] == nil {
		t.Fatalf("plain serialization = %#v", serialized)
	}
	if _, ok := serialized["aliases"]; ok {
		t.Fatalf("plain serialization has aliases: %#v", serialized)
	}
	if _, ok := serialized["capabilities"]; ok {
		t.Fatalf("plain serialization has capabilities: %#v", serialized)
	}

	full := catalogPlugin{id: "full", label: "Full"}
	first := serializePlugin(full)
	if first["aliases"] == nil || first["capabilities"] == nil {
		t.Fatalf("full serialization = %#v", first)
	}
	first["id"] = "mutated"
	second := serializePlugin(full)
	if second["id"] != "full" {
		t.Fatalf("serialization reused caller map: %#v", second)
	}
}

func TestProvidersListDeterministicAndEmptyNonNil(t *testing.T) {
	empty := Methods(Deps{Providers: providercore.NewRegistry()})
	got := decodeProviderPayload[struct {
		Providers []map[string]any `json:"providers"`
	}](t, rpctest.Call(empty, "providers.list", nil))
	if got.Providers == nil || len(got.Providers) != 0 {
		t.Fatalf("empty providers = %#v", got.Providers)
	}

	r := registerPlugins(
		t,
		basicPlugin{id: "zeta", label: "Zeta"},
		aliasOnlyPlugin{basicPlugin: basicPlugin{id: "alpha", label: "Alpha"}, aliases: []string{"a", "first"}},
		basicPlugin{id: "middle", label: "Middle"},
	)
	methods := Methods(Deps{Providers: r})
	got = decodeProviderPayload[struct {
		Providers []map[string]any `json:"providers"`
	}](t, rpctest.Call(methods, "providers.list", map[string]any{"ignored": true}))
	ids := make([]string, 0, len(got.Providers))
	for _, entry := range got.Providers {
		ids = append(ids, entry["id"].(string))
	}
	if want := []string{"alpha", "middle", "zeta"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("provider order = %#v, want %#v", ids, want)
	}
}

func TestProvidersGetValidationNormalizationAliasesAndMalformed(t *testing.T) {
	r := registerPlugins(
		t,
		aliasOnlyPlugin{basicPlugin: basicPlugin{id: "open-ai", label: "Open AI"}, aliases: []string{"OpenAI", "oa"}},
	)
	methods := Methods(Deps{Providers: r})
	for _, id := range []string{"open-ai", "OPEN-AI", "OpenAI", "oa"} {
		got := decodeProviderPayload[map[string]any](t, rpctest.Call(methods, "providers.get", map[string]any{"id": id}))
		if got["id"] != "open-ai" {
			t.Errorf("get %q = %#v", id, got)
		}
	}
	for _, params := range []map[string]any{{}, {"id": ""}, {"id": "missing"}, {"id": "OPEN_AI"}} {
		resp := rpctest.Call(methods, "providers.get", params)
		rpctest.MustErr(t, resp)
		want := protocol.ErrMissingParam
		if params["id"] == "missing" || params["id"] == "OPEN_AI" {
			want = protocol.ErrNotFound
		}
		if resp.Error.Code != want {
			t.Errorf("get params %#v code = %q, want %q", params, resp.Error.Code, want)
		}
	}
	resp := methods["providers.get"](context.Background(), &protocol.RequestFrame{ID: "bad", Params: json.RawMessage(`{`)})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("malformed get code = %q", resp.Error.Code)
	}
}

func TestProvidersCatalogSuccessUnknownPlainErrorNilAndCancellation(t *testing.T) {
	entries := []providercore.CatalogEntry{
		{Provider: "good", ModelID: "m1", Label: "Model 1", ContextWindow: 128000, Reasoning: true, APIType: "openai"},
		{Provider: "good", ModelID: "m2"},
	}
	r := registerPlugins(
		t,
		catalogPlugin{id: "good", label: "Good", entries: entries},
		catalogPlugin{id: "broken", label: "Broken", err: errors.New("offline")},
		catalogPlugin{id: "nil", label: "Nil", entries: nil},
		basicPlugin{id: "plain", label: "Plain"},
		waitingCatalogPlugin{basicPlugin: basicPlugin{id: "waiting", label: "Waiting"}},
	)
	methods := Methods(Deps{Providers: r})
	good := decodeProviderPayload[providercore.CatalogResult](t, rpctest.Call(methods, "providers.catalog", map[string]any{"provider": "good"}))
	if !reflect.DeepEqual(good.Entries, entries) {
		t.Fatalf("good catalog = %#v", good)
	}
	for _, id := range []string{"", "missing", "plain", "broken", "nil"} {
		got := decodeProviderPayload[providercore.CatalogResult](t, rpctest.Call(methods, "providers.catalog", map[string]any{"provider": id}))
		if got.Entries == nil || len(got.Entries) != 0 {
			t.Errorf("catalog %q = %#v", id, got)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, _ := json.Marshal(map[string]any{"provider": "waiting"})
	start := time.Now()
	resp := methods["providers.catalog"](ctx, &protocol.RequestFrame{ID: "cancel", Params: raw})
	cancelled := decodeProviderPayload[providercore.CatalogResult](t, resp)
	if cancelled.Entries == nil || len(cancelled.Entries) != 0 || time.Since(start) > time.Second {
		t.Fatalf("cancelled catalog = %#v duration=%s", cancelled, time.Since(start))
	}
}

func TestProvidersAuthPrepareRejectsMissingProviderAndReturnsAuth(t *testing.T) {
	r := registerPlugins(t, basicPlugin{id: "plain", label: "Plain"})
	methods := Methods(Deps{Providers: r})
	resp := rpctest.Call(methods, "providers.auth.prepare", map[string]any{"apiKey": "key"})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("missing provider code = %q", resp.Error.Code)
	}
	passthrough := decodeProviderPayload[providercore.PreparedAuth](t, rpctest.Call(methods, "providers.auth.prepare", map[string]any{
		"provider": "plain", "modelId": "m", "apiKey": "direct-key", "profileId": "p",
	}))
	if passthrough.APIKey != "direct-key" || passthrough.BaseURL != "" || passthrough.ExpiresAt != 0 {
		t.Fatalf("nil manager passthrough = %#v", passthrough)
	}

	prepared := &providercore.PreparedAuth{APIKey: "managed-key", BaseURL: "https://managed", ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}
	managedRegistry := registerPlugins(t, runtimePlugin{
		catalogPlugin: catalogPlugin{id: "managed", label: "Managed"},
		prepared:      prepared,
	})
	authManager := providercore.NewAuthManager(managedRegistry, slog.New(slog.NewTextHandler(io.Discard, nil)))
	methods = Methods(Deps{Providers: managedRegistry, AuthManager: authManager})
	got := decodeProviderPayload[providercore.PreparedAuth](t, rpctest.Call(methods, "providers.auth.prepare", map[string]any{
		"provider": "managed", "apiKey": "old", "profileId": "work",
	}))
	if !reflect.DeepEqual(got, *prepared) {
		t.Fatalf("managed auth = %#v, want %#v", got, *prepared)
	}
	stored := authManager.Resolve("managed", "work")
	if stored == nil || stored.APIKey != "managed-key" || stored.BaseURL != "https://managed" || stored.ExpiresAt != prepared.ExpiresAt {
		t.Fatalf("stored auth = %#v", stored)
	}
}

func TestAuthManagerPluginFailureAndNilResultDegradeToInputKey(t *testing.T) {
	r := registerPlugins(
		t,
		runtimePlugin{catalogPlugin: catalogPlugin{id: "broken-auth"}, authErr: errors.New("oauth down")},
		runtimePlugin{catalogPlugin: catalogPlugin{id: "nil-auth"}, prepared: nil},
	)
	am := providercore.NewAuthManager(r, slog.New(slog.NewTextHandler(io.Discard, nil)))
	methods := Methods(Deps{Providers: r, AuthManager: am})
	for _, id := range []string{"broken-auth", "nil-auth", "unknown"} {
		got := decodeProviderPayload[providercore.PreparedAuth](t, rpctest.Call(methods, "providers.auth.prepare", map[string]any{
			"provider": id, "apiKey": "fallback-" + id,
		}))
		if got.APIKey != "fallback-"+id || got.BaseURL != "" {
			t.Errorf("auth fallback %q = %#v", id, got)
		}
	}
}

func TestModelsListSkipsNonCatalogErrorsNilAndPreservesProviderOrder(t *testing.T) {
	r := registerPlugins(
		t,
		catalogPlugin{id: "zeta", entries: []providercore.CatalogEntry{{Provider: "zeta", ModelID: "z1"}, {Provider: "zeta", ModelID: "z2"}}},
		catalogPlugin{id: "alpha", entries: []providercore.CatalogEntry{{Provider: "alpha", ModelID: "a1"}}},
		catalogPlugin{id: "broken", err: errors.New("offline")},
		catalogPlugin{id: "nil", entries: nil},
		basicPlugin{id: "plain"},
	)
	methods := ModelsMethods(ModelsDeps{Providers: r})
	got := decodeProviderPayload[struct {
		Models []providercore.CatalogEntry `json:"models"`
	}](t, rpctest.Call(methods, "models.list", nil))
	want := []providercore.CatalogEntry{{Provider: "alpha", ModelID: "a1"}, {Provider: "zeta", ModelID: "z1"}, {Provider: "zeta", ModelID: "z2"}}
	if !reflect.DeepEqual(got.Models, want) {
		t.Fatalf("models = %#v, want %#v", got.Models, want)
	}

	empty := registerPlugins(t, basicPlugin{id: "plain-only"})
	emptyGot := decodeProviderPayload[struct {
		Models []providercore.CatalogEntry `json:"models"`
	}](t, rpctest.Call(ModelsMethods(ModelsDeps{Providers: empty}), "models.list", nil))
	if emptyGot.Models == nil || len(emptyGot.Models) != 0 {
		t.Fatalf("non-catalog-only models = %#v", emptyGot.Models)
	}
}

func TestConcurrentProviderReadsAreDeterministic(t *testing.T) {
	r := registerPlugins(
		t,
		catalogPlugin{id: "b", entries: []providercore.CatalogEntry{{Provider: "b", ModelID: "b1"}}},
		catalogPlugin{id: "a", entries: []providercore.CatalogEntry{{Provider: "a", ModelID: "a1"}}},
	)
	providers := Methods(Deps{Providers: r})
	models := ModelsMethods(ModelsDeps{Providers: r})
	const workers = 40
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if i%3 == 0 {
				resp := rpctest.Call(providers, "providers.get", map[string]any{"id": "a"})
				if resp == nil || resp.Error != nil {
					t.Errorf("get %d = %#v", i, resp)
				}
			} else if i%3 == 1 {
				resp := rpctest.Call(providers, "providers.list", nil)
				if resp == nil || resp.Error != nil {
					t.Errorf("list %d = %#v", i, resp)
				}
			} else {
				resp := rpctest.Call(models, "models.list", nil)
				if resp == nil || resp.Error != nil {
					t.Errorf("models %d = %#v", i, resp)
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()
	listed := decodeProviderPayload[struct {
		Providers []map[string]any `json:"providers"`
	}](t, rpctest.Call(providers, "providers.list", nil))
	ids := []string{listed.Providers[0]["id"].(string), listed.Providers[1]["id"].(string)}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatalf("provider IDs after concurrent reads = %#v", ids)
	}
}

func TestMalformedAuthAndCatalogParamsReturnStableResponses(t *testing.T) {
	r := registerPlugins(t, basicPlugin{id: "plain"})
	methods := Methods(Deps{Providers: r})
	for _, method := range []string{"providers.auth.prepare", "providers.catalog"} {
		resp := methods[method](context.Background(), &protocol.RequestFrame{ID: "bad", Method: method, Params: json.RawMessage(`{"provider":`)})
		rpctest.MustErr(t, resp)
		if resp.Error.Code != protocol.ErrInvalidRequest {
			t.Errorf("%s malformed code = %q", method, resp.Error.Code)
		}
	}
}

func TestCatalogPluginFixtureIDsPreserveUniqueness(t *testing.T) {
	plugins := []providercore.Plugin{
		basicPlugin{id: "a"}, aliasOnlyPlugin{basicPlugin: basicPlugin{id: "b"}, aliases: []string{"bee"}}, catalogPlugin{id: "c"},
	}
	ids := make([]string, 0, len(plugins))
	for _, p := range plugins {
		ids = append(ids, p.ID())
	}
	sort.Strings(ids)
	if strings.Join(ids, ",") != "a,b,c" {
		t.Fatalf("fixture IDs = %#v", ids)
	}
}
