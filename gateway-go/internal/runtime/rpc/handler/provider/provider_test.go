package provider

import (
	"context"
	"errors"
	"testing"

	providercore "github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
)

type catalogPlugin struct {
	id      string
	label   string
	entries []providercore.CatalogEntry
	err     error
}

func (p catalogPlugin) ID() string    { return p.id }
func (p catalogPlugin) Label() string { return p.label }
func (p catalogPlugin) AuthMethods() []providercore.AuthMethod {
	return []providercore.AuthMethod{{ID: "token", Kind: "token"}}
}
func (p catalogPlugin) Aliases() []string { return []string{"alias-" + p.id} }
func (p catalogPlugin) Capabilities() providercore.Capabilities {
	return providercore.Capabilities{SupportsTools: true}
}

func (p catalogPlugin) Catalog(context.Context, providercore.CatalogContext) (*providercore.CatalogResult, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &providercore.CatalogResult{Entries: p.entries}, nil
}

func TestMethodsReturnNilAndEmptyModelsWithoutRegistry(t *testing.T) {
	if got := Methods(Deps{}); got != nil {
		t.Fatalf("Methods() = %v, want nil", got)
	}
	resp := rpctest.Call(ModelsMethods(ModelsDeps{}), "models.list", nil)
	rpctest.MustOK(t, resp)
	if models := rpctest.Result(t, resp)["models"].([]any); len(models) != 0 {
		t.Fatalf("models = %v", models)
	}
}

func TestProviderHandlersReturnSortedAndSerializedPlugins(t *testing.T) {
	registry := providercore.NewRegistry()
	for _, plugin := range []catalogPlugin{
		{id: "zeta", label: "Zeta", entries: []providercore.CatalogEntry{{Provider: "zeta", ModelID: "z1"}}},
		{id: "alpha", label: "Alpha", entries: []providercore.CatalogEntry{{Provider: "alpha", ModelID: "a1"}}},
	} {
		if err := registry.Register(plugin); err != nil {
			t.Fatal(err)
		}
	}
	methods := Methods(Deps{Providers: registry})

	list := rpctest.Call(methods, "providers.list", nil)
	rpctest.MustOK(t, list)
	providers := rpctest.Result(t, list)["providers"].([]any)
	first := providers[0].(map[string]any)
	if first["id"] != "alpha" || first["capabilities"] == nil || first["aliases"] == nil {
		t.Fatalf("first provider = %#v", first)
	}

	get := rpctest.Call(methods, "providers.get", map[string]string{"id": "alias-zeta"})
	rpctest.MustOK(t, get)
	if got := rpctest.Result(t, get)["id"]; got != "zeta" {
		t.Fatalf("provider id = %v", got)
	}

	models := rpctest.Call(ModelsMethods(ModelsDeps{Providers: registry}), "models.list", nil)
	rpctest.MustOK(t, models)
	if got := rpctest.Result(t, models)["models"].([]any); len(got) != 2 {
		t.Fatalf("models = %#v", got)
	}
}

func TestCatalogErrorsDegradeToEmpty(t *testing.T) {
	registry := providercore.NewRegistry()
	if err := registry.Register(catalogPlugin{id: "broken", err: errors.New("offline")}); err != nil {
		t.Fatal(err)
	}
	resp := rpctest.Call(Methods(Deps{Providers: registry}), "providers.catalog", map[string]string{"provider": "broken"})
	rpctest.MustOK(t, resp)
	entries := rpctest.Result(t, resp)["entries"].([]any)
	if len(entries) != 0 {
		t.Fatalf("entries = %#v", entries)
	}
}
