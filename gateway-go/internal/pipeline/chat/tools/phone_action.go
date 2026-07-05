package tools

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
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

// phoneActions is the P1 allowlist: operations the app executes in-process —
// plain Android Intents (no Accessibility tap-loop) plus the app-permission
// successors of the retired SSH ops (notify=NotificationManager,
// speak=TTS engine, clipboard=ClipboardManager set, sync_state=push fresh
// location+battery+usage state). Fixed set — the tool never emits an action outside it.
var phoneActions = map[string]bool{
	"open_url":   true,
	"open_app":   true,
	"share":      true,
	"message":    true,
	"dial":       true,
	"photo":      true,
	"notify":     true,
	"speak":      true,
	"clipboard":  true,
	"sync_state": true,
	// alarm/timer ride the clock app's public intents (ACTION_SET_ALARM /
	// ACTION_SET_TIMER — normal-level permission, auto-granted).
	"alarm": true,
	"timer": true,
}

// isPhoneAction reports whether `to` is a P1 app action — Intent-backed
// (open_url, dial, ...) or in-app (notify, speak, clipboard, sync_state).
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
	case "notify":
		if text == "" {
			return "", nil, fmt.Errorf("notify needs text")
		}
		title := strings.TrimSpace(p.Title)
		if title == "" {
			title = "Deneb"
		}
		args["title"] = title
		args["text"] = text
	case "speak", "clipboard":
		if text == "" {
			return "", nil, fmt.Errorf("%s needs text", action)
		}
		args["text"] = text
	case "sync_state":
		// No args — the app pushes fresh location+battery+usage state.
	case "alarm":
		hh, mm, err := parseAlarmTime(target)
		if err != nil {
			return "", nil, err
		}
		args["hour"] = fmt.Sprintf("%d", hh)
		args["minute"] = fmt.Sprintf("%d", mm)
		if text != "" {
			args["label"] = text
		}
	case "timer":
		secs, err := parseTimerSeconds(target)
		if err != nil {
			return "", nil, err
		}
		args["seconds"] = fmt.Sprintf("%d", secs)
		if text != "" {
			args["label"] = text
		}
	}
	return action, args, nil
}

// alarmTimeRe matches a strict 24h "HH:MM" / "H:MM" — no trailing junk.
var alarmTimeRe = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)

// parseAlarmTime parses a 24h "HH:MM" (or "H:MM") alarm time.
func parseAlarmTime(s string) (hour, minute int, err error) {
	m := alarmTimeRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, fmt.Errorf(`alarm target %q must be 24h "HH:MM" (예: "07:00")`, s)
	}
	hour, _ = strconv.Atoi(m[1])
	minute, _ = strconv.Atoi(m[2])
	if hour > 23 || minute > 59 {
		return 0, 0, fmt.Errorf("alarm target %q out of range (00:00–23:59)", s)
	}
	return hour, minute, nil
}

// parseTimerSeconds parses a timer duration: Go-style ("10m", "90s", "1h30m")
// or a bare number meaning minutes ("10" → 600s). Bounded to 24h.
func parseTimerSeconds(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf(`timer needs target as a duration (예: "10m", "90s", "1h30m", 분 단위 숫자)`)
	}
	if mins, err := strconv.Atoi(s); err == nil {
		s = fmt.Sprintf("%dm", mins)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf(`timer target %q must be a duration (예: "10m", "90s", "1h30m")`, s)
	}
	if d < time.Second || d > 24*time.Hour {
		return 0, fmt.Errorf("timer target %q out of range (1s–24h)", s)
	}
	return int(d / time.Second), nil
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
