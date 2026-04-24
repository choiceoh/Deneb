package insights

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/usage"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// fakeLister is a minimal in-memory session list for tests.
type fakeLister struct {
	items []*session.Session
}

func (f *fakeLister) List() []*session.Session { return f.items }
func (f *fakeLister) Count() int               { return len(f.items) }

// fakeUsage wraps a pre-built StatusReport.
type fakeUsage struct {
	report *usage.StatusReport
}

func (f *fakeUsage) Status() *usage.StatusReport { return f.report }

// newSession builds a session fixture with the given tokens/model.
func newSession(key, model, channel string, in, out int64, status session.RunStatus, updatedMs int64) *session.Session {
	inTok := in
	outTok := out
	total := in + out
	s := &session.Session{
		Key:          key,
		Kind:         session.KindDirect,
		Status:       status,
		Channel:      channel,
		Model:        model,
		UpdatedAt:    updatedMs,
		InputTokens:  &inTok,
		OutputTokens: &outTok,
		TotalTokens:  &total,
	}
	startedAt := updatedMs - 60_000
	s.StartedAt = &startedAt
	return s
}

func TestEngineGenerateBasic(t *testing.T) {
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	nowMs := now.UnixMilli()

	sessions := &fakeLister{
		items: []*session.Session{
			newSession("tg:alice", "gpt-4", "telegram", 10_000, 2_000, session.StatusDone, nowMs-3_600_000),
			newSession("tg:bob", "gpt-4", "telegram", 500, 100, session.StatusDone, nowMs-7_200_000),
			newSession("cron:daily", "claude-3.5", "cron", 999_999, 0, session.StatusDone, nowMs-60_000),     // internal — must be skipped
			newSession("tg:old", "gpt-4", "telegram", 1_000, 200, session.StatusDone, nowMs-40*24*3_600_000), // out of window
		},
	}
	// Mark cron session as internal kind.
	sessions.items[2].Kind = session.KindCron

	usageRep := &usage.StatusReport{
		Uptime:    "10m",
		StartedAt: now.Add(-10 * time.Minute).Format(time.RFC3339),
		Providers: map[string]*usage.ProviderStats{
			"openai": {Calls: 42, Tokens: usage.TokenUsage{Input: 12_000, Output: 3_000}},
		},
	}

	eng := New(sessions, &fakeUsage{report: usageRep})
	eng.now = func() time.Time { return now }

	rep, err := eng.Generate(context.Background(), 30)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if rep.Empty {
		t.Fatalf("report unexpectedly empty")
	}
	if got, want := rep.Overview.Sessions, 2; got != want {
		t.Errorf("overview.sessions = %d; want %d (cron + out-of-window excluded)", got, want)
	}
	if got, want := rep.Overview.InputTokens, int64(10_500); got != want {
		t.Errorf("overview.inputTokens = %d; want %d", got, want)
	}
	if got, want := rep.Overview.OutputTokens, int64(2_100); got != want {
		t.Errorf("overview.outputTokens = %d; want %d", got, want)
	}
	if got, want := rep.Overview.TotalTokens, int64(12_600); got != want {
		t.Errorf("overview.totalTokens = %d; want %d", got, want)
	}
	if len(rep.Models) != 1 || rep.Models[0].Model != "gpt-4" {
		t.Errorf("models = %+v; want one entry for gpt-4", rep.Models)
	}
	if rep.Models[0].Sessions != 2 {
		t.Errorf("models[gpt-4].sessions = %d; want 2", rep.Models[0].Sessions)
	}
	if len(rep.TopSessions) != 2 {
		t.Errorf("topSessions len = %d; want 2", len(rep.TopSessions))
	}
	if rep.TopSessions[0].Key != "tg:alice" {
		t.Errorf("top session = %q; want tg:alice (highest tokens)", rep.TopSessions[0].Key)
	}
	if len(rep.Providers) != 1 || rep.Providers[0].Provider != "openai" {
		t.Errorf("providers = %+v; want openai", rep.Providers)
	}
	if rep.Overview.CostUSD != 0 {
		t.Errorf("costUSD = %.4f; want 0 (schema gap)", rep.Overview.CostUSD)
	}
}

func TestEngineGenerateEmpty(t *testing.T) {
	eng := New(&fakeLister{}, nil)
	rep, err := eng.Generate(context.Background(), 7)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if !rep.Empty {
		t.Errorf("expected empty report")
	}
	if rep.Days != 7 {
		t.Errorf("days = %d; want 7", rep.Days)
	}
	if len(rep.SchemaNotes) == 0 {
		t.Errorf("expected at least one schema note in empty report")
	}
}

func TestEngineGenerateDefaultsDays(t *testing.T) {
	eng := New(&fakeLister{}, nil)
	rep, err := eng.Generate(context.Background(), 0)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if rep.Days != 30 {
		t.Errorf("days default = %d; want 30", rep.Days)
	}
}

func TestEngineGenerateContextCanceled(t *testing.T) {
	eng := New(&fakeLister{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := eng.Generate(ctx, 7)
	if err == nil {
		t.Fatalf("expected context error, got nil")
	}
}

func TestEngineToolAggregator(t *testing.T) {
	now := time.Now()
	eng := New(&fakeLister{items: []*session.Session{
		newSession("tg:one", "gpt-4", "telegram", 100, 50, session.StatusDone, now.UnixMilli()),
	}}, nil)
	eng.SetToolAggregator(func(_ context.Context, _ time.Time) []ToolStat {
		return []ToolStat{
			{Name: "fs.read", Calls: 5, ErrorRate: 0},
			{Name: "exec", Calls: 2, ErrorRate: 0.5},
		}
	})
	rep, err := eng.Generate(context.Background(), 1)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if len(rep.Tools) != 2 {
		t.Errorf("tools len = %d; want 2", len(rep.Tools))
	}
	// schema note should no longer say tools-empty when we have tools.
	joined := strings.Join(rep.SchemaNotes, "\n")
	if strings.Contains(joined, "도구 사용량 수집 미연결") {
		t.Errorf("did not expect 'tools missing' note; got %q", joined)
	}
}

func TestEngineSkipsInternalKinds(t *testing.T) {
	now := time.Now()
	subagent := newSession("sub:1", "claude", "", 1_000, 100, session.StatusDone, now.UnixMilli())
	subagent.Kind = session.KindSubagent
	cron := newSession("cron:1", "claude", "", 5_000, 500, session.StatusDone, now.UnixMilli())
	cron.Kind = session.KindCron

	eng := New(&fakeLister{items: []*session.Session{subagent, cron}}, nil)
	rep, err := eng.Generate(context.Background(), 30)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if rep.Overview.Sessions != 0 {
		t.Errorf("expected internal kinds skipped; got sessions=%d", rep.Overview.Sessions)
	}
}

func TestEngineTopSessionsTrimsZeroTokenStubs(t *testing.T) {
	now := time.Now().UnixMilli()
	stub := &session.Session{
		Key: "tg:stub", Kind: session.KindDirect,
		Status: session.StatusDone, Channel: "telegram",
		UpdatedAt: now,
	}
	actual := newSession("tg:real", "gpt-4", "telegram", 100, 50, session.StatusDone, now)
	eng := New(&fakeLister{items: []*session.Session{stub, actual}}, nil)
	rep, err := eng.Generate(context.Background(), 30)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if len(rep.TopSessions) != 1 || rep.TopSessions[0].Key != "tg:real" {
		t.Errorf("topSessions = %+v; want only tg:real", rep.TopSessions)
	}
}
