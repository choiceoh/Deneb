package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func mustPayload(t *testing.T, v any) EventPayload {
	t.Helper()
	p, err := PayloadOf(v)
	if err != nil {
		t.Fatalf("PayloadOf: %v", err)
	}
	return p
}

func subscriberFrames(t *testing.T, sub *mockSubscriber) []protocol.EventFrame {
	t.Helper()
	sub.mu.Lock()
	raw := make([][]byte, len(sub.received))
	for i := range sub.received {
		raw[i] = append([]byte(nil), sub.received[i]...)
	}
	sub.mu.Unlock()
	frames := make([]protocol.EventFrame, 0, len(raw))
	for i, data := range raw {
		var frame protocol.EventFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatalf("decode frame %d: %v (%q)", i, err, data)
		}
		frames = append(frames, frame)
	}
	return frames
}

func waitForFrameCount(t *testing.T, sub *mockSubscriber, want int) []protocol.EventFrame {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		frames := subscriberFrames(t, sub)
		if len(frames) >= want {
			return frames
		}
		time.Sleep(time.Millisecond)
	}
	frames := subscriberFrames(t, sub)
	t.Fatalf("frame count = %d, want at least %d", len(frames), want)
	return nil
}

func payloadMap(t *testing.T, frame protocol.EventFrame) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(frame.Payload, &payload); err != nil {
		t.Fatalf("decode payload for %q: %v", frame.Event, err)
	}
	return payload
}

func TestFilterNilEmptyAndSelectiveContracts(t *testing.T) {
	var nilFilter *Filter
	if !nilFilter.Accepts("anything") {
		t.Fatal("nil filter should accept all events")
	}
	empty := Filter{}
	if !empty.Accepts("anything") {
		t.Fatal("empty filter should accept all events")
	}
	f := Filter{Events: map[string]struct{}{"wanted": {}, "also": {}}}
	if !f.Accepts("wanted") || !f.Accepts("also") || f.Accepts("other") || f.Accepts("") {
		t.Fatalf("selective filter mismatch: %+v", f.Events)
	}
}

func TestSubscribeReplacementAndNilContract(t *testing.T) {
	b := NewBroadcaster()
	b.Subscribe(nil, Filter{})
	if b.Count() != 0 {
		t.Fatalf("nil subscription count = %d", b.Count())
	}
	first := &mockSubscriber{id: "same", authed: true}
	second := &mockSubscriber{id: "same", authed: true}
	b.Subscribe(first, Filter{})
	b.Subscribe(second, Filter{Events: map[string]struct{}{"new": {}}})
	if b.Count() != 1 {
		t.Fatalf("replacement count = %d", b.Count())
	}
	if sent, errs := b.Broadcast("old", EventPayload{}); sent != 0 || len(errs) != 0 {
		t.Fatalf("old event = sent=%d errs=%v", sent, errs)
	}
	if sent, errs := b.Broadcast("new", EventPayload{}); sent != 1 || len(errs) != 0 {
		t.Fatalf("new event = sent=%d errs=%v", sent, errs)
	}
	if len(subscriberFrames(t, first)) != 0 || len(subscriberFrames(t, second)) != 1 {
		t.Fatalf("replacement delivery first=%d second=%d", len(subscriberFrames(t, first)), len(subscriberFrames(t, second)))
	}
}

