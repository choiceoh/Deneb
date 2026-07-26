// Package phoneevents — POST /api/event/ingest
// Formerly server_http_event_ingest.go.
//
// Receives a real-time event from the user's phone — today the authenticated
// native-app path (miniapp.event.ingest, NotificationListener); the loopback
// path remains from the retired Termux/SSH bridge (#3099) — and runs a proactive
// 비서실장 judgment turn on it. If the event is worth surfacing, the agent's
// report lands in the native 업무 chat (client:main transcript + work-feed card +
// live push) through the SAME proactiveRelay path cron and gmail-poll already
// use; if not, relayNative's noise floor suppresses it.
//
// This is the server half of phone sensing: a single generic ingestion door that
// notification / context / clipboard / cached state sources on the phone all funnel
// into. It deliberately reuses the existing proactive machinery rather than adding
// a parallel delivery path — the phone only supplies the event text; the gateway
// does the judgment + delivery exactly like every other proactive surface.

package phoneevents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tokens "github.com/choiceoh/deneb/gateway-go/internal/core/replytokens"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// ActionResult is the native app's execution report for a dispatched phone action.
type ActionResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Config supplies the late-bound runtime dependencies for phone event handling.
type Config struct {
	ChatHandler chatport.SyncRunner
	// JudgmentModel is the model role (e.g. "submain") the phone-event judgment
	// turn runs on. Empty → the turn resolves to the main role as before; setting
	// it moves this high-volume lane off the interactive main subscription.
	JudgmentModel      string
	Relay              *proactive.Relay
	ResolvePhoneAction func(ActionResult) bool
	ShutdownContext    context.Context
	Logger             *slog.Logger
	// Ledger records notification/sms events for later wiki digestion
	// (ledger.go). nil disables recording; the judgment path is unaffected
	// either way.
	Ledger *Ledger
	// OnLocationPlace, if set, receives each location_update payload so a
	// site-visit recorder can match its geocoded place against project 현장
	// and log a visit. nil disables site-visit recording.
	OnLocationPlace func(payload string)
	// BrowserEnrich, if set, is called for electronic-approval notifications
	// before the judgment turn. The gateway orchestrates; the workstation Page
	// Agent bridge reads the user's logged-in Chrome. Return "" to skip
	// enrichment (bridge off / busy / failure) — judgment still runs on the
	// notification text alone.
	BrowserEnrich func(ctx context.Context, source, text string) string
	// TriggerApprovalScan, if set, runs one on-demand groupware-radar scan when
	// an electronic-approval notification arrives. The radar owns read→analyze→
	// feed for approvals (analysis body + 승인/반려 chips), so the phone push is
	// only a trigger, never a card source — a plain notification relay card
	// would carry no approval chips. On scan error the handler falls back to
	// the notification-relay judgment so a reader outage degrades to a
	// buttonless card instead of silence.
	TriggerApprovalScan func(ctx context.Context) error
}

// Handler accepts phone telemetry and runs proactive judgment turns.
type Handler struct {
	chatHandler         chatport.SyncRunner
	judgmentModel       string
	relay               *proactive.Relay
	resolvePhoneAction  func(ActionResult) bool
	shutdownContext     context.Context
	logger              *slog.Logger
	ledger              *Ledger
	onLocationPlace     func(payload string)
	browserEnrich       func(ctx context.Context, source, text string) string
	triggerApprovalScan func(ctx context.Context) error
}

// New creates a phone-event handler.
func New(cfg Config) *Handler {
	shutdownContext := cfg.ShutdownContext
	if shutdownContext == nil {
		shutdownContext = context.Background()
	}
	return &Handler{
		chatHandler:         cfg.ChatHandler,
		judgmentModel:       cfg.JudgmentModel,
		relay:               cfg.Relay,
		resolvePhoneAction:  cfg.ResolvePhoneAction,
		shutdownContext:     shutdownContext,
		logger:              cfg.Logger,
		ledger:              cfg.Ledger,
		onLocationPlace:     cfg.OnLocationPlace,
		browserEnrich:       cfg.BrowserEnrich,
		triggerApprovalScan: cfg.TriggerApprovalScan,
	}
}

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

