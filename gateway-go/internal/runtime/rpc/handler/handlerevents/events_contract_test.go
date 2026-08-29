package handlerevents

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func invokeEventHandler(t *testing.T, handler rpcutil.HandlerFunc, id, params string) *protocol.ResponseFrame {
	t.Helper()
	return handler(context.Background(), &protocol.RequestFrame{
		ID:     id,
		Params: json.RawMessage(params),
	})
}

func decodePayload[T any](t *testing.T, response *protocol.ResponseFrame) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(response.Payload, &value); err != nil {
		t.Fatalf("decode payload %s: %v", response.Payload, err)
	}
	return value
}

func requireRPCSuccess(t *testing.T, response *protocol.ResponseFrame, id string) {
	t.Helper()
	if response == nil || !response.OK || response.Error != nil || response.ID != id || response.Type != protocol.FrameTypeResponse {
		t.Fatalf("response = %+v", response)
	}
}

func requireRPCError(t *testing.T, response *protocol.ResponseFrame, code string) {
	t.Helper()
	if response == nil || response.OK || response.Error == nil || response.Error.Code != code {
		t.Fatalf("response = %+v, want error %q", response, code)
	}
}

func TestEventsMethodsReturnsAliasSurfaceAndBroadcastNilWithoutBroadcaster(t *testing.T) {
	broadcaster := events.NewBroadcaster()
	methods := EventsMethods(EventsDeps{Broadcaster: broadcaster})
	want := []string{
		"subscribe.session",
		"unsubscribe.session",
		"subscribe.session.messages",
		"unsubscribe.session.messages",
		"sessions.subscribe",
		"sessions.unsubscribe",
		"sessions.messages.subscribe",
		"sessions.messages.unsubscribe",
		"sessions.tools.subscribe",
		"sessions.tools.unsubscribe",
		// Client-bridge (miniapp.*) aliases for the desktop spectate surface.
		"miniapp.sessions.events.subscribe",
		"miniapp.sessions.events.unsubscribe",
	}
	if len(methods) != len(want) {
		t.Fatalf("method count = %d, want %d: %#v", len(methods), len(want), methods)
	}
	for _, name := range want {
		if methods[name] == nil {
			t.Errorf("missing method %q", name)
		}
	}
	if BroadcastMethods(EventsDeps{}) != nil {
		t.Fatal("BroadcastMethods with nil broadcaster is non-nil")
	}
	broadcast := BroadcastMethods(EventsDeps{Broadcaster: broadcaster})
	if len(broadcast) != 1 || broadcast["events.broadcast"] == nil {
		t.Fatalf("broadcast methods = %#v", broadcast)
	}
}

func TestSessionSubscribeAliasAndUnsubscribeUpdateSharedSubscriberSet(t *testing.T) {
	broadcaster := events.NewBroadcaster()
	methods := EventsMethods(EventsDeps{Broadcaster: broadcaster})

	response := invokeEventHandler(t, methods["subscribe.session"], "sub-1", `{"connId":" conn-1 "}`)
	requireRPCSuccess(t, response, "sub-1")
	if payload := decodePayload[map[string]bool](t, response); !payload["subscribed"] {
		t.Fatalf("payload = %#v", payload)
	}
	if got := broadcaster.SessionEventSubscriberConnIDs(); !reflect.DeepEqual(got, map[string]struct{}{"conn-1": {}}) {
		t.Fatalf("session subscribers = %#v", got)
	}

	response = invokeEventHandler(t, methods["sessions.subscribe"], "sub-2", `{"connId":"conn-2"}`)
	requireRPCSuccess(t, response, "sub-2")
	if got := broadcaster.SessionEventSubscriberConnIDs(); len(got) != 2 {
		t.Fatalf("alias did not subscribe: %#v", got)
	}

	response = invokeEventHandler(t, methods["sessions.unsubscribe"], "unsub-1", `{"connId":" conn-1 "}`)
	requireRPCSuccess(t, response, "unsub-1")
	if payload := decodePayload[map[string]bool](t, response); !payload["unsubscribed"] {
		t.Fatalf("payload = %#v", payload)
	}
	if got := broadcaster.SessionEventSubscriberConnIDs(); len(got) != 1 {
		t.Fatalf("unsubscribe result = %#v", got)
	}
}

func TestSessionMessageSubscribeAddsThenUnsubscribeClearsSubscriberSet(t *testing.T) {
	broadcaster := events.NewBroadcaster()
	methods := EventsMethods(EventsDeps{Broadcaster: broadcaster})
	response := invokeEventHandler(t, methods["sessions.messages.subscribe"], "msg-sub", `{"connId":" conn ","sessionKey":" session:1 "}`)
	requireRPCSuccess(t, response, "msg-sub")
	if got := broadcaster.SessionMessageSubscriberConnIDs("session:1"); !reflect.DeepEqual(got, map[string]struct{}{"conn": {}}) {
		t.Fatalf("message subscribers = %#v", got)
	}
	response = invokeEventHandler(t, methods["unsubscribe.session.messages"], "msg-unsub", `{"connId":"conn","sessionKey":"session:1"}`)
	requireRPCSuccess(t, response, "msg-unsub")
	if got := broadcaster.SessionMessageSubscriberConnIDs("session:1"); got != nil {
		t.Fatalf("message unsubscribe left %#v", got)
	}
}

