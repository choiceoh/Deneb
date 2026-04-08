package telegram

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func newTestBotSetup(t *testing.T, handler http.HandlerFunc) (*Bot, *Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c := NewClient(ClientConfig{Token: "test-token"})
	c.baseURL = srv.URL + "/bottest-token"
	cfg := &Config{BotToken: "test-token"}
	bot := NewBot(c, cfg, nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	return bot, c, srv
}

func TestBot_StartAndStop(t *testing.T) {
	getMeCalled := 0
	getUpdatesCalled := 0

	bot, _, srv := newTestBotSetup(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bottest-token/getMe":
			getMeCalled++
			resp := APIResponse{
				OK:     true,
				Result: json.RawMessage(`{"id":123,"is_bot":true,"first_name":"TestBot"}`),
			}
			json.NewEncoder(w).Encode(resp)
		case "/bottest-token/getUpdates":
			getUpdatesCalled++
			resp := APIResponse{OK: true, Result: json.RawMessage(`[]`)}
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Start in goroutine since Start blocks.
	done := make(chan error, 1)
	go func() {
		done <- bot.Start(ctx)
	}()

	// Wait for polling to start.
	time.Sleep(200 * time.Millisecond)

	if !bot.IsRunning() {
		t.Error("expected bot to be running")
	}

	// Stop bot.
	cancel()
	<-done

	if bot.IsRunning() {
		t.Error("expected bot to not be running after stop")
	}
}

func TestBot_InboundMessageCallback(t *testing.T) {
	receivedCh := make(chan *Update, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bottest-token/getUpdates":
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			offset := int64(0)
			if v, ok := req["offset"].(float64); ok {
				offset = int64(v)
			}

			if offset == 0 {
				resp := APIResponse{
					OK: true,
					Result: json.RawMessage(`[{
						"update_id": 42,
						"message": {"message_id": 1, "chat": {"id": 100, "type": "private"}, "from": {"id": 1, "is_bot": false, "first_name": "User"}, "text": "hello from user"}
					}]`),
				}
				json.NewEncoder(w).Encode(resp)
			} else {
				resp := APIResponse{OK: true, Result: json.RawMessage(`[]`)}
				json.NewEncoder(w).Encode(resp)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Token: "test-token"})
	c.baseURL = srv.URL + "/bottest-token"
	cfg := &Config{BotToken: "test-token"}

	bot := NewBot(c, cfg, func(_ context.Context, update *Update) {
		select {
		case receivedCh <- update:
		default:
		}
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- bot.Start(ctx)
	}()

	// Wait for the update to be processed.
	var received *Update
	select {
	case received = <-receivedCh:
	case <-time.After(2 * time.Second):
	}

	cancel()
	<-done

	if received == nil {
		t.Fatal("expected to receive an update")
	}
	if received.UpdateID != 42 {
		t.Errorf("got %d, want update_id 42", received.UpdateID)
	}
	if received.Message == nil || received.Message.Text != "hello from user" {
		t.Error("expected message text 'hello from user'")
	}
}

func TestBot_Deduplication(t *testing.T) {
	bot, _, srv := newTestBotSetup(t, func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	if bot.isDuplicate(100) {
		t.Error("expected 100 to not be duplicate on first check")
	}
	bot.markSeen(100)
	if !bot.isDuplicate(100) {
		t.Error("expected 100 to be duplicate after markSeen")
	}
}

func TestBot_DrainMessages(t *testing.T) {
	bot, _, srv := newTestBotSetup(t, func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	// Add messages manually.
	bot.msgMu.Lock()
	bot.messages = append(bot.messages, &Message{MessageID: 1, Text: "hello"})
	bot.messages = append(bot.messages, &Message{MessageID: 2, Text: "world"})
	bot.msgMu.Unlock()

	msgs := bot.DrainMessages()
	if len(msgs) != 2 {
		t.Fatalf("got %d, want 2 messages", len(msgs))
	}

	// Drain again should be empty.
	msgs = bot.DrainMessages()
	if len(msgs) != 0 {
		t.Errorf("got %d, want 0 messages after drain", len(msgs))
	}
}

func TestExponentialBackoff(t *testing.T) {
	b := &ExponentialBackoff{
		Initial: 10 * time.Millisecond,
		Max:     100 * time.Millisecond,
		Factor:  2.0,
		Jitter:  0.0,
	}

	ctx := context.Background()
	start := time.Now()
	b.Wait(ctx)
	d1 := time.Since(start)
	if d1 < 5*time.Millisecond || d1 > 50*time.Millisecond {
		t.Errorf("first wait duration unexpected: %v", d1)
	}

	for range 10 {
		b.Wait(ctx)
	}
	if b.Current() > b.Max {
		t.Errorf("current %v exceeds max %v", b.Current(), b.Max)
	}

	b.Reset()
	if b.Current() != b.Initial {
		t.Errorf("after reset, current %v != initial %v", b.Current(), b.Initial)
	}
}

