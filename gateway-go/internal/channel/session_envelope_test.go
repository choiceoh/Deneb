package channel

import (
	"testing"
	"time"
)

func TestResolveInboundSessionEnvelopeContext(t *testing.T) {
	t.Run("basic fields", func(t *testing.T) {
		ctx := ResolveInboundSessionEnvelopeContext("/store", "agent1", "sess1", nil)
		if ctx.StorePath != "/store" {
			t.Errorf("StorePath = %q, want %q", ctx.StorePath, "/store")
		}
		if !ctx.EnvelopeOptions.IncludeTimestamp {
			t.Error("IncludeTimestamp should be true")
		}
		if ctx.EnvelopeOptions.TimestampFormat != time.RFC3339 {
			t.Errorf("TimestampFormat = %q, want %q", ctx.EnvelopeOptions.TimestampFormat, time.RFC3339)
		}
		if ctx.PreviousTimestamp != nil {
			t.Errorf("PreviousTimestamp should be nil when not provided")
		}
	})

	t.Run("with previous timestamp", func(t *testing.T) {
		ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
		ctx := ResolveInboundSessionEnvelopeContext("/store", "agent1", "sess1", &ts)
		if ctx.PreviousTimestamp == nil {
			t.Fatal("PreviousTimestamp should not be nil")
		}
		if !ctx.PreviousTimestamp.Equal(ts) {
			t.Errorf("PreviousTimestamp = %v, want %v", ctx.PreviousTimestamp, ts)
		}
	})

	t.Run("empty store path", func(t *testing.T) {
		ctx := ResolveInboundSessionEnvelopeContext("", "", "", nil)
		if ctx.StorePath != "" {
			t.Errorf("StorePath = %q, want empty", ctx.StorePath)
		}
		// Format options should still be set.
		if !ctx.EnvelopeOptions.IncludeTimestamp {
			t.Error("IncludeTimestamp should always be true")
		}
	})
}
