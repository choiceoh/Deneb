// Package server — auto-verification embeds for the Discord coding channel.
//
// Runs build and test commands after code changes and sends result embeds
// to give vibe coders immediate feedback. Includes smart test targeting
// that runs only changed Go packages instead of the full test suite.
package server

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/discord"
)

// sendAutoVerifyEmbed runs build and test commands in the workspace and sends
// a result embed to the Discord channel. This gives vibe coders immediate
// feedback on whether the agent's changes actually work.
//
// For Go projects with uncommitted changes, uses smart test targeting:
// only tests the packages that actually changed (via changedGoPackages).
func (s *Server) sendAutoVerifyEmbed(ctx context.Context, client *discord.Client, channelID string) {
	if s.discordPlug == nil {
		return
	}

	// Resolve workspace for this channel. For threads, prefer worktree.
	sessionKey := discordSessionKeyForChannel(s.discordPlug, channelID)
	workspaceDir := resolveDiscordWorkspace(s, sessionKey, channelID)
	if workspaceDir == "" {
		return
	}

	// Build always runs fully.
	buildResult, buildOk := runQuickVerify(workspaceDir, "build")

	// Smart test: for Go projects, test only changed packages.
	var (
		testResult  string
		testOk      bool
		smartPkgs   []string
		smartPassed int
		smartFailed int
		smartSkip   int
		usedSmart   bool
	)
	projType := detectProjectType(workspaceDir)
	if projType == "go" {
		smartPkgs = changedGoPackages(workspaceDir)
		if len(smartPkgs) > 0 {
			testResult, testOk = runSmartGoTest(workspaceDir, smartPkgs)
			smartPassed, smartFailed, smartSkip = parseGoTestCounts(testResult)
			usedSmart = true
		}
	}
	if !usedSmart {
		testResult, testOk = runQuickVerify(workspaceDir, "test")
	}

	// Send the appropriate embed.
	if usedSmart {
		s.sendSmartTestEmbed(ctx, client, channelID, sessionKey,
			buildResult, buildOk,
			smartPkgs, smartPassed, smartFailed, smartSkip, testResult, testOk)
	} else {
		s.sendGenericVerifyEmbed(ctx, client, channelID, sessionKey,
			buildResult, buildOk, testResult, testOk)
	}
}

// sendSmartTestEmbed sends the smart test result embed (changed packages only).
func (s *Server) sendSmartTestEmbed(
	ctx context.Context, client *discord.Client,
	channelID, sessionKey string,
	buildResult string, buildOk bool,
	pkgs []string, passed, failed, skipped int,
	testOutput string, testOk bool,
) {
	// Build status field.
	buildEmoji := "✅"
	if !buildOk {
		buildEmoji = "❌"
	}

	// Use the smart test embed from coding_enhance.go.
	testEmbed := discord.FormatSmartTestEmbed(pkgs, passed, failed, skipped, testOutput)

	// Prepend build status as first field.
	buildField := discord.EmbedField{
		Name: "🔨 빌드", Value: buildEmoji + " " + discord.TruncateText(buildResult, 150), Inline: true,
	}
	testEmbed.Fields = append([]discord.EmbedField{buildField}, testEmbed.Fields...)

	// Override color if build failed.
	if !buildOk {
		testEmbed.Color = discord.ColorError
		testEmbed.Title = "⚠️ 빌드 실패 — " + testEmbed.Title
	}

	// Add footer.
	testEmbed.Footer = &discord.EmbedFooter{Text: "코드 변경 감지 → 변경 패키지만 자동 테스트"}

	// Select buttons.
	var components []discord.Component
	if !buildOk || !testOk {
		components = discord.SmartTestButtons(sessionKey, true)
	} else {
		components = discord.SmartTestButtons(sessionKey, false)
	}

	client.SendMessage(ctx, channelID, &discord.SendMessageRequest{
		Embeds:          []discord.Embed{testEmbed},
		Components:      components,
		AllowedMentions: &discord.AllowedMentions{Parse: []string{}},
	})
}

