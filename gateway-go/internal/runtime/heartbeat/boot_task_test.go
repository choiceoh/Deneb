package heartbeat

import (
	"context"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// The boot self-check routes to the configured role/model (production wires
// the fallback role — local, always-resident — because the turn fires right
// after gateway start when cloud reachability is least trustworthy). Empty
// keeps the default main resolution.
func TestBootTaskRoutesToConfiguredModel(t *testing.T) {
	for _, model := range []string{"fallback", ""} {
		runner := &deliveryCaptureRunner{result: &chatport.SyncResult{Text: "특이사항 없음"}}
		task := NewBootTask(runner, nil, slog.Default(), t.TempDir(), model)
		if err := task.Run(context.Background()); err != nil {
			t.Fatalf("model %q: unexpected error: %v", model, err)
		}
		if runner.req.Model != model {
			t.Fatalf("model %q: SyncRequest.Model = %q", model, runner.req.Model)
		}
		if runner.req.SessionKey != "boot" {
			t.Fatalf("model %q: SessionKey = %q", model, runner.req.SessionKey)
		}
		// Ephemeral contract must survive the model plumbing (a persisted boot
		// session once grew to 738K tokens and stalled the compaction path).
		if !runner.req.EphemeralUser || !runner.req.EphemeralAssistant {
			t.Fatalf("model %q: boot turn must stay ephemeral", model)
		}
	}
}
