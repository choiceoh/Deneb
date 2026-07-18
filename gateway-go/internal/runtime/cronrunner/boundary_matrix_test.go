package cronrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
)

type cronSyncStub struct {
	result   *chatport.SyncResult
	err      error
	requests []chatport.SyncRequest
}

func (s *cronSyncStub) ChatReady() bool { return true }

func (s *cronSyncStub) RunSync(_ context.Context, req chatport.SyncRequest) (*chatport.SyncResult, error) {
	s.requests = append(s.requests, req)
	return s.result, s.err
}

type cronNotReadyStub struct{}

func (cronNotReadyStub) ChatReady() bool { return false }

func (cronNotReadyStub) RunSync(context.Context, chatport.SyncRequest) (*chatport.SyncResult, error) {
	panic("RunSync must not be called when chat is not ready")
}

func cronTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWeeklyReportCommandBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{
			name:    "slash weekly",
			command: "/weekly",
			want:    true,
		},
		{
			name:    "korean alias",
			command: "/주간보고",
			want:    true,
		},
		{
			name:    "leading ascii space",
			command: " /weekly",
			want:    true,
		},
		{
			name:    "trailing ascii space",
			command: "/weekly ",
			want:    true,
		},
		{
			name:    "tabs",
			command: "\t/weekly\t",
			want:    true,
		},
		{
			name:    "newlines",
			command: "\n/주간보고\n",
			want:    true,
		},
		{
			name:    "carriage return",
			command: "\r/weekly\r",
			want:    true,
		},
		{
			name:    "mixed whitespace",
			command: " \t\n/주간보고\r\n ",
			want:    true,
		},
		{
			name:    "empty",
			command: "",
			want:    false,
		},
		{
			name:    "spaces",
			command: "   ",
			want:    false,
		},
		{
			name:    "weekly no slash",
			command: "weekly",
			want:    false,
		},
		{
			name:    "korean no slash",
			command: "주간보고",
			want:    false,
		},
		{
			name:    "uppercase",
			command: "/WEEKLY",
			want:    false,
		},
		{
			name:    "mixed case",
			command: "/Weekly",
			want:    false,
		},
		{
			name:    "suffix",
			command: "/weekly-now",
			want:    false,
		},
		{
			name:    "prefix",
			command: "run /weekly",
			want:    false,
		},
		{
			name:    "double slash",
			command: "//weekly",
			want:    false,
		},
		{
			name:    "trailing arg",
			command: "/weekly now",
			want:    false,
		},
		{
			name:    "query",
			command: "/weekly?x=1",
			want:    false,
		},
		{
			name:    "fragment",
			command: "/weekly#x",
			want:    false,
		},
		{
			name:    "slash only",
			command: "/",
			want:    false,
		},
		{
			name:    "similar korean",
			command: "/주간 보고",
			want:    false,
		},
		{
			name:    "nul suffix",
			command: "/weekly\u0000",
			want:    false,
		},
		{
			name:    "nonbreaking prefix",
			command: " /weekly",
			want:    true,
		},
		{
			name:    "em space prefix",
			command: " /weekly",
			want:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isWeeklyReportCommand(tc.command); got != tc.want {
				t.Fatalf("isWeeklyReportCommand(%q) = %v, want %v", tc.command, got, tc.want)
			}
		})
	}
}

