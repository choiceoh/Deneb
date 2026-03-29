// Package discord — reply analysis for context-aware vibe-coder UX.
//
// Analyzes agent replies to determine:
//   - What kind of outcome occurred (code change, error, test result, commit, etc.)
//   - Which follow-up buttons to show
//   - Whether to auto-verify (run build/test) and attach result embeds
package discord

import (
	"strings"
	"time"
)

// ReplyOutcome classifies the type of agent reply for UI decisions.
type ReplyOutcome int

const (
	OutcomeGeneral       ReplyOutcome = iota // conversational reply, no special action
	OutcomeCodeChange                        // agent modified files
	OutcomeTestPass                          // tests ran and passed
	OutcomeTestFail                          // tests ran and failed
	OutcomeBuildFail                         // build failed
	OutcomeCommitDone                        // agent committed changes
	OutcomeMergeConflict                     // merge conflict detected
	OutcomeError                             // agent encountered an error
	OutcomeBranchCreate                      // agent created a new branch
	OutcomePRCreate                          // agent created a pull request
	OutcomeRevert                            // agent reverted changes
)

// AnalyzeReply classifies an agent reply text into an outcome for UI decisions.
func AnalyzeReply(text string) ReplyOutcome {
	lower := strings.ToLower(text)

	// Check most specific patterns first.
	if matchesAny(lower, mergeConflictIndicators) {
		return OutcomeMergeConflict
	}
	if matchesAny(lower, revertIndicators) {
		return OutcomeRevert
	}
	if matchesAny(lower, prCreateIndicators) {
		return OutcomePRCreate
	}
	if matchesAny(lower, branchCreateIndicators) {
		return OutcomeBranchCreate
	}
	if matchesAny(lower, commitIndicators) {
		return OutcomeCommitDone
	}
	if matchesAny(lower, testFailIndicators) {
		return OutcomeTestFail
	}
	if matchesAny(lower, testPassIndicators) {
		return OutcomeTestPass
	}
	if matchesAny(lower, buildFailIndicators) {
		return OutcomeBuildFail
	}
	if matchesAny(lower, errorIndicators) {
		return OutcomeError
	}
	if matchesAny(lower, codeChangeIndicators) {
		return OutcomeCodeChange
	}

	return OutcomeGeneral
}

// Indicator lists for reply classification.
var (
	codeChangeIndicators = []string{
		"```diff",
		"--- a/", "+++ b/",
		"파일을 수정했습니다", "파일을 생성했습니다", "파일을 삭제했습니다",
		"wrote to", "edited", "applied edit",
		"변경 요약", "변경했습니다", "수정했습니다", "추가했습니다",
		"created file", "modified file",
	}
	testPassIndicators = []string{
		"테스트 통과", "테스트 성공", "all tests passed", "tests passed",
		"🧪 테스트 결과", "test pass", "tests pass",
		"ok  \t", // Go test output
	}
	testFailIndicators = []string{
		"테스트 실패", "test fail", "tests fail", "test error",
		"fail\t", "--- fail", "failed",
		"assertion error", "assert", "expected",
	}
	buildFailIndicators = []string{
		"빌드 실패", "build fail", "compilation error", "compile error",
		"cannot find", "undefined:", "syntax error",
		"error[e", // Rust error codes like error[E0308]
	}
	commitIndicators = []string{
		"커밋 완료", "커밋했습니다", "committed", "git commit",
		"[main ", "[master ", // git commit output prefix
	}
	mergeConflictIndicators = []string{
		"merge conflict", "병합 충돌", "충돌이 발생",
		"conflict in", "conflicts:", "unmerged paths",
		"both modified", "both added",
		"<<<<<<< ", ">>>>>>> ", // conflict markers
		"fix conflicts and then commit",
		"automatic merge failed",
		"unmerged files",
	}
	errorIndicators = []string{
		"오류가 발생", "에러가 발생", "실패했습니다",
		"error:", "panic:", "fatal:",
	}
	branchCreateIndicators = []string{
		"브랜치를 생성", "브랜치 생성 완료", "새 브랜치",
		"created branch", "switched to new branch", "switched to a new branch",
		"git checkout -b", "git switch -c",
	}
	prCreateIndicators = []string{
		"pull request를 생성", "pr을 생성", "pr 생성 완료",
		"created pull request", "pull request #", "pr #",
		"successfully created", "github.com/",
	}
	revertIndicators = []string{
		"되돌렸습니다", "되돌리기 완료", "변경을 취소",
		"reverted", "git revert", "changes undone",
		"이전 상태로 복원",
	}
)

func matchesAny(text string, indicators []string) bool {
	for _, ind := range indicators {
		if strings.Contains(text, strings.ToLower(ind)) {
			return true
		}
	}
	return false
}