func TestBroadcastEmitsSequencedFramesWithStateAndTargeting(t *testing.T) {
	b := NewBroadcaster()
	one := &mockSubscriber{id: "one", authed: true}
	two := &mockSubscriber{id: "two", authed: true}
	b.Subscribe(one, Filter{})
	b.Subscribe(two, Filter{})
	version := protocol.StateVersion{Presence: 4, Health: 9}

	stateWire, _ := PayloadOf(map[string]any{"ready": true})
	if sent, errs := b.BroadcastWithOpts("state.changed", stateWire, BroadcastOpts{StateVersion: &version}); sent != 2 || len(errs) != 0 {
		t.Fatalf("broadcast = sent=%d errs=%v", sent, errs)
	}
	first := subscriberFrames(t, one)[0]
	if first.Type != protocol.FrameTypeEvent || first.Event != "state.changed" || first.Seq == nil || *first.Seq != 1 {
		t.Fatalf("first frame = %+v", first)
	}
	if first.StateVersion == nil || *first.StateVersion != version {
		t.Fatalf("state version = %+v", first.StateVersion)
	}
	if got := payloadMap(t, first)["ready"]; got != true {
		t.Fatalf("payload ready = %#v", got)
	}

	targets := map[string]struct{}{"two": {}}
	privateWire, _ := PayloadOf("private")
	if sent, errs := b.BroadcastWithOpts("targeted", privateWire, BroadcastOpts{TargetConnIDs: targets}); sent != 1 || len(errs) != 0 {
		t.Fatalf("targeted = sent=%d errs=%v", sent, errs)
	}
	if len(subscriberFrames(t, one)) != 1 {
		t.Fatal("untargeted subscriber received private event")
	}
	targeted := subscriberFrames(t, two)[1]
	if targeted.Seq != nil {
		t.Fatalf("targeted frame exposed broadcast sequence: %+v", targeted.Seq)
	}
	var text string
	if err := json.Unmarshal(targeted.Payload, &text); err != nil || text != "private" {
		t.Fatalf("target payload = %q/%v", text, err)
	}

	if sent, errs := b.BroadcastToConnIDs("nobody", EventPayload{}, map[string]struct{}{}); sent != 0 || len(errs) != 0 {
		t.Fatalf("empty target set = sent=%d errs=%v", sent, errs)
	}
}

func TestPayloadOfRejectsUnmarshalableValue(t *testing.T) {
	bad := make(chan int)
	if _, err := PayloadOf(bad); err == nil {
		t.Fatal("want marshal error for unmarshalable payload")
	}
}

func TestBroadcastStillDispatchesTapOnSendFailure(t *testing.T) {
	b := NewBroadcaster()
	sub := &mockSubscriber{id: "one", authed: true, failSend: true}
	b.Subscribe(sub, Filter{})
	var tapped atomic.Int64
	var gotEvent string
	var gotPayload EventPayload
	b.RegisterTap(func(event string, payload EventPayload) {
		tapped.Add(1)
		gotEvent, gotPayload = event, payload
	})
	wire, _ := PayloadOf(map[string]string{"k": "v"})
	sent, errs := b.Broadcast("bad.payload", wire)
	if sent != 0 || len(errs) != 1 {
		t.Fatalf("send failure = sent=%d errs=%v", sent, errs)
	}
	if tapped.Load() != 1 || gotEvent != "bad.payload" || gotPayload.IsZero() {
		t.Fatalf("tap = count=%d event=%q payload=%#v", tapped.Load(), gotEvent, gotPayload)
	}
	if len(subscriberFrames(t, sub)) != 0 {
		t.Fatal("failed send should not deliver subscriber frame")
	}
}

func TestSlowConsumerBoundaryForStructuredAndRawBroadcast(t *testing.T) {
	for _, tt := range []struct {
		name     string
		buffered int64
		want     int
	}{
		{name: "below", buffered: maxBufferedBytes - 1, want: 1},
		{name: "exactly threshold", buffered: maxBufferedBytes, want: 1},
		{name: "above", buffered: maxBufferedBytes + 1, want: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBroadcaster()
			b.SetLogger(nil)
			sub := &mockSubscriber{id: "slow", authed: true, bufferedAmount: tt.buffered}
			b.Subscribe(sub, Filter{})
			if sent, errs := b.BroadcastWithOpts("structured", EventPayload{}, BroadcastOpts{DropIfSlow: true}); sent != tt.want || len(errs) != 0 {
				t.Fatalf("structured = sent=%d errs=%v want=%d", sent, errs, tt.want)
			}
			if sent := b.BroadcastRaw("raw", []byte(`{"event":"raw"}`)); sent != tt.want {
				t.Fatalf("raw sent=%d want=%d", sent, tt.want)
			}
		})
	}
}

