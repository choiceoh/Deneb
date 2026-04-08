package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// newTestClient creates a Client pointing at a local httptest server.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c := NewClient(ClientConfig{Token: "test-token"})
	c.baseURL = srv.URL + "/bottest-token"
	return c, srv
}

func TestGetMe(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/getMe" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := APIResponse{
			OK:     true,
			Result: json.RawMessage(`{"id":123,"is_bot":true,"first_name":"TestBot","username":"test_bot"}`),
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	user := testutil.Must(c.GetMe(context.Background()))
	want := User{ID: 123, IsBot: true, FirstName: "TestBot", Username: "test_bot"}
	if diff := cmp.Diff(want, *user); diff != "" {
		t.Errorf("GetMe mismatch (-want +got):\n%s", diff)
	}
}

func TestSendMessage(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/sendMessage" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["chat_id"].(float64) != 456 {
			t.Errorf("got %v, want chat_id 456", req["chat_id"])
		}
		if req["text"] != "hello" {
			t.Errorf("got %v, want text 'hello'", req["text"])
		}

		resp := APIResponse{
			OK:     true,
			Result: json.RawMessage(`{"message_id":1,"chat":{"id":456,"type":"private"},"text":"hello"}`),
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	result, err := c.Call(context.Background(), "sendMessage", map[string]any{
		"chat_id":    456,
		"text":       "hello",
		"parse_mode": "HTML",
	})
	testutil.NoError(t, err)
	var msg Message
	json.Unmarshal(result, &msg)
	if msg.MessageID != 1 {
		t.Errorf("got %d, want message_id 1", msg.MessageID)
	}
}

func TestAPIError(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse{
			OK:          false,
			ErrorCode:   400,
			Description: "Bad Request: can't parse entities",
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	_, err := c.Call(context.Background(), "sendMessage", map[string]any{
		"chat_id": 1,
		"text":    "test",
	})
	if err == nil {
		t.Fatal("got nil, want error")
	}

	apiErr, ok := err.(*httpretry.APIError)
	if !ok {
		t.Fatalf("got %T, want *httpretry.APIError", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("got %d, want StatusCode 400", apiErr.StatusCode)
	}
	if !isParseError(apiErr) {
		t.Error("expected isParseError to be true")
	}
}

func TestRateLimitError(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse{
			OK:          false,
			ErrorCode:   429,
			Description: "Too Many Requests: retry after 5",
			Parameters:  &ResponseParameters{RetryAfter: 5},
		}
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	_, err := c.Call(context.Background(), "sendMessage", map[string]any{
		"chat_id": 1,
		"text":    "test",
	})
	apiErr, ok := err.(*httpretry.APIError)
	if !ok {
		t.Fatalf("got %T, want *httpretry.APIError", err)
	}
	if !apiErr.IsRateLimited() {
		t.Error("expected IsRateLimited to be true")
	}
	if apiErr.RetryAfter != 5*time.Second {
		t.Errorf("got %v, want RetryAfter 5s", apiErr.RetryAfter)
	}
}

func TestGetUpdates(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse{
			OK: true,
			Result: json.RawMessage(`[
				{"update_id":100,"message":{"message_id":1,"chat":{"id":789,"type":"private"},"text":"hi"}},
				{"update_id":101,"message":{"message_id":2,"chat":{"id":789,"type":"private"},"text":"there"}}
			]`),
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	updates := testutil.Must(c.GetUpdates(context.Background(), 100, 1))
	if len(updates) != 2 {
		t.Fatalf("got %d, want 2 updates", len(updates))
	}
	if updates[0].Message.Text != "hi" {
		t.Errorf("got %q, want first message 'hi'", updates[0].Message.Text)
	}
	if updates[1].UpdateID != 101 {
		t.Errorf("got %d, want update_id 101", updates[1].UpdateID)
	}
}

func TestDeleteMessage(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse{OK: true, Result: json.RawMessage("true")}
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	if err := c.DeleteMessage(context.Background(), 123, 456); err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}
}
