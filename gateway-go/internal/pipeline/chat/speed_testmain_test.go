package chat

import (
	"os"
	"testing"
	"time"
)

// Shrink production settle delays so the chat unit suite is not dominated by
// fixed sleeps (transient LLM retry 2.5s, channel-reply retry 500ms). Live
// behavior is unchanged — only this package's TestMain overrides the knobs.
func TestMain(m *testing.M) {
	transientRetryBackoff = time.Millisecond
	channelReplyRetryDelay = time.Millisecond
	os.Exit(m.Run())
}
