package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestThinkingRoute_RoutesOffWhenTurnIsSimple(t *testing.T) {
	entry := modelEntry{Name: "dsv4", ToggleKwarg: "thinking"}
	body := []byte(`{"model":"dsv4","messages":[{"role":"user","content":"hi"}]}`)
	out, reason, off := thinkingRoute(body, entry)
	if !off {
		t.Fatalf("a short conversational turn should route thinking off; reason=%q", reason)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	kw, _ := m["chat_template_kwargs"].(map[string]any)
	if kw["thinking"] != false {
		t.Errorf("expected chat_template_kwargs.thinking=false, got %v", m["chat_template_kwargs"])
	}
}

func TestThinkingRoute_KeepsThinkingWhenTurnIsHard(t *testing.T) {
	entry := modelEntry{Name: "dsv4", ToggleKwarg: "thinking"}
	// "분석" is a hard signal in the Ares DefaultProfile → keep thinking on.
	body := []byte(`{"model":"dsv4","messages":[{"role":"user","content":"이거 분석해줘"}]}`)
	out, _, off := thinkingRoute(body, entry)
	if off {
		t.Error("a hard-signal turn should keep thinking on")
	}
	if bytes.Contains(out, []byte("chat_template_kwargs")) {
		t.Error("no injection expected on a hard turn")
	}
}

func TestThinkingRoute_StaticOffAlwaysInjectsWhenHard(t *testing.T) {
	entry := modelEntry{Name: "dsv4-nothink", ToggleKwarg: "thinking", ThinkingMode: thinkingModeOff}
	// "분석" is a hard signal — the judge mode would keep thinking; static off must not.
	body := []byte(`{"model":"dsv4-nothink","messages":[{"role":"user","content":"이거 분석해줘"}]}`)
	out, reason, off := thinkingRoute(body, entry)
	if !off || reason != "mode-off" {
		t.Fatalf("mode off must always inject; off=%v reason=%q", off, reason)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	kw, _ := m["chat_template_kwargs"].(map[string]any)
	if kw["thinking"] != false {
		t.Errorf("expected chat_template_kwargs.thinking=false, got %v", m["chat_template_kwargs"])
	}
}

func TestThinkingRoute_OffUnlessHardModeWithMixedSignals(t *testing.T) {
	entry := modelEntry{Name: "dsv4-auto", ToggleKwarg: "thinking", ThinkingMode: thinkingModeOffUnlessHard}

	// Long-but-plain input ("long" — the ambiguous middle) routes OFF under the
	// inverted bias, where the judge mode would have kept thinking.
	long := strings.Repeat("보고 내용 정리 문장입니다 ", 60)
	body := []byte(`{"model":"dsv4-auto","messages":[{"role":"user","content":"` + long + `"}]}`)
	out, reason, off := thinkingRoute(body, entry)
	if !off {
		t.Fatalf("ambiguous-middle turn must route off in off-unless-hard mode; reason=%q", reason)
	}
	if !bytes.Contains(out, []byte("chat_template_kwargs")) {
		t.Error("expected kwargs injection on the ambiguous-middle turn")
	}

	// A clear hard signal keeps the model's thinking (no injection).
	hard := []byte(`{"model":"dsv4-auto","messages":[{"role":"user","content":"이거 분석해줘"}]}`)
	out, reason, off = thinkingRoute(hard, entry)
	if off || bytes.Contains(out, []byte("chat_template_kwargs")) {
		t.Errorf("hard-signal turn must keep thinking; off=%v reason=%q", off, reason)
	}

	// Structured (code-fenced) input also counts as clearly hard.
	structured := []byte(`{"model":"dsv4-auto","messages":[{"role":"user","content":"` + "``` -\\n- x\\n- y ```" + `"}]}`)
	_, reason, off = thinkingRoute(structured, entry)
	if off {
		t.Errorf("structured turn must keep thinking; reason=%q", reason)
	}
}

func TestApplyThinking_StaticOffIgnoresNoEffortHeader(t *testing.T) {
	rt := &router{log: quietLog()}
	staticOff := modelEntry{Name: "dsv4-nothink", ToggleKwarg: "thinking", ThinkingMode: thinkingModeOff}
	judge := modelEntry{Name: "dsv4", ToggleKwarg: "thinking"}
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)

	// noEffort=true (the gateway's header): judge mode must NOT touch the body…
	if out := rt.applyThinking(judge, body, true); !bytes.Equal(out, body) {
		t.Error("judge mode must be suppressed under X-Wormhole-No-Effort")
	}
	// …but a static off entry is the caller's explicit choice — still injected.
	if out := rt.applyThinking(staticOff, body, true); !bytes.Contains(out, []byte(`"thinking":false`)) {
		t.Errorf("static off entry must inject even under X-Wormhole-No-Effort; got %s", out)
	}
}

func TestThinkingRoute_NoTogglePreservesBody(t *testing.T) {
	entry := modelEntry{Name: "x"} // no ToggleKwarg
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}]}`)
	out, reason, off := thinkingRoute(body, entry)
	if off || reason != "" {
		t.Errorf("a model without a toggle must be a no-op; off=%v reason=%q", off, reason)
	}
	if !bytes.Equal(out, body) {
		t.Error("body should be unchanged for a model without a toggle")
	}
}

func TestInjectKwarg_MergesAndPreserves(t *testing.T) {
	body := []byte(`{"model":"x","chat_template_kwargs":{"foo":1},"temperature":0.5}`)
	out := injectKwarg(body, "thinking", false)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	kw := m["chat_template_kwargs"].(map[string]any)
	if kw["thinking"] != false {
		t.Error("thinking toggle not injected")
	}
	if kw["foo"] != float64(1) {
		t.Error("existing chat_template_kwargs.foo was dropped")
	}
	if m["temperature"] != 0.5 {
		t.Error("unrelated field temperature was dropped")
	}
}

func TestContentText_ParsesStringAndArrayContent(t *testing.T) {
	s, a := contentText(json.RawMessage(`"hello"`))
	if s != "hello" || a {
		t.Errorf("string content = (%q, %v), want (hello, false)", s, a)
	}
	s, a = contentText(json.RawMessage(`[{"type":"text","text":"hi"},{"type":"image_url","image_url":{}}]`))
	if !strings.Contains(s, "hi") || !a {
		t.Errorf("array-with-image = (%q, %v), want (contains hi, true)", s, a)
	}
}

// A short follow-up steering a thread already deep in tool work must KEEP
// thinking — the reconstructed History (h_t) carries the tool activity the
// current message alone can't show. Proven for both wire shapes.
func TestThinkingRoute_KeepsThinkingWhenContextIsHeavy(t *testing.T) {
	entry := modelEntry{Name: "dsv4", ToggleKwarg: "thinking"}
	cases := []struct {
		name string
		body string
	}{
		{"openai tool_calls + tool result", `{"model":"dsv4","messages":[
			{"role":"user","content":"이 코드 분석해줘"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"c1","content":"file body"},
			{"role":"assistant","content":"분석 결과입니다"},
			{"role":"user","content":"계속해줘"}
		]}`},
		{"anthropic content blocks", `{"model":"dsv4","messages":[
			{"role":"user","content":[{"type":"text","text":"이 코드 분석해줘"}]},
			{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"read","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"file body"}]},
			{"role":"assistant","content":[{"type":"text","text":"분석 결과입니다"}]},
			{"role":"user","content":"계속해줘"}
		]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, reason, off := thinkingRoute([]byte(c.body), entry)
			if off {
				t.Errorf("short follow-up in a heavy thread must keep thinking; reason=%q", reason)
			}
			if bytes.Contains(out, []byte("chat_template_kwargs")) {
				t.Error("no injection expected when thinking is kept")
			}
			if reason != "context-heavy" {
				t.Errorf("reason = %q, want context-heavy", reason)
			}
		})
	}
}

