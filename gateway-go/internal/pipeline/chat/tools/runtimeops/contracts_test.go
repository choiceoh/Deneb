package runtimeops

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

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
	deps := GatewayDeps{Runner: runner, Signaller: signaller, ConfigPath: "/custom/config", Now: func() time.Time { return now }, Version: "v-test"}
	if deps.runner() != runner || deps.signaller() != signaller || deps.configPath() != "/custom/config" || !deps.now().Equal(now) || deps.version() != "v-test" {
		t.Fatal("overrides not returned")
	}
	defaults := GatewayDeps{}
	if defaults.runner() == nil || defaults.signaller() == nil || defaults.configPath() == "" || defaults.now().IsZero() || defaults.version() != "dev" {
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