func TestToolRecipientSubscribeReplacesPriorConnAndUnsubscribeClearsRecipient(t *testing.T) {
	broadcaster := events.NewBroadcaster()
	methods := EventsMethods(EventsDeps{Broadcaster: broadcaster})
	for i, conn := range []string{"conn-1", " conn-2 "} {
		response := invokeEventHandler(t, methods["sessions.tools.subscribe"], "tool-sub", `{"connId":"`+conn+`","runId":" run-1 "}`)
		requireRPCSuccess(t, response, "tool-sub")
		want := "conn-1"
		if i == 1 {
			want = "conn-2"
		}
		if got := broadcaster.ToolEventRecipient("run-1"); got != want {
			t.Fatalf("recipient = %q, want %q", got, want)
		}
	}
	response := invokeEventHandler(t, methods["sessions.tools.unsubscribe"], "tool-unsub", `{"runId":" run-1 "}`)
	requireRPCSuccess(t, response, "tool-unsub")
	if got := broadcaster.ToolEventRecipient("run-1"); got != "" {
		t.Fatalf("recipient after unsubscribe = %q", got)
	}
}

func TestEventsMethodsValidationAndMalformedJSON(t *testing.T) {
	methods := EventsMethods(EventsDeps{Broadcaster: events.NewBroadcaster()})
	for _, tc := range []struct {
		method string
		params string
	}{
		{method: "subscribe.session", params: `{}`},
		{method: "subscribe.session", params: `{"connId":"   "}`},
		{method: "unsubscribe.session", params: `{}`},
		{method: "subscribe.session.messages", params: `{"connId":"c"}`},
		{method: "subscribe.session.messages", params: `{"sessionKey":"s"}`},
		{method: "subscribe.session.messages", params: `{"connId":" ","sessionKey":"s"}`},
		{method: "unsubscribe.session.messages", params: `{"connId":"c","sessionKey":" "}`},
		{method: "sessions.tools.subscribe", params: `{"connId":"c"}`},
		{method: "sessions.tools.subscribe", params: `{"runId":"r"}`},
		{method: "sessions.tools.unsubscribe", params: `{}`},
		{method: "sessions.tools.unsubscribe", params: `{"runId":" "}`},
	} {
		t.Run(tc.method+tc.params, func(t *testing.T) {
			response := invokeEventHandler(t, methods[tc.method], "bad", tc.params)
			requireRPCError(t, response, "MISSING_PARAM")
		})
	}
	for name, handler := range methods {
		t.Run("malformed "+name, func(t *testing.T) {
			response := invokeEventHandler(t, handler, "malformed", `{`)
			requireRPCError(t, response, "INVALID_REQUEST")
		})
	}
}

func TestBroadcastHandlerDeliversEventAndPayload(t *testing.T) {
	broadcaster := events.NewBroadcaster()
	var mu sync.Mutex
	var seenEvent string
	var seenPayload events.EventPayload
	broadcaster.RegisterTap(func(event string, payload events.EventPayload) {
		mu.Lock()
		seenEvent, seenPayload = event, payload
		mu.Unlock()
	})
	handler := BroadcastMethods(EventsDeps{Broadcaster: broadcaster})["events.broadcast"]
	response := invokeEventHandler(t, handler, "broadcast", `{"event":" custom.event ","payload":{"name":"테스트","count":3}}`)
	requireRPCSuccess(t, response, "broadcast")
	if payload := decodePayload[map[string]int](t, response); payload["sent"] != 0 {
		t.Fatalf("sent payload = %#v", payload)
	}
	mu.Lock()
	event, payload := seenEvent, seenPayload
	mu.Unlock()
	if event != "custom.event" {
		t.Fatalf("event = %q", event)
	}
	var payloadMap map[string]any
	_ = json.Unmarshal(seenPayload.Bytes(), &payloadMap)
	if payloadMap == nil || payloadMap["name"] != "테스트" || payloadMap["count"] != float64(3) {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestBroadcastHandlerReturnsErrorsForMissingEventAndMalformedJSON(t *testing.T) {
	handler := BroadcastMethods(EventsDeps{Broadcaster: events.NewBroadcaster()})["events.broadcast"]
	for _, params := range []string{`{}`, `{"event":""}`, `{"event":"   "}`} {
		response := invokeEventHandler(t, handler, "missing", params)
		requireRPCError(t, response, "MISSING_PARAM")
	}
	malformed := invokeEventHandler(t, handler, "malformed", `{`)
	requireRPCError(t, malformed, "INVALID_REQUEST")
}

func TestEventHandlersConcurrentSubscriptions(t *testing.T) {
	broadcaster := events.NewBroadcaster()
	methods := EventsMethods(EventsDeps{Broadcaster: broadcaster})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn := "conn-" + string(rune('a'+i))
			run := "run-" + string(rune('a'+i))
			for j := 0; j < 50; j++ {
				invokeEventHandler(t, methods["sessions.subscribe"], "id", `{"connId":"`+conn+`"}`)
				invokeEventHandler(t, methods["sessions.messages.subscribe"], "id", `{"connId":"`+conn+`","sessionKey":"shared"}`)
				invokeEventHandler(t, methods["sessions.tools.subscribe"], "id", `{"connId":"`+conn+`","runId":"`+run+`"}`)
				if j%2 == 0 {
					invokeEventHandler(t, methods["sessions.unsubscribe"], "id", `{"connId":"`+conn+`"}`)
					invokeEventHandler(t, methods["sessions.messages.unsubscribe"], "id", `{"connId":"`+conn+`","sessionKey":"shared"}`)
					invokeEventHandler(t, methods["sessions.tools.unsubscribe"], "id", `{"runId":"`+run+`"}`)
				}
			}
		}(i)
	}
	wg.Wait()
	// The race detector is the assertion; also verify returned snapshots can be
	// read without exposing broadcaster internals.
	_ = broadcaster.SessionEventSubscriberConnIDs()
	_ = broadcaster.SessionMessageSubscriberConnIDs("shared")
	if strings.Contains(broadcaster.ToolEventRecipient("missing"), "conn") {
		t.Fatal("missing run unexpectedly has recipient")
	}
}
