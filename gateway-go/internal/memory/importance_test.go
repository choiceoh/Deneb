package memory

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
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

func TestExtractJSON(t *testing.T) {
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
			result := extractJSON(tt.input)
			var obj map[string]json.RawMessage
			err := json.Unmarshal([]byte(result), &obj)
			if tt.wantValid && err != nil {
				t.Errorf("extractJSON() not valid JSON: %v\nresult: %q", err, result)
			}
			if !tt.wantValid && err == nil {
				t.Errorf("extractJSON() unexpectedly valid JSON: %s", result)
			}
			if tt.wantValid && tt.wantKey != "" {
				if _, ok := obj[tt.wantKey]; !ok {
					t.Errorf("extractJSON() missing expected key %q in: %s", tt.wantKey, result)
				}
			}
		})
	}
}

// RecoverTruncated and StripThinkingTags tests have moved to pkg/jsonutil/extract_test.go.
// These tests verify the integration still works through the jsonutil package.

func TestRecoverTruncated_Integration(t *testing.T) {
	// Verify jsonutil.RecoverTruncated works for the truncated facts case.
	input := `{"facts": [{"content": "good", "importance": 0.8}, {"content": "잘린`
	result := jsonutil.RecoverTruncated(input)
	if result == "" {
		t.Error("RecoverTruncated() returned empty, want recovery")
	}
	if result != "" && !json.Valid([]byte(result)) {
		t.Errorf("RecoverTruncated() not valid JSON: %s", result)
	}
}

func TestStripThinkingTags_Integration(t *testing.T) {
	got := jsonutil.StripThinkingTags("<thinking>reasoning</thinking>\n{\"facts\": []}")
	want := "{\"facts\": []}"
	if got != want {
		t.Errorf("StripThinkingTags() = %q, want %q", got, want)
	}
}