func TestStripCardPreambleBoundaryMatrix(t *testing.T) {
	t.Parallel()
	longASCII := strings.Repeat("a", cardGreetingMaxRunes+1)
	maxASCII := strings.Repeat("b", cardGreetingMaxRunes)
	longKorean := strings.Repeat("가", cardGreetingMaxRunes+1)
	maxKorean := strings.Repeat("나", cardGreetingMaxRunes)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no fence",
			input: "plain answer",
			want:  "plain answer",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "already fence",
			input: "```deneb-ui\n{}\n```",
			want:  "```deneb-ui\n{}\n```",
		},
		{
			name:  "spaces before fence",
			input: "   ```deneb-ui\n{}\n```",
			want:  "   ```deneb-ui\n{}\n```",
		},
		{
			name:  "short greeting",
			input: "Good morning\n```deneb-ui\n{}\n```",
			want:  "Good morning\n\n```deneb-ui\n{}\n```",
		},
		{
			name:  "korean greeting",
			input: "좋은 아침입니다\n```deneb-ui\n{}\n```",
			want:  "좋은 아침입니다\n\n```deneb-ui\n{}\n```",
		},
		{
			name:  "greeting surrounding spaces",
			input: "  hello  \n```deneb-ui\n{}\n```",
			want:  "hello\n\n```deneb-ui\n{}\n```",
		},
		{
			name:  "one blank line",
			input: "hello\n\n```deneb-ui\n{}\n```",
			want:  "hello\n\n```deneb-ui\n{}\n```",
		},
		{
			name:  "many blank lines",
			input: "hello\n\n\n```deneb-ui\n{}\n```",
			want:  "hello\n\n```deneb-ui\n{}\n```",
		},
		{
			name:  "two lines",
			input: "thinking\nworking\n```deneb-ui\n{}\n```",
			want:  "```deneb-ui\n{}\n```",
		},
		{
			name:  "three lines",
			input: "one\ntwo\nthree\n```deneb-ui\n{}\n```",
			want:  "```deneb-ui\n{}\n```",
		},
		{
			name:  "blank split lines",
			input: "one\n\n two\n```deneb-ui\n{}\n```",
			want:  "```deneb-ui\n{}\n```",
		},
		{
			name:  "markdown header leak",
			input: "# analysis\nbody\n```deneb-ui\n{}\n```",
			want:  "```deneb-ui\n{}\n```",
		},
		{
			name:  "json leak",
			input: "{\"raw\":1}\nmore\n```deneb-ui\n{}\n```",
			want:  "```deneb-ui\n{}\n```",
		},
		{
			name:  "fence later no closing",
			input: "leak\nline\n```deneb-ui",
			want:  "```deneb-ui",
		},
		{
			name:  "other fence",
			input: "hello\n```json\n{}\n```",
			want:  "hello\n```json\n{}\n```",
		},
		{
			name:  "inline fence",
			input: "hello ```deneb-ui\n{}\n```",
			want:  "hello\n\n```deneb-ui\n{}\n```",
		},
		{
			name:  "suffix preserved",
			input: "a\nb\n```deneb-ui\n{}\n```\nafter",
			want:  "```deneb-ui\n{}\n```\nafter",
		},
		{
			name:  "unicode greeting",
			input: "오늘의 핵심은 계약입니다 🚀\n```deneb-ui\n{}\n```",
			want:  "오늘의 핵심은 계약입니다 🚀\n\n```deneb-ui\n{}\n```",
		},
		{
			name:  "single punctuation",
			input: "—\n```deneb-ui\n{}\n```",
			want:  "—\n\n```deneb-ui\n{}\n```",
		},
		{
			name:  "single heading",
			input: "# Report\n```deneb-ui\n{}\n```",
			want:  "# Report\n\n```deneb-ui\n{}\n```",
		},
		{
			name:  "single json",
			input: "{\"raw\":1}\n```deneb-ui\n{}\n```",
			want:  "{\"raw\":1}\n\n```deneb-ui\n{}\n```",
		},
		{
			name:  "maximum ascii greeting",
			input: maxASCII + "\n```deneb-ui\n{}\n```",
			want:  maxASCII + "\n\n```deneb-ui\n{}\n```",
		},
		{
			name:  "overlong ascii greeting",
			input: longASCII + "\n```deneb-ui\n{}\n```",
			want:  "```deneb-ui\n{}\n```",
		},
		{
			name:  "maximum korean greeting",
			input: maxKorean + "\n```deneb-ui\n{}\n```",
			want:  maxKorean + "\n\n```deneb-ui\n{}\n```",
		},
		{
			name:  "overlong korean greeting",
			input: longKorean + "\n```deneb-ui\n{}\n```",
			want:  "```deneb-ui\n{}\n```",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := stripCardPreamble(tc.input); got != tc.want {
				t.Fatalf("stripCardPreamble() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveCronCommandBoundaryMatrix(t *testing.T) {
	sentinel := errors.New("collector failed")
	tests := []struct {
		name         string
		command      string
		data         string
		err          error
		collector    bool
		wantSame     bool
		wantContains string
	}{
		{
			name:         "ordinary bypass",
			command:      "do work",
			data:         "unused",
			err:          nil,
			collector:    false,
			wantSame:     true,
			wantContains: "",
		},
		{
			name:         "empty bypass",
			command:      "",
			data:         "unused",
			err:          nil,
			collector:    false,
			wantSame:     true,
			wantContains: "",
		},
		{
			name:         "near weekly bypass",
			command:      "/Weekly",
			data:         "unused",
			err:          nil,
			collector:    false,
			wantSame:     true,
			wantContains: "",
		},
		{
			name:         "weekly no collector",
			command:      "/weekly",
			data:         "",
			err:          nil,
			collector:    false,
			wantSame:     true,
			wantContains: "",
		},
		{
			name:         "alias no collector",
			command:      "/주간보고",
			data:         "",
			err:          nil,
			collector:    false,
			wantSame:     true,
			wantContains: "",
		},
		{
			name:         "weekly collector error",
			command:      "/weekly",
			data:         "",
			err:          sentinel,
			collector:    true,
			wantSame:     true,
			wantContains: "",
		},
		{
			name:         "alias collector error",
			command:      "/주간보고",
			data:         "",
			err:          sentinel,
			collector:    true,
			wantSame:     true,
			wantContains: "",
		},
		{
			name:         "weekly empty data",
			command:      "/weekly",
			data:         "",
			err:          nil,
			collector:    true,
			wantSame:     true,
			wantContains: "",
		},
		{
			name:         "weekly whitespace data",
			command:      "/weekly",
			data:         "  \n\t",
			err:          nil,
			collector:    true,
			wantSame:     true,
			wantContains: "",
		},
		{
			name:         "weekly json data",
			command:      "/weekly",
			data:         "{\"projects\":[1]}",
			err:          nil,
			collector:    true,
			wantSame:     false,
			wantContains: "{\"projects\":[1]}",
		},
		{
			name:         "alias json data",
			command:      "/주간보고",
			data:         "{\"issues\":[\"x\"]}",
			err:          nil,
			collector:    true,
			wantSame:     false,
			wantContains: "{\"issues\":[\"x\"]}",
		},
		{
			name:         "trimmed command",
			command:      " /weekly ",
			data:         "payload",
			err:          nil,
			collector:    true,
			wantSame:     false,
			wantContains: "payload",
		},
		{
			name:         "multiline payload",
			command:      "/weekly",
			data:         "line1\nline2",
			err:          nil,
			collector:    true,
			wantSame:     false,
			wantContains: "line1\nline2",
		},
		{
			name:         "unicode payload",
			command:      "/주간보고",
			data:         "프로젝트 데이터",
			err:          nil,
			collector:    true,
			wantSame:     false,
			wantContains: "프로젝트 데이터",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			adapter := New(Config{Logger: cronTestLogger()})
			if tc.collector {
				adapter.weeklyReportData = func(context.Context) (string, error) { calls.Add(1); return tc.data, tc.err }
			}
			got := adapter.resolveCronCommand(context.Background(), tc.command)
			if tc.wantSame && got != tc.command {
				t.Fatalf("got %q, want unchanged %q", got, tc.command)
			}
			if !tc.wantSame && got == tc.command {
				t.Fatalf("command was not expanded: %q", got)
			}
			if tc.wantContains != "" && !strings.Contains(got, tc.wantContains) {
				t.Errorf("expanded command missing %q", tc.wantContains)
			}
			if tc.collector && isWeeklyReportCommand(tc.command) && calls.Load() != 1 {
				t.Errorf("collector calls=%d", calls.Load())
			}
			if (!tc.collector || !isWeeklyReportCommand(tc.command)) && calls.Load() != 0 {
				t.Errorf("unexpected collector calls=%d", calls.Load())
			}
		})
	}
}

func TestDeterministicWeeklyRunBoundary(t *testing.T) {
	tests := []struct {
		name          string
		command       string
		text          string
		want          string
		wantTextCalls int64
		wantFormCalls int64
	}{
		{name: "weekly", command: "/weekly", text: "report", want: "report", wantTextCalls: 1, wantFormCalls: 1},
		{name: "alias", command: "/주간보고", text: "보고서", want: "보고서", wantTextCalls: 1, wantFormCalls: 1},
		{name: "trimmed weekly", command: " /weekly ", text: "  report  ", want: "  report  ", wantTextCalls: 1, wantFormCalls: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var textCalls atomic.Int64
			var formCalls atomic.Int64
			runner := New(Config{
				Logger:            cronTestLogger(),
				WeeklyReportText:  func(context.Context) (string, error) { textCalls.Add(1); return tc.text, nil },
				WeeklyFormDeliver: func(context.Context) error { formCalls.Add(1); return nil },
			})
			got, err := runner.RunAgentTurn(context.Background(), cron.AgentTurnParams{Command: tc.command})
			if err != nil {
				t.Fatalf("RunAgentTurn: %v", err)
			}
			if got != tc.want {
				t.Errorf("output=%q want=%q", got, tc.want)
			}
			if textCalls.Load() != tc.wantTextCalls {
				t.Errorf("text calls=%d", textCalls.Load())
			}
			if formCalls.Load() != tc.wantFormCalls {
				t.Errorf("form calls=%d", formCalls.Load())
			}
		})
	}
}