// sendGenericVerifyEmbed sends the generic build+test verification embed
// (used for non-Go projects or when no changed packages are detected).
func (s *Server) sendGenericVerifyEmbed(
	ctx context.Context, client *discord.Client,
	channelID, sessionKey string,
	buildResult string, buildOk bool,
	testResult string, testOk bool,
) {
	var fields []discord.EmbedField

	buildEmoji := "✅"
	if !buildOk {
		buildEmoji = "❌"
	}
	fields = append(fields, discord.EmbedField{
		Name: "🔨 빌드", Value: buildEmoji + " " + discord.TruncateText(buildResult, 200), Inline: false,
	})

	testEmoji := "✅"
	if !testOk {
		testEmoji = "❌"
	}
	fields = append(fields, discord.EmbedField{
		Name: "🧪 테스트", Value: testEmoji + " " + discord.TruncateText(testResult, 200), Inline: false,
	})

	color := discord.ColorSuccess
	title := "✅ 자동 검증 통과"
	if !buildOk || !testOk {
		color = discord.ColorError
		title = "⚠️ 자동 검증 실패"
	}

	embed := discord.Embed{
		Title:     title,
		Color:     color,
		Fields:    fields,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Footer:    &discord.EmbedFooter{Text: "코드 변경 감지 → 자동 빌드/테스트 실행"},
	}

	var components []discord.Component
	if !buildOk || !testOk {
		components = discord.BuildFailButtons(sessionKey)
	}

	client.SendMessage(ctx, channelID, &discord.SendMessageRequest{
		Embeds:          []discord.Embed{embed},
		Components:      components,
		AllowedMentions: &discord.AllowedMentions{Parse: []string{}},
	})
}

// resolveDiscordWorkspace resolves the workspace directory for a Discord channel.
// For thread sessions, prefers the isolated worktree directory.
func resolveDiscordWorkspace(s *Server, sessionKey, channelID string) string {
	if strings.HasPrefix(sessionKey, "discord:thread:") {
		threadID := strings.TrimPrefix(sessionKey, "discord:thread:")
		if s.discordWorktrees != nil {
			if ws := s.discordWorktrees.Get(threadID); ws != nil {
				return ws.Dir
			}
		}
	}
	wsChannelID := channelID
	if bot := s.discordPlug.Bot(); bot != nil {
		if parentID := bot.ThreadParent(channelID); parentID != "" {
			wsChannelID = parentID
		}
	}
	return s.discordPlug.Config().WorkspaceForChannel(wsChannelID)
}

// runSmartGoTest runs `go test` on specific packages (changed files only)
// with a 30-second timeout. Returns (output, success).
func runSmartGoTest(workspaceDir string, pkgs []string) (string, bool) {
	args := append([]string{"test", "-count=1"}, pkgs...)
	output := runCmdWithTimeout(workspaceDir, 30*time.Second, "go", args...)
	if output == "" {
		return "성공", true
	}
	lower := strings.ToLower(output)
	isError := strings.Contains(lower, "fail") || strings.Contains(lower, "error") || strings.Contains(lower, "panic")
	if isError {
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) > 8 {
			lines = lines[len(lines)-8:]
		}
		return strings.Join(lines, "\n"), false
	}
	return "성공", true
}

// parseGoTestCounts parses Go test output to extract pass/fail/skip counts.
// Counts package-level results (lines like "ok  \tpkg" and "FAIL\tpkg").
func parseGoTestCounts(output string) (passed, failed, skipped int) {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "ok"):
			passed++
		case strings.HasPrefix(trimmed, "FAIL"):
			failed++
		case strings.Contains(trimmed, "[no test files]"):
			skipped++
		case strings.Contains(trimmed, "--- SKIP"):
			skipped++
		}
	}
	return
}

// runQuickVerify runs a build or test command and returns (summary, success).
func runQuickVerify(workspaceDir, kind string) (string, bool) {
	projType := detectProjectType(workspaceDir)
	if projType == "" {
		return "프로젝트 타입 감지 실패", false
	}

	var cmdName string
	var cmdArgs []string

	switch kind {
	case "build":
		cmdName, cmdArgs = buildCommand(projType)
	case "test":
		cmdName, cmdArgs = testCommand(projType)
	}

	if cmdName == "" {
		return "해당 없음", true
	}

	output := runCmdWithTimeout(workspaceDir, 30*time.Second, cmdName, cmdArgs...)
	if output == "" {
		return "성공", true
	}

	lower := strings.ToLower(output)
	isError := strings.Contains(lower, "error") || strings.Contains(lower, "fail") || strings.Contains(lower, "panic")
	if isError {
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) > 5 {
			lines = lines[len(lines)-5:]
		}
		return strings.Join(lines, "\n"), false
	}

	return "성공", true
}
