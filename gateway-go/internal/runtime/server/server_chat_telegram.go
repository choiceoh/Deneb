// Telegram chat handler wiring and configuration.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// telegramDedupKey builds a dedup cache key from chat ID and the *full* reply
// text. Using a content hash (instead of a byte prefix) avoids two different
// long replies that happen to share an opening header from collapsing into one
// dedup bucket — and sidesteps UTF-8 boundary issues of a byte-length cut.
func telegramDedupKey(chatID, text string) string {
	h := sha256.Sum256([]byte(text))
	return chatID + ":" + hex.EncodeToString(h[:8])
}

func (s *Server) wireTelegramChatHandler() {
	// Recent-send dedup cache: prevents the same text from being delivered
	// to the same chat twice within a short window (e.g. when the LLM uses
	// the message tool AND also produces a text response without NO_REPLY).
	var recentMu sync.Mutex
	recentSends := make(map[string]time.Time) // key: "chatID:text[:200]"
	const recentTTL = 10 * time.Second

	// Set reply function: delivers assistant responses back to Telegram.
	s.chatHandler.SetReplyFunc(func(ctx context.Context, delivery *chat.DeliveryContext, text string) error {
		if delivery == nil || delivery.Channel != "telegram" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return fmt.Errorf("telegram client not connected")
		}
		chatID, err := telegram.ParseChatID(delivery.To)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}

		// Dedup: skip if the same text was sent to this chat recently.
		// When a streaming draft exists and the dedup fires (e.g. the message
		// tool already sent the same text), the draft must still be deleted so
		// it does not linger in a half-finished state.
		dedupKey := telegramDedupKey(delivery.To, text)
		recentMu.Lock()
		sentAt, isDup := recentSends[dedupKey]
		isDup = isDup && time.Since(sentAt) < recentTTL
		recentMu.Unlock()
		if isDup {
			// Clean up an orphaned streaming draft so it does not linger.
			if delivery.DraftMsgID != "" {
				draftID := delivery.DraftMsgID
				delivery.DraftMsgID = ""
				if editMsgID, parseErr := strconv.ParseInt(draftID, 10, 64); parseErr == nil {
					_ = client.DeleteMessage(ctx, chatID, editMsgID)
				}
			}
			s.logger.Info("suppressed duplicate reply to telegram",
				"chatId", delivery.To, "textLen", len(text))
			return nil
		}

		// recordSent commits a successful delivery to the dedup cache. Called
		// only after Telegram confirms acceptance, so a transient API failure
		// does not poison the cache and silently drop the user's next retry.
		recordSent := func() {
			recentMu.Lock()
			for k, t := range recentSends {
				if time.Since(t) >= recentTTL {
					delete(recentSends, k)
				}
			}
			recentSends[dedupKey] = time.Now()
			recentMu.Unlock()
		}

		// Parse optional button directive from agent reply.
		cleanText, keyboard := parseReplyButtons(text)
		html := telegram.MarkdownToTelegramHTML(cleanText)

		// If a draft streaming message exists, edit it in-place with the final
		// formatted text instead of deleting and sending a new message. This
		// prevents the "disappear then reappear" flicker on completion.
		if delivery.DraftMsgID != "" {
			draftID := delivery.DraftMsgID
			delivery.DraftMsgID = "" // consumed
			editMsgID, parseErr := strconv.ParseInt(draftID, 10, 64)
			if parseErr == nil {
				_, editErr := telegram.EditMessageText(ctx, client, chatID, editMsgID, html, "HTML", keyboard)
				if editErr == nil {
					recordSent()
					return nil
				}
				// "Message is not modified" means the draft already shows the
				// correct content — treat as success to avoid the visible
				// delete-then-resend flicker.
				if telegram.IsMessageNotModifiedError(editErr) {
					recordSent()
					return nil
				}
				// Edit failed (e.g. message too long for single edit, or API error).
				// Delete the draft and fall through to send as new message.
				s.logger.Warn("draft edit failed, falling back to new message",
					"msgId", draftID, "error", editErr)
				_ = client.DeleteMessage(ctx, chatID, editMsgID) // best-effort: draft cleanup is non-critical
			}
		}

		opts := telegram.SendOptions{ParseMode: "HTML", Keyboard: keyboard}
		if _, err = telegram.SendText(ctx, client, chatID, html, opts); err != nil {
			return err
		}
		recordSent()
		return nil
	})

	// Set media send function: delivers files back to Telegram.
	s.chatHandler.SetMediaSendFunc(func(ctx context.Context, delivery *chat.DeliveryContext, filePath, mediaType, caption string, silent bool) error {
		if delivery == nil {
			return nil
		}

		if delivery.Channel != "telegram" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return fmt.Errorf("telegram client not connected")
		}
		chatID, err := telegram.ParseChatID(delivery.To)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}

		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open file: %w", err)
		}
		defer f.Close()

		fileName := filepath.Base(filePath)
		opts := telegram.SendOptions{DisableNotification: silent}

		switch mediaType {
		case "photo":
			if telegram.ValidatePhotoMetadata(f) {
				_, err = telegram.UploadPhoto(ctx, client, chatID, fileName, f, caption, opts)
				if err != nil {
					// Telegram rejected the photo (e.g. format issue, server-side resize
					// failure). Seek back and retry as a document so the file is not lost.
					s.logger.Warn("uploadPhoto failed, falling back to document",
						"file", fileName, "error", err)
					if _, seekErr := f.Seek(0, 0); seekErr == nil {
						_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
					}
				}
			} else {
				// Metadata check failed (unsupported format, oversized dimensions, bad
				// aspect ratio) — skip sendPhoto and send directly as a document.
				s.logger.Info("photo metadata invalid, sending as document", "file", fileName)
				_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
			}
		case "video":
			// Upload as document — Telegram sendVideo requires a URL/file_id, not multipart.
			_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
		case "audio":
			_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
		case "voice":
			_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
		default: // "document" or unknown
			_, err = telegram.UploadDocument(ctx, client, chatID, fileName, f, caption, opts)
		}
		return err
	})

	// Set typing indicator function: sends "typing" chat action to Telegram
	// periodically during agent runs so the user sees "typing..." in the chat.
	s.chatHandler.SetTypingFunc(func(ctx context.Context, delivery *chat.DeliveryContext) error {
		if delivery == nil {
			return nil
		}

		if delivery.Channel != "telegram" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return nil
		}
		chatID, err := telegram.ParseChatID(delivery.To)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}
		return client.SendChatAction(ctx, chatID, "typing")
	})

	// Set reaction function: sets emoji reactions on the user's triggering message
	// to show agent status phases (👀→🤔→🔥→👍).
	s.chatHandler.SetReactionFunc(func(ctx context.Context, delivery *chat.DeliveryContext, emoji string) error {
		if delivery == nil || delivery.Channel != "telegram" || delivery.MessageID == "" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return nil
		}
		chatID, err := telegram.ParseChatID(delivery.To)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}
		msgID, err := strconv.ParseInt(delivery.MessageID, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid message ID %q: %w", delivery.MessageID, err)
		}
		return client.SetMessageReaction(ctx, chatID, msgID, emoji)
	})

	// Set draft edit function: sends or edits a streaming draft message in Telegram.
	// Used by DraftStreamLoop for real-time message editing during LLM streaming.
	s.chatHandler.SetDraftEditFunc(func(ctx context.Context, delivery *chat.DeliveryContext, msgID string, text string) (string, error) {
		if delivery == nil || delivery.Channel != "telegram" {
			return "", nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return "", fmt.Errorf("telegram client not connected")
		}
		chatID, err := telegram.ParseChatID(delivery.To)
		if err != nil {
			return "", fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}
		html := telegram.MarkdownToTelegramHTML(text)

		if msgID == "" {
			// First call: send a new message.
			// SendText handles chunking automatically, but for draft streaming
			// we only want a single message to edit later, so truncate to fit.
			if len(html) > telegram.MaxTextLength {
				html = telegram.TruncateDraftHTML(html, telegram.MaxTextLength)
			}
			results, err := telegram.SendText(ctx, client, chatID, html, telegram.SendOptions{
				ParseMode:          "HTML",
				DisableLinkPreview: true,
			})
			if err != nil || len(results) == 0 {
				return "", err
			}
			return strconv.FormatInt(results[0].MessageID, 10), nil
		}

		// Subsequent calls: edit the existing message.
		// Telegram editMessageText has a 4096-char hard limit; truncate to
		// show the tail (most recent streaming output) when text is too long.
		if len(html) > telegram.MaxTextLength {
			html = telegram.TruncateDraftHTML(html, telegram.MaxTextLength)
		}
		editMsgID, err := strconv.ParseInt(msgID, 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid message ID %q: %w", msgID, err)
		}
		_, err = telegram.EditMessageText(ctx, client, chatID, editMsgID, html, "HTML", nil)
		if err != nil {
			// Telegram rejects edits where the content is identical to the
			// current message. This is harmless during draft streaming (the
			// sanitized text may not change between ticks), so treat it as
			// success to avoid noisy warnings in the log.
			if telegram.IsMessageNotModifiedError(err) {
				return msgID, nil
			}
			return msgID, err
		}
		return msgID, nil
	})

	// Set message deleter: removes a previously-sent Telegram message.
	// Used by the chat run goroutine on cancellation (e.g. quick-fire
	// merge) to clean up an orphan streaming draft so the chat doesn't
	// fill up with half-finished responses.
	s.chatHandler.SetMessageDeleter(func(ctx context.Context, delivery *chat.DeliveryContext, msgID string) error {
		if delivery == nil || delivery.Channel != "telegram" || msgID == "" {
			return nil
		}
		client := s.telegramPlug.Client()
		if client == nil {
			return nil
		}
		chatID, err := telegram.ParseChatID(delivery.To)
		if err != nil {
			return fmt.Errorf("invalid chat ID %q: %w", delivery.To, err)
		}
		id, err := strconv.ParseInt(msgID, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid message ID %q: %w", msgID, err)
		}
		return client.DeleteMessage(ctx, chatID, id)
	})

	// Create the inbound processor that routes Telegram messages through
	// the autoreply command/directive pipeline before dispatching to chat.send.
	inbound := NewInboundProcessor(s)

	// Set update handler: routes through autoreply preprocessing → chat.send.
	s.telegramPlug.SetHandler(func(_ context.Context, update *telegram.Update) {
		inbound.HandleTelegramUpdate(update)
	})

	// Register Telegram's per-channel upload limit for the send_file tool.
	s.chatHandler.SetChannelUploadLimit("telegram", s.telegramPlug.MaxUploadBytes())

	s.logger.Debug("telegram chat handler wired")
}

// loadTelegramConfig extracts Telegram channel config from deneb.json.
// Returns nil if Telegram is not configured.
func loadTelegramConfig(_ *config.GatewayRuntimeConfig) *telegram.Config {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil {
		slog.Warn("telegram config: failed to load config", "error", err)
		return nil
	}
	if snapshot == nil || !snapshot.Valid {
		slog.Warn("telegram config: snapshot invalid or nil")
		return nil
	}

	// Extract channels.telegram from raw config JSON.
	if snapshot.Raw == "" {
		slog.Warn("telegram config: snapshot.Raw is empty")
		return nil
	}

	var root struct {
		Channels struct {
			Telegram *telegram.Config `json:"telegram"`
		} `json:"channels"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		slog.Warn("telegram config: JSON unmarshal failed", "error", err)
		return nil
	}
	if root.Channels.Telegram == nil {
		slog.Warn("telegram config: channels.telegram section not found in config")
	}
	return root.Channels.Telegram
}
