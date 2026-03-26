package provider_test

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/provider"
)

// testProvider is a minimal provider plugin for testing.
type testProvider struct {
	id    string
	label string
}

func (p *testProvider) ID() string    { return p.id }
func (p *testProvider) Label() string { return p.label }
func (p *testProvider) AuthMethods() []provider.AuthMethod {
	return []provider.AuthMethod{
		{ID: "api_key", Label: "API Key", Kind: "api_key", Hint: "TEST_API_KEY"},
	}
}

func TestProtocolAdapterListProviders(t *testing.T) {
	reg := provider.NewRegistry()
	if err := reg.Register(&testProvider{id: "test", label: "Test"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	adapter := provider.NewProtocolAdapter(reg)
	providers := adapter.ListProviders()
	if len(providers) != 1 {
		t.Fatalf("ListProviders() returned %d, want 1", len(providers))
	}
	if providers[0].ID != "test" {
		t.Errorf("ID = %q, want %q", providers[0].ID, "test")
	}
	if providers[0].Label != "Test" {
		t.Errorf("Label = %q, want %q", providers[0].Label, "Test")
	}
	if len(providers[0].EnvVars) != 1 || providers[0].EnvVars[0] != "TEST_API_KEY" {
		t.Errorf("EnvVars = %v, want [TEST_API_KEY]", providers[0].EnvVars)
	}
}

func TestProtocolAdapterListCatalogEntries(t *testing.T) {
	reg := provider.NewRegistry()
	adapter := provider.NewProtocolAdapter(reg)
	entries := adapter.ListCatalogEntries()
	if entries != nil {
		t.Errorf("ListCatalogEntries() = %v, want nil", entries)
	}
}
