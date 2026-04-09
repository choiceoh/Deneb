package provider

import (
	"testing"
)

func TestNormalizeDiscoveryResultSingleProvider(t *testing.T) {
	result := NormalizeDiscoveryResult("openai", &DiscoveryCatalogResult{
		Provider: &DiscoveryProviderConfig{ID: "openai", BaseURL: "https://api.openai.com"},
	})
	if len(result) != 1 {
		t.Fatalf("got %d, want 1 entry", len(result))
	}
	if _, ok := result["openai"]; !ok {
		t.Error("expected key 'openai' in result")
	}
}

func TestNormalizeDiscoveryResultMultipleProviders(t *testing.T) {
	result := NormalizeDiscoveryResult("volcengine", &DiscoveryCatalogResult{
		Providers: map[string]*DiscoveryProviderConfig{
			"volcengine":      {ID: "volcengine"},
			"volcengine-plan": {ID: "volcengine-plan"},
			"":                {ID: "empty"}, // should be skipped
		},
	})
	if len(result) != 2 {
		t.Fatalf("got %d, want 2 entries (empty key skipped)", len(result))
	}
}

func TestNormalizeDiscoveryResultNormalizesKeys(t *testing.T) {
	result := NormalizeDiscoveryResult("bedrock", &DiscoveryCatalogResult{
		Providers: map[string]*DiscoveryProviderConfig{
			"aws-bedrock": {ID: "aws-bedrock"},
		},
	})
	// aws-bedrock normalizes to amazon-bedrock.
	if _, ok := result["amazon-bedrock"]; !ok {
		t.Errorf("got %v, want normalized key 'amazon-bedrock'", result)
	}
}

func TestGroupDiscoveryProvidersByOrder(t *testing.T) {
	providers := []DiscoveryProvider{
		{Label: "Bravo", CatalogOrder: DiscoveryOrderSimple},
		{Label: "Alpha", CatalogOrder: DiscoveryOrderSimple},
		{Label: "Charlie", CatalogOrder: DiscoveryOrderLate},
		{Label: "Delta", CatalogOrder: DiscoveryOrderProfile},
	}

	grouped := GroupDiscoveryProvidersByOrder(providers)

	// Check simple group is sorted.
	simple := grouped[DiscoveryOrderSimple]
	if len(simple) != 2 {
		t.Fatalf("got %d, want 2 simple providers", len(simple))
	}
	if simple[0].Label != "Alpha" || simple[1].Label != "Bravo" {
		t.Errorf("got [%s, %s], want alphabetical order [Alpha, Bravo]",
			simple[0].Label, simple[1].Label)
	}

	// Check other groups.
	if len(grouped[DiscoveryOrderProfile]) != 1 {
		t.Errorf("got %d, want 1 profile provider", len(grouped[DiscoveryOrderProfile]))
	}
	if len(grouped[DiscoveryOrderPaired]) != 0 {
		t.Errorf("got %d, want 0 paired providers", len(grouped[DiscoveryOrderPaired]))
	}
	if len(grouped[DiscoveryOrderLate]) != 1 {
		t.Errorf("got %d, want 1 late provider", len(grouped[DiscoveryOrderLate]))
	}
}
