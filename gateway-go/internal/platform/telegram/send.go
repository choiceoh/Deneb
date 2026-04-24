package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for ValidatePhotoMetadata
	_ "image/png"  // register PNG decoder for ValidatePhotoMetadata
	"io"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

// redactOutbound applies secret redaction to user-visible text just before
// it leaves the gateway via a Telegram API call. This is the defense-in-depth
// egress guard described in .claude/rules (Phase 3-D2) — any leak path that
// bypassed the transcript/wiki layer still short-circuits here before
// Telegram retains the message.
//
// Callers should never mutate their own copy of the text; redactOutbound
// returns a fresh string and leaves the input alone.
//
// Redaction runs BEFORE any markup-specific escaping (HTML entity encoding,
// coremarkdown sanitization, etc.) so that regex matchers see the raw token
// shape. Masked output ("prefix6...suffix4") is plain ASCII safe for every
// parse_mode Telegram supports.
//
// The operation is nil-safe and idempotent (pkg/redact guarantees
// String(String(x)) == String(x)), so applying it twice by accident in a
// layered pipeline is harmless.
func redactOutbound(text string) string {
	return redact.String(text)
}

// redactKeyboard returns a fresh copy of markup with each button's visible
// Text redacted. custom_id (CallbackData) and URL targets are pure routing
// keys / navigation primitives and are NEVER touched — mutating them would
// break button dispatch in HandleTelegramInteraction.
//
// Returns nil when markup is nil so callers can pass through without
// special-casing.
func redactKeyboard(markup *InlineKeyboardMarkup) *InlineKeyboardMarkup {
	if markup == nil {
		return nil
	}
	rows := make([][]InlineKeyboardButton, len(markup.InlineKeyboard))
	for i, row := range markup.InlineKeyboard {
		cloned := make([]InlineKeyboardButton, len(row))
		for j, btn := range row {
			cloned[j] = InlineKeyboardButton{
				Text:         redactOutbound(btn.Text),
				CallbackData: btn.CallbackData, // routing key — DO NOT redact
				URL:          btn.URL,          // nav target — DO NOT redact
			}
		}
		rows[i] = cloned
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// SendOptions configures message sending behavior.
type SendOptions struct {
	// ParseMode: "HTML" or "" (plain text).
	ParseMode string
	// ThreadID for forum topic messages.
	ThreadID int64
	// DisableNotification sends the message silently.
	DisableNotification bool
	// DisableLinkPreview disables link previews.
	DisableLinkPreview bool
	// ReplyToMessageID quotes a specific message.
	ReplyToMessageID int64
	// Keyboard attaches an inline keyboard.
	Keyboard *InlineKeyboardMarkup
	// ChunkLimit overrides the max chunk size in characters (0 = default).
	ChunkLimit int
	// ChunkMode controls splitting: "newline" or "length" (default).
	ChunkMode string
}

// SendResult holds the result of a send operation.
type SendResult struct {
	MessageID int64 `json:"messageId"`
	ChatID    int64 `json:"chatId"`
}

// SendText sends a text message, automatically chunking if needed.
// Returns results for all chunks sent.
// chunkLimit overrides the default max chunk size (0 = use MaxTextLength).
// chunkMode controls splitting: "newline" splits on every newline, "length" (default) splits by size.
//
// Secret redaction runs BEFORE chunking so tokens that would otherwise split
// across a chunk boundary (and escape per-chunk redaction) are collapsed to
// their masked form first — this prevents the "half-token leak" edge case
// when a key spans the 4000-char boundary.
func SendText(ctx context.Context, c *Client, chatID int64, text string, opts SendOptions) ([]SendResult, error) {
	if text == "" {
		return nil, fmt.Errorf("empty text")
	}

	// Defense-in-depth egress guard: redact before chunking so split-across-
	// boundary tokens collapse into their 13-char masked form first. The
	// per-chunk redaction below is a second pass for idempotent safety.
	text = redactOutbound(text)
	keyboard := redactKeyboard(opts.Keyboard)

	maxLen := TextChunkLimit
	if opts.ChunkLimit > 0 && opts.ChunkLimit < maxLen {
		maxLen = opts.ChunkLimit
	}

	var chunks []string
	switch {
	case opts.ChunkMode == string(ChunkModeNewline):
		chunks = ChunkByNewline(text, maxLen)
	case opts.ParseMode == "HTML":
		chunks = ChunkHTML(text, maxLen)
	default:
		chunks = ChunkText(text, maxLen)
	}

	// Track whether HTML parse failed so subsequent chunks skip HTML entirely.
	parseMode := opts.ParseMode

	var (
		results      []SendResult
		failedChunks []int
		firstErr     chunkError
	)
	for i, chunk := range chunks {
		// Second pass: redact the individual chunk. pkg/redact is idempotent,
		// so if the text was already fully masked above this is a no-op; any
		// pattern that first becomes visible post-chunking still gets caught.
		chunk = redactOutbound(chunk)
		params := map[string]any{
			"chat_id": chatID,
			"text":    chunk,
		}
		if parseMode != "" {
			params["parse_mode"] = parseMode
		}
		if opts.ThreadID != 0 {
			params["message_thread_id"] = opts.ThreadID
		}
		if opts.DisableNotification {
			params["disable_notification"] = true
		}
		if opts.DisableLinkPreview {
			params["link_preview_options"] = LinkPreviewOptions{IsDisabled: true}
		}
		// Only attach reply and keyboard to the first chunk.
		if i == 0 {
			if opts.ReplyToMessageID != 0 {
				params["reply_parameters"] = map[string]any{
					"message_id": opts.ReplyToMessageID,
				}
			}
			if keyboard != nil {
				params["reply_markup"] = keyboard
			}
		}

		result, err := c.Call(ctx, "sendMessage", params)
		if err != nil {
			// If HTML parse fails, retry as plain text and disable HTML for remaining chunks.
			if parseMode == "HTML" && isHTMLParseError(err) {
				delete(params, "parse_mode")
				parseMode = "" // all subsequent chunks sent as plain text
				result, err = c.Call(ctx, "sendMessage", params)
			}
			if err != nil {
				// Do NOT return early — remaining chunks may still be deliverable.
				// Record the failure and try the rest so the user at least gets
				// a partial reply instead of total silence.
				failedChunks = append(failedChunks, i)
				firstErr = firstErr.wrap(i, err)
				continue
			}
		}

		var msg Message
		if err := json.Unmarshal(result, &msg); err == nil {
			results = append(results, SendResult{
				MessageID: msg.MessageID,
				ChatID:    msg.Chat.ID,
			})
		}
	}

	if len(failedChunks) > 0 {
		return results, fmt.Errorf("sendMessage: %d/%d chunks failed (indices %v): %w",
			len(failedChunks), len(chunks), failedChunks, firstErr.err)
	}
	return results, nil
}

// chunkError carries the first failure encountered while sending chunks, so
// callers can unwrap it while SendText continues trying remaining chunks.
type chunkError struct {
	idx int
	err error
}

func (c chunkError) wrap(i int, e error) chunkError {
	if c.err != nil {
		return c
	}
	return chunkError{idx: i, err: e}
}

// SendPhoto sends a photo by file_id or URL.
func SendPhoto(ctx context.Context, c *Client, chatID int64, photo, caption string, opts SendOptions) (*SendResult, error) {
	params := map[string]any{
		"chat_id": chatID,
		"photo":   photo,
	}
	applyMediaOpts(params, caption, opts)

	result, err := c.Call(ctx, "sendPhoto", params)
	if err != nil {
		return nil, fmt.Errorf("sendPhoto: %w", err)
	}
	return parseSendResult(result)
}

// SendDocument sends a document by file_id or URL.
func SendDocument(ctx context.Context, c *Client, chatID int64, document, caption string, opts SendOptions) (*SendResult, error) {
	params := map[string]any{
		"chat_id":  chatID,
		"document": document,
	}
	applyMediaOpts(params, caption, opts)

	result, err := c.Call(ctx, "sendDocument", params)
	if err != nil {
		return nil, fmt.Errorf("sendDocument: %w", err)
	}
	return parseSendResult(result)
}

// SendVideo sends a video by file_id or URL.
func SendVideo(ctx context.Context, c *Client, chatID int64, video, caption string, opts SendOptions) (*SendResult, error) {
	params := map[string]any{
		"chat_id": chatID,
		"video":   video,
	}
	applyMediaOpts(params, caption, opts)

	result, err := c.Call(ctx, "sendVideo", params)
	if err != nil {
		return nil, fmt.Errorf("sendVideo: %w", err)
	}
	return parseSendResult(result)
}

// SendAudio sends an audio file by file_id or URL.
func SendAudio(ctx context.Context, c *Client, chatID int64, audio, caption string, opts SendOptions) (*SendResult, error) {
	params := map[string]any{
		"chat_id": chatID,
		"audio":   audio,
	}
	applyMediaOpts(params, caption, opts)

	result, err := c.Call(ctx, "sendAudio", params)
	if err != nil {
		return nil, fmt.Errorf("sendAudio: %w", err)
	}
	return parseSendResult(result)
}

// SendVoice sends a voice note by file_id or URL.
func SendVoice(ctx context.Context, c *Client, chatID int64, voice, caption string, opts SendOptions) (*SendResult, error) {
	params := map[string]any{
		"chat_id": chatID,
		"voice":   voice,
	}
	applyMediaOpts(params, caption, opts)

	result, err := c.Call(ctx, "sendVoice", params)
	if err != nil {
		return nil, fmt.Errorf("sendVoice: %w", err)
	}
	return parseSendResult(result)
}

// UploadDocument uploads a document file. Caption is redacted at egress.
func UploadDocument(ctx context.Context, c *Client, chatID int64, fileName string, fileData io.Reader, caption string, opts SendOptions) (*SendResult, error) {
	params := map[string]string{
		"chat_id": strconv.FormatInt(chatID, 10),
	}
	if caption != "" {
		params["caption"] = redactOutbound(caption)
	}
	if opts.ParseMode != "" {
		params["parse_mode"] = opts.ParseMode
	}
	if opts.ThreadID != 0 {
		params["message_thread_id"] = strconv.FormatInt(opts.ThreadID, 10)
	}
	if opts.DisableNotification {
		params["disable_notification"] = "true"
	}

	result, err := c.Upload(ctx, "sendDocument", "document", fileName, fileData, params)
	if err != nil {
		return nil, fmt.Errorf("uploadDocument: %w", err)
	}
	return parseSendResult(result)
}

// UploadPhoto uploads a photo file. Caption is redacted at egress.
func UploadPhoto(ctx context.Context, c *Client, chatID int64, fileName string, fileData io.Reader, caption string, opts SendOptions) (*SendResult, error) {
	params := map[string]string{
		"chat_id": strconv.FormatInt(chatID, 10),
	}
	if caption != "" {
		params["caption"] = redactOutbound(caption)
	}
	if opts.ParseMode != "" {
		params["parse_mode"] = opts.ParseMode
	}
	if opts.ThreadID != 0 {
		params["message_thread_id"] = strconv.FormatInt(opts.ThreadID, 10)
	}
	if opts.DisableNotification {
		params["disable_notification"] = "true"
	}

	result, err := c.Upload(ctx, "sendPhoto", "photo", fileName, fileData, params)
	if err != nil {
		return nil, fmt.Errorf("uploadPhoto: %w", err)
	}
	return parseSendResult(result)
}

// EditMessageText edits the text of an existing message.
// An optional keyboard can be attached (pass nil to omit).
// Returns the edited message result.
//
// Secret redaction is applied to both the text body and any button labels
// before the edit is sent. The caller's inputs are not mutated.
func EditMessageText(ctx context.Context, c *Client, chatID, messageID int64, text, parseMode string, keyboard *InlineKeyboardMarkup) (*SendResult, error) {
	// Egress redaction — see redactOutbound comment in SendText.
	text = redactOutbound(text)
	keyboard = redactKeyboard(keyboard)

	params := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
	}
	if parseMode != "" {
		params["parse_mode"] = parseMode
	}
	if keyboard != nil {
		params["reply_markup"] = keyboard
	}
	params["link_preview_options"] = LinkPreviewOptions{IsDisabled: true}

	result, err := c.Call(ctx, "editMessageText", params)
	if err != nil {
		// If HTML parse fails, retry as plain text.
		if parseMode == "HTML" && isHTMLParseError(err) {
			delete(params, "parse_mode")
			result, err = c.Call(ctx, "editMessageText", params)
		}
		if err != nil {
			return nil, fmt.Errorf("editMessageText: %w", err)
		}
	}
	return parseSendResult(result)
}

// AnswerCallbackQuery acknowledges a callback query. The optional text is
// redacted before being shown to the user (it appears as a toast popup so
// even short leaks are visible).
func AnswerCallbackQuery(ctx context.Context, c *Client, queryID, text string) error {
	params := map[string]any{
		"callback_query_id": queryID,
	}
	if text != "" {
		params["text"] = redactOutbound(text)
	}
	_, err := c.Call(ctx, "answerCallbackQuery", params)
	return err
}

// BuildInlineKeyboard creates an inline keyboard from rows of button definitions.
func BuildInlineKeyboard(rows [][]InlineKeyboardButton) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: rows,
	}
}

