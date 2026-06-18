// phone.go — phone_read / phone_write tools that reach the user's phone over
// reverse SSH (gateway → phone Termux sshd) and run termux-api commands.
//
// This closes the SSH loop. The phone pushes events IN (deneb-emit →
// /api/event/ingest); here the agent reads the phone (location/clipboard/battery)
// to ENRICH a turn and acts on it (notification/tts/clipboard) to respond OUT —
// during any turn, including the proactive judgment turn (which runs with no tool
// preset, so these are available automatically).
//
// Auth is already in place: the gateway host's public key is in the phone's
// authorized_keys (registered during phone setup), so `ssh phone <cmd>` needs no
// password. The "phone" destination is an ~/.ssh/config alias by default; set
// DENEB_PHONE_SSH to override the whole ssh target (e.g. "-p 8022 100.93.163.49").

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// phoneSSHTarget returns the ssh destination args for the phone. Default is the
// "phone" config alias; DENEB_PHONE_SSH overrides with arbitrary ssh args.
func phoneSSHTarget() []string {
	if v := strings.TrimSpace(os.Getenv("DENEB_PHONE_SSH")); v != "" {
		return strings.Fields(v)
	}
	return []string{"phone"}
}

// runPhone runs one command on the phone over ssh. When stdinText is non-empty it
// is piped to the remote command's stdin, so notification/tts/clipboard text
// (quotes, newlines, emoji) never needs shell-escaping. Returns trimmed combined
// output; a non-zero exit (tunnel down, termux-api app missing, no permission) is
// an error the agent sees and can relay.
func runPhone(ctx context.Context, stdinText, remoteCmd string) (string, error) {
	args := make([]string, 0, 8)
	if stdinText == "" {
		args = append(args, "-n") // no stdin to consume
	}
	args = append(args, "-o", "BatchMode=yes", "-o", "ConnectTimeout=10")
	args = append(args, phoneSSHTarget()...)
	args = append(args, remoteCmd)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	if stdinText != "" {
		cmd.Stdin = strings.NewReader(stdinText)
	}
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("phone ssh failed: %w (output: %q)", err, trimmed)
	}
	return trimmed, nil
}

// ToolPhoneRead queries the phone: what = location | clipboard | battery |
// calllog | contacts.
func ToolPhoneRead() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			What string `json:"what"`
		}
		if err := jsonutil.UnmarshalInto("phone_read params", input, &p); err != nil {
			return "", err
		}
		switch strings.ToLower(strings.TrimSpace(p.What)) {
		case "location":
			// network provider: fast + low battery. JSON {latitude, longitude, ...}.
			return runPhone(ctx, "", "termux-location -p network")
		case "clipboard":
			return runPhone(ctx, "", "termux-clipboard-get")
		case "battery":
			return runPhone(ctx, "", "termux-battery-status")
		case "calllog", "calls":
			// Recent call history (Termux:API). JSON array of {name, phone_number,
			// type(incoming/outgoing/missed), date, duration}. Capped to the latest
			// 20 so a long history doesn't blow the turn's context.
			return runPhone(ctx, "", "termux-call-log -l 20")
		case "contacts", "addressbook":
			// Live address book from the phone (Termux:API). JSON array of
			// {name, number}. For a targeted lookup prefer the `contacts` tool
			// (the synced store with phone/company search); this is the raw phone list.
			return runPhone(ctx, "", "termux-contact-list")
		default:
			return "", fmt.Errorf("phone_read: unknown what=%q (use location|clipboard|battery|calllog|contacts)", p.What)
		}
	}
}

// ToolPhoneWrite acts on the phone: to = notification | tts | clipboard.
func ToolPhoneWrite() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			To    string `json:"to"`
			Text  string `json:"text"`
			Title string `json:"title"`
		}
		if err := jsonutil.UnmarshalInto("phone_write params", input, &p); err != nil {
			return "", err
		}
		text := strings.TrimSpace(p.Text)
		if text == "" {
			return "", fmt.Errorf("phone_write: text is required")
		}
		switch strings.ToLower(strings.TrimSpace(p.To)) {
		case "notification", "notify":
			title := strings.TrimSpace(p.Title)
			if title == "" {
				title = "Deneb"
			}
			// termux-notification has no stdin content option (unlike tts/clipboard),
			// so title + body go as args — quote both for safety.
			if _, err := runPhone(ctx, "", fmt.Sprintf("termux-notification -t %s -c %s",
				phoneShellQuote(title), phoneShellQuote(text))); err != nil {
				return "", err
			}
			return "phone notification sent", nil
		case "tts", "speak":
			if _, err := runPhone(ctx, text, "termux-tts-speak"); err != nil { // text via stdin
				return "", err
			}
			return "phone TTS spoken", nil
		case "clipboard":
			if _, err := runPhone(ctx, text, "termux-clipboard-set"); err != nil { // text via stdin
				return "", err
			}
			return "phone clipboard set", nil
		default:
			return "", fmt.Errorf("phone_write: unknown to=%q (use notification|tts|clipboard)", p.To)
		}
	}
}

// phoneShellQuote wraps s in single quotes for safe use as one shell argument,
// escaping embedded single quotes. Only termux-notification's -t/-c args need
// this (tts/clipboard take text via stdin).
func phoneShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
