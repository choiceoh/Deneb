package channel

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func Test_parseChatID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"valid positive", "12345", 12345, false},
		{"valid negative", "-100", -100, false},
		{"telegram prefix", "telegram:12345", 12345, false},
		{"session-like key", "te:7074071666:fix-release-please:1774610514127", 7074071666, false},
		{"invalid string", "abc", 0, true},
		{"empty string", "", 0, true},
		{"max int64", "9223372036854775807", 9223372036854775807, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseChatID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseChatID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseChatID(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestLifecycleMethods_nilDeps(t *testing.T) {
	m := LifecycleMethods(LifecycleDeps{})
	if m != nil {
		t.Fatal("expected nil for nil ChannelLifecycle")
	}
}

func TestEventsMethods_nilDeps(t *testing.T) {
	m := EventsMethods(EventsDeps{})
	if m != nil {
		t.Fatal("expected nil for nil Broadcaster")
	}
}

func TestMessagingMethods_returnsHandlers(t *testing.T) {
	m := MessagingMethods(MessagingDeps{})
	if m == nil {
		t.Fatal("expected non-nil handler map")
	}
	for _, name := range []string{"send", "poll"} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

func TestMessagingPoll_nilPlugin(t *testing.T) {
	handlers := MessagingMethods(MessagingDeps{})
	resp := handlers["poll"](context.Background(), &protocol.RequestFrame{ID: "test-1"})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	var payload struct {
		Messages []any `json:"messages"`
		Count    int   `json:"count"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if payload.Count != 0 {
		t.Errorf("expected count 0, got %d", payload.Count)
	}
	if len(payload.Messages) != 0 {
		t.Errorf("expected empty messages, got %d", len(payload.Messages))
	}
}

func TestMessagingSend_noChannel(t *testing.T) {
	handlers := MessagingMethods(MessagingDeps{})
	req := &protocol.RequestFrame{
		ID:     "test-2",
		Params: json.RawMessage(`{"text":"hello","to":"12345"}`),
	}
	resp := handlers["send"](context.Background(), req)
	if resp.OK {
		t.Fatal("expected error response when no channel available")
	}
}
