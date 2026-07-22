package mailanalysis

import (
	"encoding/json"
	"reflect"
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
	"status_signal":         statusSignalSchema,
}

// schemaWrapper is the OpenAI structured-output envelope each schema must carry.
type schemaWrapper struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

func TestExtractorSchemasValidStrictWrapperFormat(t *testing.T) {
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

// TestExtractorSchemasEncodeToWireFormat proves a schema threaded through
// ResponseFormat marshals to the {type:json_schema, json_schema:{...}} body the
// live probe verified against the production qwen vLLM.
func TestExtractorSchemasEncodeToWireFormat(t *testing.T) {
	rf := &llm.ResponseFormat{Type: "json_schema", JSONSchema: llm.FlexibleFromRaw(actionItemsSchema)}
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

func TestActionPrioritySchemaEnumContract(t *testing.T) {
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

// TestActionItemsSchemaMatchesLiveVerifiedContract locks the reflection-generated
// schema to the exact json_schema body that was confirmed working (HTTP 200 +
// conforming output) against the production qwen vLLM during development. If the
// generator or the ActionItem struct ever changes the wire shape, this fails —
// proving the derived schema still equals the live-verified contract.
func TestActionItemsSchemaMatchesLiveVerifiedContract(t *testing.T) {
	const liveVerified = `{
      "name": "action_items",
      "strict": true,
      "schema": {
        "type": "object",
        "properties": {
          "actions": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "title": {"type": "string"},
                "dueHint": {"type": "string"},
                "priority": {"type": "string", "enum": ["high", "medium", "low"]}
              },
              "required": ["title", "dueHint", "priority"],
              "additionalProperties": false
            }
          }
        },
        "required": ["actions"],
        "additionalProperties": false
      }
    }`
	t.Logf("generated action_items schema:\n%s", actionItemsSchema)
	var got, want any
	if err := json.Unmarshal(actionItemsSchema, &got); err != nil {
		t.Fatalf("generated schema invalid: %v", err)
	}
	if err := json.Unmarshal([]byte(liveVerified), &want); err != nil {
		t.Fatalf("reference schema invalid: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("generated schema diverged from the live-verified shape\n got: %s\nwant: %s", actionItemsSchema, liveVerified)
	}
}

func TestWikiFactTypeSchemaEnumContract(t *testing.T) {
	got := leafEnum(t, wikiFactsSchema, func(root map[string]any) any {
		props := root["properties"].(map[string]any)
		facts := props["facts"].(map[string]any)
		items := facts["items"].(map[string]any)
		return items["properties"].(map[string]any)["type"]
	})
	assertSameSet(t, "fact type", got, []string{"person", "org", "project", "deal", "decision", "deadline"})
}

func TestStatusSignalSchemaEnumContract(t *testing.T) {
	signalType := leafEnum(t, statusSignalSchema, func(root map[string]any) any {
		props := root["properties"].(map[string]any)
		sig := props["signal"].(map[string]any)
		return sig["properties"].(map[string]any)["signalType"]
	})
	assertSameSet(t, "signalType", signalType, []string{"결정", "리스크", "블로커", "의존성", "진행", "없음"})

	decisionStatus := leafEnum(t, statusSignalSchema, func(root map[string]any) any {
		props := root["properties"].(map[string]any)
		sig := props["signal"].(map[string]any)
		return sig["properties"].(map[string]any)["decisionStatus"]
	})
	assertSameSet(t, "decisionStatus", decisionStatus, []string{"제안", "승인", "반려", "해당없음"})
}

// TestStatusSignalTag locks the bullet-tag rendering: decisions carry their
// state, risk/blocker/dependency get a plain tag, and the unremarkable
// 진행/없음/unknown produce no tag so ordinary bullets stay clean.
func TestStatusSignalTag(t *testing.T) {
	cases := []struct {
		signalType, decisionStatus, want string
	}{
		{"결정", "승인", "[결정·승인]"},
		{"결정", "제안", "[결정·제안]"},
		{"결정", "반려", "[결정·반려]"},
		{"결정", "해당없음", "[결정]"},
		{"결정", "", "[결정]"},
		{"리스크", "해당없음", "[리스크]"},
		{"블로커", "", "[블로커]"},
		{"의존성", "", "[의존성]"},
		{"진행", "해당없음", ""},
		{"없음", "", ""},
		{"", "", ""},
		{"뭔가이상", "승인", ""},
	}
	for _, c := range cases {
		got := statusSignalTag(StatusSignal{SignalType: c.signalType, DecisionStatus: c.decisionStatus})
		if got != c.want {
			t.Errorf("statusSignalTag(%q,%q) = %q, want %q", c.signalType, c.decisionStatus, got, c.want)
		}
	}
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
