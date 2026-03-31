// Telegram-specific reply pipeline: vibe coder formatting and analysis.
//
// The pipeline decorates raw agent reply text before sending to Telegram,
// ensuring the vibe coder never sees raw code and always gets Korean
// explanations with one-click action buttons.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

// codeBlockMinChars is the threshold (in characters) above which a fenced
// code block is extracted as a file attachment instead of being shown inline.
const codeBlockMinChars = 200

// codeBlockPattern matches fenced code blocks with optional language tag.
// Group 1: language (may be empty), Group 2: code content.
var codeBlockPattern = regexp.MustCompile("(?s)```(\\w*)\\n?(.*?)```")

// langDisplayNames maps code fence language tags to Korean display labels.
var langDisplayNames = map[string]string{
	"go":         "Go",
	"rust":       "Rust",
	"python":     "Python",
	"javascript": "JavaScript",
	"typescript": "TypeScript",
	"bash":       "Bash",
	"sh":         "Shell",
	"shell":      "Shell",
	"sql":        "SQL",
	"json":       "JSON",
	"yaml":       "YAML",
	"yml":        "YAML",
	"toml":       "TOML",
	"html":       "HTML",
	"css":        "CSS",
	"proto":      "Protobuf",
	"dockerfile": "Dockerfile",
	"makefile":   "Makefile",
	"c":          "C",
	"cpp":        "C++",
	"java":       "Java",
	"kotlin":     "Kotlin",
	"swift":      "Swift",
	"ruby":       "Ruby",
	"lua":        "Lua",
	"r":          "R",
	"xml":        "XML",
	"graphql":    "GraphQL",
	"markdown":   "Markdown",
	"md":         "Markdown",
}

// langFileExtensions maps fence language tags to file extensions.
var langFileExtensions = map[string]string{
	"go":         ".go",
	"rust":       ".rs",
	"python":     ".py",
	"javascript": ".js",
	"typescript": ".ts",
	"bash":       ".sh",
	"sh":         ".sh",
	"shell":      ".sh",
	"sql":        ".sql",
	"json":       ".json",
	"yaml":       ".yaml",
	"yml":        ".yaml",
	"toml":       ".toml",
	"html":       ".html",
	"css":        ".css",
	"proto":      ".proto",
	"dockerfile": "",
	"makefile":   "",
	"c":          ".c",
	"cpp":        ".cpp",
	"java":       ".java",
	"kotlin":     ".kt",
	"swift":      ".swift",
	"ruby":       ".rb",
	"lua":        ".lua",
	"r":          ".r",
	"xml":        ".xml",
	"graphql":    ".graphql",
	"markdown":   ".md",
	"md":         ".md",
}

// fileAttachment represents a code block extracted as a file.
type fileAttachment struct {
	Filename string
	Content  string
	Language string
	Lines    int
}

// vibeCoderReplyPipeline wraps the raw reply text through the vibe coder
// formatting and analysis pipeline before sending to Telegram.
//
// Pipeline stages:
//  1. FormatReply — extract large code blocks as file attachments, collapse
//     remaining code blocks into Korean summaries.
//  2. AnalyzeReply — classify the reply outcome (code change, test, error, etc.).
//  3. ContextButtons — select appropriate action buttons for the outcome.
//  4. Send — chunk text, attach buttons to the last chunk.
//  5. sendVibeCoderFollowUps — post-reply error translation and auto-verification.
type vibeCoderReplyPipeline struct {
	client     *telegram.Client
	chatID     int64
	sessionKey string
	logger     *slog.Logger
}

// newVibeCoderReplyPipeline creates a pipeline bound to a specific Telegram chat.
func newVibeCoderReplyPipeline(client *telegram.Client, chatID int64, sessionKey string, logger *slog.Logger) *vibeCoderReplyPipeline {
	if logger == nil {
		logger = slog.Default()
	}
	return &vibeCoderReplyPipeline{
		client:     client,
		chatID:     chatID,
		sessionKey: sessionKey,
		logger:     logger,
	}
}