// ContextButtons returns the appropriate action buttons based on the reply outcome.
func ContextButtons(outcome ReplyOutcome, sessionKey string) []Component {
	switch outcome {
	case OutcomeCodeChange:
		return CodeActionButtons(sessionKey)
	case OutcomeTestPass:
		return AfterTestPassButtons(sessionKey)
	case OutcomeTestFail:
		return TestResultButtons(sessionKey)
	case OutcomeBuildFail:
		return BuildFailButtons(sessionKey)
	case OutcomeMergeConflict:
		return MergeConflictButtons(sessionKey)
	case OutcomeCommitDone:
		return AfterCommitButtons(sessionKey)
	case OutcomeError:
		return ErrorButtons(sessionKey)
	case OutcomeBranchCreate:
		return AfterBranchCreateButtons(sessionKey)
	case OutcomePRCreate:
		return AfterPRCreateButtons(sessionKey)
	case OutcomeRevert:
		return AfterUndoButtons(sessionKey)
	default:
		return nil
	}
}

// AfterTestPassButtons returns buttons shown when tests pass.
func AfterTestPassButtons(sessionKey string) []Component {
	return SmartTestButtons(sessionKey, false)
}

// BuildFailButtons returns buttons shown when build fails.
// Uses the enhanced error recovery flow.
func BuildFailButtons(sessionKey string) []Component {
	return ErrorRecoveryButtons(sessionKey)
}

// ErrorButtons returns buttons shown when an error occurs.
// Uses the enhanced error recovery flow with auto-fix and alternative approach options.
func ErrorButtons(sessionKey string) []Component {
	return ErrorRecoveryButtons(sessionKey)
}

// --- Error Korean translation for vibe coders ---

// TranslateErrorToKorean converts common build/test error messages into
// human-readable Korean summaries for non-developers.
func TranslateErrorToKorean(errorText string) string {
	lower := strings.ToLower(errorText)

	// Merge conflict errors.
	if strings.Contains(lower, "merge conflict") || strings.Contains(lower, "unmerged paths") ||
		strings.Contains(lower, "automatic merge failed") {
		return "병합 충돌이 발생했어요. 같은 파일을 서로 다르게 수정해서 자동 병합이 안 됐어요. \"충돌 해결\" 버튼을 눌러주세요."
	}

	// Go errors.
	if strings.Contains(lower, "undefined:") {
		return "정의되지 않은 이름을 사용하고 있어요. 오타이거나 빠진 코드가 있을 수 있어요."
	}
	if strings.Contains(lower, "cannot find package") || strings.Contains(lower, "no required module") {
		return "필요한 패키지를 찾을 수 없어요. 의존성을 설치해야 할 수 있어요."
	}
	if strings.Contains(lower, "type mismatch") || strings.Contains(lower, "cannot use") {
		return "데이터 타입이 맞지 않아요. 코드에서 다른 종류의 값을 잘못 쓰고 있어요."
	}
	if strings.Contains(lower, "syntax error") {
		return "문법 오류가 있어요. 코드 작성 규칙에 맞지 않는 부분이 있어요."
	}
	if strings.Contains(lower, "import cycle") {
		return "코드가 서로를 참조하는 순환 구조가 있어요. 구조를 정리해야 해요."
	}

	// Rust errors.
	if strings.Contains(lower, "error[e") {
		return "Rust 컴파일 오류가 발생했어요. 에이전트에게 수정을 요청하세요."
	}
	if strings.Contains(lower, "borrow checker") || strings.Contains(lower, "lifetime") {
		return "Rust 메모리 관리 규칙에 어긋나는 코드가 있어요."
	}

	// Test errors.
	if strings.Contains(lower, "assertion") || strings.Contains(lower, "expected") {
		return "테스트에서 예상한 결과와 실제 결과가 달라요."
	}
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out") {
		return "작업이 시간 내에 완료되지 않았어요. 처리가 너무 오래 걸리고 있어요."
	}
	if strings.Contains(lower, "permission denied") {
		return "파일이나 명령어에 대한 권한이 없어요."
	}
	if strings.Contains(lower, "connection refused") || strings.Contains(lower, "connection reset") {
		return "서버에 연결할 수 없어요. 서비스가 실행 중인지 확인이 필요해요."
	}
	if strings.Contains(lower, "out of memory") || strings.Contains(lower, "oom") {
		return "메모리가 부족해요. 프로그램이 너무 많은 메모리를 사용하고 있어요."
	}
	if strings.Contains(lower, "panic:") {
		return "프로그램이 예기치 않게 멈췄어요 (패닉). 에이전트에게 수정을 요청하세요."
	}

	return ""
}

// FormatErrorTranslationEmbed creates a Korean error explanation embed
// to accompany a raw error in agent replies.
func FormatErrorTranslationEmbed(errorText string) *Embed {
	translation := TranslateErrorToKorean(errorText)
	if translation == "" {
		return nil
	}
	return &Embed{
		Title:       "💡 오류 설명",
		Description: translation,
		Color:       ColorWarning,
		Footer:      &EmbedFooter{Text: "에이전트에게 \"수정해줘\"라고 말하면 자동으로 고칩니다"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}