// A pure ack stays routable even in a heavy thread (it steers nothing), and a
// short follow-up in a LIGHT thread still routes off — so History reconstruction
// doesn't over-trigger.
func TestThinkingRoute_RoutesOffForAckWithoutOverTriggering(t *testing.T) {
	entry := modelEntry{Name: "dsv4", ToggleKwarg: "thinking"}
	ack := `{"model":"dsv4","messages":[
		{"role":"user","content":"이 코드 분석해줘"},
		{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"file body"},
		{"role":"user","content":"고마워!"}
	]}`
	if _, reason, off := thinkingRoute([]byte(ack), entry); !off {
		t.Errorf("a pure ack stays routable even in a heavy thread; reason=%q", reason)
	}
	light := `{"model":"dsv4","messages":[
		{"role":"user","content":"안녕"},
		{"role":"assistant","content":"안녕하세요!"},
		{"role":"user","content":"잘 지냈어?"}
	]}`
	if _, _, off := thinkingRoute([]byte(light), entry); !off {
		t.Error("a light thread must not block routing")
	}
}

func TestThinkingRoute_WritesThinkingFalseOnForward(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	rt := quietRouter(config{Models: []modelEntry{
		{Name: "dsv4", URL: upstream.URL + "/v1", ToggleKwarg: "thinking", UpstreamModel: "dsv4"},
	}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	_, _ = http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"dsv4","messages":[{"role":"user","content":"hi"}]}`))
	if !strings.Contains(gotBody, `"thinking":false`) {
		t.Errorf("a simple request should reach the upstream with thinking:false; upstream saw: %s", gotBody)
	}
}

func TestReasoningRoute_GLMOffWithSimpleTurn(t *testing.T) {
	entry := modelEntry{Name: "glm-5.2", Reasoning: "glm"}
	// A short conversational turn → route reasoning off. The gateway's stray
	// reasoning_effort:"low" (which GLM would treat as MAX) must be stripped.
	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"안녕"}],"reasoning_effort":"low"}`)
	out, _, off := reasoningRoute(body, entry)
	if !off {
		t.Fatal("a short conversational turn should route reasoning off")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if think, _ := m["thinking"].(map[string]any); think["type"] != "disabled" {
		t.Errorf("expected thinking.type=disabled, got %v", m["thinking"])
	}
	if _, ok := m["reasoning_effort"]; ok {
		t.Errorf("reasoning_effort must be stripped when reasoning is off, got %v", m["reasoning_effort"])
	}
}

func TestReasoningRoute_GLMHighWhenTurnIsHard(t *testing.T) {
	entry := modelEntry{Name: "glm-5.2", Reasoning: "glm"}
	// "분석" is a hard signal in the Ares DefaultProfile → keep reasoning on, and
	// pin "high" (GLM resolves anything but an explicit "high" to MAX). No
	// explicit reasoning_effort on the request — an explicit one now expresses
	// caller intent and bypasses Ares (see TestReasoningRoute_GLMExplicitEffortWithLowAndHigh).
	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"이 데이터를 심층 분석해줘"}]}`)
	out, _, off := reasoningRoute(body, entry)
	if off {
		t.Fatal("a hard-signal turn should keep reasoning on")
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if m["reasoning_effort"] != "high" {
		t.Errorf("expected reasoning_effort=high, got %v", m["reasoning_effort"])
	}
	if think, _ := m["thinking"].(map[string]any); think["type"] != "enabled" {
		t.Errorf("expected thinking.type=enabled, got %v", m["thinking"])
	}
}

// TestReasoningRoute_GLMExplicitEffortWithLowAndHigh: an explicit inbound
// reasoning_effort is caller intent and bypasses Ares — non-high maps to
// thinking OFF (GLM has no true "low": anything but explicit high/max
// silently means MAX, which is how the skill evolver's 12K budget drowned in
// reasoning, live 2026-07-04), while an explicit high stays high with
// thinking enabled.
func TestReasoningRoute_GLMExplicitEffortWithLowAndHigh(t *testing.T) {
	entry := modelEntry{Name: "glm-5.2", Reasoning: "glm"}

	// Explicit low (the gateway's thinking-disabled translation) → OFF, even
	// on a hard-signal message.
	body := []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"이 데이터를 심층 분석해줘"}],"reasoning_effort":"low"}`)
	out, reason, off := reasoningRoute(body, entry)
	if !off || reason != "explicit-low" {
		t.Fatalf("explicit low must turn thinking off (off=%v reason=%q)", off, reason)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if _, has := m["reasoning_effort"]; has {
		t.Errorf("reasoning_effort must be stripped on the off path, got %v", m["reasoning_effort"])
	}
	if think, _ := m["thinking"].(map[string]any); think["type"] != "disabled" {
		t.Errorf("expected thinking.type=disabled, got %v", m["thinking"])
	}

	// Explicit high → pinned high, thinking enabled, no Ares involvement.
	body = []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"안녕"}],"reasoning_effort":"high"}`)
	out, reason, off = reasoningRoute(body, entry)
	if off || reason != "explicit-high" {
		t.Fatalf("explicit high must keep thinking on (off=%v reason=%q)", off, reason)
	}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if m["reasoning_effort"] != "high" {
		t.Errorf("expected reasoning_effort=high, got %v", m["reasoning_effort"])
	}
}

func TestReasoningRoute_NoStylePreservesBody(t *testing.T) {
	entry := modelEntry{Name: "x"} // no Reasoning style
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"안녕"}]}`)
	out, reason, off := reasoningRoute(body, entry)
	if off || reason != "" || !bytes.Equal(out, body) {
		t.Errorf("no reasoning style must be a no-op; off=%v reason=%q changed=%v", off, reason, !bytes.Equal(out, body))
	}
}

