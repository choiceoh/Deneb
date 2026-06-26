// verify_gate.go — Verification gate: a run that mutated files must verify
// before it is allowed to finish (Plan→Build→Verify→Fix; see
// docs/research/ideal-agent-environment-harness.md §10).
//
// Mechanism: ToolRegistry.Execute records successful write/edit calls as
// "mutated" and successful verification commands (go build/test, make check,
// gradle compile, …) as "verified". When the model tries to finish while
// mutated-and-unverified, the executor's FinalizeGate injects a user-role
// prompt demanding verification and keeps the loop alive. The gate now has
// TEETH (harness §10 follow-up): it injects up to TWICE with escalating
// firmness, and on the final allowed injection it HARD-BLOCKS the finish —
// it keeps refusing end_turn until either a verification command is observed
// OR the model emits a parseable opt-out line ("검증 불필요:" /
// "verification not applicable:") acknowledging the change needs no build.
// A run that finishes while still armed (no verify, no opt-out) is a silent
// escape; that path is logged distinctly so it is observable, not invisible.
//
// Default ON; DENEB_VERIFY_GATE=0 disables. The gate is inert for runs that
// never write/edit, so chat/analysis turns are unaffected.
package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"regexp"
	"sync"
)

// verifyGateMaxInjections caps how many finish-blocking prompts the gate may
// inject per run. The first is a reminder; the second (final) escalates to a
// hard block that refuses end_turn until verification or an explicit opt-out
// is observed. Raised from 1 (nag-once) to 2 to give the model a firm second
// chance before the gate stops yielding.
const verifyGateMaxInjections = 2

// verifyGateState tracks mutation/verification ordering for one agent run.
// Thread-safe: tools may run from the executor goroutine while the gate is
// consulted at finish time.
type verifyGateState struct {
	mu          sync.Mutex
	mutated     bool   // a write/edit succeeded with no verification since
	mutatedTool string // last mutating tool name (log/prompt context)
	injections  int    // finish-blocking prompts injected so far (cap verifyGateMaxInjections)
	optedOut    bool   // model emitted a parseable "verification not applicable" line
	escaped     bool   // a finish was allowed while still armed (silent escape) — logged once
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

// awaitingVerify reports the reasoning sandwich's back-half trigger: the gate
// has ALREADY injected at least one demand (the model tried to finish and was
// held) and is still armed (no verify, no opt-out). True only on the
// verify/finish turn(s) the gate is actively blocking — NOT on ordinary
// mid-run edit turns where a mutation is merely pending — so re-boosting
// reasoning here targets exactly the turn a fix/verify plan must form without
// inflating the middle of the sandwich. Nil-receiver safe.
func (g *verifyGateState) awaitingVerify() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.mutated && !g.optedOut && g.injections >= 1
}

// verifyCmdRe matches shell commands that count as verification: builds,
// tests, vets, type checks. Matched against the exec tool's command string.
var verifyCmdRe = regexp.MustCompile(`(?i)\bgo\s+(build|test|vet)\b|\bmake\s+\S*(check|test|build)\b|\bmake\s+go\b|gradlew?\S*\s+\S*(compile|test|build|assemble|render)|\bnpm\s+test\b|\bnpm\s+run\s+\S*build\b|\bpnpm\s+(test|build)\b|\bpytest\b|\bcargo\s+(build|test|check)\b|\btsc\b|\bgofmt\b`)

// execFailureRe spots a non-zero exit annotation in exec output — the exec
// tool returns err=nil for some annotated non-zero exits, which must not
// count as a passing verification.
var execFailureRe = regexp.MustCompile(`exit (code|status):? [1-9]`)

// verifyOptOutRe matches the explicit opt-out line the gate's hard block tells
// the model to emit when a change genuinely needs no build/test (pure docs,
// config notes). Anchored to a line start so it is a deliberate signal, not an
// incidental mention inside prose. Both the Korean and English forms are
// recognized; a colon (full- or half-width) must follow so a reason is given.
var verifyOptOutRe = regexp.MustCompile(`(?im)^\s*(검증\s*불필요|verification\s+not\s+applicable)\s*[:：]`)

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

// observeFinishText inspects an assistant turn's text as the model heads for
// finish and records an explicit verification opt-out ("검증 불필요:" /
// "verification not applicable:"). The opt-out is the escape hatch for the
// hard block: once seen, the gate disarms and lets the run finish. Fed from
// the chat-side persister wrapper at the moment the executor consults the gate
// (run_agent_config.go), so it is recognized on the SAME finishing turn — the
// model is never nagged after giving a valid reason. Nil-receiver safe; a
// no-text turn is a no-op.
func (g *verifyGateState) observeFinishText(text string) {
	if g == nil || text == "" {
		return
	}
	if !verifyOptOutRe.MatchString(text) {
		return
	}
	g.mu.Lock()
	g.optedOut = true
	g.mu.Unlock()
}

