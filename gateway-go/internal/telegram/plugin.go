package telegram

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
)

// Plugin implements the channel.Plugin interface for Telegram.
type Plugin struct {
	mu     sync.Mutex
	client *Client
	bot    *Bot
	config *Config
	logger *slog.Logger
	status  channel.Status
	botUser *User
}

// NewPlugin creates a new Telegram channel plugin.
func NewPlugin(cfg *Config, logger *slog.Logger) *Plugin {
	return &Plugin{
		config: cfg,
		logger: logger,
		status: channel.Status{Connected: false},
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
		p.status = channel.Status{Connected: false, Error: "account disabled"}
		return nil
	}

	if p.config.BotToken == "" {
		p.status = channel.Status{Connected: false, Error: "no bot token configured"}
		return nil
	}

	// Create HTTP client with IPv4-fallback transport.
	timeout := time.Duration(p.config.EffectiveTimeout()) * time.Second
	httpClient := NewTelegramHTTPClient(timeout, p.logger)
	if p.config.Proxy != "" {
		proxyURL, err := url.Parse(p.config.Proxy)
		if err != nil {
			p.status = channel.Status{Connected: false, Error: "invalid proxy URL: " + err.Error()}
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
		p.status = channel.Status{Connected: false, Error: "getMe failed: " + err.Error()}
		return nil
	}
	p.logger.Info("telegram bot verified", "username", me.Username, "id", me.ID)
	p.botUser = me

	// Start polling in background.
	p.bot = NewBot(p.client, p.config, nil, p.logger)
	go func() {
		if err := p.bot.Start(ctx); err != nil && ctx.Err() == nil {
			p.logger.Error("telegram polling error", "error", err)
		}
	}()

	p.status = channel.Status{Connected: true}
	return nil
}

// Stop implements channel.Plugin.
func (p *Plugin) Stop(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.bot != nil {
		p.bot.Stop()
	}
	p.status = channel.Status{Connected: false}
	return nil
}

// Status implements channel.Plugin.
func (p *Plugin) Status() channel.Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
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

// Ensure Plugin satisfies the channel.Plugin interface at compile time.
var _ channel.Plugin = (*Plugin)(nil)
