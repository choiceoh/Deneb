package jsonutil

import (
	"encoding/json"
	"testing"
)

func TestStripThinkingTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "think tags",
			input: "<think>reasoning here</think>\n{\"facts\": []}",
			want:  "{\"facts\": []}",
		},
		{
			name:  "thinking tags",
			input: "<thinking>reasoning here</thinking>\n{\"facts\": []}",
			want:  "{\"facts\": []}",
		},
		{
			name:  "multiline thinking",
			input: "<thinking>\nstep 1\nstep 2\n</thinking>\n{\"result\": true}",
			want:  "{\"result\": true}",
		},
		{
			name:  "no tags",
			input: "{\"facts\": []}",
			want:  "{\"facts\": []}",
		},
		{
			name:  "multiple think blocks",
			input: "<think>first</think>\n<think>second</think>\nContent",
			want:  "Content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripThinkingTags(tt.input)
			if got != tt.want {
				t.Errorf("StripThinkingTags() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractObject(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool // whether the result is valid JSON object
		wantKey   string
	}{
		{
			name:      "clean JSON object",
			input:     `{"results": [{"id": 1, "valid": true}]}`,
			wantValid: true,
			wantKey:   "results",
		},
		{
			name:      "JSON wrapped in code fences",
			input:     "```json\n{\"results\": [{\"id\": 1}]}\n```",
			wantValid: true,
			wantKey:   "results",
		},
		{
			name:      "prose before JSON",
			input:     "Here is the result:\n{\"results\": [{\"id\": 1}]}",
			wantValid: true,
			wantKey:   "results",
		},
		{
			name:      "prose before and after JSON",
			input:     "분석 결과입니다:\n{\"conflicts\": []}\n이상입니다.",
			wantValid: true,
			wantKey:   "conflicts",
		},
		{
			name:      "think tags wrapping JSON",
			input:     "<think>Let me analyze...</think>\n{\"patterns\": []}",
			wantValid: true,
			wantKey:   "patterns",
		},
		{
			name:      "thinking tags (long form)",
			input:     "<thinking>Reasoning about facts...</thinking>\n{\"results\": []}",
			wantValid: true,
			wantKey:   "results",
		},
		{
			name:      "code fences with prose before",
			input:     "결과:\n```json\n{\"merged_content\": \"test\"}\n```",
			wantValid: true,
			wantKey:   "merged_content",
		},
		{
			name:      "nested objects in prose",
			input:     "분석:\n{\"a\": {\"b\": {\"c\": 1}}} 완료",
			wantValid: true,
			wantKey:   "a",
		},
		{
			name:      "JSON with string containing braces",
			input:     `{"content": "use fmt.Sprintf(\"%s{%s}\")", "importance": 0.5}`,
			wantValid: true,
			wantKey:   "content",
		},
		{
			name:      "empty input",
			input:     "",
			wantValid: false,
		},
		{
			name:      "no JSON at all",
			input:     "This is just plain text with no JSON",
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractObject(tt.input)
			var obj map[string]json.RawMessage
			err := json.Unmarshal([]byte(result), &obj)
			if tt.wantValid && err != nil {
				t.Errorf("ExtractObject() not valid JSON: %v\nresult: %q", err, result)
			}
			if !tt.wantValid && err == nil {
				t.Errorf("ExtractObject() unexpectedly valid JSON: %s", result)
			}
			if tt.wantValid && tt.wantKey != "" {
				if _, ok := obj[tt.wantKey]; !ok {
					t.Errorf("ExtractObject() missing expected key %q in: %s", tt.wantKey, result)
				}
			}
		})
	}
}

func TestExtractArray(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{
			name:   "clean array",
			input:  `["a", "b", "c"]`,
			want:   `["a", "b", "c"]`,
			wantOK: true,
		},
		{
			name:   "prose wrapped array",
			input:  `Here are the facts: [{"id": 1}] end`,
			want:   `[{"id": 1}]`,
			wantOK: true,
		},
		{
			name:   "no brackets",
			input:  `just text`,
			want:   "",
			wantOK: false,
		},
		{
			name:   "only opening bracket",
			input:  `[incomplete`,
			want:   "",
			wantOK: false,
		},
		{
			name:   "reversed brackets",
			input:  `] before [`,
			want:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExtractArray(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ExtractArray() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("ExtractArray() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRecoverTruncated(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool
	}{
		{
			name:      "truncated mid-second object",
			input:     `{"results": [{"id": 1, "valid": true}, {"id": 2, "val`,
			wantValid: true,
		},
		{
			name:      "truncated after first complete object",
			input:     `{"facts": [{"content": "good", "importance": 0.8}, {"content": "잘린`,
			wantValid: true,
		},
		{
			name:      "no array",
			input:     `{"key": "val`,
			wantValid: false,
		},
		{
			name:      "no complete object in array",
			input:     `{"facts": [{"content": "잘린`,
			wantValid: false,
		},
		{
			name:      "already valid JSON not recoverable",
			input:     `{"results": [{"id": 1}]}`,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RecoverTruncated(tt.input)
			if tt.wantValid && result == "" {
				t.Error("RecoverTruncated() returned empty, want recovery")
			}
			if tt.wantValid && result != "" && !json.Valid([]byte(result)) {
				t.Errorf("RecoverTruncated() not valid JSON: %s", result)
			}
			if !tt.wantValid && result != "" {
				t.Errorf("RecoverTruncated() unexpectedly recovered: %s", result)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"한국어 테스트", 3, "한국어..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := Truncate(tt.input, tt.max)
		if got != tt.expected {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.expected)
		}
	}
}
