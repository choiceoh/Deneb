package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// OutboundMessage represents a message to be sent through Telegram.
type OutboundMessage struct {
	To      string `json:"to"`
	Text    string `json:"text"`
	ReplyTo string `json:"replyTo,omitempty"`
	// Media is a list of media attachment URLs or paths.
	Media []string `json:"media,omitempty"`
}

// Plugin implements the Telegram Bot API channel.
type Plugin struct {
	statusMu  sync.Mutex // protects status
	status    Status
	mu        sync.Mutex // protects client, bot, botUser, handler
	client    *Client
	bot       *Bot
	config    *Config
	logger    *slog.Logger
	botUser   *User
	handler   UpdateHandler // stored until bot is created in Start()
	startedAt int64         // unix ms when Start() succeeded; 0 if not started
}

// NewPlugin creates a new Telegram channel plugin.
func NewPlugin(cfg *Config, logger *slog.Logger) *Plugin {
	return &Plugin{
		config: cfg,
		logger: logger,
	}
}

// ID returns the channel identifier.
func (p *Plugin) ID() string { return "telegram" }

// Status returns the plugin's current connection status.
func (p *Plugin) Status() Status {
	p.statusMu.Lock()
	defer p.statusMu.Unlock()
	return p.status
}

// SetStatus updates the plugin's connection status. Safe to call from any goroutine.
func (p *Plugin) SetStatus(s Status) {
	p.statusMu.Lock()
	defer p.statusMu.Unlock()
	p.status = s
}

// Capabilities returns the capabilities of the Telegram channel.
type Capabilities struct {
	ChatTypes       []string `json:"chatTypes"`
	Polls           bool     `json:"polls,omitempty"`
	Reactions       bool     `json:"reactions,omitempty"`
	Edit            bool     `json:"edit,omitempty"`
	Unsend          bool     `json:"unsend,omitempty"`
	Reply           bool     `json:"reply,omitempty"`
	Effects         bool     `json:"effects,omitempty"`
	GroupManagement bool     `json:"groupManagement,omitempty"`
	Threads         bool     `json:"threads,omitempty"`
	Media           bool     `json:"media,omitempty"`
	NativeCommands  bool     `json:"nativeCommands,omitempty"`
	BlockStreaming  bool     `json:"blockStreaming,omitempty"`
}

// Capabilities returns Telegram's capabilities.
func (p *Plugin) Capabilities() Capabilities {
	return Capabilities{
		ChatTypes:      []string{"private", "group", "supergroup"},
		Media:          true,
		Threads:        true,
		Edit:           true,
		Reply:          true,
		BlockStreaming: !p.config.IsBlockStreamingDisabled(),
	}
}

// Start initializes the Telegram bot and begins long-polling.
func (p *Plugin) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.config.IsEnabled() {
		p.SetStatus(Status{Connected: false, Error: "account disabled"})
		return nil
	}

	if p.config.BotToken == "" {
		p.SetStatus(Status{Connected: false, Error: "no bot token configured"})
		return nil
	}

	// Create HTTP client with IPv4-fallback transport.
	// HTTP client timeout must exceed the long-poll timeout (DefaultPollTimeout)
	// to prevent the client from killing the connection before Telegram responds.
	timeout := time.Duration(p.config.EffectiveTimeout()+DefaultPollTimeout+15) * time.Second
	httpClient := NewTelegramHTTPClient(timeout, p.logger)
	if p.config.Proxy != "" {
		proxyURL, err := url.Parse(p.config.Proxy)
		if err != nil {
			p.SetStatus(Status{Connected: false, Error: "invalid proxy URL: " + err.Error()})
			return nil
		}
		// Overlay proxy on the existing transport.
		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	clientCfg := ClientConfig{
		Token:      p.config.BotToken,
		HTTPClient: httpClient,
		Logger:     p.logger,
	}
	// TELEGRAM_API_BASE allows pointing the client at a mock server for testing.
	if base := os.Getenv("TELEGRAM_API_BASE"); base != "" {
		clientCfg.BaseURL = base + p.config.BotToken
		p.logger.Info("using custom telegram API base", "base", base)
	}
	p.client = NewClient(clientCfg)

	// Verify bot token.
	me, err := p.client.GetMe(ctx)
	if err != nil {
		p.SetStatus(Status{Connected: false, Error: "getMe failed: " + err.Error()})
		return nil
	}
	p.botUser = me

	// Start polling in background.
	// Use a detached context so polling survives beyond the RPC request that triggered Start.
	p.bot = NewBot(p.client, p.config, p.handler, p.logger)
	go func() { //nolint:gosec // G118 — intentionally detached context so polling survives beyond the triggering RPC request
		if err := p.bot.Start(context.Background()); err != nil {
			if errors.Is(err, context.Canceled) {
				p.logger.Info("telegram polling stopped")
			} else {
				p.logger.Error("telegram polling error", "error", err)
			}
		}
	}()

	p.startedAt = time.Now().UnixMilli()
	p.SetStatus(Status{Connected: true})
	return nil
}

