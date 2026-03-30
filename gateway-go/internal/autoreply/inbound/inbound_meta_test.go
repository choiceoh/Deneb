package inbound

import (
	"strings"
	"testing"
)

func TestBuildInboundMetaSystemPrompt_DirectMessage(t *testing.T) {
	ctx := &InboundMetaContext{
		ChatType:  "direct",
		Provider:  "telegram",
		Surface:   "telegram",
		AccountID: "acc-123",
	}

	result := BuildInboundMetaSystemPrompt(ctx)

	if !strings.Contains(result, "deneb.inbound_meta.v1") {
		t.Error("expected schema version in output")
	}
	if !strings.Contains(result, `"chat_type": "direct"`) {
		t.Error("expected chat_type: direct")
	}
	if !strings.Contains(result, "trusted metadata") {
		t.Error("expected trusted metadata header")
	}
}

func TestBuildInboundMetaSystemPrompt_GroupMessage(t *testing.T) {
	ctx := &InboundMetaContext{
		ChatType:  "group",
		Provider:  "telegram",
		Surface:   "telegram",
		AccountID: "acc-456",
	}

	result := BuildInboundMetaSystemPrompt(ctx)

	if !strings.Contains(result, `"chat_type": "group"`) {
		t.Error("expected chat_type: group")
	}
}

func TestBuildInboundUserContextPrefix_ConversationInfo(t *testing.T) {
	ts := int64(1700000000000)
	ctx := &InboundMetaContext{
		ChatType:     "group",
		SenderId:     "user-123",
		SenderName:   "Alice",
		WasMentioned: true,
		GroupSubject: "Test Group",
		Surface:      "telegram",
		Timestamp:    &ts,
	}

	result := BuildInboundUserContextPrefix(ctx)

	if !strings.Contains(result, "Conversation info") {
		t.Error("expected conversation info block")
	}
	if !strings.Contains(result, "was_mentioned") {
		t.Error("expected was_mentioned field")
	}
	if !strings.Contains(result, "is_group_chat") {
		t.Error("expected is_group_chat for group message")
	}
	if !strings.Contains(result, "Test Group") {
		t.Error("expected group_subject in output")
	}
}

func TestBuildInboundUserContextPrefix_SenderInfo(t *testing.T) {
	ctx := &InboundMetaContext{
		ChatType:       "direct",
		SenderName:     "Bob",
		SenderUsername: "@bob",
		SenderId:       "bob-id",
		Surface:        "telegram",
	}

	result := BuildInboundUserContextPrefix(ctx)

	if !strings.Contains(result, "Sender (untrusted metadata)") {
		t.Error("expected sender info block")
	}
	if !strings.Contains(result, "Bob") {
		t.Error("expected sender name in output")
	}
}

func TestBuildInboundUserContextPrefix_ReplyContext(t *testing.T) {
	ctx := &InboundMetaContext{
		ChatType:      "group",
		ReplyToBody:   "original message",
		ReplyToSender: "Alice",
		Surface:       "telegram",
	}

	result := BuildInboundUserContextPrefix(ctx)

	if !strings.Contains(result, "Replied message") {
		t.Error("expected replied message block")
	}
	if !strings.Contains(result, "original message") {
		t.Error("expected reply body in output")
	}
}

func TestBuildInboundUserContextPrefix_ForwardedContext(t *testing.T) {
	ctx := &InboundMetaContext{
		ChatType:          "group",
		ForwardedFrom:     "Channel News",
		ForwardedFromType: "channel",
		Surface:           "telegram",
	}

	result := BuildInboundUserContextPrefix(ctx)

	if !strings.Contains(result, "Forwarded message context") {
		t.Error("expected forwarded context block")
	}
	if !strings.Contains(result, "Channel News") {
		t.Error("expected forwarded from in output")
	}
}

func TestBuildInboundUserContextPrefix_ChatHistory(t *testing.T) {
	ctx := &InboundMetaContext{
		ChatType: "group",
		Surface:  "telegram",
		InboundHistory: []InboundHistoryEntry{
			{Sender: "Alice", Timestamp: 1700000000000, Body: "Hi"},
			{Sender: "Bob", Timestamp: 1700000001000, Body: "Hello"},
		},
	}

	result := BuildInboundUserContextPrefix(ctx)

	if !strings.Contains(result, "Chat history since last reply") {
		t.Error("expected chat history block")
	}
	if !strings.Contains(result, "Alice") || !strings.Contains(result, "Bob") {
		t.Error("expected sender names in history")
	}
}

func TestBuildInboundUserContextPrefix_Empty(t *testing.T) {
	ctx := &InboundMetaContext{
		ChatType: "direct",
	}

	result := BuildInboundUserContextPrefix(ctx)

	if result != "" {
		t.Errorf("expected empty output for minimal direct context, got: %q", result)
	}
}

func TestResolveSenderLabel(t *testing.T) {
	tests := []struct {
		name string
		ctx  InboundMetaContext
		want string
	}{
		{
			name: "name and id",
			ctx:  InboundMetaContext{SenderName: "Alice", SenderId: "123"},
			want: "Alice (123)",
		},
		{
			name: "name only",
			ctx:  InboundMetaContext{SenderName: "Alice"},
			want: "Alice",
		},
		{
			name: "username only",
			ctx:  InboundMetaContext{SenderUsername: "@alice"},
			want: "@alice",
		},
		{
			name: "id only",
			ctx:  InboundMetaContext{SenderId: "123"},
			want: "123",
		},
		{
			name: "name and e164",
			ctx:  InboundMetaContext{SenderName: "Alice", SenderE164: "+1234567890"},
			want: "Alice (+1234567890)",
		},
		{
			name: "name same as id",
			ctx:  InboundMetaContext{SenderName: "Alice", SenderId: "Alice"},
			want: "Alice",
		},
		{
			name: "empty",
			ctx:  InboundMetaContext{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveSenderLabel(&tt.ctx)
			if got != tt.want {
				t.Errorf("resolveSenderLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}
