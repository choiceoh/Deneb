package telegram

import "testing"

func TestResolveConversationLabel(t *testing.T) {
	tests := []struct {
		name   string
		fields ConversationLabelFields
		want   string
	}{
		{
			name:   "explicit label",
			fields: ConversationLabelFields{ConversationLabel: "My Chat"},
			want:   "My Chat",
		},
		{
			name:   "thread label",
			fields: ConversationLabelFields{ThreadLabel: "Thread A"},
			want:   "Thread A",
		},
		{
			name:   "direct uses sender name",
			fields: ConversationLabelFields{ChatType: "direct", SenderName: "Alice"},
			want:   "Alice",
		},
		{
			name:   "direct falls back to from",
			fields: ConversationLabelFields{ChatType: "direct", From: "alice@example.com"},
			want:   "alice@example.com",
		},
		{
			name:   "group uses GroupChannel",
			fields: ConversationLabelFields{ChatType: "group", GroupChannel: "#general"},
			want:   "#general",
		},
		{
			name:   "group uses GroupSubject",
			fields: ConversationLabelFields{ChatType: "group", GroupSubject: "Dev Team"},
			want:   "Dev Team",
		},
		{
			name:   "group appends numeric ID",
			fields: ConversationLabelFields{ChatType: "group", GroupChannel: "#general", From: "channel:12345"},
			want:   "#general",
		},
		{
			name:   "group appends JID-like ID",
			fields: ConversationLabelFields{ChatType: "group", GroupSubject: "Family", From: "12345@g.us"},
			want:   "Family id:12345@g.us",
		},
		{
			name:   "empty returns empty",
			fields: ConversationLabelFields{ChatType: "group"},
			want:   "",
		},
		{
			name:   "group with From only and numeric ID",
			fields: ConversationLabelFields{ChatType: "group", From: "12345"},
			want:   "12345",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveConversationLabel(tt.fields)
			if got != tt.want {
				t.Errorf("ResolveConversationLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractConversationID(t *testing.T) {
	tests := []struct {
		from string
		want string
	}{
		{"", ""},
		{"12345", "12345"},
		{"channel:12345", "12345"},
		{"a:b:c", "c"},
	}
	for _, tt := range tests {
		got := extractConversationID(tt.from)
		if got != tt.want {
			t.Errorf("extractConversationID(%q) = %q, want %q", tt.from, got, tt.want)
		}
	}
}
