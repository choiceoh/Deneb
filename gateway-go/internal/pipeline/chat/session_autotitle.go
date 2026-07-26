package chat

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// Auto-titling of native-client conversations.
//
// The native client's conversation drawer renders each session's Label (via the
// miniapp.sessions.recent RPC). A fresh native chat — started by the app's "new
// chat" with a session key like "client:main:<uuid>" (a child of the client:main
// home) — has no label, so the drawer fell back to a generic "내 대화 · a1b2c3d4".
// Here we derive a short Korean title from the first exchange using the
// lightweight model role and patch it onto the session. The client needs zero
// changes: every surface that reads Label benefits.

const (
	sessionTitleMaxTokens = 24
	sessionTitleInputCap  = 600 // runes of the user message fed to the titler
	sessionTitleReplyCap  = 400 // runes of the reply fed for topic context
	sessionTitleLabelCap  = 40  // runes; the drawer truncates anyway
	sessionTitleTimeout   = 15 * time.Second
	// retitleEveryTurns: after the first title, refresh it every Nth user turn so
	// a long conversation's title tracks its drifting topic instead of freezing on
	// the opening question. The turn number = the transcript's user-message count.
	retitleEveryTurns = 20
	// sessionRetitleMinUserLen: a re-title turn whose user message is shorter than
	// this (runes) is skipped, so a trivial follow-up ("고마워") can't overwrite a
	// good title with "감사 인사" — the next boundary turn re-titles instead.
	sessionRetitleMinUserLen = 10
)

// nativeChatSessionPrefixes scopes auto-titling to per-conversation native
// chats: the 업무 home's children ("client:main:<uuid>") plus the legacy chat:
// namespace (the retired 챗봇 workspace — no new chat: sessions are minted, but
// an old conversation continued from the drawer still deserves a title). The
// 업무 proactive-report home ("client:main", no trailing ":") and an empty key
// are excluded by the non-empty-suffix check in isAutoTitleSession.
var nativeChatSessionPrefixes = []string{"client:main:", "chat:"}

// isAutoTitleSession reports whether a session key is an eligible per-conversation
// native chat that should receive an auto-derived title. The suffix after the
// prefix must be non-empty, so the bare 업무 home "client:main" and a bare
// "chat:" never match — only real conversations do.
func isAutoTitleSession(sessionKey string) bool {
	// A delegated sub-agent run lives under client:main:<label>:<ms> — same
	// prefix, but it is work the agent spawned, not a conversation with a
	// drawer row to name. Titling one burned an LLM call per spawn.
	if session.IsSpawnedChildKey(sessionKey) {
		return false
	}
	for _, p := range nativeChatSessionPrefixes {
		if strings.HasPrefix(sessionKey, p) && len(sessionKey) > len(p) {
			return true
		}
	}
	return false
}

// IsAutoTitleSessionKey is the exported eligibility check for callers outside
// the chat package (the server's restart-backfill titles restored sessions with
// the same scope this live path uses).
func IsAutoTitleSessionKey(sessionKey string) bool { return isAutoTitleSession(sessionKey) }

const sessionTitleSystemPrompt = "다음 대화가 '무엇에 관한 것인지'를 한국어 명사구 제목으로 요약하라. " +
	"대화에 나오는 구체적 대상(회사·거래처·인물·문서·안건 같은 고유명사)이 있으면 그것을 제목의 핵심으로 삼아라. " +
	"'~완료'·'~확인'·'~요청' 같은 행위·상태 서술이 아니라 무엇에 관한 대화인지(주제·대상)를 제목으로 하라. " +
	"3~6단어, 한 줄, 따옴표·마침표·번호 매기기·\"제목:\" 같은 접두어 없이 제목 텍스트만 출력하라."

// A shared-document filename is often the single strongest topic signal of a
// 공유(share)/첨부 conversation's opening message (e.g. "…양명에너지 주식양수도계약
// .docx"). The small titler model tends to over-index on the question/answer
// verbs ("근거 확인 및 판정 기록 완료") and skip it, so we lift document names out
// and hand them to the model as an explicit title hint rather than trusting it
// to notice them in the body. Two shapes:
//   - parenthesized "(NAME.ext)" — the share/attachment markers wrap the name in
//     parens, so this captures names that contain spaces (Korean filenames do).
//   - bare "NAME.ext" — a whitespace-delimited token, for names dropped inline.
var (
	docExtGroup    = `(?:pdf|docx?|xlsx?|pptx?|hwpx?|odt|ods|odp|txt|csv)`
	docNameParenRe = regexp.MustCompile(`(?i)\(([^()\n]{1,80}?\.` + docExtGroup + `)\)`)
	docNameBareRe  = regexp.MustCompile(`(?i)\S+\.` + docExtGroup + `\b`)
)

// documentHints returns up to two distinct shared-document names found in the
// message. Parenthesized names win; a bare token that is only the tail of an
// already-found fuller name is dropped (so a spaced name isn't double-counted).
func documentHints(msg string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(name string) bool {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return false
		}
		for _, ex := range out {
			if strings.HasSuffix(ex, name) {
				return false // just the tail of a fuller name already captured
			}
		}
		seen[name] = true
		out = append(out, name)
		return len(out) >= 2
	}
	for _, m := range docNameParenRe.FindAllStringSubmatch(msg, 8) {
		if add(m[1]) {
			return out
		}
	}
	for _, m := range docNameBareRe.FindAllString(msg, 8) {
		if add(m) {
			return out
		}
	}
	return out
}

