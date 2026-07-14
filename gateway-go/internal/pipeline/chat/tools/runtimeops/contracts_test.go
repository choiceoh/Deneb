package runtimeops

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
)

func TestFormatExecResultJSONContract(t *testing.T) {
	r := &process.ExecResult{Stdout: "out", Stderr: "err", ExitCode: 7, RuntimeMs: 123, Error: "failed", StdoutDroppedBytes: 11, StderrDroppedBytes: 22}
	got := formatExecResultJSON(r)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"stdout": "out", "stderr": "err", "exit_code": float64(7), "runtime_ms": float64(123),
		"error": "failed", "stdout_dropped_bytes": float64(11), "stderr_dropped_bytes": float64(22),
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded = %#v, want %#v", decoded, want)
	}
	minimal := formatExecResultJSON(&process.ExecResult{})
	if strings.Contains(minimal, "dropped") || strings.Contains(minimal, `"error"`) {
		t.Fatalf("minimal has optional fields: %s", minimal)
	}
}

func TestFormatExecResultContractAdditional(t *testing.T) {
	for _, tt := range []struct {
		name string
		r    *process.ExecResult
		want []string
		gone []string
	}{
		{name: "empty", r: &process.ExecResult{}, want: []string{"(no output)"}},
		{name: "stdout", r: &process.ExecResult{Stdout: "hello"}, want: []string{"hello"}, gone: []string{"Exit code"}},
		{name: "stderr", r: &process.ExecResult{Stderr: "warning"}, want: []string{"STDERR:\nwarning"}},
		{name: "all", r: &process.ExecResult{Stdout: "out", Stderr: "err", Error: "boom", ExitCode: 2}, want: []string{"out", "STDERR:\nerr", "Error: boom", "Exit code: 2"}},
		{name: "truncated", r: &process.ExecResult{Stdout: "tail", Stderr: "etail", StdoutDroppedBytes: 10, StderrDroppedBytes: 20}, want: []string{"first 10 bytes dropped", "first 20 bytes dropped", "tail", "etail"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := formatExecResult(tt.r)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q: %s", want, got)
				}
			}
			for _, gone := range tt.gone {
				if strings.Contains(got, gone) {
					t.Errorf("unexpected %q: %s", gone, got)
				}
			}
		})
	}
}

