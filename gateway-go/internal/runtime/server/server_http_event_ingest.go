// server_http_event_ingest.go — POST /api/event/ingest
//
// Receives a real-time event from the user's phone (a Termux agent reaching this
// loopback endpoint over an SSH session into the host) and runs a proactive
// 비서실장 judgment turn on it. If the event is worth surfacing, the agent's
// report lands in the native 업무 chat (client:main transcript + work-feed card +
// live push) through the SAME proactiveRelay path cron and gmail-poll already
// use; if not, relayNative's noise floor suppresses it.
//
// This is the server half of "phone ↔ server SSH link" Phase 0: a single generic
// ingestion door that notification / context / clipboard sources on the phone all
// funnel into. It deliberately reuses the existing proactive machinery rather than
// adding a parallel delivery path — the phone only supplies the event text; the
// gateway does the judgment + delivery exactly like every other proactive surface.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// phoneEventMaxTokens caps the judgment turn's reply. A phone-event alert should
// be a tight "왜 지금 중요한가 + 무엇을 언제까지" message, not an essay.
const phoneEventMaxTokens = 1536

// phoneEventTurnDeadline bounds the async judgment turn. Long enough for a few
// tool calls (calendar/wiki/mail/contact lookups) but capped so a wedged turn
// cannot leak a goroutine past graceful shutdown.
const phoneEventTurnDeadline = 4 * time.Minute

// phoneEventSessionPrefix scopes each event to a throwaway session key
// ("phone-event:<id>"). Combined with EphemeralUser/Assistant the run persists
// nothing, so events never accumulate history — unlike a fixed boot/heartbeat
// session that silently grows unbounded (the EphemeralUser doctrine).
const phoneEventSessionPrefix = "phone-event"

// phoneEventPromptTmpl frames a real-time phone event for the 비서실장 persona.
// The four %s are, in order: kind label, source, body text, and the NO_REPLY
// token.
//
// Two-stage noise control: this prompt instructs the model to emit NO_REPLY for
// non-actionable events (ads, OTP codes, receipts, routine system pings), and
// relayNative strips/suppresses that downstream (StripSilentToken +
// isContentlessProactive) — so a "nothing to report" judgment never reaches the
// work feed or the push. The phone forwards everything; the gateway decides.
const phoneEventPromptTmpl = `[실시간 스마트폰 이벤트 — %s]
출처: %s
내용:
%s

위는 사용자 스마트폰에서 방금 발생한 이벤트다. 비서실장으로서 판단하라:

1. 지금 사용자에게 알릴 가치가 있는가? 광고·스팸·인증번호(OTP)·결제 영수증·일상적 시스템 알림처럼 별도 행동이 필요 없으면 다른 말 없이 %s 만 출력하라.
2. 알릴 가치가 있으면, 필요한 도구(캘린더·메일·위키·연락처)로 맥락을 직접 확인한 뒤 한 메시지로 보고하라:
   • 왜 지금 중요한가 — 관련 일정·거래·인물 맥락
   • 무엇을·언제까지 — 구체적인 다음 행동
3. 인사·빈 서두·내부 토큰 금지. 능동 알림이므로 사용자 호명 없이 바로 본론으로.`

// phoneEventKindLabel maps an event type to a short Korean descriptor used in the
// judgment prompt. Unknown types pass through verbatim — the type is a display
// label, not a hard enum, so Phase 0 keeps ingestion permissive.
func phoneEventKindLabel(eventType string) string {
	switch strings.TrimSpace(strings.ToLower(eventType)) {
	case "notification", "":
		return "앱 알림"
	case "context":
		return "상황 변화 (위치·네트워크 등)"
	case "clipboard":
		return "클립보드 캡처"
	case "sms":
		return "문자 메시지"
	default:
		return eventType
	}
}

// handleEventIngest accepts a phone event and queues a proactive judgment turn.
//
// Body: {"type":"notification|context|clipboard|...","source":"카카오톡","text":"..."}
//   - text is required; type defaults to "notification"; source is a free label.
//
// Response:
//
//	202 {"status":"accepted"}        — judgment queued (fire-and-forget)
//	400 {"error":"text is required"}
//	403 {"error":"localhost only"}
//	503 {"error":"chat handler unavailable"}
//
// Auth: localhost-only, identical to /api/cron/run — the phone authenticates by
// holding an SSH session into the host, and the gateway binds loopback by default,
// so no gateway-level token is required. The judgment runs asynchronously on the
// server lifecycle context (it survives the HTTP request but is cancelled on
// graceful shutdown); the HTTP response returns as soon as the turn is queued.
func (s *Server) handleEventIngest(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRemote(r.RemoteAddr) {
		s.writeJSON(w, http.StatusForbidden, map[string]any{"error": "localhost only"})
		return
	}
	if s.chatHandler == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "chat handler unavailable"})
		return
	}

	var req struct {
		Type   string `json:"type"`
		Source string `json:"source"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "text is required"})
		return
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "(미상)"
	}
	eventType := req.Type

	command := fmt.Sprintf(phoneEventPromptTmpl,
		phoneEventKindLabel(eventType), source, text, chat.SilentReplyToken)

	// Fire-and-forget: the judgment turn (with tool calls) can take seconds, but
	// the phone only needs to know the event was accepted. The proactive report
	// arrives later via push — exactly like a cron run.
	safego.GoWithSlog(s.logger, "phone-event-ingest", func() {
		ctx, cancel := context.WithTimeout(s.ShutdownCtx(), phoneEventTurnDeadline)
		defer cancel()

		maxTok := phoneEventMaxTokens
		sessionKey := phoneEventSessionPrefix + ":" + shortid.New("e")
		result, err := s.chatHandler.SendSync(ctx, sessionKey, command, "", &chat.SyncOptions{
			MaxTokens:           &maxTok,
			EphemeralUser:       true, // throwaway session — persist nothing
			EphemeralAssistant:  true,
			AutoDeliveredOutput: true, // relayNative delivers; agent must not use message tool
		})
		if err != nil {
			s.logger.Error("phone-event judgment turn failed",
				"source", source, "type", eventType, "error", err)
			return
		}
		// relayNative applies the same noise floor as every proactive surface:
		// a NO_REPLY or "별 일 없음" judgment is suppressed (delivered=false) and
		// never reaches the work feed or push.
		output := result.BestText()
		delivered, relayErr := s.proactiveRelay.relayNative(output)
		if relayErr != nil {
			s.logger.Error("phone-event relay failed",
				"source", source, "type", eventType, "error", relayErr)
			return
		}
		s.logger.Info("phone-event processed",
			"source", source, "type", eventType,
			"delivered", delivered, "outputLen", len(output))
	})

	s.writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted"})
}
