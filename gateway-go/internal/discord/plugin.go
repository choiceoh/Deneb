package discord

import (
	"context"
	"log/slog"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
)

// Plugin implements the channel.Plugin interface for Discord.
type Plugin struct {
	mu      sync.Mutex
	client  *Client
	bot     *Bot
	config  *Config
	logger  *slog.Logger
	status  channel.Status
	botUser *User
	handler MessageHandler
}

// NewPlugin creates a new Discord channel plugin.
func NewPlugin(cfg *Config, logger *slog.Logger) *Plugin {
	return &Plugin{
		config: cfg,
		logger: logger,
		status: channel.Status{Connected: false},
	}
}

// ID implements channel.Plugin.
func (p *Plugin) ID() string { return "discord" }

// Meta implements channel.Plugin.
func (p *Plugin) Meta() channel.Meta {
	return channel.Meta{
		ID:    "discord",
		Label: "Discord",
		Blurb: "Discord coding channel",
	}
}

// Capabilities implements channel.Plugin.
func (p *Plugin) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		ChatTypes:      []string{"guild_text", "dm"},
		Reactions:      true,
		Edit:           true,
		Reply:          true,
		Threads:        true,
		Media:          true,
		BlockStreaming: true,
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

	p.client = NewClient(p.config.BotToken, p.logger)

	// Verify bot token.
	me, err := p.client.GetCurrentUser(ctx)
	if err != nil {
		p.status = channel.Status{Connected: false, Error: "token verification failed: " + err.Error()}
		return nil
	}
	p.logger.Info("discord bot verified", "username", me.Username, "id", me.ID)
	p.botUser = me

	// Start Gateway connection in background.
	p.bot = NewBot(p.client, p.config, p.handler, p.logger)
	go func() {
		if err := p.bot.Start(context.Background()); err != nil {
			if err != context.Canceled {
				p.logger.Error("discord bot error", "error", err)
			}
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

// Client returns the underlying Discord REST API client.
func (p *Plugin) Client() *Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.client
}

// SetHandler sets the message handler. If the bot is already running,
// the handler is applied immediately.
func (p *Plugin) SetHandler(h MessageHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handler = h
	if p.bot != nil {
		p.bot.SetHandler(h)
	}
}

// BotUser returns the verified bot user. Returns nil before Start.
func (p *Plugin) BotUser() *User {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.botUser
}

// Config returns the plugin's configuration.
func (p *Plugin) Config() *Config {
	return p.config
}

// Ensure Plugin satisfies the channel.Plugin interface at compile time.
var _ channel.Plugin = (*Plugin)(nil)
