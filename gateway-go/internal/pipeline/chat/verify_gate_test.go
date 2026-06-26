package chat

import (
	"encoding/json"
	"log/slog"
	"testing"
)

func execInput(cmd string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

// The core ordering contract: write→finish demands verification (escalating,
// not just once); write→verify→finish passes; verify-before-write stays armed.
func TestVerifyGate_Ordering(t *testing.T) {
	g := &verifyGateState{}

	// No mutation → inert.
	if p := g.finalizePrompt(nil); p != "" {
		t.Fatalf("inert gate fired: %q", p)
	}

	// write → armed. First finish → softer reminder.
	g.recordTool("write", json.RawMessage(`{"path":"a.go"}`), "ok", nil)
	if p := g.finalizePrompt(nil); p != verifyGateReminderPrompt {
		t.Fatalf("first finish must demand verification (reminder), got %q", p)
	}
	// TEETH: the gate no longer yields after one nag. Second finish escalates to
	// the hard block, and every further still-armed finish keeps hard-blocking.
	if p := g.finalizePrompt(nil); p != verifyGateHardBlockPrompt {
		t.Fatalf("second finish must escalate to hard block, got %q", p)
	}
	if p := g.finalizePrompt(nil); p != verifyGateHardBlockPrompt {
		t.Fatalf("still-armed finish past the budget must keep hard-blocking, got %q", p)
	}

	// Fresh run: write → verify → finish passes silently.
	g2 := &verifyGateState{}
	g2.recordTool("edit", json.RawMessage(`{}`), "ok", nil)
	g2.recordTool("exec", execInput("cd gateway-go && go build ./..."), "ok", nil)
	if p := g2.finalizePrompt(nil); p != "" {
		t.Fatalf("verified run must pass, got %q", p)
	}

	// Verify BEFORE mutation does not count.
	g3 := &verifyGateState{}
	g3.recordTool("exec", execInput("go test ./..."), "ok", nil)
	g3.recordTool("write", json.RawMessage(`{}`), "ok", nil)
	if p := g3.finalizePrompt(nil); p == "" {
		t.Fatal("verify-then-mutate must stay armed")
	}
}

// TEETH: an explicit opt-out line disarms the gate so a docs-only change can
// finish without a build — observed from the finishing turn's text.
func TestVerifyGate_OptOutEscape(t *testing.T) {
	g := &verifyGateState{}
	g.recordTool("write", json.RawMessage(`{"path":"README.md"}`), "ok", nil)
	if p := g.finalizePrompt(nil); p == "" {
		t.Fatal("mutated run must be gated before the opt-out")
	}
	// Model emits the opt-out as its finishing prose.
	g.observeFinishText("문서만 고쳤습니다.\n검증 불필요: 순수 문서 변경이라 빌드 영향 없음")
	if p := g.finalizePrompt(nil); p != "" {
		t.Fatalf("opt-out must disarm the gate, got %q", p)
	}
	// awaitingVerify must also clear once opted out (sandwich back-half off).
	if g.awaitingVerify() {
		t.Fatal("opted-out gate must not request the back-half boost")
	}

	// English form, line-anchored, is recognized too.
	g2 := &verifyGateState{}
	g2.recordTool("edit", json.RawMessage(`{}`), "ok", nil)
	g2.observeFinishText("verification not applicable: config-only note")
	if p := g2.finalizePrompt(nil); p != "" {
		t.Fatalf("english opt-out must disarm, got %q", p)
	}

	// An incidental mid-prose mention (no line anchor) must NOT disarm.
	g3 := &verifyGateState{}
	g3.recordTool("write", json.RawMessage(`{}`), "ok", nil)
	g3.observeFinishText("나중에 검증 불필요 여부를 따져보겠습니다")
	if p := g3.finalizePrompt(nil); p == "" {
		t.Fatal("non-anchored mention must not count as an opt-out")
	}
}

// awaitingVerify (reasoning-sandwich back-half trigger) is true only once the
// gate has injected a demand and is still armed — not on a fresh mutation.
func TestVerifyGate_AwaitingVerify(t *testing.T) {
	g := &verifyGateState{}
	if g.awaitingVerify() {
		t.Fatal("inert gate must not await verify")
	}
	g.recordTool("write", json.RawMessage(`{}`), "ok", nil)
	if g.awaitingVerify() {
		t.Fatal("a pending mutation pre-injection must not trigger the back-half boost")
	}
	g.finalizePrompt(nil) // first injection
	if !g.awaitingVerify() {
		t.Fatal("after the gate demands verification, the back-half boost must arm")
	}
	g.recordTool("exec", execInput("go build ./..."), "ok", nil) // verify disarms
	if g.awaitingVerify() {
		t.Fatal("a verified gate must not await verify")
	}
}

// logFinishedWhileArmed fires exactly once and only for a still-armed run.
func TestVerifyGate_FinishedWhileArmedLogsOnce(t *testing.T) {
	disc := slog.New(slog.DiscardHandler)

	// Disarmed run: no escape, escaped stays false (idempotency tracked there).
	g := &verifyGateState{}
	g.recordTool("write", json.RawMessage(`{}`), "ok", nil)
	g.recordTool("exec", execInput("make check"), "All checks passed", nil)
	g.logFinishedWhileArmed(disc)
	if g.escaped {
		t.Fatal("a verified run did not escape; escaped must stay false")
	}

	// Still-armed run: escape recorded once.
	g2 := &verifyGateState{}
	g2.recordTool("write", json.RawMessage(`{}`), "ok", nil)
	g2.logFinishedWhileArmed(disc)
	if !g2.escaped {
		t.Fatal("a still-armed run that finished must record the escape")
	}
	g2.logFinishedWhileArmed(disc) // idempotent — must not re-flip or panic
	if !g2.escaped {
		t.Fatal("escaped must remain set")
	}

	// Nil receiver / nil logger are no-ops.
	var nilGate *verifyGateState
	nilGate.logFinishedWhileArmed(disc)
	g2.logFinishedWhileArmed(nil)
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
			gated := g.finalizePrompt(nil) != ""
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
	if p := g.finalizePrompt(nil); p != "" {
		t.Fatal("errored write must not arm the gate")
	}

	var nilGate *verifyGateState
	nilGate.recordTool("write", json.RawMessage(`{}`), "ok", nil) // must not panic
	if p := nilGate.finalizePrompt(nil); p != "" {
		t.Fatal("nil gate must be inert")
	}
}

var errTest = json.Unmarshal([]byte("x"), &struct{}{}) // any non-nil error
