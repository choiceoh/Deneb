package jsonutil

import (
	"encoding/json"
	"strings"
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
		{
			name:  "thinking tags with JSON inside",
			input: "<thinking>{\"reasoning\": true}</thinking>\n{\"answer\": 42}",
			want:  "{\"answer\": 42}",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only thinking tags",
			input: "<think>nothing useful</think>",
			want:  "",
		},
		{
			name:  "nested angle brackets in reasoning",
			input: "<think>if a < b && c > d</think>\nresult",
			want:  "result",
		},
		{
			name:  "angle bracket not a thinking tag",
			input: "x < y and {\"ok\": true}",
			want:  "x < y and {\"ok\": true}",
		},
		{
			name:  "partial tag name not stripped",
			input: "<thinker>not a tag</thinker>\ndata",
			want:  "<thinker>not a tag</thinker>\ndata",
		},
		{
			name:  "unclosed thinking tag",
			input: "<thinking>never closed\n{\"data\": 1}",
			want:  "",
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
			name:      "JSON with trailing prose",
			input:     `{"answer": "yes"} 좋은 결과입니다!`,
			wantValid: true,
			wantKey:   "answer",
		},
		{
			name:      "JSON with trailing newlines and prose",
			input:     "{\"ok\": true}\n\nI hope this helps!",
			wantValid: true,
			wantKey:   "ok",
		},
		{
			name:      "JSON wrapped in code fences",
			input:     "```json\n{\"results\": [{\"id\": 1}]}\n```",
			wantValid: true,
			wantKey:   "results",
		},
		{
			name:      "code fence with uppercase JSON",
			input:     "```JSON\n{\"data\": true}\n```",
			wantValid: true,
			wantKey:   "data",
		},
		{
			name:      "code fence with jsonc",
			input:     "```jsonc\n{\"value\": 1}\n```",
			wantValid: true,
			wantKey:   "value",
		},
		{
			name:      "code fence bare backticks",
			input:     "```\n{\"bare\": true}\n```",
			wantValid: true,
			wantKey:   "bare",
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
			name:      "multiple JSON objects returns first",
			input:     `{"first": 1} {"second": 2}`,
			wantValid: true,
			wantKey:   "first",
		},
		{
			name:      "deeply nested",
			input:     `{"a":{"b":{"c":{"d":{"e":"deep"}}}}}`,
			wantValid: true,
			wantKey:   "a",
		},
		{
			name:      "escaped quotes in strings",
			input:     `{"msg": "he said \"hello\"", "ok": true}`,
			wantValid: true,
			wantKey:   "msg",
		},
		{
			name:      "backslash at end of string",
			input:     `{"path": "C:\\Users\\test\\"}`,
			wantValid: true,
			wantKey:   "path",
		},
		{
			name:      "thinking + code fence + prose combo",
			input:     "<thinking>hmm</thinking>\nHere:\n```json\n{\"combo\": true}\n```\nDone.",
			wantValid: true,
			wantKey:   "combo",
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
		{
			name:      "unclosed brace",
			input:     `{"unclosed": true`,
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
			name:   "thinking tags before array",
			input:  "<thinking>reasoning with [stuff]</thinking>\n[\"real\", \"data\"]",
			want:   `["real", "data"]`,
			wantOK: true,
		},
		{
			name:   "code fence wrapped array",
			input:  "```json\n[\"a\", \"b\"]\n```",
			want:   `["a", "b"]`,
			wantOK: true,
		},
		{
			name:   "array with trailing prose",
			input:  `["a", "b"] 이상입니다.`,
			want:   `["a", "b"]`,
			wantOK: true,
		},
		{
			name:   "array with brackets in strings",
			input:  `prefix [{"msg": "use arr[0]"}, {"msg": "ok"}] suffix`,
			want:   `[{"msg": "use arr[0]"}, {"msg": "ok"}]`,
			wantOK: true,
		},
		{
			name:   "nested arrays",
			input:  `result: [[1, 2], [3, 4]] done`,
			want:   `[[1, 2], [3, 4]]`,
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
		{
			name:   "empty string",
			input:  "",
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
		wantCount int // expected number of items in recovered array (-1 to skip)
	}{
		{
			name:      "truncated mid-second object",
			input:     `{"results": [{"id": 1, "valid": true}, {"id": 2, "val`,
			wantValid: true,
			wantCount: 1,
		},
		{
			name:      "truncated after first complete object",
			input:     `{"facts": [{"content": "good", "importance": 0.8}, {"content": "잘린`,
			wantValid: true,
			wantCount: 1,
		},
		{
			name:      "truncated after two complete objects",
			input:     `{"items": [{"id": 1}, {"id": 2}, {"id": 3, "incomplete`,
			wantValid: true,
			wantCount: 2,
		},
		{
			name:      "bare array truncated",
			input:     `[{"a": 1}, {"b": 2}, {"c":`,
			wantValid: true,
			wantCount: 2,
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
		{
			name:      "empty string",
			input:     "",
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RecoverTruncated(tt.input)
			if tt.wantValid && result == "" {
				t.Error("RecoverTruncated() returned empty, want recovery")
			}
			if tt.wantValid && result != "" {
				if !json.Valid([]byte(result)) {
					t.Errorf("RecoverTruncated() not valid JSON: %s", result)
				}
				// Verify recovered item count if specified.
				if tt.wantCount > 0 {
					// Parse to count array items.
					arrStart := strings.Index(result, "[")
					if arrStart >= 0 {
						var items []json.RawMessage
						sub := result[arrStart:]
						arrEnd := strings.LastIndex(sub, "]")
						if arrEnd > 0 {
							if json.Unmarshal([]byte(sub[:arrEnd+1]), &items) == nil {
								if len(items) != tt.wantCount {
									t.Errorf("RecoverTruncated() recovered %d items, want %d", len(items), tt.wantCount)
								}
							}
						}
					}
				}
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
		{"a", 1, "a"},
		{"ab", 1, "a..."},
	}

	for _, tt := range tests {
		got := Truncate(tt.input, tt.max)
		if got != tt.expected {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.expected)
		}
	}
}

func TestStripThinkingPreamble(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "thinking process prefix",
			input: "Thinking Process:\nAnalyze the Request:\nThe user wants to fix a bug",
			want:  "The user wants to fix a bug",
		},
		{
			name:  "thinking process inline",
			input: "Thinking Process: some reasoning here",
			want:  "some reasoning here",
		},
		{
			name:  "chained prefixes",
			input: "Analyze the Request:\nLet me think about this",
			want:  "about this",
		},
		{
			name:  "no prefix",
			input: "The user wants to add a login feature",
			want:  "The user wants to add a login feature",
		},
		{
			name:  "whitespace around prefix",
			input: "  Thinking Process:\n  actual content  ",
			want:  "actual content",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "prefix only",
			input: "Thinking Process:",
			want:  "",
		},
		{
			name:  "okay let me prefix",
			input: "Okay, let me analyze this.\nThe code has a bug.",
			want:  "analyze this.\nThe code has a bug.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripThinkingPreamble(tt.input); got != tt.want {
				t.Errorf("StripThinkingPreamble() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- Benchmarks ---

func BenchmarkExtractObject_Clean(b *testing.B) {
	input := `{"results": [{"id": 1, "name": "test", "valid": true}]}`
	for b.Loop() {
		ExtractObject(input)
	}
}

func BenchmarkExtractObject_WithProse(b *testing.B) {
	input := "분석 결과입니다:\n{\"conflicts\": [{\"id\": 1}]}\n이상입니다."
	for b.Loop() {
		ExtractObject(input)
	}
}

func BenchmarkExtractObject_ThinkingTags(b *testing.B) {
	input := "<thinking>\nLet me analyze this carefully.\nStep 1: check data\nStep 2: validate\n</thinking>\n{\"answer\": \"yes\", \"confidence\": 0.95}"
	for b.Loop() {
		ExtractObject(input)
	}
}

func BenchmarkExtractObject_CodeFence(b *testing.B) {
	input := "```json\n{\"data\": [1, 2, 3], \"status\": \"ok\"}\n```"
	for b.Loop() {
		ExtractObject(input)
	}
}

func BenchmarkExtractObject_Large(b *testing.B) {
	// Simulate a large LLM response with thinking + prose + JSON.
	thinking := "<thinking>" + strings.Repeat("reasoning step. ", 200) + "</thinking>\n"
	jsonObj := `{"facts": [` + strings.Repeat(`{"content": "사용자가 Go를 선호함", "category": "preference", "importance": 0.8},`, 20)
	jsonObj = jsonObj[:len(jsonObj)-1] + "]}"
	input := thinking + "분석 결과:\n" + jsonObj + "\n완료."
	b.ResetTimer()
	for b.Loop() {
		ExtractObject(input)
	}
}

func BenchmarkStripThinkingTags(b *testing.B) {
	input := "<thinking>\n" + strings.Repeat("reasoning step. ", 100) + "\n</thinking>\n{\"result\": true}"
	for b.Loop() {
		StripThinkingTags(input)
	}
}

func BenchmarkExtractArray(b *testing.B) {
	input := `<thinking>analyzing</thinking> results: ["term1", "term2", "term3", "term4", "term5"]`
	for b.Loop() {
		ExtractArray(input)
	}
}

func BenchmarkStripTrailingCommas(b *testing.B) {
	input := `{"facts": [{"content": "사용자가 Go를 선호", "importance": 0.8,}, {"content": "DGX Spark 사용", "importance": 0.7,},]}`
	for b.Loop() {
		StripTrailingCommas(input)
	}
}

func BenchmarkStripTrailingCommas_NoCommas(b *testing.B) {
	input := `{"facts": [{"content": "clean", "importance": 0.8}]}`
	for b.Loop() {
		StripTrailingCommas(input)
	}
}
