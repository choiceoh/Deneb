package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind"
)

func ingestPhoneActionResult(s *Server, eventType, source, text string) {
	phoneevents.New(phoneevents.Config{
		Logger: s.logger,
		ResolvePhoneAction: func(res phoneevents.ActionResult) bool {
			return s.phoneActions.resolve(phoneActionResult{ID: res.ID, OK: res.OK, Error: res.Error})
		},
	}).IngestAsync(eventType, source, text)
}

func TestPhoneActionAwaiterResolvesOnceAndIgnoresLateOrUnknownReports(t *testing.T) {
	a := newPhoneActionAwaiter()

	ch := a.register("pa1", 1)
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
	_ = a.register("pa2", 1)
	a.drop("pa2")
	if a.resolve(phoneActionResult{ID: "pa2", OK: true}) {
		t.Error("late report after drop must find no waiter")
	}
	if a.resolve(phoneActionResult{ID: "never-dispatched", OK: true}) {
		t.Error("unknown id must find no waiter")
	}
}

func TestPhoneActionAwaiterAggregationPreservesSuccessOverPartialFailure(t *testing.T) {
	a := newPhoneActionAwaiter()

	// fanout=2: one failure is absorbed, a later success resolves.
	ch := a.register("agg1", 2)
	if !a.resolve(phoneActionResult{ID: "agg1", OK: false}) {
		t.Fatal("first failure must be absorbed by the waiter")
	}
	select {
	case <-ch:
		t.Fatal("one failure of two must not resolve the dispatch")
	default:
	}
	if !a.resolve(phoneActionResult{ID: "agg1", OK: true}) {
		t.Fatal("success after absorbed failure must resolve")
	}
	if res := <-ch; !res.OK {
		t.Error("the delivered verdict must be the success")
	}

	// fanout=2: only when EVERY subscriber failed does failure resolve.
	ch2 := a.register("agg2", 2)
	a.resolve(phoneActionResult{ID: "agg2", OK: false})
	a.resolve(phoneActionResult{ID: "agg2", OK: false})
	select {
	case res := <-ch2:
		if res.OK {
			t.Error("all-failed must deliver a failure verdict")
		}
	default:
		t.Fatal("second failure of two must resolve the dispatch")
	}
}

