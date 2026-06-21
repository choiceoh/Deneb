package gmailpoll

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// The extractor json_schemas are raw string literals, so a typo wouldn't fail
// compilation — it would only surface at runtime as an invalid request body that
// silently falls back to json_object (losing the guided-decoding guarantee).
// These tests are the compile-time-ish guard: valid JSON + the strict OpenAI
// wrapper shape + the exact enums the downstream code depends on.

// extractorSchemas is every json_schema this package sends strict, keyed by name.
// (The deal + thread extractors deliberately stay on json_object — see
// callLocalLLMJSON — so they have no schema here.)
var extractorSchemas = map[string]json.RawMessage{
	"wiki_facts":            wikiFactsSchema,
	"action_items":          actionItemsSchema,
	"attachment_selections": attachGateSchema,
}

// schemaWrapper is the OpenAI structured-output envelope each schema must carry.
type schemaWrapper struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

func TestExtractorSchemas_ValidStrictWrapper(t *testing.T) {
	for key, raw := range extractorSchemas {
		if !json.Valid(raw) {
			t.Errorf("%s: schema is not valid JSON", key)
			continue
		}
		var w schemaWrapper
		if err := json.Unmarshal(raw, &w); err != nil {
			t.Errorf("%s: unmarshal wrapper: %v", key, err)
			continue
		}
		if w.Name != key {
			t.Errorf("%s: wrapper name = %q, want %q (map key must match the schema's own name)", key, w.Name, key)
		}
		if !w.Strict {
			t.Errorf("%s: strict must be true", key)
		}
		if w.Schema["type"] != "object" {
			t.Errorf("%s: schema.type = %v, want object", key, w.Schema["type"])
		}
		// Strict structured outputs require additionalProperties:false on the root
		// object (and the probe confirmed vLLM honors it).
		if ap, ok := w.Schema["additionalProperties"].(bool); !ok || ap {
			t.Errorf("%s: schema.additionalProperties must be false, got %v", key, w.Schema["additionalProperties"])
		}
	}
}

// TestExtractorSchemas_WireShape proves a schema threaded through ResponseFormat
// marshals to the {type:json_schema, json_schema:{...}} body the live probe
// verified against the production qwen vLLM.
func TestExtractorSchemas_WireShape(t *testing.T) {
	rf := &llm.ResponseFormat{Type: "json_schema", JSONSchema: actionItemsSchema}
	raw, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal ResponseFormat: %v", err)
	}
	var got struct {
		Type       string         `json:"type"`
		JSONSchema *schemaWrapper `json:"json_schema"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal wire body: %v", err)
	}
	if got.Type != "json_schema" {
		t.Errorf("wire type = %q, want json_schema", got.Type)
	}
	if got.JSONSchema == nil || got.JSONSchema.Name != "action_items" {
		t.Errorf("wire json_schema wrapper missing/wrong: %+v", got.JSONSchema)
	}
}

// enumOf pulls the enum []string for a leaf property path schema.properties[...]
// (optionally nested through an array's items). Returns nil if absent.
func leafEnum(t *testing.T, raw json.RawMessage, walk func(root map[string]any) any) []string {
	t.Helper()
	var w schemaWrapper
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	node := walk(w.Schema)
	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	rawEnum, ok := m["enum"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(rawEnum))
	for _, e := range rawEnum {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestActionPrioritySchema_Enum(t *testing.T) {
	// The headline invariant: priority is exactly high|medium|low — the set the
	// downstream high-priority calendar-proposal gate keys on.
	got := leafEnum(t, actionItemsSchema, func(root map[string]any) any {
		props := root["properties"].(map[string]any)
		actions := props["actions"].(map[string]any)
		items := actions["items"].(map[string]any)
		return items["properties"].(map[string]any)["priority"]
	})
	assertSameSet(t, "priority", got, []string{"high", "medium", "low"})
}

func TestWikiFactTypeSchema_Enum(t *testing.T) {
	got := leafEnum(t, wikiFactsSchema, func(root map[string]any) any {
		props := root["properties"].(map[string]any)
		facts := props["facts"].(map[string]any)
		items := facts["items"].(map[string]any)
		return items["properties"].(map[string]any)["type"]
	})
	assertSameSet(t, "fact type", got, []string{"person", "org", "project", "deal", "decision", "deadline"})
}

func assertSameSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s enum = %v, want %v", label, got, want)
	}
	set := make(map[string]bool, len(got))
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Errorf("%s enum missing %q (got %v)", label, w, got)
		}
	}
}