// Stop stops the Telegram bot polling.
func (p *Plugin) Stop(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.bot != nil {
		p.bot.Stop()
	}
	p.SetStatus(Status{Connected: false})
	return nil
}

// StartedAt returns the unix ms timestamp when Start() last succeeded.
// Returns 0 if the plugin has never successfully started.
func (p *Plugin) StartedAt() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startedAt
}

// Client returns the underlying Telegram API client (for RPC methods).
func (p *Plugin) Client() *Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.client
}

// Bot returns the underlying bot instance (for poll draining).
func (p *Plugin) Bot() *Bot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bot
}

// SetHandler sets the update handler. If the bot is already running, the
// handler is applied immediately. Otherwise it is stored and applied when
// Start() creates the bot.
func (p *Plugin) SetHandler(h UpdateHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handler = h
	if p.bot != nil {
		p.bot.SetHandler(h)
	}
}

// BotUser returns the verified bot user (set after Start). Returns nil before Start.
func (p *Plugin) BotUser() *User {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.botUser
}

// Config returns the plugin's configuration. The returned pointer is read-only.
func (p *Plugin) Config() *Config {
	return p.config
}

// PrimaryChatID returns the operator's DM chat ID from config, for use as a
// default delivery target (e.g. cron output). Returns "" if none configured.
func (p *Plugin) PrimaryChatID() string {
	if p.config == nil || p.config.ChatID == 0 {
		return ""
	}
	return fmt.Sprintf("%d", p.config.ChatID)
}

// BotUserID returns the bot's user ID, or 0 if not yet verified.
func (p *Plugin) BotUserID() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.botUser == nil {
		return 0
	}
	return p.botUser.ID
}

// MaxUploadBytes returns the maximum upload size (50 MB for Telegram Bot API).
func (p *Plugin) MaxUploadBytes() int64 { return 50 * 1024 * 1024 }

// SendMessage delivers a text message (with optional media) to the given Telegram chat ID.
func (p *Plugin) SendMessage(ctx context.Context, msg OutboundMessage) error {
	p.mu.Lock()
	c := p.client
	p.mu.Unlock()

	if c == nil {
		return errors.New("telegram client not initialized")
	}

	chatID, err := ParseChatID(msg.To)
	if err != nil {
		return fmt.Errorf("invalid chat ID %q: %w", msg.To, err)
	}

	// Send text message.
	if msg.Text != "" {
		_, err := SendText(ctx, c, chatID, msg.Text, SendOptions{
			ParseMode: "HTML",
		})
		if err != nil {
			return fmt.Errorf("send text: %w", err)
		}
	}

	// Send media attachments (URLs or file_ids).
	for _, media := range msg.Media {
		if _, err := SendDocument(ctx, c, chatID, media, "", SendOptions{}); err != nil {
			return fmt.Errorf("send media: %w", err)
		}
	}

	return nil
}