func TestValidateWorkdirCacheContracts(t *testing.T) {
	workdirCache = sync.Map{}
	dir := t.TempDir()
	if err := validateWorkdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	// Positive result remains valid inside the deliberate short cache TTL.
	if err := validateWorkdir(dir); err != nil {
		t.Fatalf("cached positive = %v", err)
	}
	workdirCache.Delete(dir)
	if err := validateWorkdir(dir); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing = %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Negative result is cached too.
	if err := validateWorkdir(dir); err == nil {
		t.Fatal("negative cache unexpectedly refreshed")
	}
	workdirCache.Delete(dir)
	if err := validateWorkdir(dir); err != nil {
		t.Fatalf("recreated = %v", err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateWorkdir(file); err == nil {
		t.Fatal("regular file accepted")
	}
}

func TestToolExecFallbackValidationStructuredAndHints(t *testing.T) {
	tool := ToolExec(nil, t.TempDir())
	if _, err := callTool(t, tool, map[string]any{}); err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("missing command = %v", err)
	}
	if _, err := callTool(t, tool, map[string]any{"command": "pwd", "workdir": filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("missing workdir accepted")
	}
	out, err := callTool(t, tool, map[string]any{"command": "printf hello", "structured": true})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result["stdout"] != "hello" || result["exit_code"] != float64(0) || result["timed_out"] != false {
		t.Fatalf("structured = %#v", result)
	}
	out, err = callTool(t, tool, map[string]any{"command": "grep needle /dev/null"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no matches") || !strings.Contains(out, "exit status 1") {
		t.Fatalf("grep hint = %q", out)
	}
	blocked, err := callTool(t, tool, map[string]any{"command": "rm -rf /"})
	if err != nil || !strings.Contains(blocked, "실행 거부") {
		t.Fatalf("blocked = %q/%v", blocked, err)
	}
}

func TestToolProcessNilManagerContracts(t *testing.T) {
	tool := ToolProcess(nil)
	for _, action := range []string{"list", "poll", "log", "write", "kill", "unknown"} {
		out, err := callTool(t, tool, map[string]any{"action": action})
		if err != nil || out != "Process manager not available." {
			t.Errorf("action %s = %q/%v", action, out, err)
		}
	}
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

func TestDottedGetSetContracts(t *testing.T) {
	root := map[string]any{"model": map[string]any{"main": "a"}, "scalar": 1.0}
	if got, ok := dottedGet(root, "model.main"); !ok || got != "a" {
		t.Fatalf("get = %#v/%v", got, ok)
	}
	for _, path := range []string{"model.missing", "scalar.child", "missing.child"} {
		if got, ok := dottedGet(root, path); ok || got != nil {
			t.Errorf("get %q = %#v/%v", path, got, ok)
		}
	}
	if err := dottedSet(root, "model.fallback", "b"); err != nil {
		t.Fatal(err)
	}
	if err := dottedSet(root, "new.nested.value", 3); err != nil {
		t.Fatal(err)
	}
	if got, _ := dottedGet(root, "model.fallback"); got != "b" {
		t.Fatalf("fallback = %#v", got)
	}
	if got, _ := dottedGet(root, "new.nested.value"); got != 3 {
		t.Fatalf("nested = %#v", got)
	}
	if err := dottedSet(root, "scalar.child", 2); err == nil || !strings.Contains(err.Error(), "path conflict") {
		t.Fatalf("conflict = %v", err)
	}
	for _, path := range []string{"", ".a", "a.", "a..b", "  .a"} {
		if err := dottedSet(root, path, 1); err == nil || !strings.Contains(err.Error(), "empty path") {
			t.Errorf("path %q = %v", path, err)
		}
	}
}

func TestLoadRawConfigMapContract(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	if got, err := loadRawConfigMap(missing); err != nil || len(got) != 0 {
		t.Fatalf("missing = %#v/%v", got, err)
	}
	nullPath := filepath.Join(t.TempDir(), "null.json")
	if err := os.WriteFile(nullPath, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := loadRawConfigMap(nullPath); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("null = %#v/%v", got, err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRawConfigMap(bad); err == nil {
		t.Fatal("bad JSON accepted")
	}
	valid := filepath.Join(t.TempDir(), "valid.json")
	if err := os.WriteFile(valid, []byte(`{"a":{"b":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := loadRawConfigMap(valid); err != nil || got["a"] == nil {
		t.Fatalf("valid = %#v/%v", got, err)
	}
}

func TestFindSecretKeyNestedAndArrayContracts(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value map[string]any
		want  string
	}{
		{name: "clean", value: map[string]any{"model": map[string]any{"main": "x"}}},
		{name: "root", value: map[string]any{"password": "x"}, want: "password"},
		{name: "nested", value: map[string]any{"provider": map[string]any{"api_key": "x"}}, want: "provider.api_key"},
		{name: "array", value: map[string]any{"providers": []any{map[string]any{"name": "x"}, map[string]any{"credential": "x"}}}, want: "providers[1].credential"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := findSecretKey("", tt.value); got != tt.want {
				t.Fatalf("findSecretKey = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApprovalRegistryRejectsExpiredOrReusedApproval(t *testing.T) {
	pendingApprovalsMu.Lock()
	pendingApprovals = map[string]pendingApproval{}
	pendingApprovalsMu.Unlock()
	registerPendingApproval("token", "config_set", `["a",1]`)
	if got := consumePendingApproval("token", "update", `["a",1]`); !strings.Contains(got, "config_set") {
		t.Fatalf("action mismatch = %q", got)
	}
	if got := consumePendingApproval("token", "config_set", `["b",1]`); !strings.Contains(got, "내용과 실행 요청") {
		t.Fatalf("payload mismatch = %q", got)
	}
	if got := consumePendingApproval("token", "config_set", `["a",1]`); got != "" {
		t.Fatalf("valid = %q", got)
	}
	if got := consumePendingApproval("token", "config_set", `["a",1]`); !strings.Contains(got, "유효하지 않거나 만료") {
		t.Fatalf("reuse = %q", got)
	}
	pendingApprovalsMu.Lock()
	pendingApprovals["expired"] = pendingApproval{action: "x", expires: time.Now().Add(-time.Second)}
	pendingApprovalsMu.Unlock()
	if got := consumePendingApproval("expired", "x", ""); !strings.Contains(got, "만료") {
		t.Fatalf("expired = %q", got)
	}
}

func TestApprovalPayloadEnvelopeAndValueFormatting(t *testing.T) {
	p1 := approvalPayload(map[string]any{"b": 2, "a": 1})
	p2 := approvalPayload(map[string]any{"a": 1, "b": 2})
	if p1 != p2 {
		t.Fatalf("payload not canonical: %q %q", p1, p2)
	}
	if got := formatValueForSummary("a\n b"); got != `"a\n b"` {
		t.Fatalf("string summary = %q", got)
	}
	if got := formatValueForSummary([]int{1, 2}); got != `[1,2]` {
		t.Fatalf("array summary = %q", got)
	}
	if got := formatValueForSummary(math.Inf(1)); got != "+Inf" {
		t.Fatalf("fallback summary = %q", got)
	}
	out, err := approvalEnvelope("tok", "restart", "summary", "Restart")
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	button := env["confirm_button"].(map[string]any)
	if env["needs_approval"] != true || env["action_token"] != "tok" || button["action"] != "restart.confirmed" || button["token"] != "tok" || button["text"] != "Restart" {
		t.Fatalf("envelope = %#v", env)
	}
}

func TestNewActionTokenCreatesUniquePrefixedTokens(t *testing.T) {
	seen := map[string]bool{}
	for range 128 {
		token := newActionToken()
		if !strings.HasPrefix(token, "tok_") || len(token) < 8 || seen[token] {
			t.Fatalf("token = %q", token)
		}
		seen[token] = true
	}
}

func TestGatewayDepsReturnsOverridesOrDefaults(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	runner := &fakeRunner{}
	signaller := &fakeSignaller{}
	deps := GatewayDeps{Runner: runner, Signaller: signaller, ConfigPath: "/custom/config", Now: func() time.Time { return now }}
	if deps.runner() != runner || deps.signaller() != signaller || deps.configPath() != "/custom/config" || !deps.now().Equal(now) {
		t.Fatal("overrides not returned")
	}
	defaults := GatewayDeps{}
	if defaults.runner() == nil || defaults.signaller() == nil || defaults.configPath() == "" || defaults.now().IsZero() {
		t.Fatal("defaults missing")
	}
}

func TestGatewayStatusUnreadableConfigStillReportsHealth(t *testing.T) {
	dir := t.TempDir()
	out, err := gatewayStatus(GatewayDeps{ConfigPath: dir, Signaller: &fakeSignaller{}, Now: func() time.Time { return gatewayStartTime.Add(time.Minute) }})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got["config"] != dir || got["config_error"] == nil || got["pid"] != float64(4242) || got["uptime"] != "1m" {
		t.Fatalf("status = %#v", got)
	}
}

func TestFormatGatewayUptimeBoundaries(t *testing.T) {
	for _, tt := range []struct {
		d    time.Duration
		want string
	}{
		{d: -time.Second, want: "0s"},
		{d: 0, want: "0s"},
		{d: 59*time.Second + 999*time.Millisecond, want: "59s"},
		{d: time.Minute, want: "1m"},
		{d: 59*time.Minute + 59*time.Second, want: "59m"},
		{d: time.Hour, want: "1h 0m"},
		{d: 25*time.Hour + 2*time.Minute, want: "1d 1h 2m"},
	} {
		if got := formatGatewayUptime(tt.d); got != tt.want {
			t.Errorf("formatGatewayUptime(%s) = %q", tt.d, got)
		}
	}
}

func TestUnicodeFormattingRemainsValid(t *testing.T) {
	long := strings.Repeat("가", 20)
	if got := shortHash(long); !utf8.ValidString(got) {
		// Hashes are normally ASCII, but helper output must not corrupt strings if
		// an alternate provenance backend supplies an opaque Unicode identifier.
		t.Fatalf("shortHash returned invalid UTF-8: %q", got)
	}
}
