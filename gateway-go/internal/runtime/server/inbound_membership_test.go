package server

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// TestClassifyMembershipChange covers the decision table that drives
// the my_chat_member handler. The dispatcher only does string-format
// + sendMessage on top of the action returned here, so unit-testing
// the classifier alone is enough to lock in the policy:
//
//   - greet ONCE on first promotion to administrator
//   - warn ONCE when Manage Topics is rolled back while still admin
//   - log silently when the bot is demoted or removed
//   - ignore events about anyone other than our bot
func TestClassifyMembershipChange(t *testing.T) {
	const botID = int64(1000001)

	makeEvent := func(userID int64, oldStatus, newStatus string, oldTopics, newTopics bool) *telegram.ChatMemberUpdated {
		return &telegram.ChatMemberUpdated{
			Chat: telegram.Chat{ID: -1001, Type: "supergroup", IsForum: true},
			OldChatMember: telegram.ChatMember{
				Status: oldStatus, User: telegram.User{ID: userID}, CanManageTopics: oldTopics,
			},
			NewChatMember: telegram.ChatMember{
				Status: newStatus, User: telegram.User{ID: userID}, CanManageTopics: newTopics,
			},
		}
	}

	tests := []struct {
		name string
		evt  *telegram.ChatMemberUpdated
		want membershipAction
	}{
		{
			name: "nil event",
			evt:  nil,
			want: membershipNoOp,
		},
		{
			name: "event about another user (not our bot)",
			evt:  makeEvent(999999, "member", "administrator", false, true),
			want: membershipNoOp,
		},
		{
			name: "bot promoted member → administrator",
			evt:  makeEvent(botID, "member", "administrator", false, true),
			want: membershipGreet,
		},
		{
			name: "bot first-joined (left → administrator)",
			evt:  makeEvent(botID, "left", "administrator", false, true),
			want: membershipGreet,
		},
		{
			name: "permission drop while admin (Manage Topics revoked)",
			evt:  makeEvent(botID, "administrator", "administrator", true, false),
			want: membershipWarnPermissionLoss,
		},
		{
			name: "permission unchanged while admin (no-op)",
			evt:  makeEvent(botID, "administrator", "administrator", true, true),
			want: membershipNoOp,
		},
		{
			name: "permission added while admin (still no-op, only loss is interesting)",
			evt:  makeEvent(botID, "administrator", "administrator", false, true),
			want: membershipNoOp,
		},
		{
			name: "bot demoted administrator → member",
			evt:  makeEvent(botID, "administrator", "member", true, false),
			want: membershipLogDemoted,
		},
		{
			name: "bot kicked",
			evt:  makeEvent(botID, "administrator", "kicked", true, false),
			want: membershipLogRemoved,
		},
		{
			name: "bot left voluntarily",
			evt:  makeEvent(botID, "member", "left", false, false),
			want: membershipLogRemoved,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyMembershipChange(tt.evt, botID)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

// TestClassifyMembershipChange_UnknownBotID covers the early-boot case
// where the bot's user ID is not yet resolved. We degrade to "trust the
// event" so a promotion that lands in the polling buffer before getMe
// finishes is still acted on, not silently dropped.
func TestClassifyMembershipChange_UnknownBotID(t *testing.T) {
	evt := &telegram.ChatMemberUpdated{
		Chat: telegram.Chat{ID: -1001, Type: "supergroup", IsForum: true},
		OldChatMember: telegram.ChatMember{
			Status: "member", User: telegram.User{ID: 42},
		},
		NewChatMember: telegram.ChatMember{
			Status: "administrator", User: telegram.User{ID: 42},
			CanManageTopics: true,
		},
	}
	if got := classifyMembershipChange(evt, 0); got != membershipGreet {
		t.Errorf("with botID=0, expected greet (trust the event), got %d", got)
	}
}
