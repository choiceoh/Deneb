package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// registerSampleTools populates a registry with N tools that have realistic schemas.
func registerSampleTools(r *ToolRegistry, n int) {
	for i := range n {
		name := "tool_" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		r.RegisterTool(ToolDef{
			Name:        name,
			Description: "Sample tool " + name + " for benchmarking purposes",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "The absolute path to the file",
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
			},
			Fn: func(_ context.Context, _ json.RawMessage) (string, error) {
				return "ok", nil
			},
		})
	}
}

func BenchmarkLLMTools_Marshal(b *testing.B) {
	reg := NewToolRegistry()
	registerSampleTools(reg, 40)
	tools := reg.LLMTools()

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		// Simulate what happens on every LLM API call: marshal the tool list.
		if _, err := json.Marshal(tools); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExtractCompressFlag(b *testing.B) {
	input := json.RawMessage(`{"file_path":"/tmp/test.go","pattern":"func.*","max_results":100}`)
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		extractCompressFlag(input)
	}
}

func BenchmarkExtractCompressFlag_WithFlag(b *testing.B) {
	input := json.RawMessage(`{"file_path":"/tmp/test.go","compress":true}`)
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		extractCompressFlag(input)
	}
}

// BenchmarkPreSerialize_vs_RawMarshal compares pre-serialized schema marshal
// against marshaling map[string]any from scratch every time.
func BenchmarkPreSerialize_vs_RawMarshal(b *testing.B) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path":   map[string]any{"type": "string", "description": "path"},
			"pattern":     map[string]any{"type": "string", "description": "regex"},
			"max_results": map[string]any{"type": "number", "default": 100, "minimum": 1},
			"ignore_case": map[string]any{"type": "boolean", "default": false},
		},
		"required": []string{"file_path"},
	}

	b.Run("map_marshal", func(b *testing.B) {
		t := llm.Tool{Name: "test", Description: "test tool", InputSchema: schema}
		b.ResetTimer()
		b.ReportAllocs()
		for range b.N {
			if _, err := json.Marshal(t.InputSchema); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("pre_serialized", func(b *testing.B) {
		t := llm.Tool{Name: "test", Description: "test tool", InputSchema: schema}
		t.PreSerialize()
		b.ResetTimer()
		b.ReportAllocs()
		for range b.N {
			// RawInputSchema is already []byte — marshaling it is a no-op copy.
			if _, err := json.Marshal(t.RawInputSchema); err != nil {
				b.Fatal(err)
			}
		}
	})
}
