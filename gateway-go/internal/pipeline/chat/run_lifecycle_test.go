package chat

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

func TestShouldForceExternalDeliveryFailureNotice(t *testing.T) {
	delivery := &DeliveryContext{Channel: "telegram", To: "telegram:123"}
	toolActivities := []agent.ToolActivity{
		{Name: "message", IsError: true},
	}

	if !shouldForceExternalDeliveryFailureNotice(delivery, toolActivities, "", true) {
		t.Fatal("expected forced notice for silent failed external delivery")
	}
	if !shouldForceExternalDeliveryFailureNotice(delivery, toolActivities, "", false) {
		t.Fatal("expected forced notice for empty failed external delivery")
	}
	if shouldForceExternalDeliveryFailureNotice(delivery, toolActivities, "실패했습니다. 다시 시도해 주세요.", false) {
		t.Fatal("did not expect forced notice when assistant already produced a visible explanation")
	}
}

func TestShouldForceExternalDeliveryFailureNotice_IgnoresUnrelatedCases(t *testing.T) {
	delivery := &DeliveryContext{Channel: "telegram", To: "telegram:123"}

	if shouldForceExternalDeliveryFailureNotice(nil, []agent.ToolActivity{{Name: "message", IsError: true}}, "", true) {
		t.Fatal("did not expect forced notice without a delivery context")
	}
	if shouldForceExternalDeliveryFailureNotice(delivery, []agent.ToolActivity{{Name: "exec", IsError: true}}, "", true) {
		t.Fatal("did not expect forced notice for non-delivery tool errors")
	}
	if shouldForceExternalDeliveryFailureNotice(delivery, []agent.ToolActivity{{Name: "message", IsError: false}}, "", true) {
		t.Fatal("did not expect forced notice when delivery tool succeeded")
	}
}

// TestClassifyRunFailureReason_LegacyCoverage locks in the labels the
// pre-llmerr substring classifier would have produced, so the migration is
// strictly non-regressive for inputs the old code recognised.
func TestClassifyRunFailureReason_LegacyCoverage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		// Plain-string errors (no structured status). Exercise the legacy
		// bare-digit fallback and llmerr message-pattern pipeline.
		{"plain 429", errors.New("openai: 429 Too Many Requests"), "API 요청 한도 초과 (429)"},
		{"plain 401", errors.New("openai: 401 Unauthorized"), "API 인증 실패 (401)"},
		{"plain unauthorized word", errors.New("request unauthorized: bad key"), "API 인증 실패 (401)"},
		{"invalid_api_key code", errors.New("invalid_api_key provided"), "API 인증 실패 (401)"},
		{"billing word", errors.New("billing not active on account"), "결제 오류"},
		{"insufficient_quota code", errors.New("insufficient_quota: you exceeded your current quota"), "결제 오류"},
		{"plain 502", errors.New("HTTP 502 bad gateway"), "서버 일시 장애"},
		{"plain 503", errors.New("HTTP 503 service unavailable"), "서버 일시 장애"},
		{"plain 521", errors.New("HTTP 521 web server is down"), "서버 일시 장애"},
		{"plain 529", errors.New("HTTP 529 overloaded"), "서버 일시 장애"},
		{"context overflow phrase", errors.New("prompt is too long for this model"), "컨텍스트 초과"},
		{"context_length_exceeded code", errors.New("error: context_length_exceeded"), "컨텍스트 초과"},
		{"unrecognised generic", errors.New("totally unknown failure"), ""},
		{"nil error", nil, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRunFailureReason(tc.err); got != tc.want {
				t.Errorf("classifyRunFailureReason(%q) = %q, want %q", errString(tc.err), got, tc.want)
			}
		})
	}
}