func TestMorningLetterUsesOneNoToolProjectionThenFixedRenderer(t *testing.T) {
	tests := []cron.AgentTurnParams{
		{SessionKey: "cron:morning-letter:1784338800000", Command: "custom operator prompt"},
		{SessionKey: "cron:anything:1", AgentID: "morning-letter", Command: "custom operator prompt"},
		{SessionKey: "cron:anything:2", Command: "/morning"},
		{SessionKey: "cron:anything:3", Command: "/모닝레터"},
	}
	for _, params := range tests {
		chat := &cronSyncStub{result: &chatport.SyncResult{
			Text: `{"headline":"오늘 핵심"}`, DeliverableText: `{"headline":"오늘 핵심"}`,
			Turns: 1, InputTokens: 1200, OutputTokens: 80,
		}}
		var dataCalls, renderCalls atomic.Int64
		runner := New(Config{
			Chat:   chat,
			Logger: cronTestLogger(),
			MorningLetterData: func(context.Context) (string, error) {
				dataCalls.Add(1)
				return `{"date":"2026-07-18","sections":{}}`, nil
			},
			MorningLetterRender: func(dataJSON, narrativeJSON string) (string, error) {
				renderCalls.Add(1)
				if !strings.Contains(dataJSON, "2026-07-18") || !strings.Contains(narrativeJSON, "오늘 핵심") {
					t.Fatalf("renderer inputs=(%q, %q)", dataJSON, narrativeJSON)
				}
				return "fixed card", nil
			},
		})
		got, err := runner.RunAgentTurn(context.Background(), params)
		if err != nil {
			t.Fatalf("RunAgentTurn(%+v): %v", params, err)
		}
		if got != "fixed card" || dataCalls.Load() != 1 || renderCalls.Load() != 1 || len(chat.requests) != 1 {
			t.Errorf("RunAgentTurn(%+v)=(%q data=%d render=%d requests=%d)", params, got, dataCalls.Load(), renderCalls.Load(), len(chat.requests))
		}
		req := chat.requests[0]
		if req.MaxTurns == nil || *req.MaxTurns != 1 || req.MaxTokens == nil || *req.MaxTokens != 1800 {
			t.Errorf("projection budgets turns=%v tokens=%v", req.MaxTurns, req.MaxTokens)
		}
		if req.MaxToolCallAttempts == nil || *req.MaxToolCallAttempts != 0 {
			t.Errorf("projection tool-call budget=%v, want 0", req.MaxToolCallAttempts)
		}
		if req.ToolPreset != morningLetterProjectionPreset || !req.SkipRecall || !req.EphemeralUser || !req.EphemeralAssistant || req.Thinking != "off" || req.SystemPrompt != morningLetterProjectionSystem {
			t.Errorf("projection request policy=%+v", req)
		}
		if !strings.Contains(req.Message, "FACT ENVELOPE") || strings.Contains(req.Message, "custom operator prompt") {
			t.Errorf("projection message=%q", req.Message)
		}
	}
}