// TestReasoningRoute_GLMStaticModes: a cloud reasoning entry can BE the variant.
// thinkingMode "off"/"on" skips Ares entirely and pins the level, so the caller
// picks depth by model name (the dsv4-nothink contract, now on the GLM dialect).
// An inherited reasoning_effort — from the caller, or attached by a failover
// source shaped for another backend — must not override the entry's contract.
func TestReasoningRoute_GLMStaticModes(t *testing.T) {
	hard := []byte(`{"model":"glm-5.3-flash","messages":[{"role":"user","content":"이 데이터를 심층 분석해줘"}],"reasoning_effort":"max"}`)
	simple := []byte(`{"model":"glm-5.3-flash","messages":[{"role":"user","content":"안녕"}],"reasoning_effort":"low"}`)

	offEntry := modelEntry{Name: "glm-5.3-flash-nothink", Reasoning: "glm", ThinkingMode: thinkingModeOff}
	for _, body := range [][]byte{hard, simple} {
		out, reason, off := reasoningRoute(body, offEntry)
		if !off || reason != "mode-off" {
			t.Fatalf(`mode "off" must route off without classifying, got off=%v reason=%q`, off, reason)
		}
		var m map[string]any
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatalf("output not JSON: %v", err)
		}
		if think, _ := m["thinking"].(map[string]any); think["type"] != "disabled" {
			t.Errorf("expected thinking.type=disabled, got %v", m["thinking"])
		}
		if _, ok := m["reasoning_effort"]; ok {
			t.Errorf("reasoning_effort must be stripped, got %v", m["reasoning_effort"])
		}
	}

	onEntry := modelEntry{Name: "glm-5.3-flash", Reasoning: "glm", ThinkingMode: thinkingModeOn}
	for _, body := range [][]byte{hard, simple} {
		out, reason, off := reasoningRoute(body, onEntry)
		if off || reason != "mode-on" {
			t.Fatalf(`mode "on" must keep reasoning on without classifying, got off=%v reason=%q`, off, reason)
		}
		var m map[string]any
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatalf("output not JSON: %v", err)
		}
		if m["reasoning_effort"] != "high" {
			t.Errorf("expected reasoning_effort=high (never max), got %v", m["reasoning_effort"])
		}
		if think, _ := m["thinking"].(map[string]any); think["type"] != "enabled" {
			t.Errorf("expected thinking.type=enabled, got %v", m["thinking"])
		}
	}
}

// TestThinkingRoute_ModeOnKeepsThinking: the same static "on" contract on the
// vLLM toggle path — an obviously-simple turn keeps its thinking phase instead
// of being judged off, and the body is forwarded untouched (APC-safe).
func TestThinkingRoute_ModeOnKeepsThinking(t *testing.T) {
	entry := modelEntry{Name: "dsv4-think", ToggleKwarg: "thinking", ThinkingMode: thinkingModeOn}
	body := []byte(`{"model":"dsv4-think","messages":[{"role":"user","content":"안녕"}]}`)
	out, reason, off := thinkingRoute(body, entry)
	if off || reason != "mode-on" {
		t.Fatalf(`mode "on" must never route off, got off=%v reason=%q`, off, reason)
	}
	if string(out) != string(body) {
		t.Errorf("body must be forwarded untouched, got %s", out)
	}
}