func TestBroadcastDeliveryMatrixAndErrorAggregation(t *testing.T) {
	b := NewBroadcaster()
	good := &mockSubscriber{id: "good", authed: true}
	failing := &mockSubscriber{id: "failing", authed: true, failSend: true}
	unauth := &mockSubscriber{id: "unauth", authed: false}
	filtered := &mockSubscriber{id: "filtered", authed: true}
	untargeted := &mockSubscriber{id: "untargeted", authed: true}
	b.Subscribe(good, Filter{})
	b.Subscribe(failing, Filter{})
	b.Subscribe(unauth, Filter{})
	b.Subscribe(filtered, Filter{Events: map[string]struct{}{"other": {}}})
	b.Subscribe(untargeted, Filter{})
	targets := map[string]struct{}{"good": {}, "failing": {}, "unauth": {}, "filtered": {}}
	deliveryWire, _ := PayloadOf(map[string]int{"n": 1})
	sent, errs := b.BroadcastWithOpts("event", deliveryWire, BroadcastOpts{TargetConnIDs: targets})
	if sent != 1 || len(errs) != 1 || errs[0].Error() != "send failed" {
		t.Fatalf("delivery = sent=%d errs=%v", sent, errs)
	}
	if len(subscriberFrames(t, good)) != 1 || len(subscriberFrames(t, unauth)) != 0 || len(subscriberFrames(t, filtered)) != 0 || len(subscriberFrames(t, untargeted)) != 0 {
		t.Fatal("delivery guards did not hold")
	}
}

type callbackSubscriber struct {
	id       string
	callback func()
	count    atomic.Int64
}

func (s *callbackSubscriber) ID() string            { return s.id }
func (s *callbackSubscriber) IsAuthenticated() bool { return true }
func (s *callbackSubscriber) BufferedAmount() int64 { return 0 }
func (s *callbackSubscriber) SendEvent([]byte) error {
	s.count.Add(1)
	if s.callback != nil {
		s.callback()
	}
	return nil
}

func TestBroadcasterAllowsReentrantSubscriberAndTapCallbacks(t *testing.T) {
	b := NewBroadcaster()
	sub := &callbackSubscriber{id: "self"}
	sub.callback = func() { b.Unsubscribe("self") }
	b.Subscribe(sub, Filter{})
	if sent, errs := b.Broadcast("first", EventPayload{}); sent != 1 || len(errs) != 0 {
		t.Fatalf("reentrant subscriber = sent=%d errs=%v", sent, errs)
	}
	if b.Count() != 0 || sub.count.Load() != 1 {
		t.Fatalf("post callback count=%d sends=%d", b.Count(), sub.count.Load())
	}

	var first, late atomic.Int64
	b.RegisterTap(func(string, EventPayload) {
		first.Add(1)
		b.RegisterTap(func(string, EventPayload) { late.Add(1) })
	})
	_, _ = b.Broadcast("tap-one", EventPayload{})
	if first.Load() != 1 || late.Load() != 0 {
		t.Fatalf("first tap dispatch = first=%d late=%d", first.Load(), late.Load())
	}
	_, _ = b.Broadcast("tap-two", EventPayload{})
	if first.Load() != 2 || late.Load() != 1 {
		t.Fatalf("second tap dispatch = first=%d late=%d", first.Load(), late.Load())
	}
}

func TestSessionAndToolRegistriesIgnoreEmptyKeysAndReturnCopies(t *testing.T) {
	b := NewBroadcaster()
	b.SubscribeSessionEvents("")
	b.SubscribeSessionMessageEvents("", "session")
	b.SubscribeSessionMessageEvents("conn", "")
	b.RegisterToolEventRecipient("", "conn")
	b.RegisterToolEventRecipient("run", "")
	if len(b.SessionEventSubscriberConnIDs()) != 0 || b.SessionMessageSubscriberConnIDs("session") != nil || b.ToolEventRecipient("run") != "" {
		t.Fatal("empty registry key was retained")
	}

	b.SubscribeSessionEvents("global")
	b.SubscribeSessionMessageEvents("specific", "session")
	b.SubscribeSessionMessageEvents("global", "session")
	global := b.SessionEventSubscriberConnIDs()
	specific := b.SessionMessageSubscriberConnIDs("session")
	merged := b.MergedSessionRecipients("session")
	delete(global, "global")
	delete(specific, "specific")
	delete(merged, "global")
	if len(b.SessionEventSubscriberConnIDs()) != 1 || len(b.SessionMessageSubscriberConnIDs("session")) != 2 {
		t.Fatal("caller mutated internal subscription maps")
	}
	wantMerged := map[string]struct{}{"global": {}, "specific": {}}
	if got := b.MergedSessionRecipients("session"); !reflect.DeepEqual(got, wantMerged) {
		t.Fatalf("merged recipients = %#v", got)
	}
}

