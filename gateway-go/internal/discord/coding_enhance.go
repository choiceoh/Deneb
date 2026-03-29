// Package discord — coding enhancement embeds and buttons for vibe coders.
//
// Adds:
//   - Diff preview embeds with approve/reject buttons
//   - Enhanced workspace dashboard with build/test/lint status
//   - Error recovery buttons with auto-debug subagent spawn
//   - Smart test result embeds (changed files only)
//   - Git workflow buttons (branch create, PR, conflict resolution)
//   - Pilot auto-delegation indicators
//   - Subagent progress embeds
package discord

import (
	"fmt"
	"strings"
	"time"
)

// --- 1. Diff Preview & Undo/Rollback ---

// FormatDiffPreviewEmbed builds an embed showing a diff preview before applying changes.
// Allows the vibe coder to approve or reject changes before they are written.
func FormatDiffPreviewEmbed(fileName string, added, removed int, diffSnippet string) Embed {
	desc := ""
	if diffSnippet != "" {
		// Cap diff snippet for readability.
		snippet := truncate(diffSnippet, 800)
		desc = "```diff\n" + snippet + "\n```"
	}

	fields := []EmbedField{
		{Name: "📄 파일", Value: "`" + fileName + "`", Inline: true},
		{Name: "추가", Value: fmt.Sprintf("`+%d줄`", added), Inline: true},
		{Name: "삭제", Value: fmt.Sprintf("`-%d줄`", removed), Inline: true},
	}

	return Embed{
		Title:       "🔍 변경 미리보기",
		Description: desc,
		Color:       ColorInfo,
		Fields:      fields,
		Footer:      &EmbedFooter{Text: "아래 버튼으로 변경을 승인하거나 거부하세요"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// FormatMultiFileDiffPreviewEmbed builds an embed showing a summary of changes
// across multiple files for preview before applying.
func FormatMultiFileDiffPreviewEmbed(fileChanges []FileChange, totalAdded, totalRemoved int) Embed {
	var fileLines []string
	for i, fc := range fileChanges {
		if i >= 10 {
			fileLines = append(fileLines, fmt.Sprintf("... 외 %d개 파일", len(fileChanges)-10))
			break
		}
		fileLines = append(fileLines, fmt.Sprintf("`%s` (+%d/-%d)", fc.Name, fc.Added, fc.Removed))
	}

	fields := []EmbedField{
		{Name: "📄 변경 파일", Value: strings.Join(fileLines, "\n"), Inline: false},
		{Name: "추가", Value: fmt.Sprintf("`+%d줄`", totalAdded), Inline: true},
		{Name: "삭제", Value: fmt.Sprintf("`-%d줄`", totalRemoved), Inline: true},
		{Name: "파일 수", Value: fmt.Sprintf("`%d개`", len(fileChanges)), Inline: true},
	}

	return Embed{
		Title:     "🔍 변경 미리보기",
		Color:     ColorInfo,
		Fields:    fields,
		Footer:    &EmbedFooter{Text: "아래 버튼으로 변경을 승인하거나 거부하세요"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// FileChange describes a single file's diff statistics.
type FileChange struct {
	Name    string
	Added   int
	Removed int
}

// DiffPreviewButtons returns approve/reject buttons for a diff preview.
func DiffPreviewButtons(sessionKey string) []Component {
	return []Component{
		ActionRow(
			Button("✅ 적용", fmt.Sprintf("diffapply:%s", sessionKey), ButtonSuccess),
			Button("❌ 거부", fmt.Sprintf("diffreject:%s", sessionKey), ButtonDanger),
			Button("📋 전체 보기", fmt.Sprintf("difffull:%s", sessionKey), ButtonSecondary),
		),
	}
}

// FormatUndoEmbed builds an embed confirming a successful undo/rollback.
func FormatUndoEmbed(filesReverted int, commitHash string) Embed {
	desc := fmt.Sprintf("%d개 파일을 이전 상태로 되돌렸습니다.", filesReverted)
	if commitHash != "" {
		desc += fmt.Sprintf("\n되돌린 커밋: `%s`", truncate(commitHash, 8))
	}
	return Embed{
		Title:       "↩️ 되돌리기 완료",
		Description: desc,
		Color:       ColorSuccess,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// AfterUndoButtons returns follow-up buttons after an undo operation.
func AfterUndoButtons(sessionKey string) []Component {
	return []Component{
		ActionRow(
			Button("📊 현황 보기", fmt.Sprintf("dashboard:%s", sessionKey), ButtonSecondary),
			Button("🆕 새 작업", fmt.Sprintf("new:%s", sessionKey), ButtonPrimary),
		),
	}
}

// --- 2. Enhanced Workspace Dashboard ---

// DashboardData holds all information for the enhanced dashboard.
type DashboardData struct {
	Branch       string
	ChangedFiles int
	FilesSummary string // short summary of changed files
	BuildStatus  string // "✅ 성공", "❌ 실패", "⏭️ 미확인"
	TestStatus   string
	LintStatus   string
	RecentLog    string
	Upstream     string // tracking branch info
	StashCount   int
}

// FormatEnhancedDashboardEmbed builds a comprehensive project dashboard embed.
func FormatEnhancedDashboardEmbed(d DashboardData) Embed {
	fields := []EmbedField{
		{Name: "🌿 브랜치", Value: "`" + d.Branch + "`", Inline: true},
	}
	if d.Upstream != "" {
		fields = append(fields, EmbedField{
			Name: "🔗 업스트림", Value: "`" + d.Upstream + "`", Inline: true,
		})
	}

	// Changed files with detail.
	changedVal := "변경 없음 (클린 ✨)"
	if d.ChangedFiles > 0 {
		changedVal = fmt.Sprintf("**%d개** 파일 변경됨", d.ChangedFiles)
		if d.FilesSummary != "" {
			changedVal += "\n" + d.FilesSummary
		}
	}
	fields = append(fields, EmbedField{
		Name: "📝 변경사항", Value: changedVal, Inline: false,
	})

	// Build/test/lint status row.
	fields = append(fields,
		EmbedField{Name: "🔨 빌드", Value: d.BuildStatus, Inline: true},
		EmbedField{Name: "🧪 테스트", Value: d.TestStatus, Inline: true},
		EmbedField{Name: "🔍 린트", Value: d.LintStatus, Inline: true},
	)

	// Stash count if any.
	if d.StashCount > 0 {
		fields = append(fields, EmbedField{
			Name: "📦 스태시", Value: fmt.Sprintf("%d개 저장됨", d.StashCount), Inline: true,
		})
	}

	// Recent commits.
	if d.RecentLog != "" {
		fields = append(fields, EmbedField{
			Name: "📜 최근 커밋", Value: d.RecentLog, Inline: false,
		})
	}

	// Determine color based on status.
	color := ColorSuccess
	if strings.Contains(d.BuildStatus, "❌") || strings.Contains(d.TestStatus, "❌") {
		color = ColorError
	} else if strings.Contains(d.LintStatus, "⚠️") {
		color = ColorWarning
	} else if d.ChangedFiles > 0 {
		color = ColorInfo
	}

	return Embed{
		Title:     "📊 프로젝트 현황",
		Color:     color,
		Fields:    fields,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// DashboardButtons returns quick-action buttons for the dashboard.
func DashboardButtons(sessionKey string) []Component {
	return []Component{
		ActionRow(
			Button("🧪 테스트", fmt.Sprintf("test:%s", sessionKey), ButtonPrimary),
			Button("💾 커밋", fmt.Sprintf("commit:%s", sessionKey), ButtonSuccess),
			Button("🚀 푸시", fmt.Sprintf("push:%s", sessionKey), ButtonSecondary),
			Button("🔄 새로고침", fmt.Sprintf("dashboard:%s", sessionKey), ButtonSecondary),
		),
	}
}

// --- 3. Error Recovery Flow ---

// FormatErrorRecoveryEmbed builds an embed for automated error recovery suggestions.
func FormatErrorRecoveryEmbed(errorSummary, suggestedAction string) Embed {
	desc := errorSummary
	if suggestedAction != "" {
		desc += "\n\n💡 **추천 조치:** " + suggestedAction
	}
	return Embed{
		Title:       "🔧 자동 복구 제안",
		Description: desc,
		Color:       ColorWarning,
		Footer:      &EmbedFooter{Text: "에이전트가 자동으로 문제를 분석했습니다"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// ErrorRecoveryButtons returns buttons for error recovery actions.
func ErrorRecoveryButtons(sessionKey string) []Component {
	return []Component{
		ActionRow(
			Button("🤖 자동 수정", fmt.Sprintf("autofix:%s", sessionKey), ButtonPrimary),
			Button("🔀 다른 방법", fmt.Sprintf("altfix:%s", sessionKey), ButtonSecondary),
			Button("↩️ 되돌리기", fmt.Sprintf("revert:%s", sessionKey), ButtonDanger),
		),
	}
}

// --- 4. Subagent Progress ---

// FormatSubagentProgressEmbed builds an embed showing subagent task progress.
func FormatSubagentProgressEmbed(tasks []SubagentTask) Embed {
	var lines []string
	for _, t := range tasks {
		emoji := "⏳"
		switch t.Status {
		case "running":
			emoji = "🔄"
		case "completed":
			emoji = "✅"
		case "failed":
			emoji = "❌"
		}
		line := fmt.Sprintf("%s **%s** — %s", emoji, t.Name, t.Description)
		if t.Duration != "" {
			line += fmt.Sprintf(" (%s)", t.Duration)
		}
		lines = append(lines, line)
	}

	allDone := true
	hasError := false
	for _, t := range tasks {
		if t.Status != "completed" && t.Status != "failed" {
			allDone = false
		}
		if t.Status == "failed" {
			hasError = true
		}
	}

	color := ColorProgress
	title := "🤖 서브에이전트 실행 중..."
	if allDone {
		if hasError {
			color = ColorError
			title = "🤖 서브에이전트 완료 (일부 실패)"
		} else {
			color = ColorSuccess
			title = "🤖 서브에이전트 완료"
		}
	}

	return Embed{
		Title:       title,
		Description: strings.Join(lines, "\n"),
		Color:       color,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// SubagentTask represents a subagent task for progress display.
type SubagentTask struct {
	Name        string
	Description string
	Status      string // "pending", "running", "completed", "failed"
	Duration    string // e.g., "3.2s"
}

// --- 5. Smart Test Execution ---

// FormatSmartTestEmbed builds an embed for targeted test results (changed files only).
func FormatSmartTestEmbed(changedPkgs []string, passed, failed, skipped int, output string) Embed {
	color := ColorSuccess
	title := "🎯 스마트 테스트 통과"
	if failed > 0 {
		color = ColorError
		title = "🎯 스마트 테스트 실패"
	}

	fields := []EmbedField{
		{Name: "통과", Value: fmt.Sprintf("`%d`", passed), Inline: true},
		{Name: "실패", Value: fmt.Sprintf("`%d`", failed), Inline: true},
		{Name: "건너뜀", Value: fmt.Sprintf("`%d`", skipped), Inline: true},
	}

	if len(changedPkgs) > 0 {
		pkgList := make([]string, 0, len(changedPkgs))
		for i, pkg := range changedPkgs {
			if i >= 5 {
				pkgList = append(pkgList, fmt.Sprintf("... 외 %d개", len(changedPkgs)-5))
				break
			}
			pkgList = append(pkgList, "`"+pkg+"`")
		}
		fields = append(fields, EmbedField{
			Name: "🎯 테스트 대상", Value: strings.Join(pkgList, "\n"), Inline: false,
		})
	}

	desc := ""
	if output != "" && failed > 0 {
		desc = wrapCodeBlockIfNeeded(truncate(output, embedDescriptionLimit-200))
	}

	return Embed{
		Title:       title,
		Description: desc,
		Color:       color,
		Fields:      fields,
		Footer:      &EmbedFooter{Text: "변경된 파일 관련 테스트만 실행"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// SmartTestButtons returns context-aware buttons after smart test results.
func SmartTestButtons(sessionKey string, hasFailed bool) []Component {
	if hasFailed {
		return []Component{
			ActionRow(
				Button("🤖 자동 수정", fmt.Sprintf("autofix:%s", sessionKey), ButtonPrimary),
				Button("🧪 전체 테스트", fmt.Sprintf("testall:%s", sessionKey), ButtonSecondary),
				Button("📋 상세보기", fmt.Sprintf("details:%s", sessionKey), ButtonSecondary),
			),
		}
	}
	return []Component{
		ActionRow(
			Button("🧪 전체 테스트", fmt.Sprintf("testall:%s", sessionKey), ButtonSecondary),
			Button("💾 커밋", fmt.Sprintf("commit:%s", sessionKey), ButtonSuccess),
			Button("🚀 푸시", fmt.Sprintf("push:%s", sessionKey), ButtonPrimary),
		),
	}
}

// --- 6. Git Workflow Buttons ---

// GitWorkflowButtons returns buttons for common git workflow actions.
func GitWorkflowButtons(sessionKey string) []Component {
	return []Component{
		ActionRow(
			Button("🌿 브랜치 생성", fmt.Sprintf("branchcreate:%s", sessionKey), ButtonPrimary),
			Button("🔀 PR 생성", fmt.Sprintf("prcreate:%s", sessionKey), ButtonSuccess),
			Button("🔍 충돌 검사", fmt.Sprintf("mergecheck:%s", sessionKey), ButtonSecondary),
		),
	}
}

// FormatBranchCreateEmbed builds an embed confirming branch creation.
func FormatBranchCreateEmbed(branchName, baseBranch string) Embed {
	return Embed{
		Title: "🌿 브랜치 생성 완료",
		Color: ColorSuccess,
		Fields: []EmbedField{
			{Name: "새 브랜치", Value: "`" + branchName + "`", Inline: true},
			{Name: "기준 브랜치", Value: "`" + baseBranch + "`", Inline: true},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// FormatPRCreateEmbed builds an embed confirming PR creation.
func FormatPRCreateEmbed(prNumber int, prTitle, prURL, baseBranch, headBranch string) Embed {
	fields := []EmbedField{
		{Name: "제목", Value: prTitle, Inline: false},
		{Name: "브랜치", Value: fmt.Sprintf("`%s` ← `%s`", baseBranch, headBranch), Inline: false},
	}
	if prURL != "" {
		fields = append(fields, EmbedField{
			Name: "링크", Value: prURL, Inline: false,
		})
	}
	return Embed{
		Title:     fmt.Sprintf("🔀 PR #%d 생성 완료", prNumber),
		Color:     ColorSuccess,
		Fields:    fields,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// AfterBranchCreateButtons returns follow-up buttons after branch creation.
func AfterBranchCreateButtons(sessionKey string) []Component {
	return []Component{
		ActionRow(
			Button("🆕 새 작업", fmt.Sprintf("new:%s", sessionKey), ButtonPrimary),
			Button("📊 현황 보기", fmt.Sprintf("dashboard:%s", sessionKey), ButtonSecondary),
		),
	}
}

// --- 7. Pilot Auto-delegation Indicator ---

// FormatPilotDelegationEmbed builds an embed indicating a task was delegated to Pilot.
func FormatPilotDelegationEmbed(task string, sources []string) Embed {
	desc := "⚡ 로컬 AI로 빠르게 처리합니다..."
	if task != "" {
		desc = "**작업:** " + truncate(task, 200) + "\n\n" + desc
	}
	var fields []EmbedField
	if len(sources) > 0 {
		srcList := make([]string, 0, len(sources))
		for i, s := range sources {
			if i >= 5 {
				srcList = append(srcList, fmt.Sprintf("... 외 %d개", len(sources)-5))
				break
			}
			srcList = append(srcList, "• "+s)
		}
		fields = append(fields, EmbedField{
			Name: "📡 데이터 수집", Value: strings.Join(srcList, "\n"), Inline: false,
		})
	}
	return Embed{
		Title:       "🧠 Pilot 분석 중",
		Description: desc,
		Color:       ColorProgress,
		Fields:      fields,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// FormatPilotResultEmbed builds an embed showing Pilot analysis results.
func FormatPilotResultEmbed(summary string, durationMs int64) Embed {
	fields := []EmbedField{}
	if durationMs > 0 {
		dur := time.Duration(durationMs) * time.Millisecond
		fields = append(fields, EmbedField{
			Name: "⏱️ 소요 시간", Value: fmt.Sprintf("`%s`", dur.Round(time.Millisecond)), Inline: true,
		})
	}
	return Embed{
		Title:       "🧠 Pilot 분석 완료",
		Description: truncate(summary, embedDescriptionLimit),
		Color:       ColorSuccess,
		Fields:      fields,
		Footer:      &EmbedFooter{Text: "로컬 LLM으로 처리 — API 비용 없음"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}
