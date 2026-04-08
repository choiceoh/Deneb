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
		t.Errorf("got %q, want ID 'gpt-4'", result.ID)
	}

	// First matching template wins.
	result2 := FindCatalogTemplate(entries, "openai", []string{"nonexistent", "gpt-3.5-turbo"})
	if result2 == nil {
		t.Fatal("expected to find gpt-3.5-turbo")
	}
	if result2.ID != "gpt-3.5-turbo" {
		t.Errorf("got %q, want ID 'gpt-3.5-turbo'", result2.ID)
	}

	// Not found.
	result3 := FindCatalogTemplate(entries, "openai", []string{"nonexistent"})
	if result3 != nil {
		t.Errorf("got %v, want nil", result3)
	}

	// Different provider.
	result4 := FindCatalogTemplate(entries, "ANTHROPIC", []string{"claude-3"})
	if result4 == nil {
		t.Fatal("expected to find claude-3")
	}
}


func TestBuildSingleProviderAPIKeyCatalog(t *testing.T) {
	// No API key → nil.
	result := BuildSingleProviderAPIKeyCatalog(SingleProviderCatalogParams{
		ProviderID: "openai",
		APIKey:     "",
		BuildProvider: func() *ModelProviderCatalog {
			return &ModelProviderCatalog{ID: "openai", BaseURL: "https://api.openai.com"}
		},
	})
	if result != nil {
		t.Errorf("got %v, want nil with empty API key", result)
	}

	// With API key.
	result2 := BuildSingleProviderAPIKeyCatalog(SingleProviderCatalogParams{
		ProviderID: "openai",
		APIKey:     "sk-test-key",
		BuildProvider: func() *ModelProviderCatalog {
			return &ModelProviderCatalog{ID: "openai", BaseURL: "https://api.openai.com"}
		},
	})
	if result2 == nil {
		t.Fatal("expected non-nil result with API key")
	}
	if result2.Provider.APIKey != "sk-test-key" {
		t.Errorf("got %q, want API key 'sk-test-key'", result2.Provider.APIKey)
	}
	if result2.Provider.BaseURL != "https://api.openai.com" {
		t.Errorf("got %q, want base URL from builder", result2.Provider.BaseURL)
	}
}

func TestBuildSingleProviderAPIKeyCatalogWithExplicitBaseURL(t *testing.T) {
	config := map[string]any{
		"models": map[string]any{
			"providers": map[string]any{
				"openai": map[string]any{
					"baseUrl": "  https://custom.api.com  ",
				},
			},
		},
	}

	result := BuildSingleProviderAPIKeyCatalog(SingleProviderCatalogParams{
		Config:               config,
		ProviderID:           "openai",
		APIKey:               "sk-test",
		AllowExplicitBaseURL: true,
		BuildProvider: func() *ModelProviderCatalog {
			return &ModelProviderCatalog{ID: "openai", BaseURL: "https://default.com"}
		},
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Explicit baseUrl overrides the builder's.
	if result.Provider.BaseURL != "https://custom.api.com" {
		t.Errorf("got %q, want explicit base URL", result.Provider.BaseURL)
	}
}

func TestBuildPairedProviderAPIKeyCatalog(t *testing.T) {
	// No API key → nil.
	result := BuildPairedProviderAPIKeyCatalog(PairedProviderCatalogParams{
		ProviderID: "volcengine",
		APIKey:     "",
		BuildProviders: func() map[string]*ModelProviderCatalog {
			return map[string]*ModelProviderCatalog{
				"volcengine":      {ID: "volcengine"},
				"volcengine-plan": {ID: "volcengine-plan"},
			}
		},
	})
	if result != nil {
		t.Errorf("got %v, want nil with empty API key", result)
	}

	// With API key.
	result2 := BuildPairedProviderAPIKeyCatalog(PairedProviderCatalogParams{
		ProviderID: "volcengine",
		APIKey:     "vol-key",
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
		t.Fatalf("got %d, want 2 providers", len(result2.Providers))
	}
	for id, p := range result2.Providers {
		if p.APIKey != "vol-key" {
			t.Errorf("provider %q API key = %q, want 'vol-key'", id, p.APIKey)
		}
	}
}