func TestUnsubscribeClearsEveryRegistryReference(t *testing.T) {
	b := NewBroadcaster()
	b.Subscribe(&mockSubscriber{id: "gone", authed: true}, Filter{})
	b.SubscribeSessionEvents("gone")
	b.SubscribeSessionMessageEvents("gone", "one")
	b.SubscribeSessionMessageEvents("gone", "two")
	b.SubscribeSessionMessageEvents("stay", "two")
	b.RegisterToolEventRecipient("run-one", "gone")
	b.RegisterToolEventRecipient("run-two", "gone")
	b.RegisterToolEventRecipient("run-stay", "stay")
	b.Unsubscribe("gone")
	if b.Count() != 0 || len(b.SessionEventSubscriberConnIDs()) != 0 {
		t.Fatal("primary/session subscription survived unsubscribe")
	}
	if got := b.SessionMessageSubscriberConnIDs("one"); got != nil {
		t.Fatalf("empty session key retained: %#v", got)
	}
	if got := b.SessionMessageSubscriberConnIDs("two"); !reflect.DeepEqual(got, map[string]struct{}{"stay": {}}) {
		t.Fatalf("shared session cleanup = %#v", got)
	}
	if b.ToolEventRecipient("run-one") != "" || b.ToolEventRecipient("run-two") != "" || b.ToolEventRecipient("run-stay") != "stay" {
		t.Fatal("tool recipients were not selectively cleaned")
	}
}

func TestRawBroadcastAuthFilterSlowAndSendFailure(t *testing.T) {
	b := NewBroadcaster()
	good := &mockSubscriber{id: "good", authed: true}
	unauth := &mockSubscriber{id: "unauth"}
	filtered := &mockSubscriber{id: "filtered", authed: true}
	slow := &mockSubscriber{id: "slow", authed: true, bufferedAmount: maxBufferedBytes + 1}
	failing := &mockSubscriber{id: "failing", authed: true, failSend: true}
	b.Subscribe(good, Filter{})
	b.Subscribe(unauth, Filter{})
	b.Subscribe(filtered, Filter{Events: map[string]struct{}{"other": {}}})
	b.Subscribe(slow, Filter{})
	b.Subscribe(failing, Filter{})
	raw := []byte(`{"type":"event","event":"raw","payload":{"ok":true}}`)
	if sent := b.BroadcastRaw("raw", raw); sent != 1 {
		t.Fatalf("raw sent = %d", sent)
	}
	good.mu.Lock()
	got := append([]byte(nil), good.received[0]...)
	good.mu.Unlock()
	if !reflect.DeepEqual(got, raw) {
		t.Fatalf("raw payload = %q, want %q", got, raw)
	}
}

func TestPublisherEmitsEnrichedSessionMessageToScopedRecipients(t *testing.T) {
	b := NewBroadcaster()
	global := &mockSubscriber{id: "global", authed: true}
	specific := &mockSubscriber{id: "specific", authed: true}
	outsider := &mockSubscriber{id: "outsider", authed: true}
	b.Subscribe(global, Filter{})
	b.Subscribe(specific, Filter{})
	b.Subscribe(outsider, Filter{})
	b.SubscribeSessionEvents("global")
	b.SubscribeSessionMessageEvents("specific", "session-1")
	started := int64(123)
	provider := &mockSnapshotProvider{snapshots: map[string]*SessionSnapshot{
		"session-1": {SessionKey: "session-1", Status: "running", StartedAt: &started},
	}}
	pub := NewPublisher(b, provider, nil)
	seq := 7
	messageWire, _ := PayloadOf(map[string]any{"role": "assistant"})
	pub.PublishSessionMessage(TranscriptUpdate{SessionKey: "session-1", MessageID: "message-1", MessageSeq: &seq, Message: messageWire})

	globalFrames := subscriberFrames(t, global)
	specificFrames := subscriberFrames(t, specific)
	if len(globalFrames) != 2 || globalFrames[0].Event != "session.message" || globalFrames[1].Event != "sessions.changed" {
		t.Fatalf("global frames = %+v", globalFrames)
	}
	if len(specificFrames) != 1 || specificFrames[0].Event != "session.message" {
		t.Fatalf("specific frames = %+v", specificFrames)
	}
	if len(subscriberFrames(t, outsider)) != 0 {
		t.Fatal("outsider received session message")
	}
	payload := payloadMap(t, specificFrames[0])
	if payload["sessionKey"] != "session-1" || payload["messageId"] != "message-1" || payload["messageSeq"] != float64(7) {
		t.Fatalf("message payload = %#v", payload)
	}
	snapshot, ok := payload["session"].(map[string]any)
	if !ok || snapshot["status"] != "running" || snapshot["startedAt"] != float64(123) {
		t.Fatalf("snapshot payload = %#v", payload["session"])
	}
}

