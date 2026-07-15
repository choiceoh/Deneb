package streaming

import "github.com/choiceoh/deneb/gateway-go/internal/hanja"

// Streamer is the stream-safe Hanja→Hangul transliterator used on live deltas.
// Re-exported so chat run paths do not import internal/hanja directly.
type Streamer = hanja.Streamer

// NewStreamer returns a stream-safe Hanja→Hangul transliterator.
func NewStreamer() *Streamer { return hanja.NewStreamer() }

// Transliterate converts Sino-Korean Hanja in s to Hangul.
func Transliterate(s string) string { return hanja.Transliterate(s) }
