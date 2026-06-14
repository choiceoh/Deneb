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

func TestThinkingRoute_InjectsOnSimpleTurn(t *testing.T) {
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

func TestThinkingRoute_KeepsThinkingOnHardTurn(t *testing.T) {
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

func TestThinkingRoute_NoToggleIsNoOp(t *testing.T) {
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

func TestContentText(t *testing.T) {
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
func TestThinkingRoute_ContextHeavyKeepsThinking(t *testing.T) {
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
func TestThinkingRoute_HistoryDoesNotOverTrigger(t *testing.T) {
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

func TestThinkingRoute_AppliedOnForward(t *testing.T) {
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