func TestPublisherRejectsIncompleteSessionUpdates(t *testing.T) {
	b := NewBroadcaster()
	sub := &mockSubscriber{id: "sub", authed: true}
	b.Subscribe(sub, Filter{})
	pub := NewPublisher(b, nil, nil)
	pub.PublishSessionMessage(TranscriptUpdate{Message: mustPayload(t, "missing key")})
	pub.PublishSessionMessage(TranscriptUpdate{SessionKey: "session"})
	pub.PublishSessionMessage(TranscriptUpdate{SessionKey: "unsubscribed", Message: mustPayload(t, "no recipient")})
	if len(subscriberFrames(t, sub)) != 0 {
		t.Fatalf("invalid session updates were published: %+v", subscriberFrames(t, sub))
	}
}

func TestPublisherAgentEventBoundaryExcludesOutsiders(t *testing.T) {
	b := NewBroadcaster()
	member := &mockSubscriber{id: "member", authed: true}
	outsider := &mockSubscriber{id: "outsider", authed: true}
	b.Subscribe(member, Filter{})
	b.Subscribe(outsider, Filter{})
	pub := NewPublisher(b, nil, nil)

	pub.PublishAgentEvent(AgentEvent{Kind: "tool.start", SessionKey: "private", RunID: "run-1"})
	if len(subscriberFrames(t, member)) != 0 || len(subscriberFrames(t, outsider)) != 0 {
		t.Fatal("session agent event with no recipients leaked globally")
	}
	b.SubscribeSessionMessageEvents("member", "private")
	pub.PublishAgentEvent(AgentEvent{Kind: "tool.end", SessionKey: "private", RunID: "run-1", Payload: mustPayload(t, map[string]any{"ok": true})})
	frames := subscriberFrames(t, member)
	if len(frames) != 1 || frames[0].Event != "agent.event" || frames[0].Seq != nil {
		t.Fatalf("member frames = %+v", frames)
	}
	if len(subscriberFrames(t, outsider)) != 0 {
		t.Fatal("outsider received private agent event")
	}
	payload := payloadMap(t, frames[0])
	if payload["kind"] != "tool.end" || payload["sessionKey"] != "private" || payload["runId"] != "run-1" || payload["seq"] != float64(2) {
		t.Fatalf("agent payload = %#v", payload)
	}
}

func TestPublisherAgentSequenceClearsOnCleanupAndConfig(t *testing.T) {
	b := NewBroadcaster()
	sub := &mockSubscriber{id: "sub", authed: true}
	b.Subscribe(sub, Filter{})
	pub := NewPublisher(b, nil, quietEventsLogger())
	pub.PublishAgentEvent(AgentEvent{Kind: "one", RunID: "run"})
	pub.PublishAgentEvent(AgentEvent{Kind: "two", RunID: "run"})
	pub.CleanupAgentSeq("run")
	pub.PublishAgentEvent(AgentEvent{Kind: "reset", RunID: "run"})
	pub.PublishAgentEvent(AgentEvent{Kind: "unsequenced"})
	pub.PublishConfigChanged("models")
	frames := subscriberFrames(t, sub)
	if len(frames) != 5 {
		t.Fatalf("frames = %d, want 5", len(frames))
	}
	wantSeq := []any{float64(1), float64(2), float64(1), nil}
	for i := range wantSeq {
		payload := payloadMap(t, frames[i])
		if payload["seq"] != wantSeq[i] {
			t.Errorf("frame %d seq = %#v, want %#v", i, payload["seq"], wantSeq[i])
		}
	}
	configPayload := payloadMap(t, frames[4])
	if frames[4].Event != "config.changed" || configPayload["section"] != "models" || configPayload["ts"].(float64) <= 0 {
		t.Fatalf("config frame = %+v payload=%#v", frames[4], configPayload)
	}
}

func quietEventsLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPublishSessionChangedOmitsSessionOnMissingSnapshot(t *testing.T) {
	b := NewBroadcaster()
	sub := &mockSubscriber{id: "sub", authed: true}
	b.Subscribe(sub, Filter{})
	b.SubscribeSessionEvents("sub")
	pub := NewPublisher(b, &mockSnapshotProvider{}, nil)
	pub.publishSessionChanged("session", "phase", map[string]any{"reason": "done", "phase": "override"})
	frames := subscriberFrames(t, sub)
	if len(frames) != 1 || frames[0].Event != "sessions.changed" {
		t.Fatalf("frames = %+v", frames)
	}
	payload := payloadMap(t, frames[0])
	if payload["sessionKey"] != "session" || payload["phase"] != "override" || payload["reason"] != "done" {
		t.Fatalf("payload = %#v", payload)
	}
	if _, exists := payload["session"]; exists {
		t.Fatalf("nil snapshot was serialized: %#v", payload)
	}
}

func TestPublisherNilSafetyContract(t *testing.T) {
	var nilPublisher *Publisher
	nilPublisher.PublishSessionMessage(TranscriptUpdate{SessionKey: "s", Message: mustPayload(t, "m")})
	nilPublisher.PublishAgentEvent(AgentEvent{Kind: "x"})
	nilPublisher.PublishConfigChanged("x")
	nilPublisher.CleanupAgentSeq("x")
	nilPublisher.publishSessionChanged("s", "p", nil)
	empty := NewPublisher(nil, nil, nil)
	empty.PublishSessionMessage(TranscriptUpdate{SessionKey: "s", Message: mustPayload(t, "m")})
	empty.PublishAgentEvent(AgentEvent{Kind: "x"})
	empty.PublishConfigChanged("x")
	empty.publishSessionChanged("s", "p", nil)
}

func TestGatewayEmitDropCountersWithBoundedQueues(t *testing.T) {
	g := &GatewayEventSubscriptions{
		agentCh:      make(chan AgentEvent, 1),
		transcriptCh: make(chan TranscriptUpdate, 1),
		lifecycleCh:  make(chan LifecycleChangeEvent, 1),
		done:         make(chan struct{}),
	}
	g.EmitAgent(AgentEvent{Kind: "kept"})
	g.EmitAgent(AgentEvent{Kind: "dropped"})
	g.EmitTranscript(TranscriptUpdate{SessionKey: "kept"})
	g.EmitTranscript(TranscriptUpdate{SessionKey: "dropped"})
	g.EmitLifecycle(LifecycleChangeEvent{SessionKey: "kept"})
	g.EmitLifecycle(LifecycleChangeEvent{SessionKey: "dropped"})
	if g.agentDrops.Load() != 1 || g.transcriptDrops.Load() != 1 || g.lifecycleDrops.Load() != 1 {
		t.Fatalf("drops = %d/%d/%d", g.agentDrops.Load(), g.transcriptDrops.Load(), g.lifecycleDrops.Load())
	}
	if (<-g.agentCh).Kind != "kept" || (<-g.transcriptCh).SessionKey != "kept" || (<-g.lifecycleCh).SessionKey != "kept" {
		t.Fatal("bounded queues did not retain first event")
	}
	g.Stop()
	g.Stop()
}

