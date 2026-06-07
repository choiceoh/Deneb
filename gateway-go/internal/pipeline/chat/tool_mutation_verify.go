package chat

import (
	"context"
	"sort"
	"strings"
)

// tool_mutation_verify.go — post-action verification for mutation tools.
//
// Research basis: docs/research/claw-anything-always-on-assistant.md, finding A
// (the "investigation-execution gap" — the Claw-Anything benchmark's dominant
// failure mode: agents identify the right context but fail to translate it into a
// successful action, and report buried failures as done).
//
// Several mutation tools return failures *in-band* — as result text with a nil Go
// error (e.g. gmail send returns `"발송 실패: …", nil`). Because the error slot is
// nil, the agent loop marks the tool result isError=false, so a failure can be
// silently mistaken for success and reported to the user as completed. This file
// detects those buried failures and prepends a loud, standardized banner so the
// model treats them as failures (retry or tell the user) rather than success.
//
// The detector is intentionally a small, explicit per-tool phrase table (not a
// generic "실패" scan) so it never false-positives on outputs that merely mention
// failure counts or read-side errors. It is pure and unit-tested. Wiring is a
// per-tool PostProcessor registered in RegisterDefaultPostProcessors.
//
// Escalation to Error log + broadcast is handled in run_hooks.go. Remaining
// follow-up: converting each tool's in-band failure into a real error-slot
// result at the tool implementation boundary.

// mutationFailureBanner is prepended to a mutation tool's output when the result
// indicates the action did not succeed. The wording forbids reporting success and
// directs the model to retry or surface the failure to the user.
const mutationFailureBanner = "⚠️ 실행 실패 — 이 작업은 완료되지 않았습니다. 성공한 것처럼 보고하지 말고, 원인을 확인해 재시도하거나 사용자에게 실패를 명확히 알리세요."

// mutationFailureMarkers maps a mutation tool name to the in-band failure phrases
// it emits (verified against the tool implementations in tools/). When any marker
// is present in the output, the action failed despite the nil error slot.
var mutationFailureMarkers = map[string][]string{
	"gmail":   {"발송 실패", "답장 실패", "라벨 추가 실패", "라벨 제거 실패"},
	"wiki":    {"위키 페이지 쓰기 실패", "일지 쓰기 실패"},
	"cron":    {"실행 실패"},
	"gateway": {"재시작 신호 전송 실패", "설정 파일 저장 실패", "설정 저장 실패", "설정 경로 적용 실패", "update 실패"},
}

// mutationVerifyTools returns the tool names with mutation-failure verification,
// sorted for deterministic registration.
func mutationVerifyTools() []string {
	names := make([]string, 0, len(mutationFailureMarkers))
	for name := range mutationFailureMarkers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// mutationOutcomeIsFailure reports whether output carries a known in-band failure
// marker for toolName. Pure and table-driven for unit testing.
func mutationOutcomeIsFailure(toolName, output string) bool {
	for _, m := range mutationFailureMarkers[toolName] {
		if strings.Contains(output, m) {
			return true
		}
	}
	return false
}

// isMutationFailureResult reports whether a finalized tool result carries the
// mutation failure banner (i.e. MutationFailureAnnotator surfaced an in-band
// failure the agent loop saw as isError=false). Used by the run hooks to escalate
// to an Error log + operator broadcast per .claude/rules/logging.md.
func isMutationFailureResult(result string) bool {
	return strings.Contains(result, mutationFailureBanner)
}

// mutationFailureError extracts the underlying tool failure text from an
// annotated mutation result for logs/broadcasts. The result body is still
// truncated by the notification relay before it reaches clients.
func mutationFailureError(result string) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(result, mutationFailureBanner))
	if trimmed == "" || trimmed == strings.TrimSpace(result) {
		return "mutation tool returned an in-band failure"
	}
	return trimmed
}

// MutationFailureAnnotator is a per-tool PostProcessor that prepends the mutation
// failure banner when a mutation tool's output indicates an in-band failure. It is
// idempotent (won't double-annotate) and a no-op for success output or tools
// without markers.
func MutationFailureAnnotator(_ context.Context, toolName, output string) string {
	if strings.Contains(output, mutationFailureBanner) {
		return output // already annotated
	}
	if mutationOutcomeIsFailure(toolName, output) {
		return mutationFailureBanner + "\n\n" + output
	}
	return output
}
