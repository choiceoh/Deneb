package bridge

import (
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
)

var (
	callMethod    = rpctest.Call
	mustOK        = rpctest.MustOK
	mustErr       = rpctest.MustErr
	extractResult = rpctest.Result
)

// ─── Methods registration ───────────────────────────────────────────────────

func TestMethods_nilBroadcaster_returnsNil(t *testing.T) {
	m := Methods(Deps{Broadcaster: nil})
	if m != nil {
		t.Errorf("expected nil when Broadcaster is nil, got %v", m)
	}
}

func TestMethods_registersBridgeSend(t *testing.T) {
	m := Methods(Deps{
		Broadcaster: func(string, any) (int, []error) { return 0, nil },
	})
	if m == nil {
		t.Fatal("expected non-nil method map")
	}
	if _, ok := m["bridge.send"]; !ok {
		t.Error("missing handler for bridge.send")
	}
}

// ─── bridge.send: parameter validation ──────────────────────────────────────

func TestBridgeSend_missingMessage(t *testing.T) {
	m := Methods(Deps{
		Broadcaster: func(string, any) (int, []error) { return 0, nil },
	})
	resp := callMethod(m, "bridge.send", map[string]any{"source": "test"})
	mustErr(t, resp)
}

func TestBridgeSend_emptyMessage(t *testing.T) {
	m := Methods(Deps{
		Broadcaster: func(string, any) (int, []error) { return 0, nil },
	})
	resp := callMethod(m, "bridge.send", map[string]any{"message": ""})
	mustErr(t, resp)
}

func TestBridgeSend_nilParams(t *testing.T) {
	m := Methods(Deps{
		Broadcaster: func(string, any) (int, []error) { return 0, nil },
	})
	resp := callMethod(m, "bridge.send", nil)
	mustErr(t, resp)
}

// ─── bridge.send: source defaulting ─────────────────────────────────────────

func TestBridgeSend_defaultsSourceToAPI(t *testing.T) {
	var capturedPayload map[string]any
	m := Methods(Deps{
		Broadcaster: func(_ string, payload any) (int, []error) {
			capturedPayload = payload.(map[string]any)
			return 1, nil
		},
	})
	resp := callMethod(m, "bridge.send", map[string]any{"message": "hello"})
	mustOK(t, resp)
	if capturedPayload["source"] != "api" {
		t.Errorf("expected source=api, got %v", capturedPayload["source"])
	}
}

func TestBridgeSend_preservesExplicitSource(t *testing.T) {
	var capturedPayload map[string]any
	m := Methods(Deps{
		Broadcaster: func(_ string, payload any) (int, []error) {
			capturedPayload = payload.(map[string]any)
			return 1, nil
		},
	})
	resp := callMethod(m, "bridge.send", map[string]any{
		"message": "hello",
		"source":  "claude-code",
	})
	mustOK(t, resp)
	if capturedPayload["source"] != "claude-code" {
		t.Errorf("expected source=claude-code, got %v", capturedPayload["source"])
	}
}

// ─── bridge.send: broadcast ─────────────────────────────────────────────────

func TestBridgeSend_broadcastsToClients(t *testing.T) {
	var capturedEvent string
	var capturedPayload map[string]any
	m := Methods(Deps{
		Broadcaster: func(event string, payload any) (int, []error) {
			capturedEvent = event
			capturedPayload = payload.(map[string]any)
			return 3, nil
		},
	})
	resp := callMethod(m, "bridge.send", map[string]any{"message": "test msg"})
	mustOK(t, resp)

	if capturedEvent != "bridge.message" {
		t.Errorf("expected event=bridge.message, got %q", capturedEvent)
	}
	if capturedPayload["message"] != "test msg" {
		t.Errorf("expected message=test msg, got %v", capturedPayload["message"])
	}

	result := extractResult(t, resp)
	if result["sent"].(float64) != 3 {
		t.Errorf("expected sent=3, got %v", result["sent"])
	}
}