func TestMorningLetterChatNotReadyFallsBackToFactsCard(t *testing.T) {
	runner := New(Config{
		Chat:   cronNotReadyStub{},
		Logger: cronTestLogger(),
		MorningLetterData: func(context.Context) (string, error) {
			return `{"date":"2026-07-18"}`, nil
		},
		MorningLetterRender: func(_ string, narrativeJSON string) (string, error) {
			if narrativeJSON != "" {
				t.Fatalf("fallback narrative=%q, want empty", narrativeJSON)
			}
			return "facts-only card", nil
		},
	})
	got, err := runner.RunAgentTurn(context.Background(), cron.AgentTurnParams{SessionKey: "cron:morning-letter:1"})
	if err != nil || got != "facts-only card" {
		t.Fatalf("fallback=(%q, %v)", got, err)
	}
}

func TestMorningLetterProjectionFailureFallsBackToFactsCard(t *testing.T) {
	chat := &cronSyncStub{err: errors.New("model unavailable")}
	runner := New(Config{
		Chat:   chat,
		Logger: cronTestLogger(),
		MorningLetterData: func(context.Context) (string, error) {
			return `{"date":"2026-07-18"}`, nil
		},
		MorningLetterRender: func(_ string, narrativeJSON string) (string, error) {
			if narrativeJSON != "" {
				t.Fatalf("fallback narrative=%q, want empty", narrativeJSON)
			}
			return "facts-only card", nil
		},
	})
	got, err := runner.RunAgentTurn(context.Background(), cron.AgentTurnParams{SessionKey: "cron:morning-letter:1"})
	if err != nil || got != "facts-only card" {
		t.Fatalf("fallback=(%q, %v)", got, err)
	}
}

