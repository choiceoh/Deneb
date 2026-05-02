package chat

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// applyThinkSlashCommand handles `/think [subcommand]` and returns the user
// reply string. The session is mutated in place via sessions.Set when a
// settings change is requested.
//
// Supported forms:
//
//	/think                       → reports current state
//	/think interleaved           → toggles interleaved thinking
//	/think interleaved on|off    → sets interleaved thinking explicitly
//	/think show                  → toggles surfacing thinking text in chat
//	/think show on|off           → sets thinking-text surfacing explicitly
func applyThinkSlashCommand(sessions *session.Manager, sessionKey, args string) string {
	if sessions == nil {
		return "세션 매니저가 준비되지 않았습니다."
	}
	sess := sessions.Get(sessionKey)
	if sess == nil {
		return "세션이 없습니다."
	}

	tokens := strings.Fields(strings.ToLower(strings.TrimSpace(args)))
	if len(tokens) == 0 {
		return reportThinkState(sess)
	}

	switch tokens[0] {
	case "interleaved", "인터리브", "인터리브드":
		next := !interleavedOn(sess)
		if len(tokens) >= 2 {
			switch tokens[1] {
			case "on", "켜기", "켬":
				next = true
			case "off", "끄기", "끔":
				next = false
			}
		}
		sess.InterleavedThinking = boolPtr(next)
		_ = sessions.Set(sess) // best-effort: in-memory store, error unreachable
		if next {
			if sess.ThinkingLevel == "" || sess.ThinkingLevel == "off" {
				return "🧠 인터리브드 사고 ON — 도구 호출 사이에 생각 블록이 끼어듭니다.\n참고: /think 레벨이 꺼져 있어 실제 동작은 레벨이 켜진 뒤부터 적용됩니다."
			}
			return "🧠 인터리브드 사고 ON — 도구 호출 사이에 생각 블록이 끼어듭니다."
		}
		return "🧠 인터리브드 사고 OFF — 도구 호출 후 생각 블록이 다음 턴으로 넘어가지 않습니다."
	case "show", "표출", "표시", "보이기":
		next := !showThinkingOn(sess)
		if len(tokens) >= 2 {
			switch tokens[1] {
			case "on", "켜기", "켬":
				next = true
			case "off", "끄기", "끔":
				next = false
			}
		}
		sess.ShowThinkingInChat = boolPtr(next)
		_ = sessions.Set(sess) // best-effort: in-memory store, error unreachable
		if next {
			if sess.ThinkingLevel == "" || sess.ThinkingLevel == "off" {
				return "👁️ 사고 표출 ON — 답변 위에 추론 과정이 펼침 인용으로 첨부됩니다.\n참고: /think 레벨이 꺼져 있어 실제 추론 텍스트가 비어 있을 수 있습니다."
			}
			return "👁️ 사고 표출 ON — 답변 위에 추론 과정이 펼침 인용으로 첨부됩니다."
		}
		return "👁️ 사고 표출 OFF — 답변에서 추론 블록이 숨겨집니다."
	}
	return reportThinkState(sess)
}

func reportThinkState(sess *session.Session) string {
	level := sess.ThinkingLevel
	if level == "" {
		level = "off"
	}
	interleaved := "off"
	if interleavedOn(sess) {
		interleaved = "on"
	}
	show := "off"
	if showThinkingOn(sess) {
		show = "on"
	}
	return "현재 사고 모드: 레벨=" + level + ", 인터리브드=" + interleaved + ", 표출=" + show + "."
}

func interleavedOn(sess *session.Session) bool {
	return sess != nil && sess.InterleavedThinking != nil && *sess.InterleavedThinking
}

func showThinkingOn(sess *session.Session) bool {
	if sess == nil {
		return true
	}
	if sess.ShowThinkingInChat == nil {
		return true
	}
	return *sess.ShowThinkingInChat
}

func boolPtr(b bool) *bool { return &b }
