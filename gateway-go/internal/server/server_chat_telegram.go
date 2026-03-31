// Telegram chat handler wiring and configuration.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

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
		dedupKey := delivery.To + ":" + truncateForDedup(text, 200)
		recentMu.Lock()
		if sentAt, dup := recentSends[dedupKey]; dup && time.Since(sentAt) < recentTTL {
			recentMu.Unlock()
			s.logger.Info("suppressed duplicate reply to telegram",
				"chatId", delivery.To, "textLen", len(text))
			return nil
		}
		// Evict stale entries (cheap, single-user so map stays tiny).
		for k, t := range recentSends {
			if time.Since(t) >= recentTTL {
				delete(recentSends, k)
			}
		}
		recentSends[dedupKey] = time.Now()
		recentMu.Unlock()

		// Parse optional button directive from agent reply.
		cleanText, keyboard := parseReplyButtons(text)
		opts := telegram.SendOptions{ParseMode: "HTML", Keyboard: keyboard}
		html := telegram.MarkdownToTelegramHTML(cleanText)
		_, err = telegram.SendText(ctx, client, chatID, html, opts)
		return err
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

	// Set remove-reaction function: clears the status emoji when transitioning
	// between agent phases (Telegram replaces reactions, so passing "" removes).
	s.chatHandler.SetRemoveReactionFunc(func(ctx context.Context, delivery *chat.DeliveryContext, emoji string) error {
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
		return client.SetMessageReaction(ctx, chatID, msgID, "")
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

	s.logger.Info("telegram chat handler wired (with autoreply preprocessing)")
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