func TestBridgeSend_responseIncludesTimestamp(t *testing.T) {
	m := Methods(Deps{
		Broadcaster: func(string, any) (int, []error) { return 1, nil },
	})
	resp := callMethod(m, "bridge.send", map[string]any{"message": "hi"})
	mustOK(t, resp)
	result := extractResult(t, resp)
	ts, ok := result["ts"].(float64)
	if !ok || ts <= 0 {
		t.Errorf("expected positive ts, got %v", result["ts"])
	}
}

// ─── bridge.send: injection triggering ──────────────────────────────────────

func TestBridgeSend_triggersInjection_nonMainAgent(t *testing.T) {
	var injectedSource, injectedMessage string
	inj := &Injector{}
	inj.SetSend(
		func(sessionKey, message string) {
			injectedSource = sessionKey
			injectedMessage = message
		},
		func() []string { return []string{"session-1"} },
	)

	m := Methods(Deps{
		Broadcaster: func(string, any) (int, []error) { return 1, nil },
		Injector:    inj,
	})
	resp := callMethod(m, "bridge.send", map[string]any{
		"message": "external msg",
		"source":  "claude-code",
	})
	mustOK(t, resp)

	result := extractResult(t, resp)
	if result["triggered"] != true {
		t.Error("expected triggered=true for non-main-agent source")
	}
	if injectedSource != "session-1" {
		t.Errorf("expected injection to session-1, got %q", injectedSource)
	}
	if injectedMessage != "[bridge:claude-code] external msg" {
		t.Errorf("unexpected injected message: %q", injectedMessage)
	}
}

func TestBridgeSend_noInjection_mainAgentSource(t *testing.T) {
	injected := false
	inj := &Injector{}
	inj.SetSend(
		func(string, string) { injected = true },
		func() []string { return []string{"session-1"} },
	)

	m := Methods(Deps{
		Broadcaster: func(string, any) (int, []error) { return 1, nil },
		Injector:    inj,
	})

	sources := []string{"gateway", "main-agent", "deneb", "deneb-core"}
	for _, src := range sources {
		injected = false
		resp := callMethod(m, "bridge.send", map[string]any{
			"message": "test",
			"source":  src,
		})
		mustOK(t, resp)
		result := extractResult(t, resp)
		if result["triggered"] != false {
			t.Errorf("source=%q: expected triggered=false", src)
		}
		if injected {
			t.Errorf("source=%q: should not have injected", src)
		}
	}
}

func TestBridgeSend_noInjection_nilInjector(t *testing.T) {
	m := Methods(Deps{
		Broadcaster: func(string, any) (int, []error) { return 1, nil },
		Injector:    nil,
	})
	resp := callMethod(m, "bridge.send", map[string]any{
		"message": "test",
		"source":  "claude-code",
	})
	mustOK(t, resp)
	result := extractResult(t, resp)
	if result["triggered"] != false {
		t.Error("expected triggered=false when Injector is nil")
	}
}

// ─── isFromMainAgent ────────────────────────────────────────────────────────

func TestIsFromMainAgent(t *testing.T) {
	tests := []struct {
		source string
		want   bool
	}{
		{"gateway", true},
		{"main-agent", true},
		{"deneb", true},
		{"deneb-core", true},
		{"deneb-xxx", true},
		{"claude-code", false},
		{"api", false},
		{"external", false},
		{"", false},
		{"GATEWAY", false}, // case-sensitive
		{"Gateway", false}, // case-sensitive
		{"Deneb", false},   // case-sensitive: must be lowercase
		{"main-Agent", false},
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			got := isFromMainAgent(tt.source)
			if got != tt.want {
				t.Errorf("isFromMainAgent(%q) = %v, want %v", tt.source, got, tt.want)
			}
		})
	}
}

// ─── Injector ───────────────────────────────────────────────────────────────

