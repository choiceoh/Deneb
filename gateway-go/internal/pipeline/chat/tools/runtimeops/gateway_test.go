package runtimeops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeRunner records commands and returns scripted responses per argv-prefix.
type fakeRunner struct {
	responses []fakeResponse
	calls     [][]string
}

type fakeResponse struct {
	match  func(name string, args []string) bool
	output []byte
	err    error
}

func (f *fakeRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	for _, r := range f.responses {
		if r.match(name, args) {
			return r.output, r.err
		}
	}
	return nil, errors.New("fakeRunner: no matching response for " + name + " " + strings.Join(args, " "))
}

// fakeSignaller records signals without actually sending them.
type fakeSignaller struct {
	sent []os.Signal
	err  error
}

func (f *fakeSignaller) Signal(sig os.Signal) error {
	f.sent = append(f.sent, sig)
	return f.err
}

func (f *fakeSignaller) PID() int { return 4242 }

// writeTempConfig creates a deneb.json fixture in a t.TempDir and returns
// its path. The caller sets DENEB_CONFIG_PATH so production code can find it.
func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "deneb.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// parseEnvelope unmarshals a needs_approval envelope.
func parseEnvelope(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("parse envelope: %v\nraw: %s", err, s)
	}
	return m
}

// ── status ────────────────────────────────────────────────────────────────

func TestGatewayStatusReturnsVersionPortAndUptime(t *testing.T) {
	cfgPath := writeTempConfig(t, `{"gateway": {"port": 19999}}`)
	tool := ToolGatewayWithDeps("", GatewayDeps{
		ConfigPath: cfgPath,
		Signaller:  &fakeSignaller{},
		Now:        func() time.Time { return gatewayStartTime.Add(65 * time.Second) },
		Version:    "test-1.2.3",
	})
	out := mustCallTool(t, tool, map[string]any{"action": "status"})
	if !strings.Contains(out, "test-1.2.3") {
		t.Errorf("status output missing version: %s", out)
	}
	if !strings.Contains(out, "19999") {
		t.Errorf("status output missing port: %s", out)
	}
	if !strings.Contains(out, "\"pid\": 4242") {
		t.Errorf("status output missing pid: %s", out)
	}
	if !strings.Contains(out, "1m") && !strings.Contains(out, "65s") {
		t.Errorf("status output missing uptime: %s", out)
	}
}

// ── config_get ────────────────────────────────────────────────────────────

func TestGatewayConfigGetReturnsDottedPathValue(t *testing.T) {
	cfgPath := writeTempConfig(t, `{"model": {"main": "glm-5.1", "fallback": "qwen"}}`)
	tool := ToolGatewayWithDeps("", GatewayDeps{ConfigPath: cfgPath})

	out := mustCallTool(t, tool, map[string]any{"action": "config_get", "path": "model.main"})
	if !strings.Contains(out, "glm-5.1") {
		t.Errorf("expected glm-5.1 in output: %s", out)
	}
	if !strings.Contains(out, "model.main") {
		t.Errorf("expected path echo in output: %s", out)
	}

	// Missing path → Korean error.
	out = mustCallTool(t, tool, map[string]any{"action": "config_get", "path": "does.not.exist"})
	if !strings.Contains(out, "찾을 수 없습니다") {
		t.Errorf("expected Korean not-found error: %s", out)
	}
}

// ── config_set ────────────────────────────────────────────────────────────

func TestGatewayConfigSetReturnsApprovalThenWritesOnConfirm(t *testing.T) {
	cfgPath := writeTempConfig(t, `{"model": {"main": "glm-5.1"}}`)
	tool := ToolGatewayWithDeps("", GatewayDeps{ConfigPath: cfgPath})

	// First call — expect approval envelope.
	out := mustCallTool(t, tool, map[string]any{
		"action": "config_set",
		"path":   "model.main",
		"value":  "qwen36",
	})
	env := parseEnvelope(t, out)
	if env["needs_approval"] != true {
		t.Errorf("expected needs_approval=true: %#v", env)
	}
	token, ok := env["action_token"].(string)
	if !ok || !strings.HasPrefix(token, "tok_") {
		t.Errorf("expected action_token: %#v", env)
	}
	if !strings.Contains(env["summary"].(string), "model.main") {
		t.Errorf("summary should mention path: %#v", env)
	}

	// Confirmed call — should write.
	mustCallTool(t, tool, map[string]any{
		"action":       "config_set.confirmed",
		"path":         "model.main",
		"value":        "qwen36",
		"action_token": token,
	})

	// Verify the file on disk.
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read cfg: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse cfg: %v", err)
	}
	model := m["model"].(map[string]any)
	if model["main"] != "qwen36" {
		t.Errorf("expected main=qwen36, got %v", model["main"])
	}
}

