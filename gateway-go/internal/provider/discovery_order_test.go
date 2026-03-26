package provider

import (
	"testing"
)

func TestNormalizeDiscoveryResultNil(t *testing.T) {
	result := NormalizeDiscoveryResult("openai", nil)
	if len(result) != 0 {
		t.Errorf("expected empty map for nil result, got %v", result)
	}
}

func TestNormalizeDiscoveryResultSingleProvider(t *testing.T) {
	result := NormalizeDiscoveryResult("openai", &DiscoveryCatalogResult{
		Provider: &DiscoveryProviderConfig{ID: "openai", BaseURL: "https://api.openai.com"},
	})
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
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
		t.Fatalf("expected 2 entries (empty key skipped), got %d", len(result))
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
		t.Errorf("expected normalized key 'amazon-bedrock', got %v", result)
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
		t.Fatalf("expected 2 simple providers, got %d", len(simple))
	}
	if simple[0].Label != "Alpha" || simple[1].Label != "Bravo" {
		t.Errorf("expected alphabetical order [Alpha, Bravo], got [%s, %s]",
			simple[0].Label, simple[1].Label)
	}

	// Check other groups.
	if len(grouped[DiscoveryOrderProfile]) != 1 {
		t.Errorf("expected 1 profile provider, got %d", len(grouped[DiscoveryOrderProfile]))
	}
	if len(grouped[DiscoveryOrderPaired]) != 0 {
		t.Errorf("expected 0 paired providers, got %d", len(grouped[DiscoveryOrderPaired]))
	}
	if len(grouped[DiscoveryOrderLate]) != 1 {
		t.Errorf("expected 1 late provider, got %d", len(grouped[DiscoveryOrderLate]))
	}
}

func TestAllDiscoveryOrders(t *testing.T) {
	expected := []DiscoveryOrder{
		DiscoveryOrderSimple,
		DiscoveryOrderProfile,
		DiscoveryOrderPaired,
		DiscoveryOrderLate,
	}
	if len(AllDiscoveryOrders) != len(expected) {
		t.Fatalf("AllDiscoveryOrders has %d entries, want %d", len(AllDiscoveryOrders), len(expected))
	}
	for i, order := range expected {
		if AllDiscoveryOrders[i] != order {
			t.Errorf("AllDiscoveryOrders[%d] = %q, want %q", i, AllDiscoveryOrders[i], order)
		}
	}
}