// FormatReply extracts large code blocks (>=200 chars) as file attachments,
// collapses remaining code blocks into Korean summaries like _(go 코드, 42줄)_.
//
// Returns the cleaned text (code blocks replaced with summaries) and any
// extracted file attachments that should be sent separately.
func (p *vibeCoderReplyPipeline) FormatReply(text string) (string, []fileAttachment) {
	if text == "" {
		return "", nil
	}

	var attachments []fileAttachment
	attachmentCounter := 0

	cleaned := codeBlockPattern.ReplaceAllStringFunc(text, func(match string) string {
		groups := codeBlockPattern.FindStringSubmatch(match)
		if len(groups) < 3 {
			return match
		}

		lang := strings.TrimSpace(groups[1])
		code := strings.TrimSpace(groups[2])

		if len(code) == 0 {
			return ""
		}

		lineCount := countLines(code)

		// Small code blocks: collapse into a Korean summary line.
		if len(code) < codeBlockMinChars {
			return formatCodeSummary(lang, lineCount)
		}

		// Large code blocks: extract as a file attachment.
		attachmentCounter++
		filename := buildAttachmentFilename(lang, attachmentCounter)
		attachments = append(attachments, fileAttachment{
			Filename: filename,
			Content:  code,
			Language: lang,
			Lines:    lineCount,
		})

		// Replace the code block with a reference to the attachment.
		displayLang := resolveDisplayLang(lang)
		return fmt.Sprintf("📎 _%s 코드 첨부 (%s, %d줄)_", displayLang, filename, lineCount)
	})

	// Clean up excessive blank lines left after code block removal.
	cleaned = collapseBlankLines(cleaned)

	return strings.TrimSpace(cleaned), attachments
}

// ProcessReply runs the full pipeline: format -> analyze -> buttons -> send.
//
// This is the main entry point called by the reply function in
// wireTelegramChatHandler. It orchestrates all pipeline stages and
// handles errors at each step gracefully (logging rather than failing
// the entire delivery).
func (p *vibeCoderReplyPipeline) ProcessReply(ctx context.Context, delivery *chat.DeliveryContext, text string) error {
	if text == "" {
		return nil
	}

	// Stage 1: Format — extract code blocks, collapse summaries.
	cleanText, attachments := p.FormatReply(text)

	// Stage 2: Analyze — classify the reply outcome.
	outcome := telegram.AnalyzeReply(text) // analyze original text for full context

	// Stage 3: Buttons — select context-aware action buttons.
	buttons := telegram.ContextButtons(outcome, p.sessionKey)

	// Stage 4: Send — chunk text and deliver with buttons on last chunk.
	if err := p.sendFormattedReply(ctx, cleanText, buttons); err != nil {
		return fmt.Errorf("send formatted reply: %w", err)
	}

	// Stage 4b: Send file attachments as separate document messages.
	p.sendAttachments(ctx, attachments)

	// Stage 5: Follow-ups — error translation, auto-verification.
	p.sendVibeCoderFollowUps(ctx, delivery, outcome, text)

	return nil
}

// sendFormattedReply converts the cleaned text to Telegram HTML, chunks it,
// and attaches the inline keyboard to the last chunk.
func (p *vibeCoderReplyPipeline) sendFormattedReply(ctx context.Context, text string, buttons [][]telegram.ReplyButton) error {
	if text == "" {
		return nil
	}

	html := telegram.MarkdownToTelegramHTML(text)
	chunks := telegram.ChunkHTML(html, telegram.TextChunkLimit)

	for i, chunk := range chunks {
		opts := telegram.SendOptions{
			ParseMode:         "HTML",
			DisableLinkPreview: true,
		}

		// Attach buttons to the last chunk only.
		if i == len(chunks)-1 && len(buttons) > 0 {
			opts.Keyboard = buildKeyboardMarkup(buttons)
		}

		_, err := telegram.SendText(ctx, p.client, p.chatID, chunk, opts)
		if err != nil {
			return fmt.Errorf("send chunk %d/%d: %w", i+1, len(chunks), err)
		}
	}

	return nil
}

// sendAttachments uploads extracted code blocks as document files.
// Each attachment is sent as a separate message with a Korean caption
// describing the content.
func (p *vibeCoderReplyPipeline) sendAttachments(ctx context.Context, attachments []fileAttachment) {
	if len(attachments) == 0 {
		return
	}

	for _, att := range attachments {
		displayLang := resolveDisplayLang(att.Language)
		caption := fmt.Sprintf("📎 %s 코드 (%d줄)", displayLang, att.Lines)

		reader := strings.NewReader(att.Content)
		_, err := telegram.UploadDocument(ctx, p.client, p.chatID, att.Filename, reader, caption, telegram.SendOptions{
			DisableNotification: true,
		})
		if err != nil {
			p.logger.Warn("failed to upload code attachment",
				"filename", att.Filename,
				"language", att.Language,
				"error", err,
			)
		}
	}
}

