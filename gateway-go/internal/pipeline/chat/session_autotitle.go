package chat

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// Auto-titling of native-client conversations.
//
// The native client's conversation drawer renders each session's Label (via the
// miniapp.sessions.recent RPC). Telegram sessions get a label at inbound time
// (sender / chat name, see server/inbound.go), but a fresh native chat — session
// key "client:main:<uuid>" started by the app's "new chat" — has no label, so the
// drawer fell back to "내 대화 · a1b2c3d4". Here we derive a short Korean title
// from the first exchange using the lightweight model role and patch it onto the
// session. The client needs zero changes: every surface that reads Label benefits.

const (
	sessionTitleMaxTokens = 24
	sessionTitleInputCap  = 600 // runes of the user message fed to the titler
	sessionTitleReplyCap  = 400 // runes of the reply fed for topic context
	sessionTitleLabelCap  = 40  // runes; the drawer truncates anyway
	sessionTitleTimeout   = 15 * time.Second

	// nativeChatSessionPrefix scopes auto-titling to per-conversation native
	// sub-sessions. The bare "client:main" work home (where proactive reports
	// land) is intentionally NOT titled — it keeps its fixed home identity.
	nativeChatSessionPrefix = "client:main:"
)

const sessionTitleSystemPrompt = "다음 대화를 보고 주제를 한국어 명사구 제목으로 요약하라. " +
	"3~6단어, 한 줄, 따옴표·마침표·번호 매기기·\"제목:\" 같은 접두어 없이 제목 텍스트만 출력하라."

// autoTitleSessionAsync derives and stores a conversation title for a fresh
// native chat session, in the background — titling must never delay the reply.
// Set-once: a session that already has a label is skipped, so titles never churn
// (keeps the drawer and any prefix caching stable). No-op for non-native sessions,
// the bare client:main work home, an empty message, or a missing session manager.
func (h *Handler) autoTitleSessionAsync(sessionKey, userMsg string, result *SyncResult) {
	if h.sessions == nil || result == nil {
		return
	}
	// Only per-conversation native sub-sessions ("client:main:<uuid>"). The bare
	// "client:main" home, telegram:* (labeled at inbound), and cron/system keys
	// are all intentionally excluded.
	if !strings.HasPrefix(sessionKey, nativeChatSessionPrefix) {
		return
	}
	if strings.TrimSpace(userMsg) == "" {
		return
	}
	// Set-once: never overwrite an existing label.
	if s := h.sessions.Get(sessionKey); s == nil || strings.TrimSpace(s.Label) != "" {
		return
	}
	reply := result.BestText()

	safego.GoWithSlog(h.logger, "session-autotitle", func() {
		// Background context: the request ctx is canceled the moment the RPC
		// responds, but titling outlives the turn. Bounded so a stuck lightweight
		// model can't leak a goroutine. This is genuinely post-response background
		// work, hence context.Background rather than a request-scoped ctx.
		ctx, cancel := context.WithTimeout(context.Background(), sessionTitleTimeout)
		defer cancel()

		title := generateSessionTitle(ctx, userMsg, reply)
		if title == "" {
			return
		}
		// Re-check set-once: a concurrent turn may have raced a label in while the
		// model was thinking.
		if s := h.sessions.Get(sessionKey); s == nil || strings.TrimSpace(s.Label) != "" {
			return
		}
		h.sessions.Patch(sessionKey, session.PatchFields{Label: &title})
		if h.logger != nil {
			h.logger.Debug("session auto-titled", "sessionKey", sessionKey, "label", title)
		}
	})
}

// generateSessionTitle asks the lightweight model for a short Korean title,
// falling back to a heuristic (the first line of the user message) when the model
// is unavailable or returns nothing usable.
func generateSessionTitle(ctx context.Context, userMsg, reply string) string {
	prompt := "사용자: " + capRunes(userMsg, sessionTitleInputCap)
	if r := strings.TrimSpace(reply); r != "" {
		prompt += "\n어시스턴트: " + capRunes(r, sessionTitleReplyCap)
	}
	out, err := pilot.CallLocalLLM(ctx, sessionTitleSystemPrompt, prompt, sessionTitleMaxTokens)
	if title := cleanSessionTitle(out); err == nil && title != "" {
		return title
	}
	// Lightweight model down or output empty/garbage → heuristic fallback so the
	// session still gets a usable title (graceful degradation, like OCR→tesseract).
	return cleanSessionTitle(firstLine(userMsg))
}

// cleanSessionTitle normalizes a model/heuristic title to a single tidy line.
func cleanSessionTitle(s string) string {
	s = strings.TrimSpace(firstLine(s))
	// The model sometimes still prefixes "제목:" or wraps the title in quotes
	// despite the instruction — strip those.
	s = strings.TrimSpace(strings.TrimPrefix(s, "제목:"))
	s = strings.TrimSpace(strings.Trim(s, "\"'`“”‘’"))
	s = strings.TrimRight(s, ".。")
	s = strings.Join(strings.Fields(s), " ") // collapse internal whitespace/newlines
	return capRunes(s, sessionTitleLabelCap)
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func capRunes(s string, limit int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > limit {
		return strings.TrimSpace(string(r[:limit]))
	}
	return s
}
