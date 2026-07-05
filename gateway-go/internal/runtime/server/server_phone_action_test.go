package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

func TestPhoneActionAwaiter(t *testing.T) {
	a := newPhoneActionAwaiter()

	ch := a.register("pa1")
	if !a.resolve(phoneActionResult{ID: "pa1", OK: true}) {
		t.Fatal("resolve must find the registered waiter")
	}
	select {
	case res := <-ch:
		if !res.OK {
			t.Error("OK lost in transit")
		}
	default:
		t.Fatal("result not delivered to the waiter channel")
	}
	// Single-use: a duplicate report for the same id finds no waiter.
	if a.resolve(phoneActionResult{ID: "pa1", OK: false}) {
		t.Error("resolved id must be single-use")
	}

	// Timeout path: drop() removes the waiter, so a late report is discarded.
	_ = a.register("pa2")
	a.drop("pa2")
	if a.resolve(phoneActionResult{ID: "pa2", OK: true}) {
		t.Error("late report after drop must find no waiter")
	}
	if a.resolve(phoneActionResult{ID: "never-dispatched", OK: true}) {
		t.Error("unknown id must find no waiter")
	}
}

// phoneActionTestServer builds the minimal Server slice dispatchPhoneAction and
// the phone_action_result ingest branch touch: push hub + awaiter + logger.
func phoneActionTestServer() *Server {
	return &Server{
		pushHub:      newClientPushHub(),
		phoneActions: newPhoneActionAwaiter(),
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestDispatchPhoneAction_ConfirmedRoundTrip(t *testing.T) {
	s := phoneActionTestServer()
	frames, unsub := s.pushHub.subscribe(kindMobile)
	defer unsub()

	done := make(chan error, 1)
	go func() {
		done <- s.dispatchPhoneAction(context.Background(), "timer", map[string]string{"seconds": "600"})
	}()

	frame := <-frames
	if frame.Kind != pushKindPhoneAction || frame.Data["action"] != "timer" {
		t.Fatalf("unexpected frame: %+v", frame)
	}
	if frame.Ref == "" {
		t.Fatal("dispatch must carry a correlation id in Ref")
	}
	// The app reports success through the shared ingest door.
	s.ingestPhoneEventAsync("phone_action_result", "timer", `{"id":"`+frame.Ref+`","ok":true}`)
	if err := <-done; err != nil {
		t.Fatalf("confirmed dispatch must return nil, got %v", err)
	}
}

func TestDispatchPhoneAction_ReportedFailure(t *testing.T) {
	s := phoneActionTestServer()
	frames, unsub := s.pushHub.subscribe(kindMobile)
	defer unsub()

	done := make(chan error, 1)
	go func() {
		done <- s.dispatchPhoneAction(context.Background(), "alarm", map[string]string{"hour": "7", "minute": "0"})
	}()
	frame := <-frames
	s.ingestPhoneEventAsync("phone_action_result", "alarm", `{"id":"`+frame.Ref+`","ok":false}`)
	err := <-done
	if err == nil || !strings.Contains(err.Error(), "failed on the device") {
		t.Fatalf("reported failure must surface as an error, got %v", err)
	}
	if errors.Is(err, tools.ErrPhoneActionUnconfirmed) {
		t.Error("a reported failure is confirmed, not unconfirmed")
	}
}

func TestDispatchPhoneAction_UnconfirmedOnCancel(t *testing.T) {
	s := phoneActionTestServer()
	frames, unsub := s.pushHub.subscribe(kindMobile)
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.dispatchPhoneAction(ctx, "open_url", map[string]string{"url": "https://x.com"})
	}()
	<-frames // frame delivered, app never reports
	cancel()
	if err := <-done; !errors.Is(err, tools.ErrPhoneActionUnconfirmed) {
		t.Fatalf("no report + canceled turn must be unconfirmed, got %v", err)
	}
}

func TestDispatchPhoneAction_SyncStateFireAndForget(t *testing.T) {
	s := phoneActionTestServer()
	frames, unsub := s.pushHub.subscribe(kindMobile)
	defer unsub()

	// Returns nil immediately — no waiting, no correlation id.
	if err := s.dispatchPhoneAction(context.Background(), "sync_state", map[string]string{}); err != nil {
		t.Fatalf("sync_state dispatch: %v", err)
	}
	frame := <-frames
	if frame.Ref != "" {
		t.Errorf("sync_state must not carry a correlation id, got %q", frame.Ref)
	}
}

func TestDispatchPhoneAction_NoMobileSubscriber(t *testing.T) {
	s := phoneActionTestServer()
	// Desktop-only connection must not read as dispatchable.
	_, unsub := s.pushHub.subscribe(kindDesktop)
	defer unsub()
	if err := s.dispatchPhoneAction(context.Background(), "timer", map[string]string{"seconds": "60"}); err == nil {
		t.Fatal("dispatch without a mobile subscriber must error")
	}
}
