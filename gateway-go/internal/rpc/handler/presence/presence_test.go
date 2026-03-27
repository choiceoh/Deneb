package presence

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

func TestNewStore_Empty(t *testing.T) {
	s := NewStore()
	if entries := s.List(); len(entries) != 0 {
		t.Fatalf("new store should be empty, got %d entries", len(entries))
	}
}

func TestStore_UpdateAndList(t *testing.T) {
	s := NewStore()
	s.Update(PresenceEntry{Text: "online", DeviceID: "dev1"})
	s.Update(PresenceEntry{Text: "idle", DeviceID: "dev2"})

	entries := s.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestStore_KeyGeneration(t *testing.T) {
	s := NewStore()

	// Same DeviceID+InstanceID should overwrite.
	s.Update(PresenceEntry{Text: "online", DeviceID: "dev1", InstanceID: "inst1"})
	s.Update(PresenceEntry{Text: "idle", DeviceID: "dev1", InstanceID: "inst1"})
	if entries := s.List(); len(entries) != 1 {
		t.Fatalf("expected 1 entry (same key), got %d", len(entries))
	}

	// Different InstanceID should create a new entry.
	s.Update(PresenceEntry{Text: "online", DeviceID: "dev1", InstanceID: "inst2"})
	if entries := s.List(); len(entries) != 2 {
		t.Fatalf("expected 2 entries (different instance), got %d", len(entries))
	}
}

func TestStore_FallbackKeyIsText(t *testing.T) {
	s := NewStore()
	// When DeviceID is empty, Text is used as key.
	s.Update(PresenceEntry{Text: "online"})
	s.Update(PresenceEntry{Text: "online"})
	if entries := s.List(); len(entries) != 1 {
		t.Fatalf("expected 1 entry (text-keyed), got %d", len(entries))
	}
}

func TestStore_UpdateSetsTimestamp(t *testing.T) {
	s := NewStore()
	entry := s.Update(PresenceEntry{Text: "online"})
	if entry.UpdatedAt == 0 {
		t.Fatal("expected non-zero UpdatedAt")
	}
}

// ---------------------------------------------------------------------------
// HeartbeatState
// ---------------------------------------------------------------------------

func TestHeartbeatState_DefaultEnabled(t *testing.T) {
	h := NewHeartbeatState()
	if !h.Enabled() {
		t.Fatal("expected heartbeats enabled by default")
	}
}

func TestHeartbeatState_SetEnabled(t *testing.T) {
	h := NewHeartbeatState()
	h.SetEnabled(false)
	if h.Enabled() {
		t.Fatal("expected disabled")
	}
	h.SetEnabled(true)
	if !h.Enabled() {
		t.Fatal("expected enabled")
	}
}

func TestHeartbeatState_LastNilByDefault(t *testing.T) {
	h := NewHeartbeatState()
	if h.Last() != nil {
		t.Fatal("expected nil last heartbeat")
	}
}

func TestHeartbeatState_RecordAndLast(t *testing.T) {
	h := NewHeartbeatState()
	event := map[string]any{"ts": float64(123456)}
	h.RecordHeartbeat(event)
	last := h.Last()
	if last == nil {
		t.Fatal("expected non-nil last heartbeat")
	}
	if last["ts"] != float64(123456) {
		t.Fatalf("expected ts=123456, got %v", last["ts"])
	}
}

// ---------------------------------------------------------------------------
// RPC handler: system-event
// ---------------------------------------------------------------------------

func TestSystemEvent_ValidParams(t *testing.T) {
	store := NewStore()
	handlers := Methods(Deps{Store: store})
	handler := handlers["system-event"]

	req := &protocol.RequestFrame{
		ID:     "test-1",
		Params: json.RawMessage(`{"text":"online","deviceId":"dev1"}`),
	}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("expected OK, got error: %+v", resp.Error)
	}
	if entries := store.List(); len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestSystemEvent_MissingText(t *testing.T) {
	store := NewStore()
	handlers := Methods(Deps{Store: store})
	handler := handlers["system-event"]

	req := &protocol.RequestFrame{
		ID:     "test-2",
		Params: json.RawMessage(`{"deviceId":"dev1"}`),
	}
	resp := handler(context.Background(), req)
	if resp.OK {
		t.Fatal("expected error for missing text")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("expected MISSING_PARAM, got %+v", resp.Error)
	}
}

func TestSystemEvent_EmptyParams(t *testing.T) {
	store := NewStore()
	handlers := Methods(Deps{Store: store})
	handler := handlers["system-event"]

	req := &protocol.RequestFrame{
		ID: "test-3",
	}
	resp := handler(context.Background(), req)
	if resp.OK {
		t.Fatal("expected error for empty params")
	}
}

func TestSystemEvent_BroadcasterCalled(t *testing.T) {
	store := NewStore()
	var broadcasted bool
	handlers := Methods(Deps{
		Store: store,
		Broadcaster: func(event string, payload any) (int, []error) {
			broadcasted = true
			if event != "presence" {
				t.Fatalf("expected event=presence, got %s", event)
			}
			return 1, nil
		},
	})
	handler := handlers["system-event"]

	req := &protocol.RequestFrame{
		ID:     "test-bc",
		Params: json.RawMessage(`{"text":"online"}`),
	}
	handler(context.Background(), req)
	if !broadcasted {
		t.Fatal("expected broadcaster to be called")
	}
}

// ---------------------------------------------------------------------------
// RPC handler: system-presence
// ---------------------------------------------------------------------------

func TestSystemPresence_EmptyStore(t *testing.T) {
	store := NewStore()
	handlers := Methods(Deps{Store: store})
	handler := handlers["system-presence"]

	req := &protocol.RequestFrame{ID: "test-4"}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("expected OK, got error: %+v", resp.Error)
	}

	var payload struct {
		Entries []PresenceEntry `json:"entries"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(payload.Entries))
	}
}

func TestSystemPresence_PopulatedStore(t *testing.T) {
	store := NewStore()
	store.Update(PresenceEntry{Text: "online", DeviceID: "dev1"})
	store.Update(PresenceEntry{Text: "idle", DeviceID: "dev2"})

	handlers := Methods(Deps{Store: store})
	handler := handlers["system-presence"]

	req := &protocol.RequestFrame{ID: "test-5"}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("expected OK, got error: %+v", resp.Error)
	}

	var payload struct {
		Entries []PresenceEntry `json:"entries"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(payload.Entries))
	}
}

