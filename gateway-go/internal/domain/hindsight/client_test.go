package hindsight

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClientDisabledWithoutURL(t *testing.T) {
	if NewClient(Config{}) != nil {
		t.Fatal("expected nil client when no base URL is configured")
	}
}

func TestClientRecall(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody recallRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[
			{"id":"m1","text":"Peter prefers Korean replies","type":"world","context":"preference","mentioned_at":"2026-05-01T09:00:00Z"},
			{"id":"m2","text":"   "}
		]}`)
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, BankID: "deneb", APIKey: "hsk-test", Budget: "mid", RecallMaxTokens: 512})
	mems, err := c.Recall(context.Background(), "what language does Peter want")
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 memory (blank dropped), got %d", len(mems))
	}
	if mems[0].Text != "Peter prefers Korean replies" || mems[0].Type != "world" {
		t.Fatalf("unexpected memory: %+v", mems[0])
	}
	if mems[0].MentionedAt != "2026-05-01T09:00:00Z" {
		t.Fatalf("mentioned_at not parsed: %q", mems[0].MentionedAt)
	}
	if gotPath != "/v1/default/banks/deneb/memories/recall" {
		t.Fatalf("unexpected recall path: %q", gotPath)
	}
	if gotAuth != "Bearer hsk-test" {
		t.Fatalf("unexpected auth header: %q", gotAuth)
	}
	if gotBody.Budget != "mid" || gotBody.MaxTokens != 512 {
		t.Fatalf("recall request body not sent correctly: %+v", gotBody)
	}
}

func TestClientRecallServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL})
	if _, err := c.Recall(context.Background(), "anything"); err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestClientRetain(t *testing.T) {
	var gotPath string
	var gotBody retainRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"items_count":1,"async":true}`)
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, BankID: "deneb", Retain: true})
	err := c.Retain(context.Background(), []RetainItem{
		{Content: "User: hi\n\nAssistant: hello", Context: "turn", DocumentID: "telegram:1"},
		{Content: "   "}, // blank, dropped
	})
	if err != nil {
		t.Fatalf("Retain: %v", err)
	}
	if gotPath != "/v1/default/banks/deneb/memories" {
		t.Fatalf("unexpected retain path: %q", gotPath)
	}
	if !gotBody.Async {
		t.Fatal("retain request should set async=true")
	}
	if len(gotBody.Items) != 1 {
		t.Fatalf("expected 1 item (blank dropped), got %d", len(gotBody.Items))
	}
	if gotBody.Items[0].DocumentID != "telegram:1" {
		t.Fatalf("document_id not sent: %+v", gotBody.Items[0])
	}
}

func TestClientRetainDisabledMakesNoCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Retain: false})
	if c.RetainEnabled() {
		t.Fatal("RetainEnabled should be false")
	}
	if err := c.Retain(context.Background(), []RetainItem{{Content: "x"}}); err != nil {
		t.Fatalf("Retain: %v", err)
	}
	if called {
		t.Fatal("retain must not hit the server when the write path is disabled")
	}
}

func TestClientHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			t.Errorf("unexpected health path: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"version":"0.6.2"}`)
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL})
	version, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if version != "0.6.2" {
		t.Fatalf("unexpected version: %q", version)
	}
}

func TestClientHealthUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	c := NewClient(Config{BaseURL: srv.URL})
	srv.Close() // force a connection failure
	if _, err := c.Health(context.Background()); err == nil {
		t.Fatal("expected an error when the server is unreachable")
	}
}

func TestClientBankURLEscapesBankID(t *testing.T) {
	c := NewClient(Config{BaseURL: "http://h:8888", BankID: "a b/c"})
	got := c.bankURL("/memories")
	if strings.Contains(got, " ") || strings.Contains(got, "a b/c") {
		t.Fatalf("bank ID not path-escaped: %q", got)
	}
}
