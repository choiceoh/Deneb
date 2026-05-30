package chat

import "os"

// richUIChannel reports whether a channel can render kai-ui interactive blocks
// (the native Android client vendored from Kai). The client is not wired yet,
// so DENEB_RICH_UI=1 lets dev/live-tests exercise emission against the mock
// Telegram channel. Telegram itself stays false so its system-prompt bytes and
// the prompt cache remain untouched.
func richUIChannel(channel string) bool {
	if os.Getenv("DENEB_RICH_UI") == "1" {
		return true
	}
	switch channel {
	case "client", "app":
		return true
	default:
		return false
	}
}
