package chat

import "os"

// richUIChannel reports whether a channel can render deneb-ui interactive blocks.
// With Telegram retired, only the native client channels (client/app) render
// deneb-ui; the DENEB_RICH_UI env override is kept for dev/test.
func richUIChannel(channel string) bool {
	if os.Getenv("DENEB_RICH_UI") == "1" {
		return true
	}
	return channel == "client" || channel == "app"
}