// ValidatePhotoMetadata reports whether r is safe to send via Telegram's sendPhoto API.
// It peeks at the image header to check format and dimensions without decoding the full
// image, then seeks r back to the start so the caller can immediately upload it.
// Returns false for unrecognised formats, dimensions exceeding PhotoMaxDimension,
// or aspect ratios exceeding PhotoMaxAspectRatio — in those cases the caller should
// send the file as a document instead.
func ValidatePhotoMetadata(r io.ReadSeeker) bool {
	cfg, _, err := image.DecodeConfig(r)
	// Always seek back regardless of outcome so the caller reads from the start.
	_, _ = r.Seek(0, io.SeekStart)
	if err != nil {
		// Unrecognised image format — safer to send as document.
		return false
	}
	if cfg.Width > PhotoMaxDimension || cfg.Height > PhotoMaxDimension {
		return false
	}
	if cfg.Width > 0 && cfg.Height > 0 {
		w, h := float64(cfg.Width), float64(cfg.Height)
		if w/h > PhotoMaxAspectRatio || h/w > PhotoMaxAspectRatio {
			return false
		}
	}
	return true
}

// isHTMLParseError returns true if err is a Telegram API error caused by invalid HTML entities.
func isHTMLParseError(err error) bool {
	var apiErr *httpretry.APIError
	return errors.As(err, &apiErr) && isParseError(apiErr)
}