// TestClassifyRunFailureReason_StructuredAPIError exercises the migration's
// new coverage: when the error is a wrapped *httpretry.APIError, the status
// drives the label even if the string does not contain a bare digit.
func TestClassifyRunFailureReason_StructuredAPIError(t *testing.T) {
	tests := []struct {
		name string
		err  *httpretry.APIError
		want string
	}{
		{
			name: "structured 429",
			err:  &httpretry.APIError{StatusCode: 429, Message: "Too Many Requests"},
			want: "API 요청 한도 초과 (429)",
		},
		{
			name: "structured 401",
			err:  &httpretry.APIError{StatusCode: 401, Message: "bad credentials"},
			want: "API 인증 실패 (401)",
		},
		{
			// New coverage: 402 is billing in llmerr; the old substring
			// classifier would have returned "" here.
			name: "structured 402 billing",
			err:  &httpretry.APIError{StatusCode: 402, Message: "payment required"},
			want: "결제 오류",
		},
		{
			name: "structured 500",
			err:  &httpretry.APIError{StatusCode: 500, Message: "internal error"},
			want: "서버 일시 장애",
		},
		{
			name: "structured 503",
			err:  &httpretry.APIError{StatusCode: 503, Message: "service unavailable"},
			want: "서버 일시 장애",
		},
		{
			// New coverage: structured context-overflow code.
			name: "structured 400 context_length_exceeded",
			err:  &httpretry.APIError{StatusCode: 400, Message: `{"error":{"code":"context_length_exceeded"}}`},
			want: "컨텍스트 초과",
		},
		{
			// New coverage: 413 → payload_too_large → 컨텍스트 초과 label
			// (same compress action, same user-facing label).
			name: "structured 413 payload too large",
			err:  &httpretry.APIError{StatusCode: 413, Message: "payload too large"},
			want: "컨텍스트 초과",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRunFailureReason(tc.err); got != tc.want {
				t.Errorf("classifyRunFailureReason(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// TestIsSubagentSession locks in the discriminator that keeps sub-agent
// completions from raising false channel-delivery alarms. A child session
// (SpawnedBy set) has no user-facing channel — the parent reads its result via
// session.LastOutput — so a nil replyFunc at completion is the expected state,
// not a wiring bug. Without this guard every sub-agent completion logged
// Error + broadcast chat.delivery_failed (reason=reply_func_nil) as noise.
func TestIsSubagentSession(t *testing.T) {
	sm := session.NewManager()

	// Parent / main session: no SpawnedBy → not a sub-agent.
	sm.Create("client:main", session.KindDirect)

	// Child / sub-agent session: SpawnedBy points at the parent. Create stores
	// (and returns) a copy, so the SpawnedBy mutation must be persisted via Set.
	const childKey = "client:main:math-test:1780851684916"
	child := sm.Create(childKey, session.KindDirect)
	child.SpawnedBy = "client:main"
	if err := sm.Set(child); err != nil {
		t.Fatalf("Set(child) failed: %v", err)
	}

	deps := runDeps{sessions: sm}

	if !isSubagentSession(deps, childKey) {
		t.Errorf("isSubagentSession(child) = false, want true (SpawnedBy=%q)", child.SpawnedBy)
	}
	if isSubagentSession(deps, "client:main") {
		t.Error("isSubagentSession(main) = true, want false (no SpawnedBy)")
	}
	if isSubagentSession(deps, "client:does-not-exist") {
		t.Error("isSubagentSession(unknown key) = true, want false")
	}
	if isSubagentSession(runDeps{sessions: nil}, childKey) {
		t.Error("isSubagentSession(nil manager) = true, want false")
	}
}

// TestHandleRunSuccess_SubagentReplyFuncNil drives the real handleRunSuccess to
// prove the end-to-end behavior: a sub-agent (child) session that completes with
// reply text but a nil channel replyFunc must NOT raise the operator-facing
// chat.delivery_failed (reason=reply_func_nil) alarm — that nil is the expected
// state for children (the parent reads their output via session.LastOutput). A
// real top-level session in the same shape MUST still escalate, so the
// suppression stays scoped to sub-agents and never hides an actual wiring bug.
func TestHandleRunSuccess_SubagentReplyFuncNil(t *testing.T) {
	parseDirectives := func(raw, _, _ string) chatport.ReplyDirectives {
		return chatport.ReplyDirectives{Text: raw}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// run executes one completed turn through handleRunSuccess and returns every
	// event name broadcast during it. replyFunc, transcript, tools, broadcaster,
	// wikiStore, jobTracker, hindsightClient are all left nil — exactly the
	// native-only production shape where no channel replyFunc is registered.
	run := func(t *testing.T, sessionKey, spawnedBy string) []string {
		t.Helper()
		sm := session.NewManager()
		s := sm.Create(sessionKey, session.KindDirect)
		if spawnedBy != "" {
			s.SpawnedBy = spawnedBy
			if err := sm.Set(s); err != nil {
				t.Fatalf("Set(%q): %v", sessionKey, err)
			}
		}

		var mu sync.Mutex
		var events []string
		broadcast := func(event string, _ any) (int, []error) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
			return 0, nil
		}

		deps := runDeps{
			sessions:  sm,
			broadcast: broadcast,
			logger:    logger,
			chatport:  chatportAdapters{ParseReplyDirectives: parseDirectives},
		}
		params := RunParams{
			SessionKey:  sessionKey,
			ClientRunID: "run-test",
			Delivery:    deliveryFromSessionKey(sessionKey),
		}
		result := &agent.AgentResult{
			Text:       "사칙연산 결과는 4입니다.",
			AllText:    "사칙연산 결과는 4입니다.",
			StopReason: "end_turn",
			Turns:      1,
		}
		handleRunSuccess(context.Background(), params, deps, nil, logger, result, "", time.Now().UnixMilli(), nil)

		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), events...)
	}

	hasEvent := func(events []string, name string) bool {
		for _, e := range events {
			if e == name {
				return true
			}
		}
		return false
	}

	// Proper lineage: spawn captured a non-empty parent key, so the child key is
	// "client:main:label:ts", Delivery.Channel="client", and SpawnedBy is set.
	// The SpawnedBy signal must suppress the alarm.
	t.Run("subagent with SpawnedBy suppresses delivery_failed", func(t *testing.T) {
		events := run(t, "client:main:math-test:1780851684916", "client:main")
		if hasEvent(events, "chat.delivery_failed") {
			t.Errorf("sub-agent completion emitted chat.delivery_failed (false alarm); events=%v", events)
		}
	})

	// Broken lineage (the shape actually observed live): the spawn captured an
	// empty parent key, so the child key is ":label:ts", SpawnedBy is empty, and
	// deliveryFromSessionKey yields an empty Delivery.Channel. The empty-channel
	// signal must suppress the alarm even though SpawnedBy is empty.
	t.Run("channel-less subagent (empty parent key) suppresses delivery_failed", func(t *testing.T) {
		events := run(t, ":livetest:1780852962773", "")
		if hasEvent(events, "chat.delivery_failed") {
			t.Errorf("channel-less sub-agent completion emitted chat.delivery_failed (false alarm); events=%v", events)
		}
	})

	// A real top-level channel session (non-empty channel, no SpawnedBy) with a
	// nil replyFunc IS a wiring bug and must still escalate — the suppression
	// must not hide genuine delivery failures.
	t.Run("top-level channel session still escalates", func(t *testing.T) {
		events := run(t, "client:main", "")
		if !hasEvent(events, "chat.delivery_failed") {
			t.Errorf("top-level session with nil replyFunc did NOT emit chat.delivery_failed; a real wiring bug must still escalate; events=%v", events)
		}
	})
}