// finalizePrompt returns the gate prompt when the run is finishing while
// mutated-and-unverified, else "". It now has teeth:
//
//   - An explicit opt-out (observeFinishText saw "검증 불필요:" / "verification
//     not applicable:") disarms the gate — finish is allowed.
//   - Injections 1..verifyGateMaxInjections escalate in firmness. The FIRST is
//     a reminder; the LAST is a hard block.
//   - On the LAST allowed injection the gate keeps refusing finish: once the
//     injection budget is spent, a still-armed finish re-returns the hard-block
//     prompt instead of yielding — the loop only ends when a verify command
//     disarms the gate (recordTool) or the model emits the opt-out line.
//
// The one place a still-armed run is permitted to finish is the loop's own
// safety net (max turns / grace), which bypasses the gate entirely; that
// silent escape is surfaced by logFinishedWhileArmed, not here.
//
// logger may be nil (the chat closure passes the run logger); a nil logger
// just skips the escalation breadcrumb.
func (g *verifyGateState) finalizePrompt(logger *slog.Logger) string {
	if g == nil {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.mutated || g.optedOut {
		return ""
	}
	// Budget spent and still armed → hard block: re-issue the final demand
	// rather than yield. The model must verify or opt out to proceed.
	if g.injections >= verifyGateMaxInjections {
		if logger != nil {
			logger.Warn("verify gate: hard-blocking finish (injection budget spent, still unverified)",
				"mutatedTool", g.mutatedTool, "injections", g.injections)
		}
		return verifyGateHardBlockPrompt
	}
	g.injections++
	if g.injections >= verifyGateMaxInjections {
		return verifyGateHardBlockPrompt
	}
	return verifyGateReminderPrompt
}

// logFinishedWhileArmed records the rare silent-escape case: the run terminated
// (e.g. max-turns / budget grace bypasses the finish gate) while still
// mutated-and-unverified with no opt-out. Distinct from the per-injection log
// so a postmortem can grep "finished while armed" to count escapes. Idempotent:
// logs at most once per run. Nil-receiver / nil-logger safe; a disarmed or
// opted-out run is a no-op (nothing escaped).
func (g *verifyGateState) logFinishedWhileArmed(logger *slog.Logger) {
	if g == nil || logger == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.mutated || g.optedOut || g.escaped {
		return
	}
	g.escaped = true
	logger.Warn("verify gate: finished while still armed (unverified mutation escaped the gate)",
		"mutatedTool", g.mutatedTool, "injections", g.injections)
}

// verifyGateReminderPrompt is the FIRST, softer demand: a one-time nudge to
// verify before finishing, with the opt-out hatch for changes that need none.
const verifyGateReminderPrompt = `[검증 게이트] 이번 작업에서 파일을 수정했지만(write/edit) 그 후 빌드·테스트·문법 검사가 실행되지 않았습니다. 마치기 전에:
1. 변경한 파일에 맞는 검증 명령(예: go build/test, make check, gradlew compile)을 exec로 실행하세요.
2. 실패하면 고치고 재검증하세요.
3. 최종 보고에 검증 결과를 한 줄로 포함하세요.
검증이 무의미한 변경(순수 문서·메모 등)이면 "검증 불필요: <이유>" 한 줄로 밝히고 그대로 마무리하세요.`

// verifyGateHardBlockPrompt is the FINAL, firmer demand. From here the gate
// will NOT let the run finish until a verification command runs or the opt-out
// line is emitted — so the two exits are spelled out unambiguously.
const verifyGateHardBlockPrompt = `[검증 게이트 — 필수] 아직 검증되지 않은 파일 수정이 남아 있어 이대로 마칠 수 없습니다. 다음 중 하나를 반드시 하세요:
1. 변경에 맞는 검증 명령(go build/test, make check, gradlew compile 등)을 exec로 실행하고, 실패하면 고친 뒤 재검증하세요. 그리고 최종 보고에 검증 결과를 한 줄로 포함하세요.
2. 검증이 정말 무의미한 변경(순수 문서·메모 등)이라면 정확히 "검증 불필요: <이유>" 형식의 한 줄로 그 이유를 밝히세요. 이 줄이 있어야만 검증 없이 마칠 수 있습니다.
둘 중 하나를 하기 전에는 종료 요청이 받아들여지지 않습니다.`

// verifyGateEnabled reports whether the finish gate is active.
// Default ON; DENEB_VERIFY_GATE=0 (or "false") turns it off.
func verifyGateEnabled() bool {
	switch os.Getenv("DENEB_VERIFY_GATE") {
	case "0", "false", "off":
		return false
	}
	return true
}