// sendVibeCoderFollowUps sends post-reply follow-ups:
//   - Error Korean translation embed when errors/failures detected
//   - Auto build/test verification embed when code changes detected
//
// Follow-ups are best-effort: failures are logged but do not propagate.
func (p *vibeCoderReplyPipeline) sendVibeCoderFollowUps(ctx context.Context, delivery *chat.DeliveryContext, outcome telegram.ReplyOutcome, text string) {
	switch outcome {
	case telegram.OutcomeError, telegram.OutcomeTestFail, telegram.OutcomeBuildFail:
		p.sendErrorTranslation(ctx, text)

	case telegram.OutcomeCodeChange:
		p.sendAutoVerificationHint(ctx)
	}
}

// sendErrorTranslation extracts error messages from the reply text and
// sends a Korean translation embed so the vibe coder understands what
// went wrong without reading English error messages.
func (p *vibeCoderReplyPipeline) sendErrorTranslation(ctx context.Context, text string) {
	// Extract lines that look like error messages.
	errorLines := extractErrorLines(text)
	if len(errorLines) == 0 {
		return
	}

	var b strings.Builder
	b.WriteString("💡 <b>오류 설명</b>\n\n")

	translatedAny := false
	for _, errLine := range errorLines {
		korean := telegram.TranslateErrorKorean(errLine)
		if korean == "" {
			continue
		}
		translatedAny = true
		// Show the translated explanation with a bullet.
		b.WriteString("• ")
		b.WriteString(korean)
		b.WriteByte('\n')
	}

	if !translatedAny {
		// No known patterns matched; send a generic hint.
		b.Reset()
		b.WriteString("💡 오류가 발생했습니다. 수정 버튼을 눌러 자동으로 해결해 보세요.")
	}

	_, err := telegram.SendText(ctx, p.client, p.chatID, b.String(), telegram.SendOptions{
		ParseMode:           "HTML",
		DisableNotification: true,
		DisableLinkPreview:  true,
	})
	if err != nil {
		p.logger.Warn("failed to send error translation follow-up",
			"chatId", p.chatID, "error", err)
	}
}

// sendAutoVerificationHint sends a brief embed suggesting that automatic
// build/test verification is recommended after code changes.
func (p *vibeCoderReplyPipeline) sendAutoVerificationHint(ctx context.Context) {
	hintText := "🔍 코드가 변경되었습니다. 테스트를 실행하여 확인하시겠습니까?"

	// Build a single-row keyboard with test/commit actions.
	buttons := [][]telegram.ReplyButton{
		{
			{Text: "테스트 실행", Data: formatFollowUpAction(telegram.ActionTest, p.sessionKey)},
			{Text: "커밋", Data: formatFollowUpAction(telegram.ActionCommit, p.sessionKey)},
		},
	}

	_, err := telegram.SendText(ctx, p.client, p.chatID, hintText, telegram.SendOptions{
		DisableNotification: true,
		Keyboard:            buildKeyboardMarkup(buttons),
	})
	if err != nil {
		p.logger.Warn("failed to send auto-verification hint",
			"chatId", p.chatID, "error", err)
	}
}

// --- Helpers ---

// formatCodeSummary builds a Korean-language italic summary for a collapsed
// code block, e.g. "_(Go 코드, 42줄)_".
func formatCodeSummary(lang string, lineCount int) string {
	displayLang := resolveDisplayLang(lang)
	if lineCount <= 1 {
		return fmt.Sprintf("_(%s 코드)_", displayLang)
	}
	return fmt.Sprintf("_(%s 코드, %d줄)_", displayLang, lineCount)
}

// resolveDisplayLang returns a human-readable language name for display.
// Falls back to the raw fence tag or "코드" if unknown.
func resolveDisplayLang(lang string) string {
	if lang == "" {
		return "코드"
	}
	lower := strings.ToLower(lang)
	if display, ok := langDisplayNames[lower]; ok {
		return display
	}
	// Unknown language: capitalize first letter.
	return strings.ToUpper(lang[:1]) + lang[1:]
}

