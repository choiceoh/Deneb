package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// MessageHandler is called for each incoming message.
type MessageHandler func(ctx context.Context, msg *Message)

// Bot manages the Discord bot lifecycle: Gateway WebSocket connection,
// heartbeating, and dispatching message events.
type Bot struct {
	client  *Client
	config  *Config
	handler MessageHandler
	logger  *slog.Logger

	mu        sync.Mutex
	running   bool
	stopFunc  context.CancelFunc
	sessionID string
	resumeURL string
	seq       int64
	botUser   *User

	// threadParents caches thread ID → parent channel ID for allowlist checks.
	// Discord threads have their own channel IDs that aren't in the allowlist,
	// so we check the parent channel instead.
	threadParents   map[string]string
	threadParentsMu sync.Mutex
}

// NewBot creates a new Discord bot instance.
func NewBot(client *Client, config *Config, handler MessageHandler, logger *slog.Logger) *Bot {
	return &Bot{
		client:        client,
		config:        config,
		handler:       handler,
		logger:        logger,
		threadParents: make(map[string]string),
	}
}

// Start connects to the Discord Gateway and begins receiving events.
// Blocks until context is cancelled.
func (b *Bot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return nil
	}
	botCtx, cancel := context.WithCancel(ctx)
	b.running = true
	b.stopFunc = cancel
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.running = false
		b.stopFunc = nil
		b.mu.Unlock()
	}()

	b.logger.Info("discord bot starting")
	return b.connectLoop(botCtx)
}

// Stop stops the bot.
func (b *Bot) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopFunc != nil {
		b.stopFunc()
	}
}

// SetHandler replaces the message handler. Safe to call while running.
func (b *Bot) SetHandler(h MessageHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handler = h
}

// BotUser returns the authenticated bot user (set after READY). Returns nil before Start.
func (b *Bot) BotUser() *User {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.botUser
}

// IsRunning returns whether the bot is currently connected.
func (b *Bot) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

