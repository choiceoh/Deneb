package shadow

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CodeReviewer performs background code quality checks when code changes
// are detected in conversation.
type CodeReviewer struct {
	svc *Service

	// State (guarded by svc.mu).
	pendingReviews []CodeReviewResult
	lastReviewAt   int64
}

// CodeReviewResult captures the outcome of a background code check.
type CodeReviewResult struct {
	Trigger    string   `json:"trigger"`   // what triggered the review
	CheckedAt  int64    `json:"checkedAt"` // unix ms
	VetPassed  bool     `json:"vetPassed"`
	VetOutput  string   `json:"vetOutput,omitempty"`
	FmtClean   bool     `json:"fmtClean"`
	FmtFiles   []string `json:"fmtFiles,omitempty"` // unformatted files
	TestPassed *bool    `json:"testPassed,omitempty"`
	TestOutput string   `json:"testOutput,omitempty"`
	Summary    string   `json:"summary"` // Korean summary
}

func newCodeReviewer(svc *Service) *CodeReviewer {
	return &CodeReviewer{svc: svc}
}

// codeChangePatterns detect code changes in conversation.
var codeChangePatterns = []string{
	"파일 수정", "파일 작성", "코드 변경", "edit", "write",
	"수정했", "작성했", "변경했", "추가했", "삭제했",
	"refactor", "리팩토링", "구현했", "implemented",
}

// DetectCodeChange checks if a message indicates a code change.
func DetectCodeChange(content string) bool {
	lower := strings.ToLower(content)
	for _, p := range codeChangePatterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// OnCodeChangeDetected triggers background code quality checks.
func (cr *CodeReviewer) OnCodeChangeDetected(trigger string) {
	// Rate limit: at most once per 2 minutes.
	cr.svc.mu.Lock()
	if time.Now().UnixMilli()-cr.lastReviewAt < 2*60*1000 {
		cr.svc.mu.Unlock()
		return
	}
	cr.lastReviewAt = time.Now().UnixMilli()
	cr.svc.mu.Unlock()

	go cr.runReview(trigger)
}

// runReview performs vet + fmt checks in the background.
func (cr *CodeReviewer) runReview(trigger string) {
	ctx := cr.svc.svcCtx
	dir := resolveGatewayDir()

	result := CodeReviewResult{
		Trigger:   trigger,
		CheckedAt: time.Now().UnixMilli(),
	}

	// go vet
	vetCmd := exec.CommandContext(ctx, "go", "vet", "./...")
	vetCmd.Dir = dir
	vetOutput, vetErr := vetCmd.CombinedOutput()
	result.VetPassed = vetErr == nil
	if !result.VetPassed {
		result.VetOutput = truncate(string(vetOutput), 500)
	}

	// gofmt check
	fmtCmd := exec.CommandContext(ctx, "gofmt", "-l", ".")
	fmtCmd.Dir = dir
	fmtOutput, _ := fmtCmd.Output()
	fmtFiles := strings.TrimSpace(string(fmtOutput))
	result.FmtClean = fmtFiles == ""
	if !result.FmtClean {
		files := strings.Split(fmtFiles, "\n")
		if len(files) > 10 {
			files = files[:10]
		}
		result.FmtFiles = files
	}

	// Build Korean summary.
	var parts []string
	if result.VetPassed {
		parts = append(parts, "go vet 통과")
	} else {
		parts = append(parts, "go vet 실패")
	}
	if result.FmtClean {
		parts = append(parts, "포맷 정상")
	} else {
		parts = append(parts, fmt.Sprintf("포맷 필요 %d파일", len(result.FmtFiles)))
	}
	result.Summary = strings.Join(parts, " | ")

	cr.svc.mu.Lock()
	if len(cr.pendingReviews) >= 20 {
		cr.pendingReviews = cr.pendingReviews[1:]
	}
	cr.pendingReviews = append(cr.pendingReviews, result)
	cr.svc.mu.Unlock()

	cr.svc.emit(ShadowEvent{Type: "code_review", Payload: result})

	// Notify on failures.
	if !result.VetPassed || !result.FmtClean {
		cr.notifyReviewIssue(result)
	}

	cr.svc.cfg.Logger.Info("shadow: code review completed",
		"vet", result.VetPassed,
		"fmt", result.FmtClean,
	)
}

func (cr *CodeReviewer) notifyReviewIssue(result CodeReviewResult) {
	cr.svc.mu.Lock()
	notifier := cr.svc.cfg.Notifier
	cr.svc.mu.Unlock()
	if notifier == nil {
		return
	}

	msg := fmt.Sprintf("🔍 백그라운드 코드 리뷰\n%s", result.Summary)
	if result.VetOutput != "" {
		msg += fmt.Sprintf("\n\nvet 출력:\n%s", truncate(result.VetOutput, 300))
	}

	go func() {
		ctx, cancel := cr.svc.notifyCtx()
		defer cancel()
		if err := notifier.Notify(ctx, msg); err != nil {
			cr.svc.cfg.Logger.Warn("shadow: code review notification failed", "error", err)
		}
	}()
}

// GetRecentReviews returns recent code review results.
func (cr *CodeReviewer) GetRecentReviews() []CodeReviewResult {
	cr.svc.mu.Lock()
	defer cr.svc.mu.Unlock()
	result := make([]CodeReviewResult, len(cr.pendingReviews))
	copy(result, cr.pendingReviews)
	return result
}
