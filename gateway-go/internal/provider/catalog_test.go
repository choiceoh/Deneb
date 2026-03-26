package provider

import (
	"testing"
)

func TestFindCatalogTemplate(t *testing.T) {
	entries := []CatalogTemplateEntry{
		{Provider: "openai", ID: "gpt-4"},
		{Provider: "openai", ID: "gpt-3.5-turbo"},
		{Provider: "anthropic", ID: "claude-3"},
	}

	// Found (case insensitive).
	result := FindCatalogTemplate(entries, "OpenAI", []string{"GPT-4"})
	if result == nil {
		t.Fatal("expected to find gpt-4")
	}
	if result.ID != "gpt-4" {
		t.Errorf("expected ID 'gpt-4', got %q", result.ID)
	}

	// First matching template wins.
	result2 := FindCatalogTemplate(entries, "openai", []string{"nonexistent", "gpt-3.5-turbo"})
	if result2 == nil {
		t.Fatal("expected to find gpt-3.5-turbo")
	}
	if result2.ID != "gpt-3.5-turbo" {
		t.Errorf("expected ID 'gpt-3.5-turbo', got %q", result2.ID)
	}

	// Not found.
	result3 := FindCatalogTemplate(entries, "openai", []string{"nonexistent"})
	if result3 != nil {
		t.Errorf("expected nil, got %v", result3)
	}

	// Different provider.
	result4 := FindCatalogTemplate(entries, "ANTHROPIC", []string{"claude-3"})
	if result4 == nil {
		t.Fatal("expected to find claude-3")
	}
}

func TestFindCatalogTemplateEmpty(t *testing.T) {
	result := FindCatalogTemplate(nil, "openai", []string{"gpt-4"})
	if result != nil {
		t.Errorf("expected nil for empty entries, got %v", result)
	}

	result2 := FindCatalogTemplate([]CatalogTemplateEntry{}, "openai", nil)
	if result2 != nil {
		t.Errorf("expected nil for empty template IDs, got %v", result2)
	}
}

func TestBuildSingleProviderApiKeyCatalog(t *testing.T) {
	// No API key → nil.
	result := BuildSingleProviderApiKeyCatalog(SingleProviderCatalogParams{
		ProviderID: "openai",
		ApiKey:     "",
		BuildProvider: func() *ModelProviderCatalog {
			return &ModelProviderCatalog{ID: "openai", BaseURL: "https://api.openai.com"}
		},
	})
	if result != nil {
		t.Errorf("expected nil with empty API key, got %v", result)
	}

	// With API key.
	result2 := BuildSingleProviderApiKeyCatalog(SingleProviderCatalogParams{
		ProviderID: "openai",
		ApiKey:     "sk-test-key",
		BuildProvider: func() *ModelProviderCatalog {
			return &ModelProviderCatalog{ID: "openai", BaseURL: "https://api.openai.com"}
		},
	})
	if result2 == nil {
		t.Fatal("expected non-nil result with API key")
	}
	if result2.Provider.ApiKey != "sk-test-key" {
		t.Errorf("expected API key 'sk-test-key', got %q", result2.Provider.ApiKey)
	}
	if result2.Provider.BaseURL != "https://api.openai.com" {
		t.Errorf("expected base URL from builder, got %q", result2.Provider.BaseURL)
	}
}

func TestBuildSingleProviderApiKeyCatalogWithExplicitBaseUrl(t *testing.T) {
	config := map[string]any{
		"models": map[string]any{
			"providers": map[string]any{
				"openai": map[string]any{
					"baseUrl": "  https://custom.api.com  ",
				},
			},
		},
	}

	result := BuildSingleProviderApiKeyCatalog(SingleProviderCatalogParams{
		Config:               config,
		ProviderID:           "openai",
		ApiKey:               "sk-test",
		AllowExplicitBaseUrl: true,
		BuildProvider: func() *ModelProviderCatalog {
			return &ModelProviderCatalog{ID: "openai", BaseURL: "https://default.com"}
		},
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Explicit baseUrl overrides the builder's.
	if result.Provider.BaseURL != "https://custom.api.com" {
		t.Errorf("expected explicit base URL, got %q", result.Provider.BaseURL)
	}
}

func TestBuildPairedProviderApiKeyCatalog(t *testing.T) {
	// No API key → nil.
	result := BuildPairedProviderApiKeyCatalog(PairedProviderCatalogParams{
		ProviderID: "volcengine",
		ApiKey:     "",
		BuildProviders: func() map[string]*ModelProviderCatalog {
			return map[string]*ModelProviderCatalog{
				"volcengine":      {ID: "volcengine"},
				"volcengine-plan": {ID: "volcengine-plan"},
			}
		},
	})
	if result != nil {
		t.Errorf("expected nil with empty API key, got %v", result)
	}

	// With API key.
	result2 := BuildPairedProviderApiKeyCatalog(PairedProviderCatalogParams{
		ProviderID: "volcengine",
		ApiKey:     "vol-key",
		BuildProviders: func() map[string]*ModelProviderCatalog {
			return map[string]*ModelProviderCatalog{
				"volcengine":      {ID: "volcengine"},
				"volcengine-plan": {ID: "volcengine-plan"},
			}
		},
	})
	if result2 == nil {
		t.Fatal("expected non-nil result with API key")
	}
	if len(result2.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(result2.Providers))
	}
	for id, p := range result2.Providers {
		if p.ApiKey != "vol-key" {
			t.Errorf("provider %q API key = %q, want 'vol-key'", id, p.ApiKey)
		}
	}
}
