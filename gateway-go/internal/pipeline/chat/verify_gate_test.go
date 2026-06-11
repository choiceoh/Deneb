package chat

import (
	"encoding/json"
	"testing"
)

func execInput(cmd string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

// The core ordering contract: write→finish demands verification once;
// write→verify→finish passes; verify-before-write stays armed.
func TestVerifyGate_Ordering(t *testing.T) {
	g := &verifyGateState{}

	// No mutation → inert.
	if p := g.finalizePrompt(); p != "" {
		t.Fatalf("inert gate fired: %q", p)
	}

	// write → armed.
	g.recordTool("write", json.RawMessage(`{"path":"a.go"}`), "ok", nil)
	if p := g.finalizePrompt(); p == "" {
		t.Fatal("mutated run must be gated")
	}
	// Injection cap: second finish passes even without verification.
	if p := g.finalizePrompt(); p != "" {
		t.Fatalf("gate must self-limit, got %q", p)
	}

	// Fresh run: write → verify → finish passes silently.
	g2 := &verifyGateState{}
	g2.recordTool("edit", json.RawMessage(`{}`), "ok", nil)
	g2.recordTool("exec", execInput("cd gateway-go && go build ./..."), "ok", nil)
	if p := g2.finalizePrompt(); p != "" {
		t.Fatalf("verified run must pass, got %q", p)
	}

	// Verify BEFORE mutation does not count.
	g3 := &verifyGateState{}
	g3.recordTool("exec", execInput("go test ./..."), "ok", nil)
	g3.recordTool("write", json.RawMessage(`{}`), "ok", nil)
	if p := g3.finalizePrompt(); p == "" {
		t.Fatal("verify-then-mutate must stay armed")
	}
}

func TestVerifyGate_Signals(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		input    json.RawMessage
		output   string
		err      error
		verifies bool // disarms an armed gate
	}{
		{"make check", "exec", execInput("make check"), "All checks passed", nil, true},
		{"gradle compile", "exec", execInput("./gradlew :composeApp:compileKotlinDesktop"), "BUILD SUCCESSFUL", nil, true},
		{"pytest", "exec", execInput("pytest -q"), "3 passed", nil, true},
		{"non-verify exec", "exec", execInput("ls -la"), "files", nil, false},
		{"failed verify (annotated exit)", "exec", execInput("go test ./..."), "FAIL\nexit code: 1", nil, false},
		{"read tool ignored", "read", json.RawMessage(`{}`), "content", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &verifyGateState{}
			g.recordTool("write", json.RawMessage(`{}`), "ok", nil)
			g.recordTool(tc.tool, tc.input, tc.output, tc.err)
			gated := g.finalizePrompt() != ""
			if gated == tc.verifies {
				t.Fatalf("verifies=%v but gated=%v", tc.verifies, gated)
			}
		})
	}
}

// Failed tools never change gate state — an errored write doesn't arm, an
// errored verify doesn't disarm. Nil receiver is a no-op.
func TestVerifyGate_ErrorsAndNil(t *testing.T) {
	g := &verifyGateState{}
	g.recordTool("write", json.RawMessage(`{}`), "", errTest)
	if p := g.finalizePrompt(); p != "" {
		t.Fatal("errored write must not arm the gate")
	}

	var nilGate *verifyGateState
	nilGate.recordTool("write", json.RawMessage(`{}`), "ok", nil) // must not panic
	if p := nilGate.finalizePrompt(); p != "" {
		t.Fatal("nil gate must be inert")
	}
}

var errTest = json.Unmarshal([]byte("x"), &struct{}{}) // any non-nil error
