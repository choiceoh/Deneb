package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// recordingInvoker captures the tool calls the bridge forwards and returns a
// canned result, so the security boundary can be tested without real tools.
type recordingInvoker struct {
	mu     sync.Mutex
	calls  []string // "name:argsJSON"
	result string
}

func (r *recordingInvoker) Execute(_ context.Context, name string, input json.RawMessage) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, name+":"+string(input))
	r.mu.Unlock()
	return r.result, nil
}

func (r *recordingInvoker) called() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// TestCodeActionAllow pins the read-only allowlist: read actions pass, every
// mutating / outbound action and unknown tool is rejected.
func TestCodeActionAllow(t *testing.T) {
	cases := []struct {
		tool   string
		action string
		ok     bool
	}{
		{"gmail", "search", true},
		{"gmail", "inbox", true},
		{"gmail", "analyze", true},
		{"gmail", "send", false},
		{"gmail", "reply", false},
		{"gmail", "label", false},
		{"gmail", "attachment", false},
		{"calendar", "list", true},
		{"calendar", "free_slots", true},
		{"calendar", "create", false},
		{"calendar", "delete", false},
		{"contacts", "lookup", true},
		{"contacts", "search", true},
		{"wiki", "search", true},
		{"wiki", "read", true},
		{"wiki", "write", false},
		{"wiki", "log", false},
		{"read", "", false}, // arbitrary file read is not on the bridge
		{"exec", "", false},
		{"fs", "", false},
	}
	for _, c := range cases {
		args := map[string]any{}
		if c.action != "" {
			args["action"] = c.action
		}
		err := codeActionAllow(c.tool, args)
		if c.ok && err != nil {
			t.Errorf("%s/%s: expected allowed, got %v", c.tool, c.action, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s/%s: expected rejected, got nil", c.tool, c.action)
		}
	}

	// A known tool with a missing action is rejected (not silently allowed).
	if err := codeActionAllow("gmail", map[string]any{}); err == nil {
		t.Error("gmail with no action should be rejected")
	}
}

// TestCodeActionBridge covers the HTTP bridge: token auth, allowlist
// enforcement, and forwarding of permitted calls to the invoker.
func TestCodeActionBridge(t *testing.T) {
	inv := &recordingInvoker{result: "RESULT_OK"}
	b := &codeActionBridge{invoker: inv, token: "secret-token", ctx: context.Background()}
	srv := httptest.NewServer(b)
	defer srv.Close()

	post := func(token, body string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
		req.Header.Set("X-Deneb-Bridge-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	// Wrong token → 403, invoker untouched.
	if code, _ := post("wrong", `{"tool":"gmail","args":{"action":"search"}}`); code != http.StatusForbidden {
		t.Fatalf("wrong token: want 403, got %d", code)
	}

	// Allowed call → forwarded, result returned.
	code, out := post("secret-token", `{"tool":"gmail","args":{"action":"search","query":"탑솔라"}}`)
	if code != http.StatusOK || out["ok"] != true || out["result"] != "RESULT_OK" {
		t.Fatalf("allowed call: code=%d out=%v", code, out)
	}

	// Disallowed action → ok:false, invoker NOT called for it.
	_, out = post("secret-token", `{"tool":"gmail","args":{"action":"send","to":"x","body":"y"}}`)
	if out["ok"] != false {
		t.Fatalf("send should be rejected, got %v", out)
	}

	calls := inv.called()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "gmail:") || !strings.Contains(calls[0], "search") {
		t.Fatalf("invoker should have been called once for the search, got %v", calls)
	}
	for _, c := range calls {
		if strings.Contains(c, "send") {
			t.Fatalf("send must never reach the invoker, got %v", calls)
		}
	}
}

// requirePython skips the test if python3 is not on PATH (CI has no GPU host
// toolchain guarantee). The pure-Go tests above always run.
func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available; skipping code_action sandbox e2e")
	}
}

func runCodeAction(t *testing.T, inv ToolInvoker, code string) string {
	t.Helper()
	in, _ := json.Marshal(map[string]any{"code": code, "timeout": 30})
	out, err := ToolCodeAction(inv)(context.Background(), json.RawMessage(in))
	if err != nil {
		t.Fatalf("ToolCodeAction: %v", err)
	}
	return out
}

// TestCodeAction_OutputAndBridge runs real Python: print() round-trips and a
// permitted deneb.gmail call reaches the (fake) invoker.
func TestCodeAction_OutputAndBridge(t *testing.T) {
	requirePython(t)
	inv := &recordingInvoker{result: "MAILS:a,b,c"}
	out := runCodeAction(t, inv, `
mails = deneb.gmail("search", query="탑솔라", max=5)
print("GOT", mails)
`)
	if !strings.Contains(out, "GOT MAILS:a,b,c") {
		t.Fatalf("expected bridge result echoed, got:\n%s", out)
	}
	calls := inv.called()
	if len(calls) != 1 || !strings.Contains(calls[0], `"action":"search"`) || !strings.Contains(calls[0], "탑솔라") {
		t.Fatalf("expected one gmail search call, got %v", calls)
	}
}

// TestCodeAction_SandboxBlocks is the security core: the audit hook must block
// network, subprocess, out-of-sandbox writes, and secret reads — each surfacing
// as a traceback the model can read.
func TestCodeAction_SandboxBlocks(t *testing.T) {
	requirePython(t)
	inv := &recordingInvoker{result: ""}

	cases := []struct {
		name string
		code string
		want string
	}{
		{
			"network",
			`import socket; socket.create_connection(("1.1.1.1", 80), timeout=2)`,
			"network is disabled",
		},
		{
			"subprocess",
			`import os; os.system("echo pwned")`,
			"spawning processes is disabled",
		},
		{
			"write_outside",
			`open("/tmp/deneb-codeaction-escape.txt", "w").write("x")`,
			"writes are limited to the scratch directory",
		},
		{
			"secret_read",
			`import os; open(os.path.expanduser("~/.deneb/deneb.json"))`,
			"is disabled",
		},
		{
			"import_subprocess",
			`import subprocess`,
			"is disabled",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := runCodeAction(t, inv, c.code)
			if !strings.Contains(out, c.want) {
				t.Fatalf("expected block %q, got:\n%s", c.want, out)
			}
		})
	}
}

// TestCodeAction_WriteInSandboxAllowed confirms the confinement is not
// over-broad: writing inside the scratch dir works.
func TestCodeAction_WriteInSandboxAllowed(t *testing.T) {
	requirePython(t)
	out := runCodeAction(t, &recordingInvoker{}, `
import os
p = os.path.join(os.environ["DENEB_SANDBOX_DIR"], "scratch.txt")
open(p, "w").write("hello")
print("WROTE", open(p).read())
`)
	if !strings.Contains(out, "WROTE hello") {
		t.Fatalf("sandbox-local write should succeed, got:\n%s", out)
	}
	if strings.Contains(out, "sandbox:") {
		t.Fatalf("unexpected sandbox block on a legal write:\n%s", out)
	}
}
