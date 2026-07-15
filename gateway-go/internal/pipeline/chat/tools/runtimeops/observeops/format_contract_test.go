package observeops

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)


func callTool(t *testing.T, fn toolport.ToolFunc, params any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return fn(context.Background(), raw)
}

func TestShortHashAndToolProvenanceContract(t *testing.T) {
	for _, tt := range []struct{ in, want string }{{}, {in: "short", want: "short"}, {in: "123456789012", want: "123456789012"}, {in: "1234567890123", want: "123456789012"}} {
		if got := shortHash(tt.in); got != tt.want {
			t.Errorf("shortHash(%q) = %q", tt.in, got)
		}
	}
	if got := formatToolProvenance(agentlog.TurnToolData{}); got != "" {
		t.Fatalf("empty provenance = %q", got)
	}
	td := agentlog.TurnToolData{
		ToolUseID: "id", InputBytes: 7, InputHash: "123456789012345", OutputHash: "abcdefghijklmno",
		Targets:     []string{"a", "b", "c", "d"},
		FileEffects: []agentlog.ToolFileEffect{{Path: "file", ExistsBefore: true, ExistsAfter: true, Changed: true, AddedLines: 2, RemovedLines: 1}},
	}
	got := formatToolProvenance(td)
	for _, want := range []string{"id=id", "in 7B", "in#=123456789012", "out#=abcdefghijkl", "target=a,b,c", "file=file:changed +2/-1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "target=a,b,c,d") {
		t.Fatalf("targets not capped: %s", got)
	}
}

