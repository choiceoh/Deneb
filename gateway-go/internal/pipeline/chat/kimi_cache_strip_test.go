package chat

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestIsCacheIncompatibleProvider(t *testing.T) {
	for _, p := range []string{"kimi", "KIMI", " kimi "} {
		if !isCacheIncompatibleProvider(p) {
			t.Errorf("%q should be cache-incompatible", p)
		}
	}
	// MiMo/z.ai are Anthropic-wire too but accept cache_control — must NOT match.
	for _, p := range []string{"mimo", "mimo-plan", "zai", "anthropic", "openai", ""} {
		if isCacheIncompatibleProvider(p) {
			t.Errorf("%q should NOT be cache-incompatible", p)
		}
	}
}

func TestStripCacheControlMarkers_RemovesAllMarkers(t *testing.T) {
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

func TestStripCacheControlMarkers_StringSystemUnchanged(t *testing.T) {
	raw := llm.SystemString("plain system prompt")
	if out := stripCacheControlMarkers(raw); string(out) != string(raw) {
		t.Fatalf("string system prompt must be unchanged, got %s", out)
	}
}

func TestStripCacheControlMarkers_NoMarkersUnchanged(t *testing.T) {
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
