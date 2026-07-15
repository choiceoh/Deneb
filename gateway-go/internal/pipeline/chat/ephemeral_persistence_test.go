package chat

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
)

// TestBuildMessagePersister_EphemeralAssistantSuppressesAssistant verifies the
// "EphemeralAssistant=true → nil persister" gate. This is the safety boundary
// for any future autonomous trigger that wants the legacy "drop everything"
// behavior; if it regresses, all autonomous output starts polluting transcripts.
func TestBuildMessagePersister_EphemeralAssistantReturnsNilPersister(t *testing.T) {
	transcript := NewMemoryTranscriptStore()
	deps := runDeps{transcript: transcript}
	params := runParams{
		SessionKey:         "telegram:1",
		EphemeralAssistant: true,
	}

	persister := buildMessagePersister(deps, params, slog.Default())
	if persister != nil {
		t.Fatal("EphemeralAssistant=true must yield nil persister")
	}
}

// TestBuildMessagePersister_EphemeralUserDoesNotBlockAssistant preserves the
// generic contract: suppressing the inbound self-trigger alone does not
// implicitly suppress assistant persistence. Callers that need full isolation
// must also set EphemeralAssistant.
func TestBuildMessagePersister_EphemeralUserAllowsAssistantPersist(t *testing.T) {
	transcript := NewMemoryTranscriptStore()
	deps := runDeps{transcript: transcript}
	params := runParams{
		SessionKey:    "telegram:1",
		EphemeralUser: true, // trigger suppressed
		// EphemeralAssistant: false (default) — reply must persist
	}

	persister := buildMessagePersister(deps, params, slog.Default())
	if persister == nil {
		t.Fatal("EphemeralUser-only must still produce a persister for the assistant reply")
	}

	rawText, _ := json.Marshal("hello")
	persister(llm.Message{Role: "assistant", Content: llm.FlexibleFromRaw(rawText)})

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
	params := runParams{SessionKey: "x"}

	if persister := buildMessagePersister(deps, params, slog.Default()); persister != nil {
		t.Fatal("nil transcript must yield nil persister regardless of ephemeral flags")
	}
}

// TestBuildMessagePersister_StripsNoReplyOnly verifies the heartbeat-friendly
// path: when the assistant turn is exactly NO_REPLY (no tool_use), the
// message is not persisted — otherwise the next heartbeat would treat
// silence as a "report" worth comparing against and we'd repeat noise.
func TestBuildMessagePersister_IgnoresNoReplyOnlyMessage(t *testing.T) {
	transcript := NewMemoryTranscriptStore()
	deps := runDeps{transcript: transcript}
	params := runParams{SessionKey: "telegram:1"}

	persister := buildMessagePersister(deps, params, slog.Default())
	if persister == nil {
		t.Fatal("expected persister")
	}

	rawText, _ := json.Marshal("NO_REPLY")
	persister(llm.Message{Role: "assistant", Content: llm.FlexibleFromRaw(rawText)})

	msgs, _, err := transcript.Load("telegram:1", 100)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("NO_REPLY-only assistant turn must not be persisted; got %d msgs", len(msgs))
	}
}

// TestBuildMessagePersister_SubstitutesMarketLetterTokens verifies the per-turn
// persist chokepoint: a streamed/async turn that mimics the morning-letter
// token syntax must persist the recorded display value, never the raw
// "{{market:...}}" template (2026-07-11 production transcript, client:main —
// the native card rendered "{{market:usd_krw}}원" verbatim).
func TestBuildMessagePersister_FormatsMarketLetterTokensForDisplay(t *testing.T) {
	market.RecordLetterTokens(map[string]string{market.LetterTokenUSDKRW: "1,531"})
	transcript := NewMemoryTranscriptStore()
	deps := runDeps{transcript: transcript}
	params := runParams{SessionKey: "client:main"}

	persister := buildMessagePersister(deps, params, slog.Default())
	if persister == nil {
		t.Fatal("expected persister")
	}

	rawText, _ := json.Marshal("1달러는 {{market:usd_krw}}원입니다.")
	persister(llm.Message{Role: "assistant", Content: llm.FlexibleFromRaw(rawText)})

	msgs, _, err := transcript.Load("client:main", 100)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 persisted message, got %d", len(msgs))
	}
	var text string
	if err := json.Unmarshal(msgs[0].Content, &text); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if text != "1달러는 1,531원입니다." {
		t.Errorf("persisted text = %q, want substituted display value", text)
	}
}

// TestBuildMessagePersister_SubstitutesMarketTokensInTextBlocksOnly verifies
// the block-form path: text blocks are substituted while tool_use inputs (and
// any other non-text block) pass through byte-identical — a tool argument that
// happens to contain the token syntax is the model's business, not a display
// surface.
func TestBuildMessagePersister_FormatsTextBlocksOnlyPreservingToolUse(t *testing.T) {
	market.RecordLetterTokens(map[string]string{market.LetterTokenUSDKRW: "1,531"})
	transcript := NewMemoryTranscriptStore()
	deps := runDeps{transcript: transcript}
	params := runParams{SessionKey: "client:main"}

	persister := buildMessagePersister(deps, params, slog.Default())
	toolInput := `{"command":"echo {{market:usd_krw}}"}`
	blocks := []llm.ContentBlock{
		{Type: "text", Text: "환율은 {{market:usd_krw}}원."},
		{Type: "tool_use", ID: "tu_1", Name: "exec", Input: llm.FlexibleFromRaw([]byte(toolInput))},
	}
	rawBlocks, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal blocks: %v", err)
	}
	persister(llm.Message{Role: "assistant", Content: llm.FlexibleFromRaw(rawBlocks)})

	msgs, _, err := transcript.Load("client:main", 100)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 persisted message, got %d", len(msgs))
	}
	var got []llm.ContentBlock
	if err := json.Unmarshal(msgs[0].Content, &got); err != nil {
		t.Fatalf("unmarshal blocks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(got))
	}
	if got[0].Text != "환율은 1,531원." {
		t.Errorf("text block = %q, want substituted display value", got[0].Text)
	}
	if got[1].Input.String() != toolInput {
		t.Errorf("tool_use input = %s, want untouched raw input", got[1].Input)
	}
}

// TestBuildMessagePersister_BriefcaseKeepsRawMarketTokens locks the
// BestTextRaw rule for the per-turn path: deterministic Briefcase runs must
// not read the process-global, time-sensitive letter-token cache — persisted
// bytes stay exactly what the model produced.
func TestBuildMessagePersister_BriefcasePreservesRawMarketTokens(t *testing.T) {
	market.RecordLetterTokens(map[string]string{market.LetterTokenUSDKRW: "1,531"})
	transcript := NewMemoryTranscriptStore()
	deps := runDeps{transcript: transcript, briefcaseMode: true}
	params := runParams{SessionKey: "briefcase:run"}

	persister := buildMessagePersister(deps, params, slog.Default())
	rawText, _ := json.Marshal("환율은 {{market:usd_krw}}원.")
	persister(llm.Message{Role: "assistant", Content: llm.FlexibleFromRaw(rawText)})

	msgs, _, err := transcript.Load("briefcase:run", 100)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 persisted message, got %d", len(msgs))
	}
	var text string
	if err := json.Unmarshal(msgs[0].Content, &text); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if !strings.Contains(text, "{{market:usd_krw}}") {
		t.Errorf("briefcase persisted text = %q, want raw token preserved", text)
	}
}
