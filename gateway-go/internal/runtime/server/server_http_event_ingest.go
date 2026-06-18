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
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// phoneHeartbeatPath is the liveness marker touched on each deneb-heartbeat ping.
// A host-side timer (deneb-phone-link-check) reads its mtime to decide whether the
// phone↔gateway tunnel has gone silently dead.
func phoneHeartbeatPath() string {
	return filepath.Join(config.ResolveStateDir(), "phone-heartbeat")
}

// recordPhoneHeartbeat updates the liveness marker's mtime to now. Best-effort:
// a write failure is logged but never blocks the 202 (the phone only needs ack).
func recordPhoneHeartbeat(logger *slog.Logger) {
	p := phoneHeartbeatPath()
	if err := os.WriteFile(p, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil && logger != nil {
		logger.Warn("phone heartbeat: write liveness marker failed", "path", p, "error", err)
	}
}

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
// The four %s are, in order: kind label, source, body text, and the type-specific
// guidance line (built by phoneEventGuidance, which embeds the NO_REPLY token).
//
// Per-type guidance matters: a notification is a "worth surfacing?" judgment, but
// a context event (회사 WiFi 접속 → 출근) or a clipboard capture (회의록 → 추출/요약)
// carries a different proactive intent. A single generic "알릴 가치 있나" prompt made
// the model NO_REPLY every context/clipboard event, so those sources never fired.
//
// Two-stage noise control still holds: each guidance instructs NO_REPLY for its
// own non-actionable case, and relayNative strips/suppresses that downstream
// (StripSilentToken + isContentlessProactive). The phone forwards everything; the
// gateway decides per type.
const phoneEventPromptTmpl = `[실시간 스마트폰 이벤트 — %s]
출처: %s
내용:
%s

위는 사용자 스마트폰에서 방금 발생한 이벤트다. 비서실장으로서 판단하라.

%s

보고할 때는 필요한 도구(캘린더·메일·위키·연락처)로 맥락을 직접 확인한 뒤 한 메시지로:
• 왜 지금 중요한가 — 관련 일정·거래·인물 맥락
• 무엇을·언제까지 — 구체적인 다음 행동
필요하면 폰 도구로 사용자의 현재 상황을 보강하라 — phone_read/phone_write는 deferred이므로 먼저 fetch_tools(names=["phone_read","phone_write"])로 활성화한다. phone_read(위치·클립보드·배터리)는 맥락 보강에(예: 위치로 출근/외근 판단, 클립보드로 직전 작업 맥락), 사용자가 화면을 못 볼 상황이 분명하면 phone_write(tts)로 폰에 음성으로 직접 읽어줘도 된다.
인사·빈 서두·내부 토큰 금지. 능동 알림이므로 사용자 호명 없이 바로 본론으로.`

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

// phoneEventGuidance returns the type-specific proactive-judgment instruction
// injected into phoneEventPromptTmpl. Each branch contains one %s for the NO_REPLY
// token (filled by the caller). Per-type intent is the whole point: a context
// transition or a clipboard capture is not a "worth surfacing?" alert, so a single
// generic prompt suppressed them as NO_REPLY.
func phoneEventGuidance(eventType string) string {
	switch strings.TrimSpace(strings.ToLower(eventType)) {
	case "context":
		return `이것은 상황 변화 신호다(위치·네트워크 등). 대부분의 상태 변화는 알릴 가치가 없으니 기본은 침묵이다 — 평일 아침 첫 출근 도착(→오늘 일정·우선업무 브리핑)이나 저녁 귀가(→하루 마감 요약)처럼 명확히 행동을 부르는 드문 전환일 때만 보고하라. 단순 이동·경유·반복 접속·시간대상 애매한 신호, 그리고 위치·네트워크 변화 자체의 중계는 전부 다른 말 없이 %s 만 출력하라.`
	case "clipboard":
		return `이것은 사용자가 복사(캡처)한 내용이다. 일정·할일·연락처·금액·주소가 들어 있으면 추출해 정리하고, 회의록·대화·문서면 핵심을 요약하라. 그냥 짧은 보관용 텍스트라 처리할 일이 없으면 다른 말 없이 %s 만 출력하라.`
	default: // notification, sms, and any free label
		return `지금 사용자에게 알릴 가치가 있는가? 광고·스팸·인증번호(OTP)·결제 영수증·일상적 시스템 알림처럼 별도 행동이 필요 없으면 다른 말 없이 %s 만 출력하라.`
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

	// Heartbeat (deneb-heartbeat): a liveness ping that traveled the whole chain
	// (phone → tunnel → here), proving the link is up. It must NOT spend a judgment
	// turn — just record the arrival time so a host-side timer can detect a silently
	// dead tunnel (no notifications + no heartbeats = link down). Returns at once.
	if strings.EqualFold(strings.TrimSpace(req.Type), "heartbeat") {
		recordPhoneHeartbeat(s.logger)
		s.writeJSON(w, http.StatusAccepted, map[string]any{"status": "alive"})
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
		phoneEventKindLabel(eventType), source, text,
		fmt.Sprintf(phoneEventGuidance(eventType), chat.SilentReplyToken))

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
