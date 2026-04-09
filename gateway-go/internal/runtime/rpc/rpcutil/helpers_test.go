package rpcutil

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// UnmarshalParams
// ---------------------------------------------------------------------------

func TestUnmarshalParams_NilParams(t *testing.T) {
	var out struct{ Name string }
	err := UnmarshalParams(nil, &out)
	if err == nil {
		t.Fatal("expected error for nil params")
	}
	if err.Error() != "missing params" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnmarshalParams_ValidJSON(t *testing.T) {
	var out struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	raw := json.RawMessage(`{"name":"alice","age":30}`)
	if err := UnmarshalParams(raw, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Name != "alice" {
		t.Fatalf("got %s, want name=alice", out.Name)
	}
	if out.Age != 30 {
		t.Fatalf("got %d, want age=30", out.Age)
	}
}

func TestUnmarshalParams_InvalidJSON(t *testing.T) {
	var out struct{ Name string }
	raw := json.RawMessage(`{not json}`)
	err := UnmarshalParams(raw, &out)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// TruncateForError
// ---------------------------------------------------------------------------

func TestTruncateForError(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
		wantEnd string
	}{
		{
			name:    "short string unchanged",
			input:   "hello",
			wantLen: 5,
			wantEnd: "hello",
		},
		{
			name:    "exact limit unchanged",
			input:   strings.Repeat("x", MaxKeyInErrorMsg),
			wantLen: MaxKeyInErrorMsg,
			wantEnd: strings.Repeat("x", MaxKeyInErrorMsg),
		},
		{
			name:    "over limit truncated with ellipsis",
			input:   strings.Repeat("a", MaxKeyInErrorMsg+50),
			wantLen: MaxKeyInErrorMsg + 3, // 128 + "..."
			wantEnd: strings.Repeat("a", MaxKeyInErrorMsg) + "...",
		},
		{
			name:    "empty string",
			input:   "",
			wantLen: 0,
			wantEnd: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateForError(tt.input)
			if len(got) != tt.wantLen {
				t.Fatalf("len=%d, want %d", len(got), tt.wantLen)
			}
			if got != tt.wantEnd {
				t.Fatalf("got %q, want %q", got, tt.wantEnd)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RequireKey
// ---------------------------------------------------------------------------

func TestRequireKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantKey string
		wantErr bool
	}{
		{"valid key", "session-abc", "session-abc", false},
		{"trimmed key", "  session-abc  ", "session-abc", false},
		{"empty key", "", "", true},
		{"whitespace only", "   ", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, errResp := RequireKey("req-1", tt.key)
			if tt.wantErr {
				if errResp == nil {
					t.Fatal("expected error response")
				}
				if errResp.OK {
					t.Fatal("expected OK=false")
				}
				if errResp.Error == nil || errResp.Error.Code != protocol.ErrMissingParam {
					t.Fatalf("got %+v, want MISSING_PARAM error", errResp.Error)
				}
			} else {
				if errResp != nil {
					t.Fatalf("unexpected error response: %+v", errResp)
				}
				if k != tt.wantKey {
					t.Fatalf("key=%q, want %q", k, tt.wantKey)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ErrMissingKey
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// ParamError
// ---------------------------------------------------------------------------
