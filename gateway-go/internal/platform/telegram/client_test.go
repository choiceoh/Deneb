package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

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
	if !reflect.DeepEqual(want, *user) {
		t.Errorf("GetMe mismatch:\n  want: %+v\n   got: %+v", want, *user)
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
			t.Errorf("expected chat_id 456, got %v", req["chat_id"])
		}
		if req["text"] != "hello" {
			t.Errorf("expected text 'hello', got %v", req["text"])
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
		t.Errorf("expected message_id 1, got %d", msg.MessageID)
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
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*httpretry.APIError)
	if !ok {
		t.Fatalf("expected *httpretry.APIError, got %T", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("expected StatusCode 400, got %d", apiErr.StatusCode)
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
		t.Fatalf("expected *httpretry.APIError, got %T", err)
	}
	if !apiErr.IsRateLimited() {
		t.Error("expected IsRateLimited to be true")
	}
	if apiErr.RetryAfter != 5*time.Second {
		t.Errorf("expected RetryAfter 5s, got %v", apiErr.RetryAfter)
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
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}
	if updates[0].Message.Text != "hi" {
		t.Errorf("expected first message 'hi', got %q", updates[0].Message.Text)
	}
	if updates[1].UpdateID != 101 {
		t.Errorf("expected update_id 101, got %d", updates[1].UpdateID)
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
