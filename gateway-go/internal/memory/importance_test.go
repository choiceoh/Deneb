package memory

import (
	"encoding/json"
	"testing"
)

func TestParseFactsResponse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOK    bool
		wantCount int
	}{
		{
			name:      "expected object with facts key",
			input:     `{"facts": [{"content": "사용자가 Go를 선호", "category": "preference", "importance": 0.8}]}`,
			wantOK:    true,
			wantCount: 1,
		},
		{
			name:      "expected object with multiple facts",
			input:     `{"facts": [{"content": "fact1", "category": "decision", "importance": 0.9}, {"content": "fact2", "category": "context", "importance": 0.6}]}`,
			wantOK:    true,
			wantCount: 2,
		},
		{
			name:      "empty facts array",
			input:     `{"facts": []}`,
			wantOK:    true,
			wantCount: 0,
		},
		{
			name:      "bare JSON array",
			input:     `[{"content": "fact1", "category": "preference", "importance": 0.7}]`,
			wantOK:    true,
			wantCount: 1,
		},
		{
			name:      "arbitrary key wrapping array",
			input:     `{"results": [{"content": "fact1", "category": "solution", "importance": 0.8}]}`,
			wantOK:    true,
			wantCount: 1,
		},
		{
			name:      "single fact object",
			input:     `{"content": "사용자가 DGX Spark 사용 중", "category": "context", "importance": 0.7}`,
			wantOK:    true,
			wantCount: 1,
		},
		{
			name:      "wrong structure no content",
			input:     `{"user_model": "The user is technical", "expectation": "something"}`,
			wantOK:    false,
			wantCount: 0,
		},
		{
			name:      "empty object",
			input:     `{}`,
			wantOK:    false,
			wantCount: 0,
		},
		{
			name:      "prose wrapped array",
			input:     `Here are the facts: [{"content": "fact1", "category": "decision", "importance": 0.9}] end`,
			wantOK:    true,
			wantCount: 1,
		},
		{
			name:      "fact with expiry hint",
			input:     `{"facts": [{"content": "프로젝트 마감일", "category": "context", "importance": 0.9, "expiry_hint": "2026-04-15"}]}`,
			wantOK:    true,
			wantCount: 1,
		},
		{
			name:      "truncated JSON mid-fact recovers complete facts",
			input:     `{"facts": [{"content": "사용자가 Go를 선호", "category": "preference", "importance": 0.8}, {"content": "터미널 로그 확`,
			wantOK:    true,
			wantCount: 1,
		},
		{
			name:      "truncated JSON after two complete facts",
			input:     `{"facts": [{"content": "fact1", "category": "decision", "importance": 0.9}, {"content": "fact2", "category": "context", "importance": 0.6}, {"content": "잘린`,
			wantOK:    true,
			wantCount: 2,
		},
		{
			name:      "truncated JSON no complete fact",
			input:     `{"facts": [{"content": "잘린 내`,
			wantOK:    false,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facts, ok := parseFactsResponse(tt.input)
			if ok != tt.wantOK {
				t.Errorf("parseFactsResponse() ok = %v, want %v", ok, tt.wantOK)
			}
			if len(facts) != tt.wantCount {
				t.Errorf("parseFactsResponse() returned %d facts, want %d", len(facts), tt.wantCount)
			}
		})
	}
}

func TestExtractJSONObject(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValid bool // whether the result is valid JSON
	}{
		{
			name:      "clean JSON object",
			input:     `{"results": [{"id": 1, "valid": true}]}`,
			wantValid: true,
		},
		{
			name:      "JSON wrapped in code fences",
			input:     "```json\n{\"results\": [{\"id\": 1, \"valid\": true}]}\n```",
			wantValid: true,
		},
		{
			name:      "prose before JSON",
			input:     "Here is the result:\n{\"results\": [{\"id\": 1, \"valid\": true}]}",
			wantValid: true,
		},
		{
			name:      "prose before and after JSON",
			input:     "분석 결과입니다:\n{\"conflicts\": []}\n이상입니다.",
			wantValid: true,
		},
		{
			name:      "thinking tags wrapping JSON",
			input:     "<think>Let me analyze...</think>\n{\"patterns\": []}",
			wantValid: true,
		},
		{
			name:      "thinking tags (long form) wrapping JSON",
			input:     "<thinking>Reasoning about facts...</thinking>\n{\"results\": []}",
			wantValid: true,
		},
		{
			name:      "code fences with prose before",
			input:     "결과:\n```json\n{\"merged_content\": \"test\", \"category\": \"decision\", \"importance\": 0.8}\n```",
			wantValid: true,
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
			result := extractJSONObject(tt.input)
			var obj map[string]json.RawMessage
			err := json.Unmarshal([]byte(result), &obj)
			if tt.wantValid && err != nil {
				t.Errorf("extractJSONObject() result is not valid JSON: %v\nresult: %s", err, result)
			}
			if !tt.wantValid && err == nil {
				t.Errorf("extractJSONObject() unexpectedly returned valid JSON: %s", result)
			}
		})
	}
}

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripThinkingTags(tt.input)
			if got != tt.want {
				t.Errorf("stripThinkingTags() = %q, want %q", got, tt.want)
			}
		})
	}
}
