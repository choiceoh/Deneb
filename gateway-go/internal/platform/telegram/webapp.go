package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// MenuButton kinds supported by setMenuButton.
const (
	MenuButtonKindDefault  = "default"
	MenuButtonKindCommands = "commands"
	MenuButtonKindWebApp   = "web_app"
)

// SetMenuButtonWebApp configures the chat menu button (the icon left of the
// message composer) to open a Telegram WebApp at the given URL.
//
// chatID 0 sets the bot-wide default (applies to every private chat that
// does not have a per-chat override).
//
// text is the localized label shown on the menu button (max 64 chars per
// Bot API spec). Keep it short — "Deneb" is the usual default.
//
// webAppURL MUST be served over HTTPS on a domain registered with
// @BotFather (`/setdomain`). Telegram refuses to launch otherwise.
func (c *Client) SetMenuButtonWebApp(ctx context.Context, chatID int64, text, webAppURL string) error {
	if text == "" {
		return errors.New("telegram: menu button text required")
	}
	if err := validateWebAppURL(webAppURL); err != nil {
		return err
	}

	params := map[string]any{
		"menu_button": map[string]any{
			"type": MenuButtonKindWebApp,
			"text": text,
			"web_app": map[string]any{
				"url": webAppURL,
			},
		},
	}
	if chatID != 0 {
		params["chat_id"] = chatID
	}

	_, err := c.CallIdempotent(ctx, "setMenuButton", params)
	return err
}

// SetMenuButtonDefault resets the chat menu button to the bot's default
// behaviour (typically the commands list). Used when the operator disables
// the mini app — clears the WebApp launcher icon.
func (c *Client) SetMenuButtonDefault(ctx context.Context, chatID int64) error {
	params := map[string]any{
		"menu_button": map[string]any{
			"type": MenuButtonKindDefault,
		},
	}
	if chatID != 0 {
		params["chat_id"] = chatID
	}
	_, err := c.CallIdempotent(ctx, "setMenuButton", params)
	return err
}

// WebAppButton describes a single inline keyboard button that launches a
// WebApp. Mirrors Telegram's InlineKeyboardButton.web_app variant.
type WebAppButton struct {
	Text string
	URL  string
}

// WebAppKeyboard builds the Telegram API inline_keyboard structure from
// rows of WebAppButton. Each button launches the embedded mini app at the
// given URL; clicks do NOT generate callback_query updates.
//
// Returns nil for empty input so callers can pass the result through
// without nil checks (mirrors InlineKeyboard).
func WebAppKeyboard(rows [][]WebAppButton) ([][]map[string]any, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([][]map[string]any, 0, len(rows))
	for r, row := range rows {
		kbRow := make([]map[string]any, 0, len(row))
		for c, btn := range row {
			if btn.Text == "" {
				return nil, fmt.Errorf("telegram: web_app button [%d][%d] missing text", r, c)
			}
			if err := validateWebAppURL(btn.URL); err != nil {
				return nil, fmt.Errorf("telegram: web_app button [%d][%d]: %w", r, c, err)
			}
			kbRow = append(kbRow, map[string]any{
				"text":    btn.Text,
				"web_app": map[string]any{"url": btn.URL},
			})
		}
		out = append(out, kbRow)
	}
	return out, nil
}

// validateWebAppURL enforces the HTTPS-only rule. Telegram clients silently
// refuse to launch WebApps over plain HTTP, so a misconfigured URL would
// fail at runtime in a user-visible way. Bail out early at the API boundary.
func validateWebAppURL(raw string) error {
	if raw == "" {
		return errors.New("telegram: web_app URL required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("telegram: invalid web_app URL: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("telegram: web_app URL must be https (got %q)", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("telegram: web_app URL missing host")
	}
	return nil
}
