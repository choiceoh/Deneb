package llm

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func sampleSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the file to read",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression pattern to search for",
			},
			"max_results": map[string]any{
				"type":        "number",
				"description": "Maximum matches to return",
				"default":     100,
				"minimum":     1,
				"maximum":     500,
			},
			"ignore_case": map[string]any{
				"type":        "boolean",
				"description": "Case-insensitive search",
				"default":     false,
			},
		},
		"required": []string{"file_path"},
	}
}

func buildToolList(n int) []Tool {
	tools := make([]Tool, n)
	for i := range n {
		tools[i] = Tool{
			Name:        "tool_" + string(rune('a'+i%26)),
			Description: "Sample tool for benchmarking with a realistic description length",
			InputSchema: sampleSchema(),
		}
	}
	return tools
}

func buildToolListPreSerialized(n int) []Tool {
	tools := buildToolList(n)
	for i := range tools {
		tools[i].PreSerialize()
	}
	return tools
}

// oldTool simulates the pre-optimization Tool struct where InputSchema
// was serialized via reflection on every json.Marshal call.
type oldTool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"input_schema"`
	CacheControl *CacheControl  `json:"cache_control,omitempty"`
}

// BenchmarkToolList_Marshal_MapSchema benchmarks marshaling tool lists with
// map[string]any schemas (the old path before pre-serialization).
func BenchmarkToolList_Marshal_MapSchema(b *testing.B) {
	tools := make([]oldTool, 40)
	for i := range tools {
		tools[i] = oldTool{
			Name:        "tool_" + string(rune('a'+i%26)),
			Description: "Sample tool for benchmarking with a realistic description length",
			InputSchema: sampleSchema(),
		}
	}
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		if _, err := json.Marshal(tools); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkToolList_Marshal_PreSerialized benchmarks marshaling tool lists
// with pre-serialized RawInputSchema (the optimized path).
func BenchmarkToolList_Marshal_PreSerialized(b *testing.B) {
	tools := buildToolListPreSerialized(40)
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		if _, err := json.Marshal(tools); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPreSerialize(t *testing.T) {
	tool := Tool{
		Name:        "test",
		Description: "test tool",
		InputSchema: sampleSchema(),
	}
	tool.PreSerialize()

	if tool.RawInputSchema == nil {
		t.Fatal("RawInputSchema should not be nil after PreSerialize")
	}

	// Verify the pre-serialized JSON is valid and matches the schema.
	var parsed map[string]any
	if err := json.Unmarshal(tool.RawInputSchema, &parsed); err != nil {
		t.Fatalf("RawInputSchema is not valid JSON: %v", err)
	}
	if parsed["type"] != "object" {
		t.Errorf("expected type=object, got %v", parsed["type"])
	}
	props, ok := parsed["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	if _, ok := props["file_path"]; !ok {
		t.Error("missing file_path in properties")
	}
}

func TestPreSerialize_Idempotent(t *testing.T) {
	tool := Tool{
		Name:        "test",
		Description: "test tool",
		InputSchema: sampleSchema(),
	}
	tool.PreSerialize()
	first := string(tool.RawInputSchema)

	tool.PreSerialize() // should not change
	if string(tool.RawInputSchema) != first {
		t.Error("PreSerialize should be idempotent")
	}
}

func TestTool_MarshalJSON_PreSerialized(t *testing.T) {
	tool := Tool{
		Name:        "test",
		Description: "test tool",
		InputSchema: sampleSchema(),
	}
	tool.PreSerialize()

	data := testutil.Must(json.Marshal(tool))

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify input_schema is present and valid.
	if _, ok := parsed["input_schema"]; !ok {
		t.Fatal("missing input_schema in marshaled output")
	}
	var schema map[string]any
	if err := json.Unmarshal(parsed["input_schema"], &schema); err != nil {
		t.Fatalf("input_schema is not valid JSON: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("expected type=object in input_schema, got %v", schema["type"])
	}
}
