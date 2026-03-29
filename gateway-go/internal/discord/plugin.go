package discord

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
)

// Plugin implements the channel.Plugin interface for Discord.
type Plugin struct {
	channel.PluginBase          // status field + Status() + SetStatus()
	mu      sync.Mutex          // protects client, bot, botUser, handler
	client  *Client
	bot     *Bot
	config  *Config
	logger  *slog.Logger
	botUser *User
	handler MessageHandler
}

// NewPlugin creates a new Discord channel plugin.
func NewPlugin(cfg *Config, logger *slog.Logger) *Plugin {
	return &Plugin{
		config: cfg,
		logger: logger,
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
		p.SetStatus(channel.Status{Connected: false, Error: "account disabled"})
		return nil
	}

	if err := p.config.Validate(); err != nil {
		p.SetStatus(channel.Status{Connected: false, Error: err.Error()})
		return nil
	}

	p.client = NewClient(p.config.BotToken, p.logger)

	// Verify bot token.
	me, err := p.client.GetCurrentUser(ctx)
	if err != nil {
		p.SetStatus(channel.Status{Connected: false, Error: "token verification failed: " + err.Error()})
		return nil
	}
	p.logger.Info("discord bot verified", "username", me.Username, "id", me.ID)
	p.botUser = me

	// Start Gateway connection in background.
	p.bot = NewBot(p.client, p.config, p.handler, p.logger)
	go func() {
		if err := p.bot.Start(context.Background()); err != nil {
			if !errors.Is(err, context.Canceled) {
				p.logger.Error("discord bot error", "error", err)
				p.SetStatus(channel.Status{Connected: false, Error: "gateway disconnected: " + err.Error()})
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

// SetInteractionHandler sets the handler for button clicks and slash commands.
func (p *Plugin) SetInteractionHandler(h InteractionHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bot != nil {
		p.bot.SetInteractionHandler(h)
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

// MaxUploadBytes implements channel.FileUploadAdapter.
// Discord's file upload limit is 25 MB for standard servers.
// Boosted servers allow up to 50 MB (level 2) or 100 MB (level 3),
// but 25 MB is the safe baseline for all guilds.
func (p *Plugin) MaxUploadBytes() int64 { return 25 * 1024 * 1024 }

// Ensure Plugin satisfies the channel.Plugin interface at compile time.
var _ channel.Plugin = (*Plugin)(nil)
var _ channel.FileUploadAdapter = (*Plugin)(nil)
