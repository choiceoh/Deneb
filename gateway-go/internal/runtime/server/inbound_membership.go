package server

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// membershipAction is the decision a my_chat_member event boils down to:
// greet the user, warn about a permission roll-back, log silently, or skip
// entirely. Carving it out as an enum lets the policy be unit-tested
// without standing up a Telegram client.
type membershipAction int

const (
	// membershipNoOp covers the events that are not about our bot and
	// the "no meaningful change" path.
	membershipNoOp membershipAction = iota
	// membershipGreet fires once when the bot first reaches administrator
	// in a forum supergroup — the one transition where a user-visible
	// onboarding message is worth the noise.
	membershipGreet
	// membershipWarnPermissionLoss fires when the bot is still admin but
	// the Manage Topics permission was just rolled back. Topic operations
	// will fail until restored — telling the user beats letting them
	// discover via a failed /use-forum or a silent cron miss.
	membershipWarnPermissionLoss
	// membershipLogDemoted fires when the bot was admin and is now a
	// plain member, restricted, or anything else short of removed.
	membershipLogDemoted
	// membershipLogRemoved fires when the bot was kicked or left the
	// chat. We can't send a message — only log.
	membershipLogRemoved
)

// classifyMembershipChange picks the action a my_chat_member event should
// trigger. The chat type / forum flag is consulted by the dispatcher (not
// here) because a non-forum supergroup promotion still wants the
// "demoted" / "removed" / "permission revoked" paths but skips the
// greeting — keeping the gate at the dispatcher avoids branching the
// classifier on chat shape.
func classifyMembershipChange(evt *telegram.ChatMemberUpdated, botID int64) membershipAction {
	if evt == nil || evt.NewChatMember.User.ID == 0 {
		return membershipNoOp
	}
	// Only consider changes that apply to our bot. When BotUserID is
	// not yet known (botID == 0, very early after startup) we degrade
	// to "trust the event" rather than dropping it — the alternative is
	// silently missing the first promotion in a freshly booted gateway.
	if botID != 0 && evt.NewChatMember.User.ID != botID {
		return membershipNoOp
	}

	oldStatus := evt.OldChatMember.Status
	newStatus := evt.NewChatMember.Status

	switch {
	case oldStatus != "administrator" && newStatus == "administrator":
		return membershipGreet
	case oldStatus == "administrator" && newStatus == "administrator":
		if evt.OldChatMember.CanManageTopics && !evt.NewChatMember.CanManageTopics {
			return membershipWarnPermissionLoss
		}
		return membershipNoOp
	case newStatus == "kicked" || newStatus == "left":
		return membershipLogRemoved
	case oldStatus == "administrator" && newStatus != "administrator":
		return membershipLogDemoted
	}
	return membershipNoOp
}

// handleMyChatMember reacts to changes in the bot's membership status.
// Two cases produce a user-visible message:
//
//  1. Promotion to administrator in a forum supergroup — the user just
//     finished the manual setup the design pass requires (create
//     supergroup, enable Topics, invite bot, promote). We greet in the
//     chat's General topic with a one-liner pointing at /use-forum so the
//     user knows what to do next without re-reading the docs.
//
//  2. Loss of Manage Topics permission while still admin — the user kept
//     the bot but rolled back the one permission that gates every topic
//     operation. Surfacing this proactively beats letting cron output
//     silently fail or letting a future /use-forum try and fail.
//
// Other transitions (kicked, demoted to plain member) only log: the bot
// can't usefully send a message in either case (no permission, or you'd
// be telling the user something they just deliberately did).
func (p *InboundProcessor) handleMyChatMember(evt *telegram.ChatMemberUpdated) {
	if evt == nil {
		return
	}
	botID := p.server.telegramPlug.BotUserID()
	action := classifyMembershipChange(evt, botID)
	chatID := fmt.Sprintf("%d", evt.Chat.ID)

	switch action {
	case membershipNoOp:
		// Not about our bot, or no interesting change. Already filtered.
	case membershipGreet:
		p.onBotPromoted(evt, chatID)
	case membershipWarnPermissionLoss:
		p.onManageTopicsRevoked(chatID)
	case membershipLogDemoted:
		p.logger.Warn("bot demoted from administrator",
			"chatId", evt.Chat.ID, "newStatus", evt.NewChatMember.Status)
	case membershipLogRemoved:
		p.logger.Warn("bot removed from chat",
			"chatId", evt.Chat.ID, "newStatus", evt.NewChatMember.Status)
	}
}

// onBotPromoted greets the user in General the moment the bot becomes an
// admin of a forum supergroup. Stays silent for non-forum supergroups and
// for regular groups — the /use-forum migration is only meaningful when
// Topics is enabled, and a "nice to meet you" message in a non-forum
// group would be more confusing than helpful.
func (p *InboundProcessor) onBotPromoted(evt *telegram.ChatMemberUpdated, chatID string) {
	if evt.Chat.Type != "supergroup" || !evt.Chat.IsForum {
		p.logger.Info("bot promoted to administrator (no greeting — not a forum supergroup)",
			"chatId", evt.Chat.ID, "chatType", evt.Chat.Type, "isForum", evt.Chat.IsForum)
		return
	}
	if !evt.NewChatMember.CanManageTopics {
		p.logger.Warn("bot promoted to administrator without Manage Topics",
			"chatId", evt.Chat.ID,
			"hint", "토픽 생성/관리 기능을 쓰려면 Manage Topics 권한이 필요합니다")
	}
	p.logger.Info("bot promoted to administrator in forum supergroup",
		"chatId", evt.Chat.ID, "title", evt.Chat.Title)

	greeting := "👋 deneb 봇이 admin 으로 추가되었습니다.\n\n" +
		"이 supergroup 의 토픽을 통해 주제별로 대화를 분리할 수 있어요:\n" +
		"• 좌측 슬라이드로 토픽 목록 보기\n" +
		"• \"새 토픽\" 버튼으로 주제 생성\n" +
		"• 토픽 안에서 cron 만들면 결과도 같은 토픽으로\n\n" +
		"본격적으로 이곳을 deneb 의 home 으로 만들려면 `/use-forum` 을 입력하세요.\n" +
		"그 전까지는 1:1 채팅이 home 으로 유지됩니다."
	p.sendCommandReply(chatID, "", &handlers.CommandResult{Reply: greeting})
}

// onManageTopicsRevoked warns the user that the one permission gating
// topic operations was just rolled back. Posted to General (threadID="")
// because no specific topic context applies — the alert is about the
// chat-level permission, not a particular topic.
func (p *InboundProcessor) onManageTopicsRevoked(chatID string) {
	p.logger.Warn("bot lost Manage Topics permission", "chatId", chatID)
	warning := "⚠️ deneb 의 \"Manage Topics\" 권한이 회수되었습니다.\n\n" +
		"토픽 생성/수정 기능이 제한됩니다. " +
		"Admin 설정에서 \"Manage Topics\" 를 다시 활성화하면 정상화됩니다."
	p.sendCommandReply(chatID, "", &handlers.CommandResult{Reply: warning})
}
