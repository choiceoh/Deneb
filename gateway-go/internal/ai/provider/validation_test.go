package provider

import (
	"testing"
)



func TestNormalizeProviderAuthMethods(t *testing.T) {
	t.Run("valid methods", func(t *testing.T) {
		auth := []ProviderAuthMethodDef{
			{ID: "api_key", Label: "API Key", Kind: "api_key"},
			{ID: "oauth", Label: "OAuth", Kind: "oauth"},
		}
		result, diags := NormalizeProviderAuthMethods(NormalizeAuthParams{
			ProviderID: "test",
			PluginID:   "test-plugin",
			Source:     "test",
			Auth:       auth,
		})
		if len(diags) != 0 {
			t.Errorf("got %d: %v, want 0 diagnostics", len(diags), diags)
		}
		if len(result) != 2 {
			t.Fatalf("got %d, want 2 methods", len(result))
		}
	})

	t.Run("missing ID", func(t *testing.T) {
		auth := []ProviderAuthMethodDef{
			{ID: "", Label: "No ID", Kind: "api_key"},
		}
		result, diags := NormalizeProviderAuthMethods(NormalizeAuthParams{
			ProviderID: "test",
			PluginID:   "test-plugin",
			Source:     "test",
			Auth:       auth,
		})
		if len(diags) != 1 || diags[0].Level != "error" {
			t.Errorf("got %v, want 1 error diagnostic", diags)
		}
		if len(result) != 0 {
			t.Errorf("got %d, want 0 valid methods", len(result))
		}
	})

	t.Run("duplicate IDs", func(t *testing.T) {
		auth := []ProviderAuthMethodDef{
			{ID: "api_key", Label: "First", Kind: "api_key"},
			{ID: "api_key", Label: "Second", Kind: "api_key"},
		}
		result, diags := NormalizeProviderAuthMethods(NormalizeAuthParams{
			ProviderID: "test",
			PluginID:   "test-plugin",
			Source:     "test",
			Auth:       auth,
		})
		if len(diags) != 1 || diags[0].Level != "error" {
			t.Errorf("got %v, want 1 error diagnostic for duplicate", diags)
		}
		if len(result) != 1 {
			t.Errorf("got %d, want 1 valid method (first)", len(result))
		}
	})

	t.Run("label defaults to ID", func(t *testing.T) {
		auth := []ProviderAuthMethodDef{
			{ID: "my-method", Label: "", Kind: "api_key"},
		}
		result, _ := NormalizeProviderAuthMethods(NormalizeAuthParams{
			ProviderID: "test",
			PluginID:   "test-plugin",
			Source:     "test",
			Auth:       auth,
		})
		if len(result) != 1 {
			t.Fatal("expected 1 method")
		}
		if result[0].Label != "my-method" {
			t.Errorf("got %q, want label to default to ID", result[0].Label)
		}
	})
}

func TestNormalizeRegisteredProvider(t *testing.T) {
	t.Run("valid provider", func(t *testing.T) {
		provider := RegisteredProviderDef{
			ID:    " openai ",
			Label: " OpenAI ",
			Auth: []ProviderAuthMethodDef{
				{ID: "api_key", Label: "API Key", Kind: "api_key"},
			},
			Aliases:  []string{"oai", "oai"},
			EnvVars:  []string{"OPENAI_API_KEY", ""},
			DocsPath: " /providers/openai ",
		}
		result, diags := NormalizeRegisteredProvider(NormalizeProviderParams{
			PluginID: "openai-plugin",
			Source:   "test",
			Provider: provider,
		})
		if len(diags) != 0 {
			t.Errorf("got %v, want 0 diagnostics", diags)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.ID != "openai" {
			t.Errorf("got %q, want trimmed ID 'openai'", result.ID)
		}
		if result.Label != "OpenAI" {
			t.Errorf("got %q, want trimmed label 'OpenAI'", result.Label)
		}
		if len(result.Aliases) != 1 || result.Aliases[0] != "oai" {
			t.Errorf("got %v, want deduplicated aliases [oai]", result.Aliases)
		}
		if len(result.EnvVars) != 1 || result.EnvVars[0] != "OPENAI_API_KEY" {
			t.Errorf("got %v, want filtered env vars [OPENAI_API_KEY]", result.EnvVars)
		}
		if result.DocsPath != "/providers/openai" {
			t.Errorf("got %q, want trimmed docsPath", result.DocsPath)
		}
	})

	t.Run("missing ID", func(t *testing.T) {
		provider := RegisteredProviderDef{ID: "  ", Label: "No ID"}
		result, diags := NormalizeRegisteredProvider(NormalizeProviderParams{
			PluginID: "test",
			Source:   "test",
			Provider: provider,
		})
		if result != nil {
			t.Errorf("got %v, want nil for missing ID", result)
		}
		if len(diags) != 1 || diags[0].Level != "error" {
			t.Errorf("got %v, want error diagnostic for missing ID", diags)
		}
	})

	t.Run("label defaults to ID", func(t *testing.T) {
		provider := RegisteredProviderDef{
			ID:    "test",
			Label: "",
			Auth:  []ProviderAuthMethodDef{},
		}
		result, _ := NormalizeRegisteredProvider(NormalizeProviderParams{
			PluginID: "test",
			Source:   "test",
			Provider: provider,
		})
		if result.Label != "test" {
			t.Errorf("got %q, want label to default to ID 'test'", result.Label)
		}
	})

	t.Run("catalog+discovery warns", func(t *testing.T) {
		provider := RegisteredProviderDef{
			ID:           "test",
			Label:        "Test",
			Auth:         []ProviderAuthMethodDef{},
			HasCatalog:   true,
			HasDiscovery: true,
		}
		result, diags := NormalizeRegisteredProvider(NormalizeProviderParams{
			PluginID: "test",
			Source:   "test",
			Provider: provider,
		})
		if result == nil {
			t.Fatal("expected result")
		}
		found := false
		for _, d := range diags {
			if d.Level == "warn" {
				found = true
			}
		}
		if !found {
			t.Error("expected warning about catalog+discovery")
		}
		// Catalog wins.
		if !result.HasCatalog {
			t.Error("expected HasCatalog=true")
		}
		if result.HasDiscovery {
			t.Error("expected HasDiscovery=false when catalog present")
		}
	})
}
