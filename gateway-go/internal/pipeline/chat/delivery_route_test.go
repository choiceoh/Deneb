package chat

import "testing"

func TestDeliveryFromSessionKey(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		wantChannel string
		wantTo      string
		wantThread  string
		wantNil     bool
	}{
		{
			name:        "1:1 chat (no thread)",
			key:         "telegram:7074071666",
			wantChannel: "telegram",
			wantTo:      "7074071666",
		},
		{
			name:        "forum topic thread",
			key:         "telegram:-1001234567890:thread:42",
			wantChannel: "telegram",
			wantTo:      "-1001234567890",
			wantThread:  "42",
		},
		{
			name:        "non-telegram channel passes through",
			key:         "btw:abc123",
			wantChannel: "btw",
			wantTo:      "abc123",
		},
		{
			name:    "malformed key",
			key:     "no-colon",
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deliveryFromSessionKey(tt.key)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected non-nil delivery")
			}
			if got.Channel != tt.wantChannel {
				t.Errorf("Channel = %q, want %q", got.Channel, tt.wantChannel)
			}
			if got.To != tt.wantTo {
				t.Errorf("To = %q, want %q", got.To, tt.wantTo)
			}
			if got.ThreadID != tt.wantThread {
				t.Errorf("ThreadID = %q, want %q", got.ThreadID, tt.wantThread)
			}
		})
	}
}