func TestGatewayConfigSetRejectsSecretPaths(t *testing.T) {
	cfgPath := writeTempConfig(t, `{}`)
	tool := ToolGatewayWithDeps("", GatewayDeps{ConfigPath: cfgPath})

	cases := []string{
		"gateway.auth.token",
		"providers.openai.api_key",
		"apiKey",
		"some.password.value",
		"creds.secret",
		"auth.credential",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			out := mustCallTool(t, tool, map[string]any{
				"action": "config_set",
				"path":   path,
				"value":  "fake",
			})
			if !strings.Contains(out, "거부") {
				t.Errorf("expected rejection for %q, got: %s", path, out)
			}
		})
	}
}

func TestGatewayConfigSetRejectsObjectReplacement(t *testing.T) {
	cfgPath := writeTempConfig(t, `{}`)
	tool := ToolGatewayWithDeps("", GatewayDeps{ConfigPath: cfgPath})
	out := mustCallTool(t, tool, map[string]any{
		"action": "config_set",
		"path":   "model",
		"value":  map[string]any{"main": "x"},
	})
	if !strings.Contains(out, "객체") {
		t.Errorf("expected object-rejection Korean error: %s", out)
	}
}

func TestGatewayConfigSetReturnsErrorWithoutPath(t *testing.T) {
	cfgPath := writeTempConfig(t, `{}`)
	tool := ToolGatewayWithDeps("", GatewayDeps{ConfigPath: cfgPath})
	raw, _ := json.Marshal(map[string]any{"action": "config_set"})
	_, err := tool(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Errorf("expected path-required error, got: %v", err)
	}
}

// ── restart ───────────────────────────────────────────────────────────────

func TestGatewayRestartApprovalEnvelope(t *testing.T) {
	sig := &fakeSignaller{}
	tool := ToolGatewayWithDeps("", GatewayDeps{Signaller: sig})
	out := mustCallTool(t, tool, map[string]any{"action": "restart"})
	env := parseEnvelope(t, out)
	if env["needs_approval"] != true {
		t.Errorf("expected needs_approval=true: %#v", env)
	}
	if env["action"] != "restart" {
		t.Errorf("expected action=restart: %#v", env)
	}
	btn, ok := env["confirm_button"].(map[string]any)
	if !ok {
		t.Fatalf("expected confirm_button object: %#v", env)
	}
	if btn["action"] != "restart.confirmed" {
		t.Errorf("expected confirm action=restart.confirmed: %#v", btn)
	}
	if btn["text"] != "재시작" {
		t.Errorf("expected Korean button label '재시작': %#v", btn)
	}
	// No signal should have been sent yet.
	if len(sig.sent) != 0 {
		t.Errorf("approval envelope must not trigger signal: %v", sig.sent)
	}
}

func TestGatewayRestartConfirmed(t *testing.T) {
	sig := &fakeSignaller{}
	tool := ToolGatewayWithDeps("", GatewayDeps{Signaller: sig})
	env := parseEnvelope(t, mustCallTool(t, tool, map[string]any{"action": "restart"}))
	token := env["action_token"].(string)
	mustCallTool(t, tool, map[string]any{"action": "restart.confirmed", "action_token": token})
	if len(sig.sent) != 1 || sig.sent[0] != syscall.SIGUSR1 {
		t.Errorf("expected SIGUSR1, got: %v", sig.sent)
	}
}

