// Package evenapi exposes an Even Realities G2 Custom AI bridge on the
// gateway: OpenAI-shaped chat completions in, short HUD text out via a
// dedicated glasses:* sync chat session. Wormhole /v1/chat/completions is a
// model proxy — do not reuse it for agent ingress.
package evenapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

const (
	// ResponseDeadline is how long Even Custom AI typically waits before
	// giving up. Return an ack past this; the turn may continue in-session.
	ResponseDeadline = 15 * time.Second

	// DedupeWindow collapses Even's identical POST retries.
	DedupeWindow = 5 * time.Second

	glassesSystemHint = "You are answering via Even Realities G2 smart glasses. " +
		"Reply in short Korean plain text only. No markdown, code fences, URLs, or emoji. " +
		"Keep under 400 characters. If the task needs longer work, say one short status line " +
		"and continue the full result in the phone Deneb session."

	ackLongRunning = "확인. 처리 중 — 결과는 폰 데네브에서 이어서 볼게요."

	// steerMaxRunes matches chat.send_sync's auto-steer bound: short mid-turn
	// follow-ups fold into the active glasses:main run via SendSync, but longer
	// utterances would start a second concurrent run once the HTTP deadline ack
	// has already returned while the first turn is still executing.
	steerMaxRunes = 400
)

// Config wires the G2 bridge.
type Config struct {
	Chat   chatport.SyncRunner
	Logger *slog.Logger
	// Token, when non-empty, overrides env DENEB_EVEN_G2_BRIDGE_TOKEN (tests).
	Token string
	// SessionKey defaults to glasses:main.
	SessionKey string
	// ResponseDeadline overrides ResponseDeadline (tests).
	ResponseDeadline time.Duration
	// Now, when set, replaces time.Now (tests).
	Now func() time.Time
	// Sources feeds GET /api/even/glance (optional; empty → calm empty copy).
	Sources GlanceSources
	// AckAlert marks one glance alert handled (optional; nil → 503 on POST
	// /api/even/ack, which is how a build without a workfeed store behaves).
	AckAlert func(id string) error
}

// Handler serves OpenAI-shaped Custom AI requests for Even G2.
type Handler struct {
	chat     chatport.SyncRunner
	logger   *slog.Logger
	token    string
	session  string
	deadline time.Duration
	now      func() time.Time
	sources  GlanceSources
	ackAlert func(id string) error

	mu          sync.Mutex
	dedupe      map[string]dedupeEntry
	notice      pendingNotice
	glanceCache glanceCache
	activeTurn  atomic.Int32 // in-flight RunSync calls for this bridge
}

// pendingNotice is one short line Deneb wants on the glass without being asked.
//
// It exists for the answer that arrives too late: a turn past ResponseDeadline
// gets "결과는 폰 데네브에서 이어서 볼게요" and the real reply then only reaches
// the wearer if Even happens to retry the POST. Otherwise the thing they asked
// for is stranded on the phone. The Glance plugin polls this handler anyway, so
// the answer can simply ride along and land on the HUD.
type pendingNotice struct {
	ID   string
	Text string
	At   time.Time
}

type dedupeEntry struct {
	body      chatCompletionResponse
	expiresAt time.Time
}

// New builds a G2 bridge handler. When neither Config.Token nor the env token
// is set, ServeHTTP returns 503 (feature off).
func New(cfg Config) *Handler {
	sessionKey := strings.TrimSpace(cfg.SessionKey)
	if sessionKey == "" {
		sessionKey = session.GlassesWorkSessionKey
	}
	deadline := cfg.ResponseDeadline
	if deadline <= 0 {
		deadline = ResponseDeadline
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		token = LoadBridgeToken()
	}
	return &Handler{
		chat:     cfg.Chat,
		logger:   cfg.Logger,
		token:    token,
		session:  sessionKey,
		deadline: deadline,
		now:      now,
		sources:  cfg.Sources,
		ackAlert: cfg.AckAlert,
		dedupe:   make(map[string]dedupeEntry),
	}
}

// Enabled reports whether a bridge token is configured.
func (h *Handler) Enabled() bool {
	return h != nil && strings.TrimSpace(h.token) != ""
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []chatCompletionChoice `json:"choices"`
}

type chatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatCompletions handles POST /v1/chat/completions and POST /api/even/v1/chat/completions.
func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		http.Error(w, "even g2 bridge unavailable", http.StatusServiceUnavailable)
		return
	}
	if !h.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "even g2 bridge disabled (set "+EnvBridgeToken+")")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	presented := ParseBearer(r.Header.Get("Authorization"))
	if !tokenMatch(h.token, presented) {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.chat == nil || !h.chat.ChatReady() {
		writeErr(w, http.StatusServiceUnavailable, "chat not ready")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body failed")
		return
	}
	var req chatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	utterance := lastUserContent(req.Messages)
	if utterance == "" {
		writeErr(w, http.StatusBadRequest, "missing user message")
		return
	}

	if cached, ok := h.lookupDedupe(utterance); ok {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "deneb"
	}

	// Long follow-ups while a glasses turn is still running would bypass
	// auto-steer and race a second executeAgentRun on the same session once
	// this handler has already returned the deadline ack for the first turn.
	if h.activeTurn.Load() > 0 && utf8.RuneCountInString(utterance) > steerMaxRunes {
		resp := h.completion(model, ackLongRunning)
		h.storeDedupe(utterance, resp)
		writeJSON(w, http.StatusOK, resp)
		return
	}

	type runOut struct {
		res *chatport.SyncResult
		err error
	}
	outCh := make(chan runOut, 1)
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), chatport.InteractiveTurnDeadline)
	h.activeTurn.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil && h.logger != nil {
				h.logger.Error("even g2 bridge: panic in background turn", "panic", r)
			}
		}()
		defer h.activeTurn.Add(-1)
		defer cancel()
		res, err := h.chat.RunSync(runCtx, chatport.SyncRequest{
			SessionKey:   h.session,
			Message:      utterance,
			SystemPrompt: glassesSystemHint,
			Delivery: &chatport.DeliveryContext{
				Channel: "client",
				To:      h.session,
			},
			AutoDeliveredOutput: true,
			GateUntrustedTools:  true,
		})
		outCh <- runOut{res: res, err: err}
	}()

	timer := time.NewTimer(h.deadline)
	defer timer.Stop()

	select {
	case <-timer.C:
		resp := h.completion(model, ackLongRunning)
		h.storeDedupe(utterance, resp)
		writeJSON(w, http.StatusOK, resp)
		// Even retries identical POSTs within DedupeWindow. The ack must not
		// remain the cached answer once the detached turn finishes — otherwise
		// retries never see the real reply and the HUD stays on "처리 중".
		go func() {
			out := <-outCh
			if out.err != nil {
				if h.logger != nil {
					h.logger.Warn("even g2 bridge: background turn failed after ack", "error", out.err)
				}
				return
			}
			text := ""
			finalModel := model
			if out.res != nil {
				text = out.res.BestText
				if text == "" {
					text = out.res.Text
				}
				if out.res.Model != "" {
					finalModel = out.res.Model
				}
			}
			cleaned := CleanForG2(text)
			if cleaned == "" {
				return
			}
			h.storeDedupe(utterance, h.completion(finalModel, cleaned))
			// …and hand it to the glasses, which is where the wearer asked from.
			h.setNotice(cleaned)
		}()
	case out := <-outCh:
		if out.err != nil {
			if h.logger != nil {
				h.logger.Warn("even g2 bridge: sync failed", "error", out.err)
			}
			writeErr(w, http.StatusBadGateway, "chat failed")
			return
		}
		text := ""
		if out.res != nil {
			text = out.res.BestText
			if text == "" {
				text = out.res.Text
			}
			if out.res.Model != "" {
				model = out.res.Model
			}
		}
		cleaned := CleanForG2(text)
		if cleaned == "" {
			cleaned = "응답이 비어 있어요. 폰 데네브에서 확인해 주세요."
		}
		resp := h.completion(model, cleaned)
		h.storeDedupe(utterance, resp)
		writeJSON(w, http.StatusOK, resp)
	}
}

func (h *Handler) completion(model, content string) chatCompletionResponse {
	return chatCompletionResponse{
		ID:      shortid.New("chatcmpl"),
		Object:  "chat.completion",
		Created: h.now().Unix(),
		Model:   model,
		Choices: []chatCompletionChoice{{
			Index: 0,
			Message: chatMessage{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: "stop",
		}},
	}
}

func lastUserContent(messages []chatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

func (h *Handler) lookupDedupe(utterance string) (chatCompletionResponse, bool) {
	key := hashUtterance(utterance)
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.purgeDedupeLocked(now)
	ent, ok := h.dedupe[key]
	if !ok || now.After(ent.expiresAt) {
		return chatCompletionResponse{}, false
	}
	return ent.body, true
}

func (h *Handler) storeDedupe(utterance string, body chatCompletionResponse) {
	key := hashUtterance(utterance)
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.purgeDedupeLocked(now)
	h.dedupe[key] = dedupeEntry{body: body, expiresAt: now.Add(DedupeWindow)}
}

func (h *Handler) purgeDedupeLocked(now time.Time) {
	for k, ent := range h.dedupe {
		if now.After(ent.expiresAt) {
			delete(h.dedupe, k)
		}
	}
}

func hashUtterance(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

func tokenMatch(expected, presented string) bool {
	if expected == "" || presented == "" {
		return false
	}
	if len(expected) != len(presented) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(presented)) == 1
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "invalid_request_error",
		},
	})
}

// NoticeTTL bounds how long an unclaimed notice keeps being offered.
//
// The plugin only polls every 45s and may be backgrounded, so the notice cannot
// be cleared the moment it is read out — a dropped poll would lose the answer.
// It is idempotent instead: the plugin remembers the last id it showed, and the
// gateway simply stops offering this one after a while.
const NoticeTTL = 10 * time.Minute

func (h *Handler) setNotice(text string) {
	if h == nil || strings.TrimSpace(text) == "" {
		return
	}
	at := h.now()
	h.mu.Lock()
	h.notice = pendingNotice{
		// Monotonic within a process and stable across re-reads, which is all
		// the plugin needs to tell "same answer" from "a new one".
		ID:   strconv.FormatInt(at.UnixMilli(), 10),
		Text: text,
		At:   at,
	}
	h.mu.Unlock()
}

// takeNotice returns the notice still worth showing, if any.
func (h *Handler) takeNotice() (pendingNotice, bool) {
	if h == nil {
		return pendingNotice{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.notice.Text == "" {
		return pendingNotice{}, false
	}
	if h.now().Sub(h.notice.At) > NoticeTTL {
		h.notice = pendingNotice{}
		return pendingNotice{}, false
	}
	return h.notice, true
}
