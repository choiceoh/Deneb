package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// PhoneActionFunc delivers a structured phone action to the native app for
// in-app Intent execution — the SSH/Termux-free path (RFC: phone-action). The
// server wires this to the existing push channel (SSE foreground / FCM data
// background); nil means no app channel, so the action is reported unavailable
// rather than silently dropped.
type PhoneActionFunc func(ctx context.Context, action string, args map[string]string) error

// phoneWriteParams is the phone_write tool input. `to` selects the operation:
// the SSH-backed ones (notification/tts/clipboard) stay as-is for now; the
// Intent-backed P1 actions (open_url/open_app/share/message/dial/photo) route
// through PhoneActionFunc to the app.
type phoneWriteParams struct {
	To     string `json:"to"`
	Target string `json:"target"` // url / package / phone number, per action
	Text   string `json:"text"`
	Title  string `json:"title"`
}

// phoneActions is the P1 allowlist: operations the app executes via a plain
// Android Intent (no Accessibility tap-loop). Fixed set — the tool never emits
// an action outside it.
var phoneActions = map[string]bool{
	"open_url": true,
	"open_app": true,
	"share":    true,
	"message":  true,
	"dial":     true,
	"photo":    true,
}

// isPhoneAction reports whether `to` is an Intent-backed P1 action.
func isPhoneAction(to string) bool {
	return phoneActions[strings.ToLower(strings.TrimSpace(to))]
}

// buildPhoneAction validates a request against the allowlist and returns the
// (action, args) command for the app to dispatch. Pure — unit-testable without
// the app or the push channel.
func buildPhoneAction(p phoneWriteParams) (string, map[string]string, error) {
	action := strings.ToLower(strings.TrimSpace(p.To))
	if !phoneActions[action] {
		return "", nil, fmt.Errorf("phone action %q not allowed", action)
	}
	target := strings.TrimSpace(p.Target)
	text := strings.TrimSpace(p.Text)
	args := map[string]string{}
	switch action {
	case "open_url":
		if u, err := url.ParseRequestURI(target); err != nil || u.Scheme == "" {
			return "", nil, fmt.Errorf("open_url needs a valid absolute url in target")
		}
		args["url"] = target
	case "open_app":
		if target == "" {
			return "", nil, fmt.Errorf("open_app needs target (package id or app name)")
		}
		args["package"] = target
	case "share", "message":
		if text == "" {
			return "", nil, fmt.Errorf("%s needs text", action)
		}
		args["text"] = p.Text
		if target != "" {
			args["to"] = target // recipient (number/handle); optional for share
		}
	case "dial":
		if target == "" {
			return "", nil, fmt.Errorf("dial needs target (phone number)")
		}
		args["number"] = target
	case "photo":
		// No args — the app opens the camera capture intent.
	}
	return action, args, nil
}

// dispatchPhoneAction validates and delivers a P1 action via the injected
// sender, returning the agent-facing result string.
func dispatchPhoneAction(ctx context.Context, send PhoneActionFunc, p phoneWriteParams) (string, error) {
	action, args, err := buildPhoneAction(p)
	if err != nil {
		return "", err
	}
	if send == nil {
		return "", fmt.Errorf("phone action %q unavailable: native app channel not wired", action)
	}
	if err := send(ctx, action, args); err != nil {
		return "", fmt.Errorf("phone action %q delivery failed: %w", action, err)
	}
	return fmt.Sprintf("phone action dispatched to app: %s", action), nil
}