// buildAttachmentFilename generates a filename for an extracted code block.
// Format: "code_N.ext" where N is the 1-based attachment index.
func buildAttachmentFilename(lang string, index int) string {
	ext := ".txt"
	if lang != "" {
		lower := strings.ToLower(lang)
		if e, ok := langFileExtensions[lower]; ok && e != "" {
			ext = e
		}
	}
	return fmt.Sprintf("code_%d%s", index, ext)
}

// countLines returns the number of lines in a string.
// An empty string has 0 lines; a string without newlines has 1 line.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n") + 1
	// Trailing newline should not add an extra line.
	if strings.HasSuffix(s, "\n") {
		n--
	}
	return n
}

// collapseBlankLines reduces runs of 3+ consecutive blank lines to 2.
var multipleBlankLines = regexp.MustCompile(`\n{3,}`)

func collapseBlankLines(s string) string {
	return multipleBlankLines.ReplaceAllString(s, "\n\n")
}

// extractErrorLines pulls lines from text that look like error messages.
// Heuristic: lines containing "error", "failed", "panic", or Korean error keywords.
func extractErrorLines(text string) []string {
	lines := strings.Split(text, "\n")
	var errors []string
	seen := make(map[string]bool)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lower := strings.ToLower(trimmed)
		isError := strings.Contains(lower, "error") ||
			strings.Contains(lower, "failed") ||
			strings.Contains(lower, "panic") ||
			strings.Contains(lower, "fatal") ||
			strings.Contains(lower, "오류") ||
			strings.Contains(lower, "실패") ||
			strings.Contains(lower, "에러")

		if !isError {
			continue
		}

		// Deduplicate: skip if we already extracted a similar line.
		normalized := normalizeErrorLine(trimmed)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true

		errors = append(errors, trimmed)

		// Cap at 5 error lines to avoid flooding the follow-up message.
		if len(errors) >= 5 {
			break
		}
	}

	return errors
}

// normalizeErrorLine strips numbers, whitespace, and punctuation to detect
// near-duplicate error lines (e.g. same error at different line numbers).
var nonAlphaPattern = regexp.MustCompile(`[\d\s\W]+`)

func normalizeErrorLine(s string) string {
	return nonAlphaPattern.ReplaceAllString(strings.ToLower(s), "")
}

// buildKeyboardMarkup converts ReplyButton rows to an InlineKeyboardMarkup.
func buildKeyboardMarkup(rows [][]telegram.ReplyButton) *telegram.InlineKeyboardMarkup {
	if len(rows) == 0 {
		return nil
	}
	keyboard := make([][]telegram.InlineKeyboardButton, 0, len(rows))
	for _, row := range rows {
		kbRow := make([]telegram.InlineKeyboardButton, 0, len(row))
		for _, btn := range row {
			kbRow = append(kbRow, telegram.InlineKeyboardButton{
				Text:         btn.Text,
				CallbackData: btn.Data,
			})
		}
		keyboard = append(keyboard, kbRow)
	}
	return &telegram.InlineKeyboardMarkup{
		InlineKeyboard: keyboard,
	}
}

// formatFollowUpAction builds a callback_data string for follow-up buttons.
// Truncates to Telegram's MaxCallbackData limit.
func formatFollowUpAction(action, sessionKey string) string {
	data := action + ":" + sessionKey
	if len(data) > telegram.MaxCallbackData {
		data = data[:telegram.MaxCallbackData]
	}
	return data
}

// parseChatIDFromDelivery extracts the Telegram chat ID from a delivery context.
// Returns 0 and an error if the delivery is nil or the chat ID is invalid.
func parseChatIDFromDelivery(delivery *chat.DeliveryContext) (int64, error) {
	if delivery == nil {
		return 0, fmt.Errorf("nil delivery context")
	}
	return telegram.ParseChatID(delivery.To)
}

// sessionKeyFromDelivery builds a session key from delivery context fields.
// Format: "chatID:messageID" to uniquely identify the conversation turn.
func sessionKeyFromDelivery(delivery *chat.DeliveryContext) string {
	if delivery == nil {
		return ""
	}
	chatID := delivery.To
	msgID := delivery.MessageID
	if msgID == "" {
		return chatID
	}
	return chatID + ":" + msgID
}

// chatIDString converts an int64 chat ID to its string representation.
func chatIDString(chatID int64) string {
	return strconv.FormatInt(chatID, 10)
}
