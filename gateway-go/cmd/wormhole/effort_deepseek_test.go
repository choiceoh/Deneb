package main

import (
	"encoding/json"
	"testing"
)

func deepseekEntry() modelEntry {
	return modelEntry{Name: "deepseek-v4-flash-api", Reasoning: reasoningStyleDeepseek}
}

func effortOf(t *testing.T, body []byte) (string, bool) {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v, ok := parsed["reasoning_effort"].(string)
	return v, ok
}

// "low" is how the Deneb gateway spells "thinking disabled" on the OpenAI path,
// but DeepSeek reads it as MORE reasoning — measured 1,932 reasoning chars and
// finish_reason=length with empty content. It must become "none".
func TestDeepseekReasoningRouteTranslatesLowToNone(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","reasoning_effort":"low","messages":[]}`)
	out, reason, off := reasoningRoute(body, deepseekEntry())
	if !off || reason != "explicit-low" {
		t.Fatalf("off=%v reason=%q", off, reason)
	}
	if eff, ok := effortOf(t, out); !ok || eff != "none" {
		t.Fatalf("reasoning_effort = %q (present=%v), want none", eff, ok)
	}
}

// An explicit heavy effort is the caller's decision and passes through.
func TestDeepseekReasoningRouteKeepsHeavyEffort(t *testing.T) {
	for _, eff := range []string{"high", "max", "medium"} {
		body := []byte(`{"model":"deepseek-v4-flash","reasoning_effort":"` + eff + `","messages":[]}`)
		out, _, off := reasoningRoute(body, deepseekEntry())
		if off {
			t.Errorf("%s: thinking turned off", eff)
		}
		if got, _ := effortOf(t, out); got != eff {
			t.Errorf("%s: rewritten to %q", eff, got)
		}
	}
}

// A vLLM chat-template toggle aimed at a local serving is meaningless to the
// cloud API — this is the failover shape — so it is dropped, not forwarded.
func TestDeepseekReasoningRouteDropsTemplateKwargs(t *testing.T) {
	body := []byte(`{"model":"qwen3.6-35b-a3b","chat_template_kwargs":{"enable_thinking":false},"messages":[]}`)
	out, _, _ := reasoningRoute(body, deepseekEntry())
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := parsed["chat_template_kwargs"]; present {
		t.Fatalf("chat_template_kwargs survived: %s", out)
	}
}

// An entry that declares no dialect is still untouched.
func TestReasoningRouteLeavesUndeclaredStyleAlone(t *testing.T) {
	body := []byte(`{"model":"x","reasoning_effort":"low","messages":[]}`)
	out, reason, off := reasoningRoute(body, modelEntry{Name: "x"})
	if off || reason != "" || string(out) != string(body) {
		t.Fatalf("body was shaped: off=%v reason=%q out=%s", off, reason, out)
	}
}

// TestDeepseekReasoningRouteStaticModes: the entry-level variants on the
// DeepSeek dialect — "off" zeroes the channel (only "none" does that here),
// "on" pins high — with no classification either way.
func TestDeepseekReasoningRouteStaticModes(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"이 데이터를 심층 분석해줘"}],"reasoning_effort":"max"}`)
	for _, tc := range []struct {
		mode, want string
		wantOff    bool
	}{
		{thinkingModeOff, "none", true},
		{thinkingModeOn, "high", false},
	} {
		out, reason, off := deepseekReasoningRoute(body, tc.mode)
		if off != tc.wantOff || reason != "mode-"+tc.mode {
			t.Fatalf("mode %q: got off=%v reason=%q", tc.mode, off, reason)
		}
		if got, ok := effortOf(t, out); !ok || got != tc.want {
			t.Errorf("mode %q: reasoning_effort = %q (present=%v), want %q", tc.mode, got, ok, tc.want)
		}
	}
}