// A `.confirmed` call without a previously minted approval token must be
// rejected — otherwise the model can skip the human-approval step entirely.
func TestGatewayConfirmedWithoutApprovalRejected(t *testing.T) {
	sig := &fakeSignaller{}
	cfgPath := writeTempConfig(t, `{"model": {"main": "a"}}`)
	tool := ToolGatewayWithDeps("", GatewayDeps{Signaller: sig, ConfigPath: cfgPath})

	for _, call := range []map[string]any{
		{"action": "restart.confirmed"},
		{"action": "restart.confirmed", "action_token": "tok_forged"},
		{"action": "config_set.confirmed", "path": "model.main", "value": "b"},
		{"action": "config.patch.confirmed", "patch": map[string]any{"model": "x"}},
		{"action": "config.apply.confirmed", "config": map[string]any{}},
	} {
		out := mustCallTool(t, tool, call)
		if !strings.Contains(out, "거부") {
			t.Errorf("%v: expected rejection, got: %s", call["action"], out)
		}
	}
	if len(sig.sent) != 0 {
		t.Errorf("no signal may fire without approval: %v", sig.sent)
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), `"a"`) {
		t.Errorf("config must be untouched, got: %s", raw)
	}
}

// A token approved for one payload must not execute a different one.
func TestGatewayConfigSetConfirmPayloadMismatch(t *testing.T) {
	cfgPath := writeTempConfig(t, `{"model": {"main": "a"}}`)
	tool := ToolGatewayWithDeps("", GatewayDeps{ConfigPath: cfgPath})

	env := parseEnvelope(t, mustCallTool(t, tool, map[string]any{
		"action": "config_set", "path": "model.main", "value": "b",
	}))
	token := env["action_token"].(string)

	out := mustCallTool(t, tool, map[string]any{
		"action": "config_set.confirmed", "path": "model.fallback", "value": "b",
		"action_token": token,
	})
	if !strings.Contains(out, "거부") {
		t.Errorf("expected payload-mismatch rejection, got: %s", out)
	}
	// The mismatch attempt must not consume the token for the original payload.
	out = mustCallTool(t, tool, map[string]any{
		"action": "config_set.confirmed", "path": "model.main", "value": "b",
		"action_token": token,
	})
	if !strings.Contains(out, "저장했습니다") {
		t.Errorf("original approved payload should still execute, got: %s", out)
	}
	// Token is single-use.
	out = mustCallTool(t, tool, map[string]any{
		"action": "config_set.confirmed", "path": "model.main", "value": "b",
		"action_token": token,
	})
	if !strings.Contains(out, "거부") {
		t.Errorf("expected single-use token rejection on replay, got: %s", out)
	}
}

// Legacy bulk writes now share the secret block + approval gate.
func TestGatewayConfigPatchRejectsSecretsAndRequiresApproval(t *testing.T) {
	cfgPath := writeTempConfig(t, `{"model": {"main": "a"}}`)
	tool := ToolGatewayWithDeps("", GatewayDeps{ConfigPath: cfgPath})

	// Secret-looking key anywhere in the patch is rejected outright.
	out := mustCallTool(t, tool, map[string]any{
		"action": "config.patch",
		"patch":  map[string]any{"providers": map[string]any{"openai": map[string]any{"apiKey": "x"}}},
	})
	if !strings.Contains(out, "거부") {
		t.Errorf("expected secret-key rejection, got: %s", out)
	}
	// ...including inside array elements (providers: [{apiKey: ...}]).
	out = mustCallTool(t, tool, map[string]any{
		"action": "config.patch",
		"patch":  map[string]any{"providers": []any{map[string]any{"apiKey": "x"}}},
	})
	if !strings.Contains(out, "거부") {
		t.Errorf("expected secret-key-in-array rejection, got: %s", out)
	}

	// Clean patch: envelope → confirmed → written.
	patch := map[string]any{"model": map[string]any{"main": "b"}}
	env := parseEnvelope(t, mustCallTool(t, tool, map[string]any{"action": "config.patch", "patch": patch}))
	if env["needs_approval"] != true {
		t.Fatalf("expected approval envelope: %#v", env)
	}
	token := env["action_token"].(string)
	out = mustCallTool(t, tool, map[string]any{
		"action": "config.patch.confirmed", "patch": patch, "action_token": token,
	})
	if !strings.Contains(out, "패치했습니다") {
		t.Errorf("expected patch success, got: %s", out)
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), `"b"`) {
		t.Errorf("patch not written: %s", raw)
	}
}