// phoneActionTestServer builds the minimal Server slice dispatchPhoneAction and
// the phone_action_result ingest branch touch: push hub + awaiter + logger.
func phoneActionTestServer() *Server {
	return &Server{
		pushHub:      proactive.NewHub(),
		phoneActions: newPhoneActionAwaiter(),
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestDispatchPhoneAction_ConfirmedRoundTrip(t *testing.T) {
	s := phoneActionTestServer()
	frames, unsub := s.pushHub.Subscribe(proactive.KindMobile)
	defer unsub()

	done := make(chan error, 1)
	go func() {
		done <- s.dispatchPhoneAction(context.Background(), "timer", map[string]string{"seconds": "600"})
	}()

	frame := <-frames
	if frame.Kind != proactive.PushKindPhoneAction || frame.Data["action"] != "timer" {
		t.Fatalf("unexpected frame: %+v", frame)
	}
	if frame.Ref == "" {
		t.Fatal("dispatch must carry a correlation id in Ref")
	}
	// The app reports success through the shared ingest door.
	ingestPhoneActionResult(s, "phone_action_result", "timer", `{"id":"`+frame.Ref+`","ok":true}`)
	if err := <-done; err != nil {
		t.Fatalf("confirmed dispatch must return nil, got %v", err)
	}
}

func TestDispatchPhoneAction_ReportedFailure(t *testing.T) {
	s := phoneActionTestServer()
	frames, unsub := s.pushHub.Subscribe(proactive.KindMobile)
	defer unsub()

	done := make(chan error, 1)
	go func() {
		done <- s.dispatchPhoneAction(context.Background(), "alarm", map[string]string{"hour": "7", "minute": "0"})
	}()
	frame := <-frames
	ingestPhoneActionResult(s, "phone_action_result", "alarm", `{"id":"`+frame.Ref+`","ok":false}`)
	err := <-done
	if err == nil || !strings.Contains(err.Error(), "failed on the device") {
		t.Fatalf("reported failure must surface as an error, got %v", err)
	}
	if errors.Is(err, toolbind.ErrPhoneActionUnconfirmed) {
		t.Error("a reported failure is confirmed, not unconfirmed")
	}
}

// The headless verification harness connects to production as a mobile client
// and (desktop build) reports ok=false for every intent action, typically
// before the real phone answers. The harness's failure must not mask the
// phone's success.
func TestDispatchPhoneAction_HarnessFailureDoesNotMaskPhoneSuccess(t *testing.T) {
	s := phoneActionTestServer()
	phone, unsubPhone := s.pushHub.Subscribe(proactive.KindMobile)
	defer unsubPhone()
	harness, unsubHarness := s.pushHub.Subscribe(proactive.KindMobile)
	defer unsubHarness()

	done := make(chan error, 1)
	go func() {
		done <- s.dispatchPhoneAction(context.Background(), "alarm", map[string]string{"hour": "7", "minute": "0"})
	}()
	frame := <-phone
	<-harness

	// Harness reports failure first, then the real phone succeeds.
	ingestPhoneActionResult(s, "phone_action_result", "alarm", `{"id":"`+frame.Ref+`","ok":false}`)
	ingestPhoneActionResult(s, "phone_action_result", "alarm", `{"id":"`+frame.Ref+`","ok":true}`)
	if err := <-done; err != nil {
		t.Fatalf("phone success must win over harness failure, got %v", err)
	}
}

func TestDispatchPhoneAction_AllSubscribersFailed(t *testing.T) {
	s := phoneActionTestServer()
	phone, unsubPhone := s.pushHub.Subscribe(proactive.KindMobile)
	defer unsubPhone()
	harness, unsubHarness := s.pushHub.Subscribe(proactive.KindMobile)
	defer unsubHarness()

	done := make(chan error, 1)
	go func() {
		done <- s.dispatchPhoneAction(context.Background(), "timer", map[string]string{"seconds": "60"})
	}()
	frame := <-phone
	<-harness

	ingestPhoneActionResult(s, "phone_action_result", "timer", `{"id":"`+frame.Ref+`","ok":false}`)
	ingestPhoneActionResult(s, "phone_action_result", "timer", `{"id":"`+frame.Ref+`","ok":false}`)
	err := <-done
	if err == nil || !strings.Contains(err.Error(), "failed on the device") {
		t.Fatalf("unanimous failure must surface as an error, got %v", err)
	}
}

func TestDispatchPhoneAction_UnconfirmedOnCancel(t *testing.T) {
	s := phoneActionTestServer()
	frames, unsub := s.pushHub.Subscribe(proactive.KindMobile)
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.dispatchPhoneAction(ctx, "open_url", map[string]string{"url": "https://x.com"})
	}()
	<-frames // frame delivered, app never reports
	cancel()
	if err := <-done; !errors.Is(err, toolbind.ErrPhoneActionUnconfirmed) {
		t.Fatalf("no report + canceled turn must be unconfirmed, got %v", err)
	}
}

func TestDispatchPhoneActionSyncStateReturnsNilWithoutCorrelationID(t *testing.T) {
	s := phoneActionTestServer()
	frames, unsub := s.pushHub.Subscribe(proactive.KindMobile)
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

func TestDispatchPhoneActionReturnsErrorWithoutMobileSubscriber(t *testing.T) {
	s := phoneActionTestServer()
	// Desktop-only connection must not read as dispatchable.
	_, unsub := s.pushHub.Subscribe(proactive.KindDesktop)
	defer unsub()
	if err := s.dispatchPhoneAction(context.Background(), "timer", map[string]string{"seconds": "60"}); err == nil {
		t.Fatal("dispatch without a mobile subscriber must error")
	}
}