// phoneLocationPath is the native client's last-known-location cache. The payload is
// stored verbatim (the FusedLocationProvider JSON the app pushed) and the file mtime
// is the freshness signal — same convention as the heartbeat marker. The phone_read
// tool reads this so the agent gets a recent location without an SSH round-trip.
func phoneLocationPath() string {
	return filepath.Join(config.ResolveStateDir(), "phone-location.json")
}

// recordPhoneLocation caches the native client's pushed location (best-effort).
func recordPhoneLocation(logger *slog.Logger, payload string) {
	p := phoneLocationPath()
	if err := os.WriteFile(p, []byte(strings.TrimSpace(payload)), 0o600); err != nil && logger != nil {
		logger.Warn("phone location: cache write failed", "path", p, "error", err)
	}
}

// phoneUsagePath is the native client's latest coarse app-usage digest. It is
// cache-only context for phone_read("usage"), never a proactive alert by itself.
func phoneUsagePath() string {
	return filepath.Join(config.ResolveStateDir(), "phone-usage.txt")
}

func recordPhoneUsage(logger *slog.Logger, payload string) {
	p := phoneUsagePath()
	if err := os.WriteFile(p, []byte(strings.TrimSpace(payload)), 0o600); err != nil && logger != nil {
		logger.Warn("phone usage: cache write failed", "path", p, "error", err)
	}
}

// phoneCallLogPath is the native client's latest recent-call digest. Cache-only
// context for phone_read("calllog"), like the usage digest: knowing who the
// operator spoke to and for how long is 인물/미팅 context worth having on hand,
// but a finished call is not by itself worth interrupting them over.
func phoneCallLogPath() string {
	return filepath.Join(config.ResolveStateDir(), "phone-calllog.txt")
}

func recordPhoneCallLog(logger *slog.Logger, payload string) {
	p := phoneCallLogPath()
	if err := os.WriteFile(p, []byte(strings.TrimSpace(payload)), 0o600); err != nil && logger != nil {
		logger.Warn("phone calllog: cache write failed", "path", p, "error", err)
	}
}

// phoneCrashPath is the on-disk log of native-client crash reports — uncaught
// exceptions the phone captured at crash time and forwarded on its next launch.
// The observe plane / logs-errors surface the one-line summary; this file holds
// the full stacks for a deep read.
func phoneCrashPath() string {
	return filepath.Join(config.ResolveStateDir(), "client-crashes.log")
}

// maxClientCrashLogBytes bounds the crash log so a crash loop cannot grow it
// without limit; the newest entries are kept.
const maxClientCrashLogBytes = 256 * 1024

// recordClientCrash logs a native-client crash at Error (a crash is the ultimate
// user-observable failure — logging.md rule 1, so it must not hide in Warn) and
// appends the full stack to a bounded on-disk log. It never runs a judgment turn:
// a crash report is diagnostic data for the operator, not a proactive alert for
// the user. Secrets that leaked into an exception message are redacted before
// they touch the log.
func recordClientCrash(logger *slog.Logger, source, text string) {
	text = strings.TrimSpace(redact.String(text))
	if text == "" {
		return
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "native"
	}
	// One-line summary for the operator log (the full stack goes to the file so a
	// long trace doesn't bloat every log line). Rune-safe truncation for Korean.
	summary := text
	if i := strings.IndexByte(summary, '\n'); i >= 0 {
		summary = summary[:i]
	}
	if r := []rune(summary); len(r) > 200 {
		summary = string(r[:200])
	}
	if logger != nil {
		logger.Error("native client crash reported", "source", source, "summary", summary)
	}

	entry := fmt.Sprintf("===== %s | %s =====\n%s\n\n",
		time.Now().UTC().Format(time.RFC3339), source, text)
	p := phoneCrashPath()
	logBytes, _ := os.ReadFile(p)
	logBytes = append(logBytes, entry...)
	// Keep the newest maxClientCrashLogBytes; after the byte cut, realign to the
	// next entry marker so the file never starts mid-stack (a broken partial entry).
	if len(logBytes) > maxClientCrashLogBytes {
		logBytes = logBytes[len(logBytes)-maxClientCrashLogBytes:]
		if idx := bytes.Index(logBytes, []byte("===== ")); idx > 0 {
			logBytes = logBytes[idx:]
		}
	}
	//nolint:gosec // G703 — path is the internal state-dir crash log (config.ResolveStateDir + const name), not user input
	if err := os.WriteFile(p, logBytes, 0o600); err != nil && logger != nil {
		logger.Warn("client crash log: write failed", "path", p, "error", err)
	}
}

