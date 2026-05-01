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
	}
	return reportThinkState(sess)
}

func reportThinkState(sess *session.Session) string {
	level := sess.ThinkingLevel
	if level == "" {
		level = "off"
	}
	state := "off"
	if interleavedOn(sess) {
		state = "on"
	}
	return "현재 사고 모드: 레벨=" + level + ", 인터리브드=" + state + "."
}

func interleavedOn(sess *session.Session) bool {
	return sess != nil && sess.InterleavedThinking != nil && *sess.InterleavedThinking
}

func boolPtr(b bool) *bool { return &b }