func TestDeterministicWeeklyRunFormFailureIsBestEffort(t *testing.T) {
	want := "deterministic report"
	runner := New(Config{
		Logger:            cronTestLogger(),
		WeeklyReportText:  func(context.Context) (string, error) { return want, nil },
		WeeklyFormDeliver: func(context.Context) error { return errors.New("render unavailable") },
	})
	got, err := runner.RunAgentTurn(context.Background(), cron.AgentTurnParams{Command: "/weekly"})
	if err != nil {
		t.Fatalf("RunAgentTurn: %v", err)
	}
	if got != want {
		t.Fatalf("output=%q want=%q", got, want)
	}
}

func TestNewRunnerWiresCallbacksOnCreate(t *testing.T) {
	morningDataFn := func(context.Context) (string, error) { return "morning", nil }
	morningRenderFn := func(string, string) (string, error) { return "rendered", nil }
	dataFn := func(context.Context) (string, error) { return "data", nil }
	textFn := func(context.Context) (string, error) { return "text", nil }
	formFn := func(context.Context) error { return nil }
	logger := cronTestLogger()
	runner := New(Config{Logger: logger, MorningLetterData: morningDataFn, MorningLetterRender: morningRenderFn, WeeklyReportData: dataFn, WeeklyReportText: textFn, WeeklyFormDeliver: formFn})
	if runner == nil {
		t.Fatal("New returned nil")
	}
	if runner.logger != logger {
		t.Error("logger was not retained")
	}
	if runner.morningLetterData == nil || runner.morningLetterRender == nil {
		t.Error("morning projection boundaries missing")
	}
	if runner.weeklyReportData == nil {
		t.Error("data boundary missing")
	}
	if runner.weeklyReportText == nil {
		t.Error("text boundary missing")
	}
	if runner.weeklyFormDeliver == nil {
		t.Error("form boundary missing")
	}
}

func TestNilSubagentPollerBoundaries(t *testing.T) {
	poller := NewSubagentPoller(nil, nil)
	if poller == nil {
		t.Fatal("NewSubagentPoller returned nil interface")
	}
	if poller.HasActiveDescendants("client:main") {
		t.Error("nil registry reported active descendants")
	}
	if got := poller.CollectDescendantOutputs("client:main"); got != "" {
		t.Errorf("outputs=%q", got)
	}
	for _, key := range []string{"", "client:main", "cron:weekly:1", "한글", strings.Repeat("x", 4096)} {
		if poller.HasActiveDescendants(key) {
			t.Errorf("key %q reported active", key)
		}
		if got := poller.CollectDescendantOutputs(key); got != "" {
			t.Errorf("key %q outputs=%q", key, got)
		}
	}
}

func TestCronPureBoundariesConcurrent(t *testing.T) {
	const workers = 128
	const iterations = 100
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if !isWeeklyReportCommand(" \t/weekly\n") {
					errs <- fmt.Errorf("worker %d command mismatch", worker)
					return
				}
				in := fmt.Sprintf("leak-%d\nline-%d\n```deneb-ui\n{}\n```", worker, j)
				if got := stripCardPreamble(in); !strings.HasPrefix(got, "```deneb-ui") {
					errs <- fmt.Errorf("worker %d preamble=%q", worker, got)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
