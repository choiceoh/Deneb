package reply

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

func TestDeduplicateReplyPayloads(t *testing.T) {
	tests := []struct {
		name     string
		payloads []types.ReplyPayload
		wantLen  int
		wantKeys []string // expected Text values in order
	}{
		{
			name:     "nil input",
			payloads: nil,
			wantLen:  0,
		},
		{
			name:     "empty input",
			payloads: []types.ReplyPayload{},
			wantLen:  0,
		},
		{
			name: "no duplicates",
			payloads: []types.ReplyPayload{
				{Text: "hello"},
				{Text: "world"},
			},
			wantLen:  2,
			wantKeys: []string{"hello", "world"},
		},
		{
			name: "duplicate texts removed",
			payloads: []types.ReplyPayload{
				{Text: "hello"},
				{Text: "hello"},
				{Text: "world"},
			},
			wantLen:  2,
			wantKeys: []string{"hello", "world"},
		},
		{
			name: "duplicate media URLs removed",
			payloads: []types.ReplyPayload{
				{MediaURL: "https://example.com/img.png"},
				{MediaURL: "https://example.com/img.png"},
			},
			wantLen:  1,
			wantKeys: []string{""},
		},
		{
			name: "empty text and media are kept",
			payloads: []types.ReplyPayload{
				{Text: ""},
				{Text: ""},
			},
			wantLen:  2,
			wantKeys: []string{"", ""},
		},
		{
			name: "text takes precedence over media for key",
			payloads: []types.ReplyPayload{
				{Text: "msg", MediaURL: "https://a.com/1.png"},
				{Text: "msg", MediaURL: "https://a.com/2.png"},
			},
			wantLen:  1,
			wantKeys: []string{"msg"},
		},
		{
			name: "mixed text and media payloads",
			payloads: []types.ReplyPayload{
				{Text: "hello"},
				{MediaURL: "https://example.com/img.png"},
				{Text: "hello"},
				{MediaURL: "https://example.com/img.png"},
				{Text: "world"},
			},
			wantLen:  3,
			wantKeys: []string{"hello", "", "world"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeduplicateReplyPayloads(tt.payloads)
			if len(got) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(got), tt.wantLen)
			}
			for i, key := range tt.wantKeys {
				if i < len(got) && got[i].Text != key {
					t.Errorf("result[%d].Text = %q, want %q", i, got[i].Text, key)
				}
			}
		})
	}
}