// ---------------------------------------------------------------------------
// RPC handler: set-heartbeats
// ---------------------------------------------------------------------------

func TestSetHeartbeats_Enable(t *testing.T) {
	state := NewHeartbeatState()
	state.SetEnabled(false)

	handlers := HeartbeatMethods(HeartbeatDeps{State: state})
	handler := handlers["set-heartbeats"]

	req := &protocol.RequestFrame{
		ID:     "test-6",
		Params: json.RawMessage(`{"enabled":true}`),
	}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("expected OK, got error: %+v", resp.Error)
	}
	if !state.Enabled() {
		t.Fatal("expected heartbeats to be enabled")
	}

	var payload struct {
		OK      bool `json:"ok"`
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !payload.Enabled {
		t.Fatal("payload should report enabled=true")
	}
}

func TestSetHeartbeats_Disable(t *testing.T) {
	state := NewHeartbeatState()

	handlers := HeartbeatMethods(HeartbeatDeps{State: state})
	handler := handlers["set-heartbeats"]

	req := &protocol.RequestFrame{
		ID:     "test-7",
		Params: json.RawMessage(`{"enabled":false}`),
	}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("expected OK, got error: %+v", resp.Error)
	}
	if state.Enabled() {
		t.Fatal("expected heartbeats to be disabled")
	}
}