// phoneEventMaxTokens caps the judgment turn's reply. A phone-event alert should
// be a tight "왜 지금 중요한가 + 무엇을 언제까지" message, not an essay.
const phoneEventMaxTokens = 1536

// phoneEventTurnDeadline bounds the async judgment turn. Long enough for a few
// tool calls (calendar/wiki/mail/contact lookups) but capped so a wedged turn
// cannot leak a goroutine past graceful shutdown.
const phoneEventTurnDeadline = 4 * time.Minute

// phoneEventApprovalDeadline covers a Page Agent read of the e-approval document
// (browser tool timeout is 10m) plus the subsequent summarization turn.
const phoneEventApprovalDeadline = 12 * time.Minute

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
필요하면 폰 도구로 사용자의 현재 상황을 보강하라 — phone_read/phone_write는 deferred이므로 먼저 fetch_tools(names=["phone_read","phone_write"])로 활성화한다. phone_read(위치·배터리·사용 리듬)는 맥락 보강에만 사용한다(예: 위치로 출근/외근 판단, 배터리로 음성 안내 적합성 판단, 사용 리듬으로 방금 집중하던 업무 앱 파악). 사용자가 화면을 못 볼 상황이 분명하면 phone_write(speak)로 폰에 음성으로 직접 읽어줘도 된다.
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
	case "usage":
		return "앱 사용 리듬"
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
	case "usage":
		return `이것은 사용자 스마트폰의 최근 앱 사용 패턴 요약이다(앱별 사용 시간). 사용량 그 자체는 거의 항상 알릴 가치가 없으니 기본은 침묵이다 — 업무 맥락상 분명한 신호일 때만 아주 간결히 보고하라. 신호의 예: 특정 거래처/메신저 앱에 평소와 다른 장시간 집중(→ 관련 딜·이슈가 달아오르는지 일정·메일로 확인 후 한 줄), 또는 업무시간인데 업무앱 사용이 전무(→ 오늘 일정·우선업무 상기 제안). 단순 사용량 나열, 일상적 사용, 판단 근거가 약한 패턴은 전부 다른 말 없이 %s 만 출력하라.`
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
func (s *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRemote(r.RemoteAddr) {
		s.writeJSON(w, http.StatusForbidden, map[string]any{"error": "localhost only"})
		return
	}
	if s.chatHandler == nil || !s.chatHandler.ChatReady() {
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
	s.IngestAsync(req.Type, req.Source, text)

	s.writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted"})
}

// ingestPhoneEventAsync queues the proactive 비서실장 judgment turn for one phone
// event and is the single shared entry point for every phone-event source: the
// loopback /api/event/ingest (legacy — the retired Termux/SSH bridge) and the authenticated
// miniapp.event.ingest (the native NotificationListener). The phone forwards
// broadly; the gateway does the per-type judgment + relay. Fire-and-forget — the
// caller only needs to know the event was accepted; the report arrives later via
// the proactive push, exactly like a cron run.
func (s *Handler) IngestAsync(eventType, source, text string) {
	// location_update: the native client's periodic / on-demand location push. Cache it
	// for phone_read (file mtime = freshness) and return — it is data to look up, not an
	// event to surface, so it never spends a judgment turn. (Geofence arrivals are sent
	// as ordinary events and DO run the judgment, so they can surface "사무실 도착".)
	if strings.EqualFold(strings.TrimSpace(eventType), "location_update") {
		recordPhoneLocation(s.logger, text)
		// Site-visit memory: match the fix's geocoded place against project
		// 현장 and log a visit (recorder enforces the privacy boundary — only
		// a known-site match is ever written). Never spends a judgment turn.
		if s.onLocationPlace != nil {
			s.onLocationPlace(text)
		}
		return
	}
	// usage_update: coarse app-usage rhythm from the native client. Cache only; usage
	// rhythm is helpful context when asked or during another judgment, but should not
	// create proactive alerts by itself.
	if strings.EqualFold(strings.TrimSpace(eventType), "usage_update") {
		recordPhoneUsage(s.logger, text)
		return
	}
	// calllog_update: recent-call digest from the native client. Cache only, for
	// the same reason as usage_update — a call that already ended is context for
	// the next question, not a reason to speak up.
	if strings.EqualFold(strings.TrimSpace(eventType), "calllog_update") {
		recordPhoneCallLog(s.logger, text)
		return
	}
	// client_crash: an uncaught exception the native app captured at crash time and
	// forwarded on its next launch. Diagnostic data for the operator, never a
	// user-facing alert — log it + append to the bounded crash log and return
	// before any judgment turn (the app has no crash reporter otherwise, so this
	// is the only server-side record of a phone-side crash).
	if strings.EqualFold(strings.TrimSpace(eventType), "client_crash") {
		recordClientCrash(s.logger, source, text)
		return
	}
	// phone_action_result: the app's execution report for a dispatched phone_write
	// action. Pure correlation data — resolve the waiting dispatch (server_phone_action.go)
	// and return; never a judgment turn. source carries the action name for the log.
	if strings.EqualFold(strings.TrimSpace(eventType), "phone_action_result") {
		var res ActionResult
		if err := json.Unmarshal([]byte(text), &res); err != nil {
			s.logger.Warn("phone_action_result: malformed payload", "source", source, "error", err)
			return
		}
		if res.ID == "" {
			s.logger.Warn("phone_action_result: missing id", "source", source, "ok", res.OK)
			return
		}
		if s.resolvePhoneAction == nil || !s.resolvePhoneAction(res) {
			// Late (dispatch already timed out and told the agent "unconfirmed")
			// or the gateway restarted in between. The outcome still gets logged
			// so the operator can see the action did (or didn't) run.
			s.logger.Info("phone_action_result: no waiter (late report)",
				"source", source, "id", res.ID, "ok", res.OK)
		}
		return
	}
	// Gmail notifications are dropped before the judgment turn: the gateway's own
	// gmail-poll pipeline already analyzes that inbox and posts the authoritative
	// mail_report work-feed card, so a phone-driven judgment turn over the same mail
	// only produced duplicate, near-identical cards. Scope is Gmail ONLY (the polled
	// account) — other mail apps have no poll coverage, so their notification is the
	// sole proactive surface and must still run. Covers both phone paths: the
	// legacy loopback (/api/event/ingest) and the native NotificationListener
	// (miniapp.event.ingest) both funnel through here.
	if isPolledGmailNotification(eventType, source) {
		s.logger.Info("phone-event: Gmail notification skipped (gmail-poll covers this inbox)",
			"source", strings.TrimSpace(source))
		return
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "(미상)"
	}
	// Groupware radar owns analyze→wiki→feed for electronic approvals — the
	// phone push is only a trigger. Run one on-demand radar scan now (dedup
	// and state live in the radar), instead of relaying a bare notification
	// card that would carry no 승인/반려 chips. Fallback: a failed scan (reader
	// outage) degrades to the old notification relay — signal over silence.
	if s.triggerApprovalScan != nil && isElectronicApprovalEvent(source, text) {
		s.logger.Info("phone-event: electronic approval → groupware radar scan", "source", source)
		safego.GoWithSlog(s.logger, "phone-event-approval-scan", func() {
			ctx, cancel := context.WithTimeout(s.shutdownContext, phoneEventApprovalDeadline)
			defer cancel()
			if err := s.triggerApprovalScan(ctx); err != nil {
				s.logger.Warn("phone-event: approval radar scan failed; falling back to notification relay",
					"source", source, "error", err)
				_, _ = s.processJudgment(ctx, eventType, source, text)
				return
			}
			s.logger.Info("phone-event: approval radar scan ok", "source", source)
		})
		return
	}
	// Ledger BEFORE the tiny gate: the gate is tuned for push-worthiness, not
	// memory-worthiness — a "routine" KakaoTalk work message is correctly
	// NO_REPLY yet still belongs in the project log. The noti-digest task
	// applies the memory-side noise rules on its own cadence. Gmail already
	// returned above (mail pipeline is that inbox's memory path), and
	// context/clipboard are intentional user actions, not ambient signal.
	if notificationLikeEvent(eventType) {
		s.ledger.Append(eventType, source, text)
	}
	approval := isElectronicApprovalEvent(source, text)
	safego.GoWithSlog(s.logger, "phone-event-ingest", func() {
		deadline := phoneEventTurnDeadline
		if approval {
			deadline = phoneEventApprovalDeadline
		}
		ctx, cancel := context.WithTimeout(s.shutdownContext, deadline)
		defer cancel()
		_, _ = s.processJudgment(ctx, eventType, source, text)
	})
}

// processJudgment owns the expensive shared path after source-specific early
// handling: tiny gate, optional approval enrich, agent judgment, and relay.
func (s *Handler) processJudgment(ctx context.Context, eventType, source, text string) (bool, error) {
	approval := isElectronicApprovalEvent(source, text)
	guidance := phoneEventGuidance(eventType)
	if approval {
		guidance = electronicApprovalGuidance()
	}
	command := fmt.Sprintf(phoneEventPromptTmpl,
		phoneEventKindLabel(eventType), source, text,
		fmt.Sprintf(guidance, tokens.SilentReplyToken))

	// Tiered triage: a cheap tiny-model gate before the expensive tool-calling
	// judgment turn. Electronic approvals always bypass it. Fail-open.
	if !approval && notificationLikeEvent(eventType) && !worthFullJudgment(ctx, source, text) {
		s.logger.Debug("phone-event tiny-gate dropped", "source", source, "type", eventType)
		return false, nil
	}

	msg := command
	approvalDocID := extractGroupwareDocID(text)
	if approval && s.browserEnrich != nil {
		if body := strings.TrimSpace(s.browserEnrich(ctx, source, text)); body != "" {
			msg = command + "\n\n[브라우저에서 읽은 결재 본문]\n" + body
			if enrichedDocID := extractGroupwareDocID(body); enrichedDocID != "" {
				approvalDocID = enrichedDocID
			}
			s.logger.Info("phone-event approval browser enrich ok",
				"source", source, "bodyLen", len(body), "docId", approvalDocID)
		} else {
			s.logger.Info("phone-event approval browser enrich skipped",
				"source", source)
		}
	}

	if s.chatHandler == nil || !s.chatHandler.ChatReady() {
		err := errors.New("phone-event chat handler unavailable")
		s.logger.Error("phone-event judgment turn failed",
			"source", source, "type", eventType, "error", err)
		return false, err
	}
	maxTok := phoneEventMaxTokens
	sessionKey := phoneEventSessionPrefix + ":" + shortid.New("e")
	result, err := s.chatHandler.RunSync(ctx, chatport.SyncRequest{
		SessionKey:          sessionKey,
		Message:             msg,
		Model:               s.judgmentModel, // submain lane when configured, else main
		MaxTokens:           &maxTok,
		EphemeralUser:       true, // throwaway session — persist nothing
		EphemeralAssistant:  true,
		AutoDeliveredOutput: true, // relayNative delivers; agent must not use message tool
	})
	if err != nil {
		s.logger.Error("phone-event judgment turn failed",
			"source", source, "type", eventType, "error", err)
		return false, err
	}
	// relayNative applies the same noise floor as every proactive surface:
	// a NO_REPLY or "별 일 없음" judgment is suppressed (delivered=false).
	output := result.BestText
	if s.relay == nil {
		err := errors.New("phone-event relay unavailable")
		s.logger.Error("phone-event relay unavailable", "source", source, "type", eventType)
		return false, err
	}
	var delivered bool
	var relayErr error
	if approval && approvalDocID != "" {
		delivered, relayErr = s.relay.RelayNativeToOptions("", output, proactive.Options{
			WorkFeedSource: workfeed.SourceGroupwareApproval,
			RefID:          approvalDocID,
			ForceQuestion:  true,
			Actions:        groupwareApprovalFeedActions(),
		})
	} else {
		delivered, relayErr = s.relay.RelayNative(output)
	}
	if relayErr != nil {
		s.logger.Error("phone-event relay failed",
			"source", source, "type", eventType, "error", relayErr)
		return false, relayErr
	}
	s.logger.Info("phone-event processed",
		"source", source, "type", eventType,
		"delivered", delivered, "outputLen", len(output), "approval", approval,
		"docId", approvalDocID)
	return delivered, nil
}

// isPolledGmailNotification reports whether a phone event is a Gmail app
// notification. These are suppressed before the judgment turn because gmail-poll
// already analyzes that inbox and posts the mail_report card; a second, phone-
// driven judgment over the same email only duplicated it in the work feed.
//
// Scope is Gmail ONLY (the polled account). Other mail apps (Outlook, Samsung
// Email, a non-polled account) are deliberately NOT matched: gmail-poll never sees
// them, so their phone notification is the only proactive surface they have.
//
// source is the package name forwarded by deneb-notification-watch
// ("com.google.android.gm", and the ".gm.lite" variant), or a "Gmail" display
// label if a capture path forwards one instead of the package. Only notification
// events qualify (empty type defaults to notification); other types pass through.
func isPolledGmailNotification(eventType, source string) bool {
	switch strings.TrimSpace(strings.ToLower(eventType)) {
	case "notification", "":
	default:
		return false
	}
	s := strings.ToLower(strings.TrimSpace(source))
	return s == "com.google.android.gm" || s == "com.google.android.gm.lite" || s == "gmail"
}

// notificationLikeEvent reports whether an event type uses the "worth surfacing?"
// judgment (notification/sms/free labels), versus a different intent (context /
// clipboard) that should always run the full judgment regardless of the tiny gate.
func notificationLikeEvent(eventType string) bool {
	switch strings.TrimSpace(strings.ToLower(eventType)) {
	case "context", "clipboard", "usage":
		// usage digests are forwarded rarely (client throttles to ~6h) and carry a
		// default-silence guidance, so they run the full judgment directly rather than
		// the notification-specific tiny gate (whose ads/OTP prompt doesn't fit them).
		return false
	default:
		return true
	}
}

// worthFullJudgment is the tiered-triage first pass: a cheap tiny-model yes/no on
// whether a notification deserves the full tool-calling judgment turn. It catches
// the obvious noise (ads/promo/OTP/receipts/routine) the full judgment would also
// NO_REPLY, but without spending a main-model turn. Fail-open — any tiny-model error
// returns true so the full judgment still runs (never silently drop signal).
func worthFullJudgment(ctx context.Context, source, text string) bool {
	const system = "당신은 스마트폰 알림 분류기다. 사용자에게 즉시 알릴 가치가 있는 업무·일정·금전·중요 연락이면 YES, " +
		"광고·프로모션·스팸·인증번호(OTP)·결제 영수증·배송/마케팅 알림·일상적 시스템/앱 알림이면 NO. YES 또는 NO 한 단어만 답하라."
	out, err := pilot.CallTinyLLM(ctx, system, "앱: "+source+"\n알림 내용:\n"+text, 4, json.RawMessage(`{"temperature":0}`))
	if err != nil {
		return true // fail-open: run the full judgment rather than drop on a gate error
	}
	return !strings.HasPrefix(strings.TrimSpace(strings.ToUpper(out)), "NO")
}

func (s *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(value); err != nil && s.logger != nil {
		httputil.LogEncodeError(s.logger, "phone event: json encode error", err)
	}
}

func isLoopbackRemote(remoteAddr string) bool {
	if remoteAddr == "" {
		return false
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
