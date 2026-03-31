package telegram

import "strings"

// ReplyOutcome classifies the type of agent response.
// Used by the reply pipeline to select context-aware buttons and
// determine whether follow-up actions (error translation, auto-verify)
// are needed.
type ReplyOutcome string

const (
	OutcomeCodeChange ReplyOutcome = "code_change"
	OutcomeTestPass   ReplyOutcome = "test_pass"
	OutcomeTestFail   ReplyOutcome = "test_fail"
	OutcomeBuildFail  ReplyOutcome = "build_fail"
	OutcomeCommit     ReplyOutcome = "commit"
	OutcomeError      ReplyOutcome = "error"
	OutcomeGeneral    ReplyOutcome = "general"
)

// AnalyzeReply classifies an agent reply text into an outcome category.
// The classifier checks keywords in priority order: more specific outcomes
// (commit, test pass/fail, build fail) are matched before generic ones
// (error, code change). Returns OutcomeGeneral when no pattern matches.
func AnalyzeReply(text string) ReplyOutcome {
	lower := strings.ToLower(text)

	// Commit — explicit commit action or Korean equivalent.
	if containsAny(lower, "committed", "커밋 완료", "커밋했습니다", "git commit") {
		return OutcomeCommit
	}

	// Test results — check pass/fail before generic "test" match.
	hasTest := containsAny(lower, "test", "테스트")
	if hasTest {
		if containsAny(lower, "pass", "passed", "통과", "성공", "ok", "all passing") {
			return OutcomeTestPass
		}
		if containsAny(lower, "fail", "failed", "실패", "error") {
			return OutcomeTestFail
		}
	}

	// Build failure — "build" + failure indicator.
	hasBuild := containsAny(lower, "build", "빌드", "compile", "컴파일")
	if hasBuild && containsAny(lower, "fail", "failed", "error", "실패", "오류") {
		return OutcomeBuildFail
	}

	// Generic error — various error signals.
	if containsAny(lower, "error", "에러", "failed", "오류", "panic", "fatal") {
		return OutcomeError
	}

	// Code change — file write/edit/create/delete mentions.
	if containsAny(lower,
		"wrote", "written", "created", "modified", "edited", "updated",
		"파일 작성", "파일 수정", "파일 생성", "파일 삭제",
		"변경했습니다", "추가했습니다", "수정했습니다",
		"write_file", "edit_file",
	) {
		return OutcomeCodeChange
	}

	// Commit — looser match after more specific categories.
	if containsAny(lower, "commit", "커밋") {
		return OutcomeCommit
	}

	return OutcomeGeneral
}

// containsAny returns true if s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// errorTranslation maps English error patterns to non-technical Korean
// explanations. Patterns are matched case-insensitively via Contains.
// Order matters: more specific patterns should come first.
var errorTranslations = []struct {
	pattern     string
	translation string
}{
	{"permission denied", "권한이 없습니다. 파일이나 폴더에 접근할 수 없어요."},
	{"access denied", "접근이 거부되었습니다."},
	{"file not found", "파일을 찾을 수 없습니다."},
	{"no such file or directory", "파일이나 폴더가 존재하지 않습니다."},
	{"directory not found", "폴더를 찾을 수 없습니다."},
	{"connection refused", "서버에 연결할 수 없습니다. 서버가 꺼져 있을 수 있어요."},
	{"connection timed out", "서버 연결 시간이 초과되었습니다. 네트워크를 확인해 주세요."},
	{"connection reset", "서버 연결이 끊어졌습니다."},
	{"network unreachable", "네트워크에 연결할 수 없습니다."},
	{"dns resolution failed", "서버 주소를 찾을 수 없습니다."},
	{"out of memory", "메모리가 부족합니다. 다른 프로그램을 종료해 보세요."},
	{"oom", "메모리가 부족합니다."},
	{"disk full", "디스크 공간이 부족합니다."},
	{"no space left on device", "디스크 공간이 부족합니다."},
	{"syntax error", "코드 문법 오류가 있습니다."},
	{"parse error", "코드를 해석할 수 없습니다. 문법을 확인해 주세요."},
	{"unexpected token", "코드에 예상하지 못한 문법이 있습니다."},
	{"undefined variable", "정의되지 않은 변수를 사용했습니다."},
	{"undefined reference", "정의되지 않은 참조가 있습니다. 빌드 설정을 확인해 주세요."},
	{"type mismatch", "데이터 타입이 맞지 않습니다."},
	{"import cycle", "패키지 간 순환 참조가 발생했습니다."},
	{"cannot find module", "필요한 모듈을 찾을 수 없습니다."},
	{"module not found", "필요한 모듈을 찾을 수 없습니다."},
	{"compilation failed", "빌드에 실패했습니다."},
	{"build failed", "빌드에 실패했습니다."},
	{"test failed", "테스트가 실패했습니다."},
	{"assertion failed", "검증 조건이 맞지 않습니다."},
	{"timeout", "작업 시간이 초과되었습니다."},
	{"deadlock", "프로그램이 교착 상태에 빠졌습니다."},
	{"segmentation fault", "프로그램이 비정상 종료되었습니다 (메모리 접근 오류)."},
	{"stack overflow", "프로그램 호출이 너무 깊어서 오류가 발생했습니다."},
	{"panic", "프로그램에서 예상치 못한 심각한 오류가 발생했습니다."},
	{"fatal error", "치명적인 오류가 발생했습니다."},
	{"authentication failed", "인증에 실패했습니다. 로그인 정보를 확인해 주세요."},
	{"unauthorized", "인증이 필요합니다."},
	{"forbidden", "접근 권한이 없습니다."},
	{"rate limit", "요청이 너무 많습니다. 잠시 후 다시 시도해 주세요."},
	{"merge conflict", "코드 병합 충돌이 발생했습니다. 수동 확인이 필요합니다."},
	{"already exists", "이미 존재합니다."},
	{"not found", "찾을 수 없습니다."},
	{"failed", "작업이 실패했습니다."},
}

// TranslateErrorKorean converts common English error messages to
// non-technical Korean explanations for vibe coders. If no known
// pattern matches, returns an empty string (caller should use the
// original text or a generic fallback).
func TranslateErrorKorean(errText string) string {
	lower := strings.ToLower(errText)
	for _, t := range errorTranslations {
		if strings.Contains(lower, t.pattern) {
			return t.translation
		}
	}
	return ""
}