// IsMessageNotModifiedError returns true if err indicates Telegram rejected an
// edit because the message content is unchanged. This is not a real failure —
// the message already shows the desired content.
func IsMessageNotModifiedError(err error) bool {
	var apiErr *httpretry.APIError
	return errors.As(err, &apiErr) && isMessageNotModified(apiErr)
}

// isParseError returns true if the API error is an HTML/entity parsing failure.
func isParseError(e *httpretry.APIError) bool {
	return e.StatusCode == 400 && (strings.Contains(e.Message, "can't parse entities") ||
		strings.Contains(e.Message, "parse entities") ||
		strings.Contains(e.Message, "find end of the entity"))
}

// isMessageNotModified returns true if the API error indicates unchanged message content.
func isMessageNotModified(e *httpretry.APIError) bool {
	return e.StatusCode == 400 && strings.Contains(e.Message, "message is not modified")
}

// --- Helpers ---

func applyMediaOpts(params map[string]any, caption string, opts SendOptions) {
	if caption != "" {
		// Egress redaction BEFORE truncation so masks don't get sliced in half.
		caption = redactOutbound(caption)
		// Truncate caption to Telegram limit (UTF-8 safe).
		if len(caption) > MaxCaptionLength {
			caption = truncateUTF8(caption, MaxCaptionLength)
		}
		params["caption"] = caption
	}
	if opts.ParseMode != "" {
		params["parse_mode"] = opts.ParseMode
	}
	if opts.ThreadID != 0 {
		params["message_thread_id"] = opts.ThreadID
	}
	if opts.DisableNotification {
		params["disable_notification"] = true
	}
	if opts.ReplyToMessageID != 0 {
		params["reply_parameters"] = map[string]any{
			"message_id": opts.ReplyToMessageID,
		}
	}
	if kb := redactKeyboard(opts.Keyboard); kb != nil {
		params["reply_markup"] = kb
	}
}

func parseSendResult(result json.RawMessage) (*SendResult, error) {
	var msg Message
	if err := json.Unmarshal(result, &msg); err != nil {
		return nil, fmt.Errorf("decode message: %w", err)
	}
	return &SendResult{
		MessageID: msg.MessageID,
		ChatID:    msg.Chat.ID,
	}, nil
}
