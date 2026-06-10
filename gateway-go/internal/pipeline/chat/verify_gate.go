// verify_gate.go — Verification gate: a run that mutated files must verify
// before it is allowed to finish (Plan→Build→Verify→Fix; see
// docs/research/ideal-agent-environment-harness.md §10).
//
// Mechanism: ToolRegistry.Execute records successful write/edit calls as
// "mutated" and successful verification commands (go build/test, make check,
// gradle compile, …) as "verified". When the model tries to finish while
// mutated-and-unverified, the executor's FinalizeGate injects a user-role
// prompt demanding verification and keeps the loop alive — once, after which
// the next finish passes (the prompt carries an escape hatch for changes
// where verification is meaningless, e.g. pure docs).
//
// Default ON; DENEB_VERIFY_GATE=0 disables. The gate is inert for runs that
// never write/edit, so chat/analysis turns are unaffected.
package chat

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"sync"
)

// verifyGateState tracks mutation/verification ordering for one agent run.
// Thread-safe: tools may run from the executor goroutine while the gate is
// consulted at finish time.
type verifyGateState struct {
	mu          sync.Mutex
	mutated     bool   // a write/edit succeeded with no verification since
	mutatedTool string // last mutating tool name (log/prompt context)
	injections  int    // finish-blocking prompts injected so far (cap 1)
}

type verifyGateCtxKey struct{}

// WithVerifyGate attaches the run's verification-gate state to ctx so
// ToolRegistry.Execute can record mutations and verifications.
func WithVerifyGate(ctx context.Context, g *verifyGateState) context.Context {
	if g == nil {
		return ctx
	}
	return context.WithValue(ctx, verifyGateCtxKey{}, g)
}

// verifyGateFromContext returns the run's gate state, or nil (no-op receiver).
func verifyGateFromContext(ctx context.Context) *verifyGateState {
	g, _ := ctx.Value(verifyGateCtxKey{}).(*verifyGateState)
	return g
}

// verifyCmdRe matches shell commands that count as verification: builds,
// tests, vets, type checks. Matched against the exec tool's command string.
var verifyCmdRe = regexp.MustCompile(`(?i)\bgo\s+(build|test|vet)\b|\bmake\s+\S*(check|test|build)\b|\bmake\s+go\b|gradlew?\S*\s+\S*(compile|test|build|assemble|render)|\bnpm\s+test\b|\bnpm\s+run\s+\S*build\b|\bpnpm\s+(test|build)\b|\bpytest\b|\bcargo\s+(build|test|check)\b|\btsc\b|\bgofmt\b`)

// execFailureRe spots a non-zero exit annotation in exec output — the exec
// tool returns err=nil for some annotated non-zero exits, which must not
// count as a passing verification.
var execFailureRe = regexp.MustCompile(`exit (code|status):? [1-9]`)

// recordTool updates gate state from one tool execution. Nil-receiver safe.
// Successful write/edit arms the gate; a successful verification command
// (exec) disarms it. Ordering matters: verify-then-mutate stays armed.
func (g *verifyGateState) recordTool(name string, input json.RawMessage, output string, err error) {
	if g == nil || err != nil {
		return
	}
	switch name {
	case "write", "edit":
		g.mu.Lock()
		g.mutated = true
		g.mutatedTool = name
		g.mu.Unlock()
	case "exec":
		var p struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(input, &p) != nil || !verifyCmdRe.MatchString(p.Command) {
			return
		}
		if execFailureRe.MatchString(output) {
			return // verification ran but failed — stay armed
		}
		g.mu.Lock()
		g.mutated = false
		g.mu.Unlock()
	}
}

// finalizePrompt returns the gate prompt when the run is finishing while
// mutated-and-unverified (at most once per run), else "".
func (g *verifyGateState) finalizePrompt() string {
	if g == nil {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.mutated || g.injections >= 1 {
		return ""
	}
	g.injections++
	return verifyGatePrompt
}

const verifyGatePrompt = `[검증 게이트] 이번 작업에서 파일을 수정했지만(write/edit) 그 후 빌드·테스트·문법 검사가 실행되지 않았습니다. 마치기 전에:
1. 변경한 파일에 맞는 검증 명령(예: go build/test, make check, gradlew compile)을 exec로 실행하세요.
2. 실패하면 고치고 재검증하세요.
3. 최종 보고에 검증 결과를 한 줄로 포함하세요.
검증이 무의미한 변경(순수 문서·메모 등)이면 그 이유를 한 줄로 밝히고 그대로 마무리하세요.`

// verifyGateEnabled reports whether the finish gate is active.
// Default ON; DENEB_VERIFY_GATE=0 (or "false") turns it off.
func verifyGateEnabled() bool {
	switch os.Getenv("DENEB_VERIFY_GATE") {
	case "0", "false", "off":
		return false
	}
	return true
}
