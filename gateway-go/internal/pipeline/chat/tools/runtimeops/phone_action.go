package runtimeops

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

// phoneWriteParams is the phone_write tool input. `to` selects the operation:
// the app-permission ops (notify/speak/clipboard) run via platform services;
// the Intent-backed actions (open_url/open_app/share/message/dial/photo/
// alarm/timer) route through PhoneActionFunc to the app.
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
		args["hour"] = strconv.Itoa(hh)
		args["minute"] = strconv.Itoa(mm)
		if text != "" {
			args["label"] = text
		}
	case "timer":
		secs, err := parseTimerSeconds(target)
		if err != nil {
			return "", nil, err
		}
		args["seconds"] = strconv.Itoa(secs)
		if text != "" {
			args["label"] = text
		}
	}
	return action, args, nil
}

// alarmTimeRe matches a 24h clock time "H:MM"/"HH:MM"/"H:M" — no trailing junk.
// Minute accepts one or two digits (symmetric with the hour) so a model
// emitting "7:5" for 07:05 still lands.
var alarmTimeRe = regexp.MustCompile(`^(\d{1,2}):(\d{1,2})$`)

// parseAlarmTime parses a 24h clock time for the alarm action.
func parseAlarmTime(s string) (hour, minute int, err error) {
	m := alarmTimeRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, fmt.Errorf(`alarm target %q must be a 24h clock time like "07:00"`, s)
	}
	hour, _ = strconv.Atoi(m[1])
	minute, _ = strconv.Atoi(m[2])
	if hour > 23 || minute > 59 {
		return 0, 0, fmt.Errorf("alarm target %q out of range (00:00-23:59)", s)
	}
	return hour, minute, nil
}

// parseTimerSeconds parses a timer duration with an EXPLICIT unit ("10m",
// "90s", "1h30m"), bounded to 1s-24h. A bare number is rejected on purpose:
// "90" is ambiguous (the user said seconds, the schema said minutes) and a
// silent 60x mistake sets a 90-minute timer for a 90-second request — an
// explicit-unit error makes the model retry correctly instead.
func parseTimerSeconds(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf(`timer needs target as a duration with an explicit unit like "10m", "90s", "1h30m"`)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf(`timer target %q must be a duration with an explicit unit like "10m", "90s", "1h30m" (bare numbers are ambiguous and rejected)`, s)
	}
	if d < time.Second || d > 24*time.Hour {
		return 0, fmt.Errorf("timer target %q out of range (1s-24h)", s)
	}
	return int(d / time.Second), nil
}

// dispatchPhoneAction validates and delivers a P1 action via the injected
// sender, returning the agent-facing result string. Three outcomes per the
// PhoneActionFunc contract: confirmed executed (nil), dispatched-unconfirmed
// (ErrPhoneActionUnconfirmed → cautionary success, never a retry bait), or a
// real failure (error).
func dispatchPhoneAction(ctx context.Context, send tooldeps.PhoneActionFunc, p phoneWriteParams) (string, error) {
	action, args, err := buildPhoneAction(p)
	if err != nil {
		return "", err
	}
	if send == nil {
		return "", fmt.Errorf("phone action %q unavailable: native app channel not wired", action)
	}
	if err := send(ctx, action, args); err != nil {
		if errors.Is(err, tooldeps.ErrPhoneActionUnconfirmed) {
			return fmt.Sprintf("phone action %s dispatched to app, but the app did not confirm execution in time — it may still run late. Do NOT retry (risk of duplicates); tell the user to check the phone.", action), nil
		}
		// Neutral prefix: err covers both delivery failures (no app connected)
		// and device-reported execution failures ("failed on the device").
		return "", fmt.Errorf("phone action %q failed: %w", action, err)
	}
	// "launched", not "executed/completed": the app's ok=true means the intent
	// (or in-app service call) launched. For message/dial that opens the
	// composer/dialer with the content prefilled — nothing is auto-sent.
	note := ""
	if action == "message" || action == "dial" {
		note = " (composer/dialer opened with the content prefilled — the user confirms sending on the phone)"
	}
	return fmt.Sprintf("phone action launched on device: %s%s", action, note), nil
}
