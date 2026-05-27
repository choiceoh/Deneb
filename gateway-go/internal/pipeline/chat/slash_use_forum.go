package chat

import (
	"strconv"
)

// handleUseForum binds the bot's "active home" to the supergroup that
// invoked the command. After this point inbound.go rejects messages from
// other chats (the old 1:1) so the user has one obvious place to type.
//
// The handler is intentionally conservative:
//   - Refuses if AppSettings was not wired (no persistence available — would
//     forget the choice on restart).
//   - Refuses if the delivery target looks like a direct chat (positive
//     Telegram IDs). Telegram supergroup IDs are always negative, so this
//     catches the common "user ran /use-forum in the 1:1 by mistake" case
//     without an extra getChat API call.
//   - Returns a single user-facing string explaining either the success or
//     the refusal; the caller delivers it via the normal slash reply path.
//
// The old 1:1 transcript files in ~/.deneb/transcripts/ are left in place
// rather than physically moved — they become orphans of the new active
// home and survive on disk for any future inspection. That matches the
// "fresh start + read-only backup" decision (Q6-3) at minimum blast radius
// — no transcript-store interface changes, no risk of mid-rename data loss.
func (h *Handler) handleUseForum(delivery *DeliveryContext) string {
	if h.appSettings == nil {
		return "⚠️ /use-forum 사용 불가 — 영구 설정 저장소가 초기화되지 않았습니다. 게이트웨이 로그를 확인하세요."
	}
	if delivery == nil || delivery.To == "" {
		return "⚠️ /use-forum 은 텔레그램에서만 동작합니다."
	}
	chatID, err := strconv.ParseInt(delivery.To, 10, 64)
	if err != nil {
		return "⚠️ /use-forum: 채팅 ID 를 해석할 수 없습니다."
	}
	// Telegram convention: supergroup chat IDs are negative (and prefixed
	// with -100 specifically). A positive ID is a direct chat, which is
	// exactly the chat we're trying to migrate AWAY from.
	if chatID >= 0 {
		return "⚠️ /use-forum 은 supergroup 안에서 실행해야 합니다. 현재 채팅은 1:1 직접 대화로 보입니다."
	}
	if err := h.appSettings.SetActiveHome(chatID, "supergroup"); err != nil {
		h.logger.Error("use-forum: failed to persist active home",
			"chatId", chatID, "error", err)
		return "⚠️ 설정 저장에 실패했습니다. 로그를 확인하세요."
	}
	h.logger.Info("use-forum: active home set",
		"chatId", chatID, "type", "supergroup")
	return "✅ 이 supergroup 이 이제 deneb 의 home 입니다.\n\n" +
		"앞으로 모든 봇 활동(채팅·알림·cron·메모리)이 이곳에서 이루어집니다. " +
		"옛 1:1 채팅으로 보낸 메시지는 무시되고 안내만 돌아갑니다 " +
		"(옛 대화 기록은 ~/.deneb/transcripts/ 에 그대로 보존)."
}
