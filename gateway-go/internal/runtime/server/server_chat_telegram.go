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
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/secretref"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

// telegramDedupKey builds a dedup cache key from chat ID and the *full* reply
// text. Using a content hash (instead of a byte prefix) avoids two different
// long replies that happen to share an opening header from collapsing into one
// dedup bucket — and sidesteps UTF-8 boundary issues of a byte-length cut.
func telegramDedupKey(chatID, text string) string {
	h := sha256.Sum256([]byte(text))
	return chatID + ":" + hex.EncodeToString(h[:8])
}

// deliveryThreadID extracts the int64 thread ID from a delivery context, or 0
// when the field is empty or unparseable. Bot API treats 0 as "no thread"
// (message lands in the chat's General topic for forums or the main feed
// otherwise), so silently returning 0 on a bad value is the safe default.
func deliveryThreadID(delivery *chat.DeliveryContext) int64 {
	if delivery == nil || delivery.ThreadID == "" {
		return 0
	}
	id, _ := strconv.ParseInt(delivery.ThreadID, 10, 64)
	return id
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

		// Egress redaction runs BEFORE chunking. SendText/EditMessageText each
		// redact internally, but our ChunkHTML below splits the HTML up-front
		// and feeds chunks to SendHTMLChunks which only redacts per chunk — a
		// secret straddling a chunk boundary would otherwise leave the gateway
		// in raw form. Redacting here first collapses any matched token into
		// its masked shape so no boundary leak is possible. The per-chunk pass
		// inside SendHTMLChunks is idempotent and acts as defense in depth.
		html = redact.String(html)

		// Pre-chunk so the draft edit can never fail purely from length. When
		// the reply fits in a single Telegram message we keep the original
		// one-edit fast path (no flicker, no extra messages). When it exceeds
		// the limit the draft edits to chunk[0] and the remainder appends as
		// new messages — also flicker-free, replacing the old delete-then-
		// resend path that the user observed as "answer disappeared, different
		// answer reappeared".
		//
		// Length check is byte-based via len(). Telegram's 4096 hard limit is
		// nominally UTF-16 code units; len() is conservative for non-ASCII
		// (e.g. Korean text uses 3 bytes per BMP character), so we may chunk
		// slightly more eagerly than strictly required — matching how
		// SendText/ChunkHTML have always sized outbound messages.
		var chunks []string
		if len(html) <= telegram.MaxTextLength {
			chunks = []string{html}
		} else {
			chunks = telegram.ChunkHTML(html, telegram.TextChunkLimit)
		}

		// Keyboard always rides the final visible message. For single-chunk
		// replies that's the draft edit; for multi-chunk it's the last append.
		var editKeyboard, tailKeyboard *telegram.InlineKeyboardMarkup
		if len(chunks) == 1 {
			editKeyboard = keyboard
		} else {
			tailKeyboard = keyboard
		}

		// If a draft streaming message exists, edit it in-place with the
		// first chunk instead of deleting and sending a new message.
		if delivery.DraftMsgID != "" {
			draftID := delivery.DraftMsgID
			delivery.DraftMsgID = "" // consumed
			editMsgID, parseErr := strconv.ParseInt(draftID, 10, 64)
			if parseErr == nil {
				_, finalMode, editErr := telegram.EditMessageTextResolved(ctx, client, chatID, editMsgID, chunks[0], "HTML", editKeyboard)
				if editErr == nil || telegram.IsMessageNotModifiedError(editErr) {
					// Draft now shows chunks[0]; append the rest (if any).
					// finalMode tracks whether the edit fell back to plain so
					// the tail chunks render in the same mode as the head.
					if len(chunks) > 1 {
						if _, sendErr := telegram.SendHTMLChunks(ctx, client, chatID, chunks[1:], tailKeyboard, finalMode); sendErr != nil {
							// Partial delivery: chunks[0] is already on the
							// draft message, some tail chunks reached
							// Telegram, some did not. Returning the error
							// would bubble up to the caller's retry which —
							// because DraftMsgID is already consumed — would
							// fall through to SendText and resend the FULL
							// reply, leaving the edited draft plus a
							// duplicate copy. Log + treat as delivered so
							// the user keeps the partial reply intact; the
							// recordSent below also blocks a same-text
							// retry from re-entering the dedup window.
							s.logger.Error("partial reply: tail chunks failed",
								"msgId", draftID, "totalChunks", len(chunks),
								"error", sendErr)
						}
					}
					recordSent()
					return nil
				}
				// Edit failed for a non-length reason (HTML parse fallback
				// already attempted inside EditMessageTextResolved, transient
				// API error, etc.). Delete the draft and fall through to the
				// full SendText path so the user still gets the reply.
				// Draft ordering is guaranteed because DraftStreamLoop.StopForClear
				// (called via run_exec.go deferred cleanup) blocks on any in-flight
				// draft edit before this replyFunc is invoked.
				s.logger.Warn("draft edit failed, falling back to new message",
					"msgId", draftID, "error", editErr)
				if delErr := client.DeleteMessage(ctx, chatID, editMsgID); delErr != nil {
					// User may briefly see both stale draft + new message.
					// Log so operator knows why a duplicate appeared.
					s.logger.Warn("draft cleanup after failed edit also failed",
						"msgId", draftID, "error", delErr)
				}
			}
		}

		opts := telegram.SendOptions{ParseMode: "HTML", Keyboard: keyboard, ThreadID: deliveryThreadID(delivery)}
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
		opts := telegram.SendOptions{DisableNotification: silent, ThreadID: deliveryThreadID(delivery)}

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
		return client.SendChatAction(ctx, chatID, deliveryThreadID(delivery), "typing")
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
				ThreadID:           deliveryThreadID(delivery),
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
	return resolveTelegramSecretRefs(context.Background(), root.Channels.Telegram, slog.Default())
}

func resolveTelegramSecretRefs(ctx context.Context, cfg *telegram.Config, logger *slog.Logger) *telegram.Config {
	return resolveTelegramSecretRefsWith(ctx, cfg, logger, secretref.ResolveRequired)
}

func resolveTelegramSecretRefsWith(ctx context.Context, cfg *telegram.Config, logger *slog.Logger, resolve func(context.Context, string) (string, error)) *telegram.Config {
	if cfg == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	ref := strings.TrimSpace(os.ExpandEnv(cfg.BotTokenRef))
	if ref == "" {
		return cfg
	}
	token, err := resolve(ctx, ref)
	if err != nil {
		logger.Warn("telegram bot token reference resolution failed", "error", err)
		cfg.BotToken = ""
		return cfg
	}
	cfg.BotToken = strings.TrimSpace(token)
	logger.Info("resolved telegram bot token reference")
	return cfg
}