// connectLoop manages the Gateway connection with automatic reconnection.
func (b *Bot) connectLoop(ctx context.Context) error {
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		gwURL := GatewayURL
		if b.resumeURL != "" {
			gwURL = b.resumeURL + "?v=10&encoding=json"
		}

		err := b.runGateway(ctx, gwURL)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		b.logger.Warn("discord gateway disconnected, reconnecting",
			"error", err, "backoff", backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		// Exponential backoff with cap.
		backoff = time.Duration(float64(backoff) * 1.5)
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

// runGateway connects to the Gateway, authenticates, and processes events.
func (b *Bot) runGateway(ctx context.Context, gatewayURL string) error {
	conn, _, err := websocket.Dial(ctx, gatewayURL, &websocket.DialOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return fmt.Errorf("dial gateway: %w", err)
	}
	defer conn.CloseNow()

	// Increase read limit for large payloads.
	conn.SetReadLimit(16 * 1024 * 1024)

	// Read Hello.
	hello, err := b.readPayload(ctx, conn)
	if err != nil {
		return fmt.Errorf("read hello: %w", err)
	}
	if hello.Op != OpcodeHello {
		return fmt.Errorf("expected Hello (op 10), got op %d", hello.Op)
	}

	var helloData HelloData
	if err := json.Unmarshal(hello.D, &helloData); err != nil {
		return fmt.Errorf("decode hello: %w", err)
	}

	// Send Identify or Resume.
	if b.sessionID != "" {
		// Resume existing session.
		resumeData := ResumeData{
			Token:     b.config.BotToken,
			SessionID: b.sessionID,
			Seq:       b.seq,
		}
		if err := b.writePayload(ctx, conn, OpcodeResume, resumeData); err != nil {
			return fmt.Errorf("send resume: %w", err)
		}
	} else {
		// Fresh identify with coding bot presence.
		identifyData := IdentifyData{
			Token:   b.config.BotToken,
			Intents: IntentGuilds | IntentGuildMessages | IntentMessageContent | IntentDirectMessages,
			Properties: IdentifyProperties{
				OS:      "linux",
				Browser: "deneb",
				Device:  "deneb",
			},
			Presence: &PresenceUpdate{
				Status: "online",
				Activities: []Activity{
					{Name: "Coding", Type: 0}, // "Playing Coding"
				},
			},
		}
		if err := b.writePayload(ctx, conn, OpcodeIdentify, identifyData); err != nil {
			return fmt.Errorf("send identify: %w", err)
		}
	}

	// Start heartbeat loop.
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()

	go b.heartbeatLoop(heartbeatCtx, conn, helloData.HeartbeatInterval)

	// Event loop.
	for {
		payload, err := b.readPayload(ctx, conn)
		if err != nil {
			return fmt.Errorf("read event: %w", err)
		}

		// Update sequence number.
		if payload.S != nil {
			b.seq = *payload.S
		}

		switch payload.Op {
		case OpcodeDispatch:
			b.handleDispatch(ctx, payload)

		case OpcodeHeartbeat:
			// Server requests immediate heartbeat.
			if err := b.writePayload(ctx, conn, OpcodeHeartbeat, b.seq); err != nil {
				return fmt.Errorf("heartbeat response: %w", err)
			}

		case OpcodeReconnect:
			b.logger.Info("discord gateway requested reconnect")
			conn.Close(websocket.StatusNormalClosure, "reconnect")
			return nil

		case OpcodeInvalidSession:
			b.logger.Warn("discord gateway invalid session, re-identifying")
			b.sessionID = ""
			b.resumeURL = ""
			b.seq = 0
			conn.Close(websocket.StatusNormalClosure, "invalid session")
			return nil

		case OpcodeHeartbeatAck:
			// Expected, no action needed.
		}
	}
}

// handleDispatch processes dispatch events (op 0).
func (b *Bot) handleDispatch(ctx context.Context, payload *GatewayPayload) {
	switch payload.T {
	case "READY":
		var ready ReadyData
		if err := json.Unmarshal(payload.D, &ready); err != nil {
			b.logger.Error("decode READY", "error", err)
			return
		}
		b.mu.Lock()
		b.sessionID = ready.SessionID
		b.resumeURL = ready.ResumeURL
		b.botUser = ready.User
		b.mu.Unlock()
		b.logger.Info("discord bot ready",
			"username", ready.User.Username,
			"id", ready.User.ID,
			"sessionId", ready.SessionID)

	case "MESSAGE_CREATE":
		var msg Message
		if err := json.Unmarshal(payload.D, &msg); err != nil {
			b.logger.Error("decode MESSAGE_CREATE", "error", err)
			return
		}

		// Ignore bot's own messages.
		if msg.Author != nil && msg.Author.Bot {
			return
		}

		// Check access control. For threads, check the parent channel.
		if !b.isChannelOrThreadAllowed(msg.ChannelID) {
			return
		}
		if msg.Author != nil && !b.config.IsUserAllowed(msg.Author.ID) {
			return
		}

		// Mention gating: if requireMention is set, only respond when @mentioned.
		if b.config.RequireMention {
			botUser := b.BotUser()
			if botUser == nil {
				return
			}
			mentionTag := "<@" + botUser.ID + ">"
			if !strings.Contains(msg.Content, mentionTag) {
				return
			}
			// Strip mention from message content.
			msg.Content = strings.TrimSpace(strings.ReplaceAll(msg.Content, mentionTag, ""))
			if msg.Content == "" {
				return
			}
		}

		// Dispatch to handler asynchronously.
		b.mu.Lock()
		handler := b.handler
		b.mu.Unlock()
		if handler != nil {
			go handler(ctx, &msg)
		}

	case "THREAD_CREATE":
		// Cache thread → parent channel mapping for allowlist checks.
		var ch Channel
		if err := json.Unmarshal(payload.D, &ch); err == nil && ch.ParentID != "" {
			b.threadParentsMu.Lock()
			b.threadParents[ch.ID] = ch.ParentID
			b.threadParentsMu.Unlock()
		}
	}
}

// isChannelOrThreadAllowed checks if a channel (or its parent for threads) is allowed.
func (b *Bot) isChannelOrThreadAllowed(channelID string) bool {
	if b.config.IsChannelAllowed(channelID) {
		return true
	}
	// Check if this is a thread whose parent channel is allowed.
	b.threadParentsMu.Lock()
	parentID, ok := b.threadParents[channelID]
	b.threadParentsMu.Unlock()
	if ok {
		return b.config.IsChannelAllowed(parentID)
	}
	// If no allowlist is configured, allow all (IsChannelAllowed already returned true above).
	return false
}

// heartbeatLoop sends periodic heartbeats.
func (b *Bot) heartbeatLoop(ctx context.Context, conn *websocket.Conn, intervalMs int) {
	// Add jitter to first heartbeat.
	jitter := time.Duration(rand.IntN(intervalMs)) * time.Millisecond
	select {
	case <-ctx.Done():
		return
	case <-time.After(jitter):
	}

	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if err := b.writePayload(ctx, conn, OpcodeHeartbeat, b.seq); err != nil {
			b.logger.Warn("heartbeat send failed", "error", err)
			return
		}
	}
}

// readPayload reads and decodes a Gateway payload.
func (b *Bot) readPayload(ctx context.Context, conn *websocket.Conn) (*GatewayPayload, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	var payload GatewayPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	return &payload, nil
}

// writePayload encodes and sends a Gateway payload.
func (b *Bot) writePayload(ctx context.Context, conn *websocket.Conn, op int, d any) error {
	payload := GatewayPayload{Op: op}
	if d != nil {
		data, err := json.Marshal(d)
		if err != nil {
			return fmt.Errorf("marshal payload data: %w", err)
		}
		payload.D = data
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	return conn.Write(ctx, websocket.MessageText, raw)
}
