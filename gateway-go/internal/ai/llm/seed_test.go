package llm

import (
	"encoding/json"
	"testing"
)

func TestAnthropicRequestOmitsOpenAISamplingSeed(t *testing.T) {
	seed := int64(42001)
	body, err := buildAnthropicRequestBody(ChatRequest{
		Model: "test", MaxTokens: 32, Seed: &seed,
		Messages: []Message{NewTextMessage("user", "hello")},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if _, exists := wire["seed"]; exists {
		t.Fatalf("Anthropic request leaked unsupported seed: %s", body)
	}
}

func TestOpenAIRequestCarriesSamplingSeed(t *testing.T) {
	seed := int64(42001)
	body, err := json.Marshal(openAIRequest{Model: "test", Seed: &seed})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if got, ok := wire["seed"].(float64); !ok || int64(got) != seed {
		t.Fatalf("OpenAI seed = %#v, want %d", wire["seed"], seed)
	}
}
