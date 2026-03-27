package memory

import (
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
