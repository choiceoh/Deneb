// executor_tool_hint.go — harness-as-tutor: turn a raw first-time tool error
// into an actionable recovery hint appended to the error result.
//
// Why: the DeepWeb deep-research benchmark (Peking University, 2026) found that
// a dominant agent failure mode is *trajectory degeneration* — a tool returns a
// raw, opaque error (a bash "syntax error near unexpected token", a "command
// not found"), the model fails to parse the cause, and re-emits a near-identical
// call. Deneb's ToolLoopDetector (tool_loop.go) catches the *repeat*, but only
// after it crosses a threshold — i.e. after several wasted steps. This is the
// upstream complement: on the FIRST error, append a short directive that names
// the likely cause and nudges the model toward a DIFFERENT next action, so the
// loop never forms.
//
// Design:
//   - APPEND, never replace. The raw error stays verbatim for grounding (the
//     model must see what actually happened); the hint is an interpretation
//     layer, mirroring Deneb's provenance philosophy (raw + annotation).
//   - Deterministic: same raw error → same hint → identical result bytes, so
//     ToolLoopDetector's no-progress hashing and the local vLLM APC stay
//     byte-stable across turns.
//   - Substring match on the lowercased raw error. Patterns key off technical
//     *system* error strings (English — from the shell, Go stdlib, net stack)
//     that pass through raw. Deneb's own tools already return Korean guidance,
//     which never matches these, so there is no double-hinting.
//   - First match wins; at most one hint, bounded length. Every hint ends with
//     an explicit "don't re-send the same call" nudge — the whole point.
package agent

import "strings"

// toolErrorPattern maps a lowercased raw-error substring to a recovery hint.
type toolErrorPattern struct {
	substr string
	hint   string
}

// toolErrorHints is ordered most-specific-first; the generic non-zero-exit
// catch-all is last so a precise cause (syntax, missing file) is preferred when
// a raw error happens to contain several markers.
var toolErrorHints = []toolErrorPattern{
	{"syntax error", "힌트: 셸 문법 오류다. 괄호·따옴표·특수문자를 이스케이프했는지 확인하라(예: \\( \\) 또는 작은따옴표로 감싸기). 같은 명령을 그대로 다시 보내지 마라."},
	{"command not found", "힌트: 명령을 PATH에서 못 찾았다. 절대경로로 호출하거나 설치 여부를 먼저 확인하고, 안 되면 다른 도구를 고려하라. 같은 호출을 그대로 반복하지 마라."},
	{"no such file or directory", "힌트: 경로가 없다. 오타이거나 기준 디렉토리가 다를 수 있으니, 상위 디렉토리를 먼저 나열해 실제 경로를 확인한 뒤 재시도하라."},
	{"is a directory", "힌트: 디렉토리를 파일처럼 다뤘다. 파일을 읽으려면 구체적 파일 경로를, 목록을 보려면 list 계열 동작을 써서 다시 시도하라."},
	{"permission denied", "힌트: 권한이 없다(sudo 불가). 접근 가능한 다른 경로를 쓰거나 이 작업이 정말 필요한지 재검토하라. 같은 호출을 반복하지 마라."},
	{"no such host", "힌트: 호스트 이름을 못 풀었다. 엔드포인트 철자를 확인하라. 같은 호출을 즉시 반복하지 마라."},
	{"connection refused", "힌트: 대상 서비스에 연결이 거부됐다(꺼져 있을 수 있음). 같은 호출을 즉시 반복하지 말고, 다른 경로를 쓰거나 서비스 상태를 먼저 확인하라."},
	{"context deadline exceeded", "힌트: 시간 초과다. 요청을 더 좁은 범위로 쪼개라. 같은 호출을 즉시 반복하지 마라."},
	{"timeout", "힌트: 시간 초과다. 요청 범위를 줄이거나 접근을 바꿔라. 같은 호출을 즉시 반복하지 마라."},
	{"unexpected end of json", "힌트: 인자(arguments) JSON이 중간에 끊겼다. 따옴표·중괄호·이스케이프를 점검해 유효한 JSON으로 다시 호출하라."},
	{"cannot unmarshal", "힌트: 인자 타입이 스키마와 안 맞다. 각 필드의 타입(문자열/숫자/배열)을 도구 스키마에 맞춰 다시 호출하라."},
	{"invalid character", "힌트: 인자 JSON에 잘못된 문자가 있다. 유효한 JSON으로 교정해 다시 호출하라."},
	{"not registered", "힌트: 그런 도구는 등록돼 있지 않다. 사용 가능한 도구 목록에서 정확한 이름을 골라라."},
	{"unknown tool", "힌트: 그런 도구는 없다. 사용 가능한 도구 목록에서 정확한 이름을 골라라."},
	{"exit status", "힌트: 명령이 0이 아닌 종료코드로 끝났다. 위 출력의 에러를 읽고 원인을 고친 뒤 재시도하라 — 같은 명령을 그대로 반복하지 마라."},
}

// toolErrorHint returns a one-line recovery hint for a raw tool error, or ""
// when no known pattern matches. Match is case-insensitive; the first pattern
// in toolErrorHints order wins.
func toolErrorHint(rawErr string) string {
	low := strings.ToLower(rawErr)
	for _, p := range toolErrorHints {
		if strings.Contains(low, p.substr) {
			return p.hint
		}
	}
	return ""
}
