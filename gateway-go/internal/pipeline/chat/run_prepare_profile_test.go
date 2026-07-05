package chat

// Profile-isolation snapshot tests for prepareContextAndPrompt: the 챗봇
// ("chat:") and 코딩 ("code:") profiles must NOT receive the 업무 work
// context — recall preflight, ambient calendar/goal glances, the operator
// persona override, or the wiki work-loop coaching. This is a PRIVACY
// boundary (work memory must not leak into a general chat or an external
// repo worktree), not a performance optimization — lock it in.

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
)

const testPersonaMarker = "PERSONA-OVERRIDE-MARKER-7f3a"

type profilePrepFixture struct {
	deps           runDeps
	calendarCalled *atomic.Bool
	goalCalled     *atomic.Bool
}

func newProfilePrepFixture(t *testing.T) profilePrepFixture {
	t.Helper()
	// The static-prompt cache is package-global; keep this test's profile
	// entries from leaking into other tests (and vice versa).
	t.Cleanup(prompt.Cache.Reset)

	var calendarCalled, goalCalled atomic.Bool
	reg := NewToolRegistry()
	// wiki must be registered so the 업무 dynamic block renders its wiki
	// coaching — the chatbot assertion below checks that coaching is
	// withheld even though the tool is present.
	reg.Register("wiki", func(ctx context.Context, in json.RawMessage) (string, error) { return "", nil })
	reg.Register("read", func(ctx context.Context, in json.RawMessage) (string, error) { return "", nil })

	deps := runDeps{
		logger:     discardLogger(),
		tools:      reg,
		transcript: NewMemoryTranscriptStore(),
		contextCfg: DefaultContextConfig(),
		ambient: AmbientDeps{
			CalendarGlance: func(ctx context.Context, sessionKey, tz string) string {
				calendarCalled.Store(true)
				return "GLANCE-CAL-MARKER"
			},
			GoalGlance: func(ctx context.Context, sessionKey string) string {
				goalCalled.Store(true)
				return "GLANCE-GOAL-MARKER"
			},
			PersonaOverride: func() string { return testPersonaMarker },
		},
	}
	return profilePrepFixture{deps: deps, calendarCalled: &calendarCalled, goalCalled: &goalCalled}
}

func prepForSession(t *testing.T, fx profilePrepFixture, sessionKey string) prepResult {
	t.Helper()
	params := RunParams{
		SessionKey: sessionKey,
		// "기억" is a recall cue: on the 업무 path the preflight runs and (with
		// an empty transcript) yields the explicit no-evidence notice, so a
		// non-empty RecallMemory proves the preflight RAN — while the 챗봇/코딩
		// gates must leave it empty even with the cue present.
		Message: "탑솔라 지난번 단가 기억해?",
	}
	return prepareContextAndPrompt(context.Background(), params, fx.deps, t.TempDir(), "", fx.deps.logger)
}

func TestPrepareContextAndPrompt_WorkProfileGetsWorkContext(t *testing.T) {
	fx := newProfilePrepFixture(t)
	prep := prepForSession(t, fx, "client:profile-test-work")

	sys := string(prep.SystemPrompt)
	if !strings.Contains(sys, testPersonaMarker) {
		t.Error("업무 system prompt must carry the operator persona override")
	}
	if !strings.Contains(sys, "GLANCE-CAL-MARKER") {
		t.Error("업무 system prompt must carry the calendar glance")
	}
	if !fx.calendarCalled.Load() || !fx.goalCalled.Load() {
		t.Error("업무 prep must invoke calendar and goal glance providers")
	}
	if prep.RecallMemory == "" {
		t.Error("업무 prep with a recall cue must run the preflight (expected the no-evidence notice)")
	}
	if !strings.Contains(sys, "외부 메모리") {
		t.Error("업무 system prompt must include the wiki work-loop coaching when the wiki tool is registered")
	}
}

func TestPrepareContextAndPrompt_ChatbotProfileWithholdsWorkContext(t *testing.T) {
	fx := newProfilePrepFixture(t)
	prep := prepForSession(t, fx, "chat:profile-test-chatbot")

	sys := string(prep.SystemPrompt)
	if !strings.Contains(sys, "You are a helpful, knowledgeable AI assistant") {
		t.Error("챗봇 system prompt must use the neutral general-assistant identity")
	}
	if strings.Contains(sys, testPersonaMarker) {
		t.Error("챗봇 system prompt must NOT carry the 업무 persona override")
	}
	if strings.Contains(sys, "GLANCE-CAL-MARKER") || strings.Contains(sys, "GLANCE-GOAL-MARKER") {
		t.Error("챗봇 system prompt must NOT carry calendar/goal glances")
	}
	if fx.calendarCalled.Load() || fx.goalCalled.Load() {
		t.Error("챗봇 prep must not even invoke the calendar/goal glance providers")
	}
	if prep.RecallMemory != "" {
		t.Errorf("챗봇 prep must skip recall entirely, got %q", prep.RecallMemory)
	}
	if strings.Contains(sys, "외부 메모리") {
		t.Error("챗봇 system prompt must NOT include the wiki work-loop coaching")
	}
	if prep.ContextFiles != nil {
		t.Error("챗봇 prep must not load workspace context files")
	}
}

func TestPrepareContextAndPrompt_CodingProfileWithholdsWorkContext(t *testing.T) {
	fx := newProfilePrepFixture(t)
	prep := prepForSession(t, fx, "code:profile-test-task")

	sys := string(prep.SystemPrompt)
	if !strings.Contains(sys, "프로젝트 규칙") {
		t.Error("코딩 system prompt must carry the implementer repo-rules section")
	}
	if strings.Contains(sys, testPersonaMarker) {
		t.Error("코딩 system prompt must NOT carry the 업무 persona override")
	}
	if fx.calendarCalled.Load() || fx.goalCalled.Load() {
		t.Error("코딩 prep must not invoke the calendar/goal glance providers")
	}
	if prep.RecallMemory != "" {
		t.Errorf("코딩 prep must skip recall entirely, got %q", prep.RecallMemory)
	}
	if prep.ContextFiles != nil {
		t.Error("코딩 prep must not load workspace context files")
	}
	if prep.Tier1Wiki != "" {
		t.Errorf("코딩 prep must not build tier-1 wiki, got %q", prep.Tier1Wiki)
	}
}