func TestInjector_send_noop_whenNotConfigured(t *testing.T) {
	// Should not panic when sendFn and sessionsList are nil.
	inj := &Injector{}
	inj.send("test-source", "test-message") // no-op, no panic
}

func TestInjector_send_noop_whenNoActiveSessions(t *testing.T) {
	called := false
	inj := &Injector{}
	inj.SetSend(
		func(string, string) { called = true },
		func() []string { return nil }, // no sessions
	)
	inj.send("ext", "hello")
	if called {
		t.Error("sendFn should not be called when no active sessions")
	}
}

func TestInjector_send_noop_whenEmptySessions(t *testing.T) {
	called := false
	inj := &Injector{}
	inj.SetSend(
		func(string, string) { called = true },
		func() []string { return []string{} }, // empty slice
	)
	inj.send("ext", "hello")
	if called {
		t.Error("sendFn should not be called when sessions list is empty")
	}
}

func TestInjector_send_deliversToAllSessions(t *testing.T) {
	var calls []struct{ key, msg string }
	inj := &Injector{}
	inj.SetSend(
		func(key, msg string) {
			calls = append(calls, struct{ key, msg string }{key, msg})
		},
		func() []string { return []string{"sess-a", "sess-b", "sess-c"} },
	)
	inj.send("claude-code", "ping")

	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}
	expectedMsg := "[bridge:claude-code] ping"
	for i, c := range calls {
		expectedKey := []string{"sess-a", "sess-b", "sess-c"}[i]
		if c.key != expectedKey {
			t.Errorf("call %d: expected key=%q, got %q", i, expectedKey, c.key)
		}
		if c.msg != expectedMsg {
			t.Errorf("call %d: expected msg=%q, got %q", i, expectedMsg, c.msg)
		}
	}
}

func TestInjector_send_formatsMessageCorrectly(t *testing.T) {
	var captured string
	inj := &Injector{}
	inj.SetSend(
		func(_, msg string) { captured = msg },
		func() []string { return []string{"s1"} },
	)
	inj.send("my-agent", "the content")
	want := "[bridge:my-agent] the content"
	if captured != want {
		t.Errorf("got %q, want %q", captured, want)
	}
}

func TestInjector_SetSend_overridesPrevious(t *testing.T) {
	var firstCalled, secondCalled bool
	inj := &Injector{}

	inj.SetSend(
		func(string, string) { firstCalled = true },
		func() []string { return []string{"s1"} },
	)
	inj.SetSend(
		func(string, string) { secondCalled = true },
		func() []string { return []string{"s1"} },
	)

	inj.send("x", "y")
	if firstCalled {
		t.Error("first sendFn should not be called after override")
	}
	if !secondCalled {
		t.Error("second sendFn should be called")
	}
}

func TestInjector_send_nilSendFn_withLister(t *testing.T) {
	// Only sessionsList set, sendFn is nil -- should be no-op.
	inj := &Injector{}
	inj.mu.Lock()
	inj.sessionsList = func() []string { return []string{"s1"} }
	inj.mu.Unlock()
	inj.send("x", "y") // no panic
}

func TestInjector_send_nilLister_withSendFn(t *testing.T) {
	// Only sendFn set, sessionsList is nil -- should be no-op.
	called := false
	inj := &Injector{}
	inj.mu.Lock()
	inj.sendFn = func(string, string) { called = true }
	inj.mu.Unlock()
	inj.send("x", "y") // no panic
	if called {
		t.Error("sendFn should not be called when lister is nil")
	}
}

// ─── Injector: concurrency safety ───────────────────────────────────────────

func TestInjector_concurrentSetSendAndSend(t *testing.T) {
	inj := &Injector{}
	var wg sync.WaitGroup

	// Concurrent SetSend calls.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inj.SetSend(
				func(string, string) {},
				func() []string { return []string{"s1"} },
			)
		}()
	}

	// Concurrent send calls.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inj.send("src", "msg")
		}()
	}

	wg.Wait() // Must not race or panic.
}
