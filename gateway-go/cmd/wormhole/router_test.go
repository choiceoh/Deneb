package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func quietRouter(cfg config) *router {
	return newRouter(cfg, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestChatCompletions_ForwardsRewritesInjectsKeyAndStreams(t *testing.T) {
	var gotModel, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotModel = extractModel(b)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"ok\":true}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()

	rt := quietRouter(config{Models: []modelEntry{
		{Name: "claude", URL: upstream.URL + "/v1", Key: "secret", UpstreamModel: "anthropic/claude-x"},
	}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	body := `{"model":"claude","messages":[{"role":"user","content":"hi"}],"stream":true}`
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if gotModel != "anthropic/claude-x" {
		t.Errorf("upstream model = %q, want rewritten anthropic/claude-x", gotModel)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("upstream auth = %q, want injected Bearer secret", gotAuth)
	}
	if !strings.Contains(string(out), "[DONE]") {
		t.Errorf("upstream stream not passed through: %q", out)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "event-stream") {
		t.Errorf("Content-Type = %q, want upstream's event-stream", ct)
	}
}

func TestChatCompletions_UnknownModelIs404(t *testing.T) {
	rt := quietRouter(config{Models: []modelEntry{{Name: "dsv4", URL: "http://127.0.0.1:1/v1", UpstreamModel: "dsv4"}}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for unknown model", resp.StatusCode)
	}
}

func TestChatCompletions_TokenRequired(t *testing.T) {
	rt := quietRouter(config{Token: "sekret", Models: []modelEntry{{Name: "dsv4", URL: "http://127.0.0.1:1/v1", UpstreamModel: "dsv4"}}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	// No bearer → 401.
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"dsv4"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without token", resp.StatusCode)
	}
}

func TestListModels(t *testing.T) {
	rt := quietRouter(config{Models: []modelEntry{
		{Name: "dsv4", URL: "http://x/v1", UpstreamModel: "dsv4"},
		{Name: "claude", URL: "http://y/v1", UpstreamModel: "anthropic/claude-x"},
	}})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got.Object != "list" || len(got.Data) != 2 {
		t.Fatalf("models = %+v, want a 2-entry list", got)
	}
	ids := got.Data[0].ID + "," + got.Data[1].ID
	if !strings.Contains(ids, "dsv4") || !strings.Contains(ids, "claude") {
		t.Errorf("model ids = %q, want dsv4 + claude", ids)
	}
}

func TestRewriteModel_PreservesOtherFields(t *testing.T) {
	in := []byte(`{"model":"claude","temperature":0.7,"messages":[{"role":"user","content":"hi"}]}`)
	out, err := rewriteModel(in, "anthropic/claude-x")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["model"] != "anthropic/claude-x" {
		t.Errorf("model = %v, want anthropic/claude-x", m["model"])
	}
	if m["temperature"] != 0.7 {
		t.Errorf("temperature = %v, want 0.7 preserved", m["temperature"])
	}
	if _, ok := m["messages"]; !ok {
		t.Error("messages field dropped on rewrite")
	}
}

func TestExtractModel(t *testing.T) {
	if got := extractModel([]byte(`{"model":" dsv4 ","x":1}`)); got != "dsv4" {
		t.Errorf("extractModel = %q, want dsv4 (trimmed)", got)
	}
	if got := extractModel([]byte(`{"messages":[]}`)); got != "" {
		t.Errorf("extractModel(no model) = %q, want empty", got)
	}
}