func TestSetHeartbeats_MissingEnabledParam(t *testing.T) {
	state := NewHeartbeatState()

	handlers := HeartbeatMethods(HeartbeatDeps{State: state})
	handler := handlers["set-heartbeats"]

	req := &protocol.RequestFrame{
		ID:     "test-8",
		Params: json.RawMessage(`{}`),
	}
	resp := handler(context.Background(), req)
	if resp.OK {
		t.Fatal("expected error for missing enabled param")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrValidationFailed {
		t.Fatalf("expected VALIDATION_FAILED, got %+v", resp.Error)
	}
}

func TestSetHeartbeats_EmptyParams(t *testing.T) {
	state := NewHeartbeatState()

	handlers := HeartbeatMethods(HeartbeatDeps{State: state})
	handler := handlers["set-heartbeats"]

	req := &protocol.RequestFrame{ID: "test-9"}
	resp := handler(context.Background(), req)
	if resp.OK {
		t.Fatal("expected error for empty params")
	}
}

func TestSetHeartbeats_BroadcasterCalled(t *testing.T) {
	state := NewHeartbeatState()
	var broadcasted bool
	handlers := HeartbeatMethods(HeartbeatDeps{
		State: state,
		Broadcaster: func(event string, payload any) (int, []error) {
			broadcasted = true
			if event != "heartbeat.config" {
				t.Fatalf("expected event=heartbeat.config, got %s", event)
			}
			return 1, nil
		},
	})
	handler := handlers["set-heartbeats"]

	req := &protocol.RequestFrame{
		ID:     "test-bc2",
		Params: json.RawMessage(`{"enabled":false}`),
	}
	handler(context.Background(), req)
	if !broadcasted {
		t.Fatal("expected broadcaster to be called")
	}
}

// ---------------------------------------------------------------------------
// RPC handler: last-heartbeat
// ---------------------------------------------------------------------------

func TestLastHeartbeat_NoRecorded(t *testing.T) {
	state := NewHeartbeatState()

	handlers := HeartbeatMethods(HeartbeatDeps{State: state})
	handler := handlers["last-heartbeat"]

	req := &protocol.RequestFrame{ID: "test-10"}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("expected OK, got error: %+v", resp.Error)
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	// When no heartbeat recorded, should return a default with enabled and ts.
	if _, ok := payload["enabled"]; !ok {
		t.Fatal("expected 'enabled' key in default payload")
	}
	if _, ok := payload["ts"]; !ok {
		t.Fatal("expected 'ts' key in default payload")
	}
}

func TestLastHeartbeat_AfterRecording(t *testing.T) {
	state := NewHeartbeatState()
	state.RecordHeartbeat(map[string]any{"ts": float64(999), "status": "ok"})

	handlers := HeartbeatMethods(HeartbeatDeps{State: state})
	handler := handlers["last-heartbeat"]

	req := &protocol.RequestFrame{ID: "test-11"}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("expected OK, got error: %+v", resp.Error)
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["ts"] != float64(999) {
		t.Fatalf("expected ts=999, got %v", payload["ts"])
	}
	if payload["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", payload["status"])
	}
}

// ---------------------------------------------------------------------------
// Methods / HeartbeatMethods return correct keys
// ---------------------------------------------------------------------------

func TestMethods_ReturnsExpectedKeys(t *testing.T) {
	handlers := Methods(Deps{Store: NewStore()})
	for _, key := range []string{"system-presence", "system-event"} {
		if _, ok := handlers[key]; !ok {
			t.Fatalf("missing handler for %s", key)
		}
	}
}

func TestHeartbeatMethods_ReturnsExpectedKeys(t *testing.T) {
	handlers := HeartbeatMethods(HeartbeatDeps{State: NewHeartbeatState()})
	for _, key := range []string{"last-heartbeat", "set-heartbeats"} {
		if _, ok := handlers[key]; !ok {
			t.Fatalf("missing handler for %s", key)
		}
	}
}