// autoTitleSessionAsync derives (and periodically refreshes) a conversation
// title for a native chat session, in the background — titling must never delay
// the reply. The first turn titles an unlabeled session; thereafter the title is
// refreshed every retitleEveryTurns turns so it tracks a long conversation's
// drifting topic. A user rename pins the label (session.LabelPinned) and is never
// overwritten. No-op for non-native sessions, the bare client:main home, an empty
// message, or a missing session manager.
func (h *Handler) autoTitleSessionAsync(sessionKey, userMsg string, result *SyncResult) {
	if h.sessions == nil || result == nil || h.briefcaseMode {
		return
	}
	// Only per-conversation native chats ("client:main:<uuid>" / "chat:<uuid>").
	// The bare 업무 home ("client:main") and cron/system keys are excluded.
	if !isAutoTitleSession(sessionKey) {
		return
	}
	if strings.TrimSpace(userMsg) == "" {
		return
	}
	s := h.sessions.Get(sessionKey)
	if s == nil {
		return
	}
	// A user-renamed conversation is locked — the auto-titler never touches it.
	if s.LabelPinned {
		return
	}
	hadLabel := strings.TrimSpace(s.Label) != ""
	reply := result.BestText()

	safego.GoWithSlog(h.logger, "session-autotitle", func() {
		// Background context: the request ctx is canceled the moment the RPC
		// responds, but titling outlives the turn. Bounded so a stuck lightweight
		// model can't leak a goroutine. This is genuinely post-response background
		// work, hence context.Background rather than a request-scoped ctx.
		ctx, cancel := context.WithTimeout(context.Background(), sessionTitleTimeout)
		defer cancel()

		if hadLabel {
			// Already titled → refresh only on a boundary turn, and skip trivial
			// follow-ups so they can't replace a good title with a filler one.
			if len([]rune(strings.TrimSpace(userMsg))) < sessionRetitleMinUserLen {
				return
			}
			if !isRetitleBoundary(h.countUserTurns(sessionKey)) {
				return
			}
		}

		title := GenerateSessionTitle(ctx, userMsg, reply)
		if title == "" {
			return
		}
		// Re-check under fresh state: a concurrent rename may have pinned a label
		// while the model was thinking, and for the first title a concurrent turn
		// may have raced one in (set-once).
		cur := h.sessions.Get(sessionKey)
		if cur == nil || cur.LabelPinned {
			return
		}
		if !hadLabel && strings.TrimSpace(cur.Label) != "" {
			return
		}
		h.sessions.Patch(sessionKey, session.PatchFields{Label: &title})
		if h.logger != nil {
			h.logger.Debug("session auto-titled", "sessionKey", sessionKey, "label", title, "retitle", hadLabel)
		}
	})
}

// isRetitleBoundary reports whether an already-titled session is due for a title
// refresh at this turn number — every retitleEveryTurns user turns, never at 0.
func isRetitleBoundary(turns int) bool {
	return turns > 0 && turns%retitleEveryTurns == 0
}

// countUserTurns returns the number of genuine user turns in the transcript —
// the turn number used to space out re-titling. A tool_result is persisted as a
// user-role message too, so counting raw user roles overcounts wildly in a
// tool-heavy conversation (7 tool calls → 7 phantom "turns"); isRealUserTurn
// filters those out. Best-effort: no transcript store or a load error yields 0,
// which skips the re-title.
func (h *Handler) countUserTurns(sessionKey string) int {
	if h.transcript == nil {
		return 0
	}
	msgs, _, err := h.transcript.Load(sessionKey, 0) // 0 = all messages
	if err != nil {
		return 0
	}
	n := 0
	for i := range msgs {
		if msgs[i].Role == "user" && isRealUserTurn(msgs[i].Content) {
			n++
		}
	}
	return n
}

// isRealUserTurn reports whether a user-role transcript message is an actual user
// input rather than a tool_result carrier. Plain-string content (the common text
// case) fails the block-array unmarshal and counts as a turn; a block array counts
// only if it holds a non-tool_result block. Same tool_result detection as
// toolport.StripToolResultBlocksForDisplay.
func isRealUserTurn(content json.RawMessage) bool {
	var blocks []json.RawMessage
	if json.Unmarshal(content, &blocks) != nil {
		return true // plain-string content = a genuine user message
	}
	for _, b := range blocks {
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(b, &head) == nil && head.Type == "tool_result" {
			continue
		}
		return true // a non-tool_result block (e.g. text) = genuine turn
	}
	return false // only tool_result(s) = a tool-response carrier, not a turn
}

// GenerateSessionTitle asks the tiny model for a short Korean title, falling
// back to a heuristic (the first line of the user message) when the model is
// unavailable or returns nothing usable. Exported for the server's restart
// backfill (restored sessions lost their labels before the sidecar store).
func GenerateSessionTitle(ctx context.Context, userMsg, reply string) string {
	prompt := "사용자: " + capRunes(userMsg, sessionTitleInputCap)
	if r := strings.TrimSpace(reply); r != "" {
		prompt += "\n어시스턴트: " + capRunes(r, sessionTitleReplyCap)
	}
	// Surface any shared-document names as an explicit topic anchor — the titler
	// otherwise buries the filename under the turn's verbs.
	if hints := documentHints(userMsg); len(hints) > 0 {
		prompt += "\n공유 문서(제목의 핵심 후보): " + strings.Join(hints, ", ")
	}
	out, err := pilot.CallTinyLLM(ctx, sessionTitleSystemPrompt, prompt, sessionTitleMaxTokens)
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
	// The model sometimes still prefixes "제목:", echoes the prompt's speaker
	// tags ("사용자:"/"어시스턴트:"), or wraps the title in quotes despite the
	// instruction — strip those.
	s = strings.TrimSpace(strings.TrimPrefix(s, "제목:"))
	s = strings.TrimSpace(strings.TrimPrefix(s, "사용자:"))
	s = strings.TrimSpace(strings.TrimPrefix(s, "어시스턴트:"))
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