func TestGatewaySetPublisherConcurrentWithReads(t *testing.T) {
	g := &GatewayEventSubscriptions{}
	one := NewPublisher(NewBroadcaster(), nil, nil)
	two := NewPublisher(NewBroadcaster(), nil, nil)
	const iterations = 1000
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if (i+worker)%2 == 0 {
					g.SetPublisher(one)
				} else {
					g.SetPublisher(two)
				}
				if got := g.getPublisher(); got != one && got != two {
					t.Errorf("publisher = %p", got)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
}

func TestGatewayLoopsUsePublisherAndStopPromptly(t *testing.T) {
	b := NewBroadcaster()
	sub := &mockSubscriber{id: "sub", authed: true}
	b.Subscribe(sub, Filter{})
	b.SubscribeSessionMessageEvents("sub", "session")
	g := NewGatewayEventSubscriptions(GatewaySubscriptionParams{Broadcaster: b, Logger: quietEventsLogger()})
	pub := NewPublisher(b, nil, nil)
	g.SetPublisher(pub)
	g.EmitAgent(AgentEvent{Kind: "agent", SessionKey: "session", RunID: "run"})
	g.EmitTranscript(TranscriptUpdate{SessionKey: "session", Message: mustPayload(t, "hello")})
	frames := waitForFrameCount(t, sub, 2)
	eventsSeen := map[string]bool{}
	for _, frame := range frames {
		eventsSeen[frame.Event] = true
	}
	if !eventsSeen["agent.event"] || !eventsSeen["session.message"] {
		t.Fatalf("events = %#v", eventsSeen)
	}
	g.Stop()
	select {
	case <-g.done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not close done channel")
	}
}

func TestBroadcasterConcurrentMutationAndBroadcast(t *testing.T) {
	b := NewBroadcaster()
	const workers = 8
	const iterations = 100
	var wg sync.WaitGroup
	var unexpected atomic.Int64
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			id := fmt.Sprintf("conn-%d", worker)
			sub := &mockSubscriber{id: id, authed: true}
			for i := 0; i < iterations; i++ {
				b.Subscribe(sub, Filter{})
				b.SubscribeSessionEvents(id)
				b.SubscribeSessionMessageEvents(id, "session")
				b.RegisterToolEventRecipient(fmt.Sprintf("run-%d-%d", worker, i), id)
				loopWire, _ := PayloadOf(i)
				if _, errs := b.Broadcast("event", loopWire); len(errs) != 0 {
					unexpected.Add(1)
				}
				b.Unsubscribe(id)
			}
		}(worker)
	}
	wg.Wait()
	if unexpected.Load() != 0 {
		t.Fatalf("broadcast errors = %d", unexpected.Load())
	}
	if b.Count() != 0 || len(b.SessionEventSubscriberConnIDs()) != 0 || len(b.MergedSessionRecipients("session")) != 0 {
		t.Fatal("concurrent cleanup left registry entries")
	}
}

func TestTapPanicWithNilLoggerDoesNotBlockLaterTaps(t *testing.T) {
	b := NewBroadcaster()
	b.SetLogger(nil)
	b.RegisterTap(func(string, EventPayload) { panic("boom") })
	var called atomic.Bool
	b.RegisterTap(func(string, EventPayload) { called.Store(true) })
	if sent, errs := b.Broadcast("event", EventPayload{}); sent != 0 || len(errs) != 0 {
		t.Fatalf("broadcast = %d/%v", sent, errs)
	}
	if !called.Load() {
		t.Fatal("later tap did not run after panic")
	}
}

func TestCallbackSubscriberErrorStillAllowsOtherDeliveries(t *testing.T) {
	b := NewBroadcaster()
	wantErr := errors.New("closed")
	failing := &errorSubscriber{id: "fail", err: wantErr}
	good := &mockSubscriber{id: "good", authed: true}
	b.Subscribe(failing, Filter{})
	b.Subscribe(good, Filter{})
	sent, errs := b.Broadcast("event", EventPayload{})
	if sent != 1 || len(errs) != 1 || !errors.Is(errs[0], wantErr) {
		t.Fatalf("delivery = sent=%d errs=%v", sent, errs)
	}
}

type errorSubscriber struct {
	id  string
	err error
}

func (s *errorSubscriber) ID() string             { return s.id }
func (s *errorSubscriber) IsAuthenticated() bool  { return true }
func (s *errorSubscriber) BufferedAmount() int64  { return 0 }
func (s *errorSubscriber) SendEvent([]byte) error { return s.err }
