package configresolve

import (
	"testing"
)

func TestProviderCatalogParsesConfigFileMatrix(t *testing.T) {
	t.Setenv("DENEB_TEST_PROVIDER_BASE", "http://env/v1")
	t.Setenv("DENEB_TEST_PROVIDER_KEY", "env-key")
	t.Setenv("DENEB_TEST_PROVIDER_MISSING", "")
	tests := []struct {
		name        string
		provider    string
		wantBase    string
		wantKey     string
		wantAPI     string
		wantContext int
	}{
		{
			name:        "base only",
			provider:    "{\"baseUrl\":\"http://example/v1\"}",
			wantBase:    "http://example/v1",
			wantKey:     "",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "base trimmed",
			provider:    "{\"baseUrl\":\"  http://example/v1  \"}",
			wantBase:    "http://example/v1",
			wantKey:     "",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "key only",
			provider:    "{\"apiKey\":\"secret\"}",
			wantBase:    "",
			wantKey:     "secret",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "key trimmed",
			provider:    "{\"apiKey\":\"  secret  \"}",
			wantBase:    "",
			wantKey:     "secret",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "api openai",
			provider:    "{\"api\":\"openai\"}",
			wantBase:    "",
			wantKey:     "",
			wantAPI:     "openai",
			wantContext: 0,
		},
		{
			name:        "api responses",
			provider:    "{\"api\":\"responses\"}",
			wantBase:    "",
			wantKey:     "",
			wantAPI:     "responses",
			wantContext: 0,
		},
		{
			name:        "context",
			provider:    "{\"contextWindow\":128000}",
			wantBase:    "",
			wantKey:     "",
			wantAPI:     "",
			wantContext: 128000,
		},
		{
			name:        "context zero",
			provider:    "{\"contextWindow\":0}",
			wantBase:    "",
			wantKey:     "",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "context negative",
			provider:    "{\"contextWindow\":-1}",
			wantBase:    "",
			wantKey:     "",
			wantAPI:     "",
			wantContext: -1,
		},
		{
			name:        "all simple",
			provider:    "{\"baseUrl\":\" http://x \",\"apiKey\":\" k \",\"api\":\"openai\",\"contextWindow\":42}",
			wantBase:    "http://x",
			wantKey:     "k",
			wantAPI:     "openai",
			wantContext: 42,
		},
		{
			name:        "env base",
			provider:    "{\"baseUrl\":\"${DENEB_TEST_PROVIDER_BASE}\"}",
			wantBase:    "http://env/v1",
			wantKey:     "",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "env key",
			provider:    "{\"apiKey\":\"${DENEB_TEST_PROVIDER_KEY}\"}",
			wantBase:    "",
			wantKey:     "env-key",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "missing env key",
			provider:    "{\"apiKey\":\"${DENEB_TEST_PROVIDER_MISSING}\"}",
			wantBase:    "",
			wantKey:     "",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "embedded env",
			provider:    "{\"baseUrl\":\"${DENEB_TEST_PROVIDER_BASE}/chat\"}",
			wantBase:    "http://env/v1/chat",
			wantKey:     "",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "literal dollar",
			provider:    "{\"apiKey\":\"plain-$-key\"}",
			wantBase:    "",
			wantKey:     "plain-key",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "unicode key",
			provider:    "{\"apiKey\":\"비밀\"}",
			wantBase:    "",
			wantKey:     "비밀",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "unicode base",
			provider:    "{\"baseUrl\":\"https://예시.한국\"}",
			wantBase:    "https://예시.한국",
			wantKey:     "",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "empty",
			provider:    "{}",
			wantBase:    "",
			wantKey:     "",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "null fields",
			provider:    "{\"baseUrl\":null,\"apiKey\":null}",
			wantBase:    "",
			wantKey:     "",
			wantAPI:     "",
			wantContext: 0,
		},
		{
			name:        "extra",
			provider:    "{\"x\":1,\"baseUrl\":\"http://x\",\"y\":2}",
			wantBase:    "http://x",
			wantKey:     "",
			wantAPI:     "",
			wantContext: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"models":{"providers":{"test":` + tc.provider + `}}}`
			logger := writeResolveConfig(t, body)
			raw := LoadProviderConfigs(logger)
			if len(raw) != 1 {
				t.Fatalf("raw providers=%v", raw)
			}
			if _, ok := raw["test"]; !ok {
				t.Fatalf("raw provider missing: %v", raw)
			}
			catalog := ProviderCatalog(logger)
			if len(catalog) != 1 {
				t.Fatalf("catalog=%v", catalog)
			}
			got := catalog["test"]
			if got.BaseURL != tc.wantBase {
				t.Errorf("BaseURL=%q want=%q", got.BaseURL, tc.wantBase)
			}
			if got.APIKey != tc.wantKey {
				t.Errorf("APIKey=%q want=%q", got.APIKey, tc.wantKey)
			}
			if got.APIMode != tc.wantAPI {
				t.Errorf("APIMode=%q want=%q", got.APIMode, tc.wantAPI)
			}
			if got.ContextWindow != tc.wantContext {
				t.Errorf("ContextWindow=%d want=%d", got.ContextWindow, tc.wantContext)
			}
		})
	}
}

func TestProviderCatalogCapabilityAndSamplingBoundary(t *testing.T) {
	body := `{"models":{"providers":{"test":{
		"reasoning":true,
		"vision":false,
		"promptCache":true,
		"temperature":0.25,
		"topP":0.9,
		"topK":40,
		"routing":{
			"enabled":true,
			"toggleKwarg":"thinking",
			"maxSimpleRunes":10,
			"stepCeilingTurn":2,
			"observationRunes":20,
			"cumulativeRunes":30,
			"heavyHistoryRunes":40
		}
	}}}}`
	logger := writeResolveConfig(t, body)
	got := ProviderCatalog(logger)["test"]
	if got.Reasoning == nil || !*got.Reasoning {
		t.Errorf("Reasoning=%v", got.Reasoning)
	}
	if got.Vision == nil || *got.Vision {
		t.Errorf("Vision=%v", got.Vision)
	}
	if got.PromptCache == nil || !*got.PromptCache {
		t.Errorf("PromptCache=%v", got.PromptCache)
	}
	if got.Temperature == nil || *got.Temperature != 0.25 {
		t.Errorf("Temperature=%v", got.Temperature)
	}
	if got.TopP == nil || *got.TopP != 0.9 {
		t.Errorf("TopP=%v", got.TopP)
	}
	if got.TopK == nil || *got.TopK != 40 {
		t.Errorf("TopK=%v", got.TopK)
	}
	if got.Routing == nil {
		t.Fatal("Routing=nil")
	}
	if got.Routing.Enabled == nil || !*got.Routing.Enabled {
		t.Errorf("Enabled=%v", got.Routing.Enabled)
	}
	if got.Routing.ToggleKwarg == nil || *got.Routing.ToggleKwarg != "thinking" {
		t.Errorf("Toggle=%v", got.Routing.ToggleKwarg)
	}
	if got.Routing.MaxSimpleRunes == nil || *got.Routing.MaxSimpleRunes != 10 {
		t.Errorf("Simple=%v", got.Routing.MaxSimpleRunes)
	}
	if got.Routing.StepCeilingTurn == nil || *got.Routing.StepCeilingTurn != 2 {
		t.Errorf("Ceiling=%v", got.Routing.StepCeilingTurn)
	}
	if got.Routing.ObservationRunes == nil || *got.Routing.ObservationRunes != 20 {
		t.Errorf("Observation=%v", got.Routing.ObservationRunes)
	}
	if got.Routing.CumulativeRunes == nil || *got.Routing.CumulativeRunes != 30 {
		t.Errorf("Cumulative=%v", got.Routing.CumulativeRunes)
	}
	if got.Routing.HeavyHistoryRunes == nil || *got.Routing.HeavyHistoryRunes != 40 {
		t.Errorf("History=%v", got.Routing.HeavyHistoryRunes)
	}
}
