package chat

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestIsCacheIncompatibleProviderHasNoRejectingProviders(t *testing.T) {
	// Kimi K2.7's coding endpoint ACCEPTS cache_control since 2026-07 (live-
	// verified: repeat call returns cache_read_input_tokens, no 400) — the old
	// kimi strip silently discarded every prefix-cache hit and re-billed the
	// full system prompt each turn. No provider rejects markers today; the
	// seam (and the strip machinery it gates) stays for the next one.
	for _, p := range []string{"kimi", "KIMI", "kimi-code", "kimi-subagent", "mimo", "mimo-plan", "zai", "anthropic", "openai", ""} {
		if isCacheIncompatibleProvider(p) {
			t.Errorf("%q should NOT be cache-incompatible", p)
		}
	}
}

func TestStripCacheControlMarkers_ClearsAllMarkers(t *testing.T) {
	ephemeral := &llm.CacheControl{Type: "ephemeral"}
	blocks := []llm.ContentBlock{
		{Type: "text", Text: "static", CacheControl: ephemeral},
		{Type: "text", Text: "semi", CacheControl: ephemeral},
		{Type: "text", Text: "dynamic"},
	}
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got []llm.ContentBlock
	if err := json.Unmarshal(stripCacheControlMarkers(raw), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("block count changed: %d", len(got))
	}
	for i := range got {
		if got[i].CacheControl != nil {
			t.Errorf("block %d still carries cache_control", i)
		}
	}
	// Text content must be preserved verbatim.
	if got[0].Text != "static" || got[1].Text != "semi" || got[2].Text != "dynamic" {
		t.Fatalf("content altered: %+v", got)
	}
}

func TestStripCacheControlMarkers_PreservesStringSystem(t *testing.T) {
	raw := llm.SystemString("plain system prompt")
	if out := stripCacheControlMarkers(raw.Bytes()); string(out) != raw.String() {
		t.Fatalf("string system prompt must be unchanged, got %s", out)
	}
}

func TestStripCacheControlMarkers_PreservesBlocksWithoutMarkers(t *testing.T) {
	blocks := []llm.ContentBlock{{Type: "text", Text: "a"}, {Type: "text", Text: "b"}}
	raw, _ := json.Marshal(blocks)
	if out := stripCacheControlMarkers(raw); string(out) != string(raw) {
		t.Fatal("blocks without markers must be returned unchanged")
	}
}

func TestStripCacheControlMarkers_EmptyUnchanged(t *testing.T) {
	if out := stripCacheControlMarkers(nil); out != nil {
		t.Fatalf("nil should stay nil, got %q", out)
	}
}
