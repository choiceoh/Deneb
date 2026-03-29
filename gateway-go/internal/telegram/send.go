package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for ValidatePhotoMetadata
	_ "image/png"  // register PNG decoder for ValidatePhotoMetadata
	"io"
	"strconv"
)

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
func SendText(ctx context.Context, c *Client, chatID int64, text string, opts SendOptions) ([]SendResult, error) {
	if text == "" {
		return nil, fmt.Errorf("empty text")
	}

	maxLen := TextChunkLimit
	if opts.ChunkLimit > 0 && opts.ChunkLimit < maxLen {
		maxLen = opts.ChunkLimit
	}

	var chunks []string
	if opts.ChunkMode == string(ChunkModeNewline) {
		chunks = ChunkByNewline(text, maxLen)
	} else if opts.ParseMode == "HTML" {
		chunks = ChunkHTML(text, maxLen)
	} else {
		chunks = ChunkText(text, maxLen)
	}

	var results []SendResult
	for i, chunk := range chunks {
		params := map[string]any{
			"chat_id": chatID,
			"text":    chunk,
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
			if opts.Keyboard != nil {
				params["reply_markup"] = opts.Keyboard
			}
		}

		result, err := c.Call(ctx, "sendMessage", params)
		if err != nil {
			// If HTML parse fails, retry as plain text.
			if opts.ParseMode == "HTML" {
				delete(params, "parse_mode")
				result, err = c.Call(ctx, "sendMessage", params)
			}
			if err != nil {
				return results, fmt.Errorf("sendMessage chunk %d: %w", i, err)
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

	return results, nil
}

// SendPhoto sends a photo by file_id or URL.
func SendPhoto(ctx context.Context, c *Client, chatID int64, photo string, caption string, opts SendOptions) (*SendResult, error) {
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
func SendDocument(ctx context.Context, c *Client, chatID int64, document string, caption string, opts SendOptions) (*SendResult, error) {
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
func SendVideo(ctx context.Context, c *Client, chatID int64, video string, caption string, opts SendOptions) (*SendResult, error) {
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
func SendAudio(ctx context.Context, c *Client, chatID int64, audio string, caption string, opts SendOptions) (*SendResult, error) {
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
func SendVoice(ctx context.Context, c *Client, chatID int64, voice string, caption string, opts SendOptions) (*SendResult, error) {
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

// UploadDocument uploads a document file.
func UploadDocument(ctx context.Context, c *Client, chatID int64, fileName string, fileData io.Reader, caption string, opts SendOptions) (*SendResult, error) {
	params := map[string]string{
		"chat_id": strconv.FormatInt(chatID, 10),
	}
	if caption != "" {
		params["caption"] = caption
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

// UploadPhoto uploads a photo file.
func UploadPhoto(ctx context.Context, c *Client, chatID int64, fileName string, fileData io.Reader, caption string, opts SendOptions) (*SendResult, error) {
	params := map[string]string{
		"chat_id": strconv.FormatInt(chatID, 10),
	}
	if caption != "" {
		params["caption"] = caption
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

// AnswerCallbackQuery acknowledges a callback query.
func AnswerCallbackQuery(ctx context.Context, c *Client, queryID string, text string) error {
	params := map[string]any{
		"callback_query_id": queryID,
	}
	if text != "" {
		params["text"] = text
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

// --- Helpers ---

func applyMediaOpts(params map[string]any, caption string, opts SendOptions) {
	if caption != "" {
		// Truncate caption to Telegram limit.
		if len(caption) > MaxCaptionLength {
			caption = caption[:MaxCaptionLength]
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
	if opts.Keyboard != nil {
		params["reply_markup"] = opts.Keyboard
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
