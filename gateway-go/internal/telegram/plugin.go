package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
)

// Plugin implements the channel.Plugin interface for Telegram.
type Plugin struct {
	channel.PluginBase          // status field + Status() + SetStatus()
	mu      sync.Mutex          // protects client, bot, botUser, handler
	client  *Client
	bot     *Bot
	config  *Config
	logger  *slog.Logger
	botUser *User
	handler UpdateHandler // stored until bot is created in Start()
}

// NewPlugin creates a new Telegram channel plugin.
func NewPlugin(cfg *Config, logger *slog.Logger) *Plugin {
	return &Plugin{
		config: cfg,
		logger: logger,
	}
}

// ID implements channel.Plugin.
func (p *Plugin) ID() string { return "telegram" }

// Meta implements channel.Plugin.
func (p *Plugin) Meta() channel.Meta {
	return channel.Meta{
		ID:    "telegram",
		Label: "Telegram",
		Blurb: "Telegram Bot API channel",
	}
}

// Capabilities implements channel.Plugin.
func (p *Plugin) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		ChatTypes:      []string{"private", "group", "supergroup"},
		Media:          true,
		Threads:        true,
		Edit:           true,
		Reply:          true,
		BlockStreaming: !p.config.IsBlockStreamingDisabled(),
	}
}

// Start implements channel.Plugin.
func (p *Plugin) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.config.IsEnabled() {
		p.SetStatus(channel.Status{Connected: false, Error: "account disabled"})
		return nil
	}

	if p.config.BotToken == "" {
		p.SetStatus(channel.Status{Connected: false, Error: "no bot token configured"})
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
			p.SetStatus(channel.Status{Connected: false, Error: "invalid proxy URL: " + err.Error()})
			return nil
		}
		// Overlay proxy on the existing transport.
		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	p.client = NewClient(ClientConfig{
		Token:      p.config.BotToken,
		HTTPClient: httpClient,
		Logger:     p.logger,
	})

	// Verify bot token.
	me, err := p.client.GetMe(ctx)
	if err != nil {
		p.SetStatus(channel.Status{Connected: false, Error: "getMe failed: " + err.Error()})
		return nil
	}
	p.logger.Info("telegram bot verified")
	p.botUser = me

	// Start polling in background.
	// Use a detached context so polling survives beyond the RPC request that triggered Start.
	p.bot = NewBot(p.client, p.config, p.handler, p.logger)
	go func() {
		if err := p.bot.Start(context.Background()); err != nil {
			if errors.Is(err, context.Canceled) {
				p.logger.Info("telegram polling stopped")
			} else {
				p.logger.Error("telegram polling error", "error", err)
			}
		}
	}()

	p.SetStatus(channel.Status{Connected: true})
	return nil
}

// Stop implements channel.Plugin.
func (p *Plugin) Stop(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.bot != nil {
		p.bot.Stop()
	}
	p.SetStatus(channel.Status{Connected: false})
	return nil
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

// BotUserID returns the bot's user ID, or 0 if not yet verified.
func (p *Plugin) BotUserID() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.botUser == nil {
		return 0
	}
	return p.botUser.ID
}

// MaxUploadBytes implements channel.FileUploadAdapter.
// Telegram Bot API allows up to 50 MB per file upload.
func (p *Plugin) MaxUploadBytes() int64 { return 50 * 1024 * 1024 }

// SendMessage implements channel.MessagingAdapter.
// Delivers a text message (with optional media) to the given Telegram chat ID.
func (p *Plugin) SendMessage(ctx context.Context, msg channel.OutboundMessage) error {
	p.mu.Lock()
	c := p.client
	p.mu.Unlock()

	if c == nil {
		return errors.New("telegram client not initialized")
	}

	chatID, err := parseChatID(msg.To)
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

// parseChatID parses a string chat ID to int64.
func parseChatID(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}

// Ensure Plugin satisfies the channel.Plugin interface at compile time.
var _ channel.Plugin = (*Plugin)(nil)
var _ channel.FileUploadAdapter = (*Plugin)(nil)
var _ channel.MessagingAdapter = (*Plugin)(nil)