// ── update ────────────────────────────────────────────────────────────────

// matcherFor returns a responder that matches `git` with the given argv prefix.
func matcherFor(name string, argsPrefix ...string) func(string, []string) bool {
	return func(gotName string, gotArgs []string) bool {
		if gotName != name {
			return false
		}
		if len(gotArgs) < len(argsPrefix) {
			return false
		}
		for i, a := range argsPrefix {
			if gotArgs[i] != a {
				return false
			}
		}
		return true
	}
}

func TestGatewayUpdateRejectsDirtyWorktree(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{
		{match: matcherFor("git", "rev-parse"), output: []byte("main\n")},
		{match: matcherFor("git", "status"), output: []byte(" M gateway-go/some.go\n")},
	}}
	tool := ToolGatewayWithDeps("/tmp", GatewayDeps{Runner: runner, Signaller: &fakeSignaller{}})
	out := mustCallTool(t, tool, map[string]any{"action": "update"})
	if !strings.Contains(out, "거부") || !strings.Contains(out, "커밋되지 않은") {
		t.Errorf("expected Korean dirty-worktree rejection: %s", out)
	}
}

func TestGatewayUpdateRejectsNonMainBranch(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{
		{match: matcherFor("git", "rev-parse"), output: []byte("feature/foo\n")},
	}}
	tool := ToolGatewayWithDeps("/tmp", GatewayDeps{Runner: runner, Signaller: &fakeSignaller{}})
	out := mustCallTool(t, tool, map[string]any{"action": "update"})
	if !strings.Contains(out, "거부") || !strings.Contains(out, "main") {
		t.Errorf("expected Korean non-main rejection: %s", out)
	}
}

func TestGatewayUpdateReturnsApprovalEnvelope(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{
		{match: matcherFor("git", "rev-parse"), output: []byte("main\n")},
		{match: matcherFor("git", "status"), output: []byte("")},
	}}
	sig := &fakeSignaller{}
	tool := ToolGatewayWithDeps("/tmp", GatewayDeps{Runner: runner, Signaller: sig})
	out := mustCallTool(t, tool, map[string]any{"action": "update"})
	env := parseEnvelope(t, out)
	if env["needs_approval"] != true {
		t.Errorf("expected needs_approval=true: %#v", env)
	}
	btn := env["confirm_button"].(map[string]any)
	if btn["action"] != "update.confirmed" {
		t.Errorf("expected confirm action=update.confirmed: %#v", btn)
	}
	if btn["text"] != "업데이트" {
		t.Errorf("expected Korean label '업데이트': %#v", btn)
	}
	if len(sig.sent) != 0 {
		t.Errorf("approval envelope must not trigger restart: %v", sig.sent)
	}
}

func TestGatewayUpdateConfirmedHappyPath(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{
		{match: matcherFor("git", "rev-parse"), output: []byte("main\n")},
		{match: matcherFor("git", "status"), output: []byte("")},
		{match: matcherFor("git", "pull"), output: []byte("Already up to date.\n")},
		{match: matcherFor("make", "go"), output: []byte("built\n")},
	}}
	sig := &fakeSignaller{}
	tool := ToolGatewayWithDeps("/tmp", GatewayDeps{
		Runner: runner, Signaller: sig,
		Now: func() time.Time { return time.Unix(1, 0) },
	})
	env := parseEnvelope(t, mustCallTool(t, tool, map[string]any{"action": "update"}))
	token := env["action_token"].(string)
	out := mustCallTool(t, tool, map[string]any{"action": "update.confirmed", "action_token": token})
	if !strings.Contains(out, "업데이트 완료") {
		t.Errorf("expected success Korean message: %s", out)
	}
	if len(sig.sent) != 1 || sig.sent[0] != syscall.SIGUSR1 {
		t.Errorf("expected SIGUSR1 after successful update, got: %v", sig.sent)
	}
}

// ── bad input ─────────────────────────────────────────────────────────────

func TestGatewayUnknownAction(t *testing.T) {
	tool := ToolGatewayWithDeps("", GatewayDeps{})
	out := mustCallTool(t, tool, map[string]any{"action": "wat"})
	if !strings.Contains(out, "알 수 없는") {
		t.Errorf("expected Korean unknown-action error: %s", out)
	}
}
