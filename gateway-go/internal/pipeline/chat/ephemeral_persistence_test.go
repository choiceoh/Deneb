package chat

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// TestBuildMessagePersister_EphemeralAssistantSuppressesAssistant verifies the
// "EphemeralAssistant=true → nil persister" gate. This is the safety boundary
// for any future autonomous trigger that wants the legacy "drop everything"
// behavior; if it regresses, all autonomous output starts polluting transcripts.
func TestBuildMessagePersister_EphemeralAssistantSuppressesAssistant(t *testing.T) {
	transcript := NewMemoryTranscriptStore()
	deps := runDeps{transcript: transcript}
	params := RunParams{
		SessionKey:         "telegram:1",
		EphemeralAssistant: true,
	}

	persister := buildMessagePersister(deps, params, slog.Default())
	if persister != nil {
		t.Fatal("EphemeralAssistant=true must yield nil persister")
	}
}

// TestBuildMessagePersister_EphemeralUserDoesNotBlockAssistant is the
// asymmetric guarantee that fixes the heartbeat repeat-loop bug: the
// trigger is dropped, but the assistant reply IS persisted so the next
// heartbeat can see "I already reported this 30 minutes ago".
func TestBuildMessagePersister_EphemeralUserDoesNotBlockAssistant(t *testing.T) {
	transcript := NewMemoryTranscriptStore()
	deps := runDeps{transcript: transcript}
	params := RunParams{
		SessionKey:    "telegram:1",
		EphemeralUser: true, // trigger suppressed
		// EphemeralAssistant: false (default) — reply must persist
	}

	persister := buildMessagePersister(deps, params, slog.Default())
	if persister == nil {
		t.Fatal("EphemeralUser-only must still produce a persister for the assistant reply")
	}

	rawText, _ := json.Marshal("hello")
	persister(llm.Message{Role: "assistant", Content: rawText})

	msgs, _, err := transcript.Load("telegram:1", 100)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Role != "assistant" {
		t.Fatalf("expected assistant message persisted, got %d msgs", len(msgs))
	}
}

// TestBuildMessagePersister_NoTranscriptYieldsNil confirms the long-standing
// guard that protects transcripts-disabled deployments from a nil-deref.
func TestBuildMessagePersister_NoTranscriptYieldsNil(t *testing.T) {
	deps := runDeps{transcript: nil}
	params := RunParams{SessionKey: "x"}

	if persister := buildMessagePersister(deps, params, slog.Default()); persister != nil {
		t.Fatal("nil transcript must yield nil persister regardless of ephemeral flags")
	}
}

// TestBuildMessagePersister_StripsNoReplyOnly verifies the heartbeat-friendly
// path: when the assistant turn is exactly NO_REPLY (no tool_use), the
// message is not persisted — otherwise the next heartbeat would treat
// silence as a "report" worth comparing against and we'd repeat noise.
func TestBuildMessagePersister_StripsNoReplyOnly(t *testing.T) {
	transcript := NewMemoryTranscriptStore()
	deps := runDeps{transcript: transcript}
	params := RunParams{SessionKey: "telegram:1"}

	persister := buildMessagePersister(deps, params, slog.Default())
	if persister == nil {
		t.Fatal("expected persister")
	}

	rawText, _ := json.Marshal("NO_REPLY")
	persister(llm.Message{Role: "assistant", Content: rawText})

	msgs, _, err := transcript.Load("telegram:1", 100)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("NO_REPLY-only assistant turn must not be persisted; got %d msgs", len(msgs))
	}
}