func TestFormatFileEffectsMatrixAndCap(t *testing.T) {
	if got := formatFileEffects(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	effects := []agentlog.ToolFileEffect{
		{Path: "created", ExistsAfter: true, AddedLines: 3},
		{Path: "deleted", ExistsBefore: true, RemovedLines: 4},
		{Path: "changed", ExistsBefore: true, ExistsAfter: true, Changed: true, AddedLines: 2, RemovedLines: 1},
		{Path: "ignored", ExistsBefore: true, ExistsAfter: true},
	}
	got := formatFileEffects(effects)
	want := "created:created +3/-0;deleted:deleted +0/-4;changed:changed +2/-1;...+1"
	if got != want {
		t.Fatalf("effects = %q, want %q", got, want)
	}
	if got := formatFileEffects([]agentlog.ToolFileEffect{{Path: "same", ExistsBefore: true, ExistsAfter: true}}); got != "same:unchanged" {
		t.Fatalf("unchanged = %q", got)
	}
}

func TestFormatObserveLogsContract(t *testing.T) {
	if got := formatObserveLogs(nil); got != "no matching log lines in the recent ring" {
		t.Fatalf("empty = %q", got)
	}
	got := formatObserveLogs([]observe.LogLine{
		{Level: "ERROR", RunID: "run-1", Session: "ignored", Msg: "failure"},
		{Level: "INFO", Session: "client:main", Msg: "message"},
		{Level: "DEBUG", Msg: "plain"},
	})
	for _, want := range []string{"3 log lines", "[ERROR] run=run-1 failure", "[INFO] sess=client:main message", "[DEBUG] plain"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestFormatObserveProvenanceContract(t *testing.T) {
	if got := formatObserveProvenance(nil); got != "no matching tool provenance in retained agent logs" {
		t.Fatalf("empty = %q", got)
	}
	got := formatObserveProvenance([]agentlog.ToolProvenanceEvent{{
		Name: "edit", RunID: "run", Session: "sess", Turn: 2, ToolUseID: "tool", Targets: []string{"a", "b"},
		InputHash: "123456789012345", OutputHash: "abcdefghijklmno", IsError: true,
		FileEffects: []agentlog.ToolFileEffect{{Path: "x", ExistsAfter: true, AddedLines: 1}},
	}})
	for _, want := range []string{"1 tool provenance", "edit run=run session=sess turn=2", "id=tool", "target=a,b", "in#=123456789012", "out#=abcdefghijkl", "effect=x:created +1/-0", "ERROR"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestFormatTurnEffortAdditional(t *testing.T) {
	if got := formatTurnEffort(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	if got := formatTurnEffort([]agentlog.TurnLLMData{{Turn: 1}}); got != "" {
		t.Fatalf("inactive = %q", got)
	}
	got := formatTurnEffort([]agentlog.TurnLLMData{{Turn: 1, ThinkingOff: true, ObsRunes: 100}, {Turn: 2, ObsRunes: 200}})
	if got != "  effort: t1:off/obs=100 t2:on/obs=200\n" {
		t.Fatalf("effort = %q", got)
	}
}

func TestFormatObserveBehaviorContract(t *testing.T) {
	agg := agentlog.AggregateResult{
		Runs: 4, ProactiveRuns: 1, CompactedRuns: 2, TotalInputTokens: 100, TotalOutputTokens: 50, CacheReadTokens: 80,
		Tools: []agentlog.ToolStat{
			{Name: "exec", Calls: 2, Errors: 1, AvgMs: 12, Repaired: 1, Unknown: 2, Blocked: 3, TotalOutputChars: 5000, MaxOutputChars: 4000},
		},
		ProactiveDecisions: map[string]int{"delivered": 1}, BackgroundErrors: map[string]int{"mail": 2},
	}
	got := formatObserveBehavior(agg, 7)
	for _, want := range []string{"behavior (last 7d): runs=4 proactive=1 compacted=2 in=100 out=50 cacheRead=80", "exec: 2 calls, 1 err, 12ms avg", "1 repaired-args", "2 unknown-name", "3 blocked", "~2.5K out avg (max 4.0K)", "proactive funnel:", "background errors:"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	tools := make([]agentlog.ToolStat, 17)
	for i := range tools {
		tools[i] = agentlog.ToolStat{Name: string(rune('a' + i)), Calls: 1}
	}
	if got := formatObserveBehavior(agentlog.AggregateResult{Tools: tools}, 0); !strings.Contains(got, "… and 2 more tools") || !strings.Contains(got, "all retained history") {
		t.Fatalf("cap/all = %s", got)
	}
}

func TestMeanTokensAndFormatObserveEffortContracts(t *testing.T) {
	if meanTokens(100, 0) != 0 || meanTokens(101, 4) != 25 {
		t.Fatal("meanTokens")
	}
	if got := formatObserveEffort(agentlog.EffortStat{}, 0); !strings.Contains(got, "no router-active runs") || !strings.Contains(got, "all retained history") {
		t.Fatalf("empty effort = %s", got)
	}
	for _, tt := range []struct {
		name string
		stat agentlog.EffortStat
		want []string
	}{
		{name: "kept only", stat: agentlog.EffortStat{KeptRuns: 2, KeptOutputTokens: 40}, want: []string{"active=2 routed-off=0", "router never routed off"}},
		{name: "healthy", stat: agentlog.EffortStat{RoutedRuns: 10, KeptRuns: 10, EscalatedRuns: 1, RoutedEndTurn: 9, RoutedOutputTokens: 100, KeptOutputTokens: 400}, want: []string{"active=20", "routed-off=10 (50%)", "healthy", "mean output tokens: routed-off=10 kept-on=40"}},
		{name: "high", stat: agentlog.EffortStat{RoutedRuns: 10, KeptRuns: 2, EscalatedRuns: 2, RoutedTimeout: 2, RoutedEndTurn: 8, KeptReasons: map[string]int{"z": 1, "a": 2}}, want: []string{"high: gates may be too aggressive", "8 clean, 2 timeout", "kept-on gates: a=2 z=1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := formatObserveEffort(tt.stat, 3)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q:\n%s", want, got)
				}
			}
		})
	}
}

func TestFormatObserveProactiveThresholdsAndSorting(t *testing.T) {
	if got := formatObserveProactive(workfeed.EngagementStat{}); got != "proactive engagement: no retained cards.\n" {
		t.Fatalf("empty = %q", got)
	}
	for _, tt := range []struct {
		name string
		stat workfeed.EngagementStat
		want string
	}{
		{name: "pending", stat: workfeed.EngagementStat{Total: 2, Pending: 2}, want: "not enough judged"},
		{name: "healthy", stat: workfeed.EngagementStat{Total: 10, Engaged: 9, Ignored: 1}, want: "healthy"},
		{name: "watch", stat: workfeed.EngagementStat{Total: 4, Engaged: 3, Ignored: 1}, want: "watch"},
		{name: "high", stat: workfeed.EngagementStat{Total: 4, Engaged: 2, Ignored: 2}, want: "high over-intervention"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := formatObserveProactive(tt.stat)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("missing %q: %s", tt.want, got)
			}
		})
	}
	got := formatObserveProactive(workfeed.EngagementStat{Total: 3, Ignored: 3, BySource: map[string]int{"z": 1, "b": 2, "a": 2}})
	if !strings.Contains(got, "ignored by source: a=2 b=2 z=1") {
		t.Fatalf("sort = %s", got)
	}
}

func TestFormatVllmPrefixCachesContract(t *testing.T) {
	if got := formatVllmPrefixCaches(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	got := formatVllmPrefixCaches([]observe.VllmPrefixCache{{Model: "model", Hits: 82, Queries: 100, HitRatePct: 82}, {Hits: 0, Queries: 0}})
	for _, want := range []string{"prefix cache (model, since engine boot): 82/100 (82.0%)", "prefix cache (vllm, since engine boot): 0/0 (0.0%)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

func TestToolObserveNilDependencyResponsesAndInvalidJSON(t *testing.T) {
	tool := ToolObserve(nil, nil, nil, nil)
	if _, err := tool(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	for _, tt := range []struct{ action, want string }{
		{action: "logs", want: "log capture is not wired"},
		{action: "behavior", want: "agent log is not wired"},
		{action: "effort", want: "agent log is not wired"},
		{action: "proactive", want: "work feed is not wired"},
		{action: "provenance", want: "agent log is not wired"},
	} {
		out, err := callTool(t, tool, map[string]any{"action": tt.action})
		if err != nil || !strings.Contains(out, tt.want) {
			t.Errorf("%s = %q/%v", tt.action, out, err)
		}
	}
	if _, err := callTool(t, tool, map[string]any{"action": "turn"}); err == nil {
		t.Fatal("turn without id accepted")
	}
	if out, err := callTool(t, tool, map[string]any{"action": "turn", "runId": "missing"}); err != nil || !strings.Contains(out, "found=false") {
		t.Fatalf("missing turn = %q/%v", out, err)
	}
}


func TestUnicodeFormattingRemainsValid(t *testing.T) {
	long := strings.Repeat("한", 20)
	if got := shortHash(long); !utf8.ValidString(got) {
		t.Fatalf("shortHash returned invalid UTF-8: %q", got)
	}
}
