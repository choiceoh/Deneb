package provider

import (
	"testing"
)

func TestNormalizeText(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  hello  ", "hello"},
		{"", ""},
		{"  ", ""},
		{"no trim", "no trim"},
	}
	for _, tt := range tests {
		got := normalizeText(tt.input)
		if got != tt.want {
			t.Errorf("normalizeText(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeTextList(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  int // expected length, -1 for nil
	}{
		{"nil input", nil, -1},
		{"empty input", []string{}, -1},
		{"all whitespace", []string{"  ", ""}, -1},
		{"valid items", []string{"a", "b"}, 2},
		{"with duplicates", []string{"a", "b", "a"}, 2},
		{"with whitespace", []string{" a ", " b ", " a "}, 2},
		{"mixed", []string{"a", "", " b ", "  "}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeTextList(tt.input)
			if tt.want == -1 {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %d items, got nil", tt.want)
			}
			if len(got) != tt.want {
				t.Errorf("expected %d items, got %d (%v)", tt.want, len(got), got)
			}
		})
	}
}

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
			t.Errorf("expected 0 diagnostics, got %d: %v", len(diags), diags)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 methods, got %d", len(result))
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
			t.Errorf("expected 1 error diagnostic, got %v", diags)
		}
		if len(result) != 0 {
			t.Errorf("expected 0 valid methods, got %d", len(result))
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
			t.Errorf("expected 1 error diagnostic for duplicate, got %v", diags)
		}
		if len(result) != 1 {
			t.Errorf("expected 1 valid method (first), got %d", len(result))
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
			t.Errorf("expected label to default to ID, got %q", result[0].Label)
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
			t.Errorf("expected 0 diagnostics, got %v", diags)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.ID != "openai" {
			t.Errorf("expected trimmed ID 'openai', got %q", result.ID)
		}
		if result.Label != "OpenAI" {
			t.Errorf("expected trimmed label 'OpenAI', got %q", result.Label)
		}
		if len(result.Aliases) != 1 || result.Aliases[0] != "oai" {
			t.Errorf("expected deduplicated aliases [oai], got %v", result.Aliases)
		}
		if len(result.EnvVars) != 1 || result.EnvVars[0] != "OPENAI_API_KEY" {
			t.Errorf("expected filtered env vars [OPENAI_API_KEY], got %v", result.EnvVars)
		}
		if result.DocsPath != "/providers/openai" {
			t.Errorf("expected trimmed docsPath, got %q", result.DocsPath)
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
			t.Errorf("expected nil for missing ID, got %v", result)
		}
		if len(diags) != 1 || diags[0].Level != "error" {
			t.Errorf("expected error diagnostic for missing ID, got %v", diags)
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
			t.Errorf("expected label to default to ID 'test', got %q", result.Label)
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

func TestNormalizeProviderWizard(t *testing.T) {
	t.Run("nil wizard", func(t *testing.T) {
		result, diags := NormalizeProviderWizard(NormalizeWizardParams{
			ProviderID: "test",
			PluginID:   "test",
			Source:     "test",
			Auth:       nil,
			Wizard:     nil,
		})
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
		if len(diags) != 0 {
			t.Errorf("expected 0 diagnostics, got %v", diags)
		}
	})

	t.Run("no auth methods warns for model picker", func(t *testing.T) {
		wizard := &WizardDef{
			ModelPicker: &WizardModelPickerDef{Label: "Pick a model"},
		}
		result, diags := NormalizeProviderWizard(NormalizeWizardParams{
			ProviderID: "test",
			PluginID:   "test",
			Source:     "test",
			Auth:       nil,
			Wizard:     wizard,
		})
		if result != nil {
			t.Errorf("expected nil (no auth), got %v", result)
		}
		found := false
		for _, d := range diags {
			if d.Level == "warn" {
				found = true
			}
		}
		if !found {
			t.Error("expected warn diagnostic for model picker without auth")
		}
	})

	t.Run("valid wizard with setup and model picker", func(t *testing.T) {
		auth := []ProviderAuthMethodDef{{ID: "api_key"}}
		wizard := &WizardDef{
			Setup: &WizardSetupDef{
				ChoiceLabel: " My Choice ",
				MethodID:    "api_key",
			},
			ModelPicker: &WizardModelPickerDef{
				Label: " Select Model ",
			},
		}
		result, diags := NormalizeProviderWizard(NormalizeWizardParams{
			ProviderID: "test",
			PluginID:   "test",
			Source:     "test",
			Auth:       auth,
			Wizard:     wizard,
		})
		if len(diags) != 0 {
			t.Errorf("expected 0 diagnostics, got %v", diags)
		}
		if result == nil {
			t.Fatal("expected result")
		}
		if result.Setup == nil {
			t.Fatal("expected setup")
		}
		if result.Setup.ChoiceLabel != "My Choice" {
			t.Errorf("expected trimmed choice label, got %q", result.Setup.ChoiceLabel)
		}
		if result.Setup.MethodID != "api_key" {
			t.Errorf("expected methodID 'api_key', got %q", result.Setup.MethodID)
		}
		if result.ModelPicker == nil {
			t.Fatal("expected model picker")
		}
		if result.ModelPicker.Label != "Select Model" {
			t.Errorf("expected trimmed model picker label, got %q", result.ModelPicker.Label)
		}
	})
}
