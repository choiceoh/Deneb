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
