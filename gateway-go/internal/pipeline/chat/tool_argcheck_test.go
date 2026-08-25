package chat

import (
	"encoding/json"
	"slices"
	"testing"
)

func calendarSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":      map[string]any{"type": "string"},
			"hours_ahead": map[string]any{"type": "integer"},
			"from":        map[string]any{"type": "string"},
			"to":          map[string]any{"type": "string"},
		},
	}
}

func TestUnknownToolArgKeys(t *testing.T) {
	tests := []struct {
		name   string
		schema map[string]any
		input  string
		want   []string
	}{
		{
			// The 2026-08-25 puppet observation: a hallucinated filter that the
			// tool ignored while answering for its default window.
			name:   "hallucinated filter is reported",
			schema: calendarSchema(),
			input:  `{"action":"list","range":"today"}`,
			want:   []string{"range"},
		},
		{
			name:   "several unknown keys are sorted",
			schema: calendarSchema(),
			input:  `{"persno":"김대표","action":"list","zzz":1}`,
			want:   []string{"persno", "zzz"},
		},
		{
			name:   "declared arguments are clean",
			schema: calendarSchema(),
			input:  `{"action":"list","hours_ahead":24}`,
		},
		{
			name:   "the compress harness key is not the tool's business",
			schema: calendarSchema(),
			input:  `{"action":"list","compress":true}`,
		},
		{
			name:   "open schemas accept anything",
			schema: map[string]any{"additionalProperties": true, "properties": map[string]any{"a": map[string]any{}}},
			input:  `{"b":1}`,
		},
		{
			name:   "schema without properties cannot judge",
			schema: map[string]any{"type": "object"},
			input:  `{"b":1}`,
		},
		{
			name:   "non-object input is not judged",
			schema: calendarSchema(),
			input:  `"just a string"`,
		},
		{
			name:  "missing schema is not judged",
			input: `{"b":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unknownToolArgKeys(tt.schema, json.RawMessage(tt.input))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
