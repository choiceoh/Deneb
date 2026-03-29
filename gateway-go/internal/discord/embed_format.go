package discord

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Embed limits.
const (
	embedDescriptionLimit = 4096
	embedFieldValueLimit  = 1024
	embedTitleLimit       = 256
	embedTotalCharLimit   = 6000
)

// FormatToolResultEmbed builds a color-coded embed for a tool execution result.
// Green for success, red for errors.
func FormatToolResultEmbed(toolName, result string, isError bool, durationMs int64) Embed {
	color := ColorSuccess
	title := "✅ " + toolName
	if isError {
		color = ColorError
		title = "❌ " + toolName
	}

	desc := truncate(result, embedDescriptionLimit)

	e := Embed{
		Title:       truncate(title, embedTitleLimit),
		Description: wrapCodeBlockIfNeeded(desc),
		Color:       color,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	if durationMs > 0 {
		e.Footer = &EmbedFooter{Text: fmt.Sprintf("%dms", durationMs)}
	}
	return e
}

// FormatGitDiffEmbed builds a blue embed from git diff --stat output.
// Parses lines like " file.go | 5 ++--" and the summary line.
func FormatGitDiffEmbed(diffStats string) Embed {
	diffStats = strings.TrimSpace(diffStats)
	if diffStats == "" {
		return Embed{
			Title:       "📊 Git Diff",
			Description: "변경 사항 없음",
			Color:       ColorInfo,
		}
	}

	lines := strings.Split(diffStats, "\n")
	var fields []EmbedField
	var summaryLine string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The last line is typically the summary (e.g., "3 files changed, 10 insertions(+), 2 deletions(-)")
		if strings.Contains(line, "file") && strings.Contains(line, "changed") {
			summaryLine = line
			continue
		}
		// File entries: " file.go | 5 ++--"
		if strings.Contains(line, "|") {
			fields = append(fields, EmbedField{
				Name:   "📄",
				Value:  "`" + line + "`",
				Inline: false,
			})
		}
	}

	// Cap fields to avoid embed limits.
	if len(fields) > 15 {
		remaining := len(fields) - 14
		fields = fields[:14]
		fields = append(fields, EmbedField{
			Name:  "...",
			Value: fmt.Sprintf("외 %d개 파일", remaining),
		})
	}

	desc := ""
	if summaryLine != "" {
		desc = "```\n" + summaryLine + "\n```"
	}

	return Embed{
		Title:       "📊 Git Diff",
		Description: desc,
		Color:       ColorInfo,
		Fields:      fields,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// parseDiffSummary extracts file count, insertions, deletions from a diff --stat
// summary line like "3 files changed, 10 insertions(+), 2 deletions(-)".
func parseDiffSummary(summary string) (files, insertions, deletions int) {
	reFiles := regexp.MustCompile(`(\d+) files? changed`)
	reIns := regexp.MustCompile(`(\d+) insertions?\(\+\)`)
	reDel := regexp.MustCompile(`(\d+) deletions?\(-\)`)

	if m := reFiles.FindStringSubmatch(summary); len(m) > 1 {
		files, _ = strconv.Atoi(m[1])
	}
	if m := reIns.FindStringSubmatch(summary); len(m) > 1 {
		insertions, _ = strconv.Atoi(m[1])
	}
	if m := reDel.FindStringSubmatch(summary); len(m) > 1 {
		deletions, _ = strconv.Atoi(m[1])
	}
	return
}

// FormatTestResultsEmbed builds a green/red embed for test results.
func FormatTestResultsEmbed(passed, failed, total int, output string) Embed {
	color := ColorSuccess
	title := "✅ 테스트 통과"
	if failed > 0 {
		color = ColorError
		title = "❌ 테스트 실패"
	}

	fields := []EmbedField{
		{Name: "통과", Value: fmt.Sprintf("`%d`", passed), Inline: true},
		{Name: "실패", Value: fmt.Sprintf("`%d`", failed), Inline: true},
		{Name: "전체", Value: fmt.Sprintf("`%d`", total), Inline: true},
	}

	desc := ""
	if output != "" {
		desc = truncate(output, embedDescriptionLimit-200)
		desc = wrapCodeBlockIfNeeded(desc)
	}

	return Embed{
		Title:       title,
		Description: desc,
		Color:       color,
		Fields:      fields,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// FormatErrorEmbed builds a red embed for an error with optional file location.
func FormatErrorEmbed(errMsg, filePath string, lineNum int) Embed {
	desc := "```\n" + truncate(errMsg, embedDescriptionLimit-100) + "\n```"

	var fields []EmbedField
	if filePath != "" {
		loc := filePath
		if lineNum > 0 {
			loc = fmt.Sprintf("%s:%d", filePath, lineNum)
		}
		fields = append(fields, EmbedField{
			Name:  "위치",
			Value: "`" + loc + "`",
		})
	}

	return Embed{
		Title:       "❌ 오류",
		Description: desc,
		Color:       ColorError,
		Fields:      fields,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// FormatBuildEmbed builds an embed for build results.
func FormatBuildEmbed(output string, success bool) Embed {
	color := ColorSuccess
	title := "🔨 빌드 성공"
	if !success {
		color = ColorError
		title = "🔨 빌드 실패"
	}
	return Embed{
		Title:       title,
		Description: wrapCodeBlockIfNeeded(truncate(output, embedDescriptionLimit)),
		Color:       color,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// FormatGitLogEmbed builds a blue embed for git log output.
func FormatGitLogEmbed(logOutput string) Embed {
	return Embed{
		Title:       "📜 Git Log",
		Description: wrapCodeBlockIfNeeded(truncate(logOutput, embedDescriptionLimit)),
		Color:       ColorInfo,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// FormatBranchEmbed builds a blue embed for branch listing.
func FormatBranchEmbed(branchOutput string) Embed {
	return Embed{
		Title:       "🌿 Branches",
		Description: wrapCodeBlockIfNeeded(truncate(branchOutput, embedDescriptionLimit)),
		Color:       ColorInfo,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// FormatStatusEmbed builds a combined workspace status embed.
func FormatStatusEmbed(branch, status, diffStats, recentLog string) Embed {
	var fields []EmbedField

	if branch != "" {
		fields = append(fields, EmbedField{
			Name: "브랜치", Value: "`" + branch + "`", Inline: true,
		})
	}

	if status != "" {
		fields = append(fields, EmbedField{
			Name: "상태", Value: wrapCodeBlockIfNeeded(truncate(status, embedFieldValueLimit)),
		})
	}

	if diffStats != "" {
		fields = append(fields, EmbedField{
			Name: "변경 통계", Value: wrapCodeBlockIfNeeded(truncate(diffStats, embedFieldValueLimit)),
		})
	}

	if recentLog != "" {
		fields = append(fields, EmbedField{
			Name: "최근 커밋", Value: wrapCodeBlockIfNeeded(truncate(recentLog, embedFieldValueLimit)),
		})
	}

	return Embed{
		Title:     "📋 워크스페이스 상태",
		Color:     ColorInfo,
		Fields:    fields,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// FormatProgressEmbed builds an orange embed showing agent execution progress.
func FormatProgressEmbed(steps []ProgressStep) Embed {
	var lines []string
	for _, s := range steps {
		emoji := "⬜"
		switch s.Status {
		case StepRunning:
			emoji = "🔄"
		case StepDone:
			emoji = "✅"
		case StepError:
			emoji = "❌"
		}
		lines = append(lines, emoji+" "+s.Name)
	}

	color := ColorProgress
	title := "⏳ 실행 중..."
	allDone := true
	hasError := false
	for _, s := range steps {
		if s.Status != StepDone && s.Status != StepError {
			allDone = false
		}
		if s.Status == StepError {
			hasError = true
		}
	}
	if allDone {
		if hasError {
			color = ColorError
			title = "❌ 완료 (오류 발생)"
		} else {
			color = ColorSuccess
			title = "✅ 완료"
		}
	}

	return Embed{
		Title:       title,
		Description: strings.Join(lines, "\n"),
		Color:       color,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// ProgressStep represents a single step in an agent execution.
type ProgressStep struct {
	Name   string
	Status StepStatus
}

// StepStatus is the state of a progress step.
type StepStatus int

const (
	StepPending StepStatus = iota
	StepRunning
	StepDone
	StepError
)

// FormatMultiFileDiffEmbed builds an embed with per-file diff summaries.
// Parses a full git diff output and groups changes by file.
func FormatMultiFileDiffEmbed(fullDiff string) []Embed {
	if fullDiff == "" {
		return []Embed{{
			Title:       "📊 Diff",
			Description: "변경 사항 없음",
			Color:       ColorInfo,
		}}
	}

	// Split by "diff --git" to get per-file diffs.
	parts := strings.Split(fullDiff, "diff --git ")
	if len(parts) <= 1 {
		// Not a multi-file diff; return single embed.
		return []Embed{{
			Title:       "📊 Diff",
			Description: wrapCodeBlockIfNeeded(truncate(fullDiff, embedDescriptionLimit)),
			Color:       ColorInfo,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}}
	}

	var embeds []Embed
	for _, part := range parts[1:] { // skip the first empty part
		lines := strings.SplitN(part, "\n", 2)
		if len(lines) == 0 {
			continue
		}

		// Extract filename from "a/path b/path" header.
		header := lines[0]
		fileName := extractDiffFileName(header)

		diffContent := ""
		if len(lines) > 1 {
			diffContent = lines[1]
		}

		// Count additions/deletions.
		added, removed := countDiffLines(diffContent)

		desc := "```diff\n" + truncate(diffContent, 900) + "\n```"
		fields := []EmbedField{
			{Name: "추가", Value: fmt.Sprintf("`+%d`", added), Inline: true},
			{Name: "삭제", Value: fmt.Sprintf("`-%d`", removed), Inline: true},
		}

		embeds = append(embeds, Embed{
			Title:       "📄 " + fileName,
			Description: desc,
			Color:       ColorInfo,
			Fields:      fields,
		})

		// Cap at 5 embeds to stay within Discord's 10-embed limit.
		if len(embeds) >= 5 {
			remaining := len(parts) - 1 - 5
			if remaining > 0 {
				embeds = append(embeds, Embed{
					Title:       fmt.Sprintf("... 외 %d개 파일", remaining),
					Description: "전체 diff는 `/gdiff`로 확인하세요.",
					Color:       ColorInfo,
				})
			}
			break
		}
	}

	return embeds
}

// extractDiffFileName extracts the file name from a diff header like "a/path/file.go b/path/file.go".
func extractDiffFileName(header string) string {
	parts := strings.Fields(header)
	if len(parts) >= 2 {
		name := parts[1]
		if strings.HasPrefix(name, "b/") {
			return name[2:]
		}
		return name
	}
	return header
}

// countDiffLines counts added (+) and removed (-) lines in a diff chunk.
func countDiffLines(diff string) (added, removed int) {
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removed++
		}
	}
	return
}

// FormatAgentSummaryEmbed builds a summary embed shown after an agent run completes.
// Includes tool execution stats and a brief result preview.
func FormatAgentSummaryEmbed(toolsUsed []string, totalDurationMs int64, replyPreview string) Embed {
	// Deduplicate and count tool uses.
	toolCounts := make(map[string]int)
	for _, t := range toolsUsed {
		toolCounts[t]++
	}
	var toolLines []string
	for name, count := range toolCounts {
		if count > 1 {
			toolLines = append(toolLines, fmt.Sprintf("`%s` ×%d", name, count))
		} else {
			toolLines = append(toolLines, "`"+name+"`")
		}
	}

	fields := []EmbedField{
		{Name: "사용 도구", Value: strings.Join(toolLines, ", "), Inline: false},
	}

	if totalDurationMs > 0 {
		dur := time.Duration(totalDurationMs) * time.Millisecond
		fields = append(fields, EmbedField{
			Name: "소요 시간", Value: fmt.Sprintf("`%s`", dur.Round(time.Millisecond)), Inline: true,
		})
	}

	desc := ""
	if replyPreview != "" {
		desc = truncate(replyPreview, 300)
	}

	return Embed{
		Title:       "✅ 에이전트 완료",
		Description: desc,
		Color:       ColorSuccess,
		Fields:      fields,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// FormatHelpEmbed returns an embed listing all available Discord coding commands.
func FormatHelpEmbed() Embed {
	return FormatVibeCoderHelpEmbed()
}

// FormatVibeCoderHelpEmbed returns a help embed optimized for vibe coders.
// Only shows commands that a non-developer needs: project status, commit, push.
// Emphasizes that the user can just describe what they want in natural Korean.
func FormatVibeCoderHelpEmbed() Embed {
	fields := []EmbedField{
		{
			Name:   "💬 기본 사용법",
			Value:  "원하는 것을 **한국어로 자유롭게** 설명하면 에이전트가 알아서 코딩합니다.\n예: \"로그인 기능 추가해줘\", \"빌드 에러 고쳐줘\", \"테스트 돌려봐\"",
			Inline: false,
		},
		{
			Name:   "📊 프로젝트 현황",
			Value:  "`/dashboard` — 빌드·테스트·브랜치 상태 한눈에 보기",
			Inline: false,
		},
		{
			Name:   "💾 저장 & 배포",
			Value:  "`/commit [메시지]` — 변경 사항 저장\n`/push` — 원격 저장소에 업로드",
			Inline: false,
		},
		{
			Name:   "🤖 세션 관리",
			Value:  "`/new` — 새 작업 시작\n`/model [이름]` — AI 모델 변경",
			Inline: false,
		},
	}
	return Embed{
		Title:       "📖 디스코드 코딩 채널 도움말",
		Description: "코드를 직접 볼 필요 없이, 원하는 것을 말로 설명하세요!",
		Color:       ColorInfo,
		Fields:      fields,
	}
}

// --- helpers ---

// TruncateText truncates text to maxLen, appending "..." if truncated. Exported for use by other packages.
func TruncateText(s string, maxLen int) string {
	return truncate(s, maxLen)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// wrapCodeBlockIfNeeded wraps text in a code block if it isn't already wrapped.
func wrapCodeBlockIfNeeded(text string) string {
	if text == "" {
		return text
	}
	if strings.HasPrefix(text, "```") {
		return text
	}
	return "```\n" + text + "\n```"
}
