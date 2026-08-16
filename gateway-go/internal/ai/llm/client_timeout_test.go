package llm

import (
	"testing"
	"time"
)

func TestSharedTransportAllowsLongNonStreamingCompletion(t *testing.T) {
	if got := sharedTransport.ResponseHeaderTimeout; got < 20*time.Minute {
		t.Fatalf("ResponseHeaderTimeout = %v, want at least 20m", got)
	}
}
