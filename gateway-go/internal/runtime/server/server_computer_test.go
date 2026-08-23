package server

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativepush"
	handlerchatminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat/miniapp"
)

func computerTestServer() *Server {
	return &Server{
		pushHub:         nativepush.NewHub(),
		computerActions: newComputerAwaiter(),
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestComputerAwaiter_ResolveOnceLateAndAggregation(t *testing.T) {
	a := newComputerAwaiter()

	ch := a.register("cu1", 1)
	if !a.resolve(computerResult{ID: "cu1", OK: true, Text: "(1,2)"}) {
		t.Fatal("resolve must find the registered waiter")
	}
	select {
	case res := <-ch:
		if !res.OK || res.Text != "(1,2)" {
			t.Errorf("payload lost in transit: %+v", res)
		}
	default:
		t.Fatal("result not delivered")
	}
	if a.resolve(computerResult{ID: "cu1", OK: false}) {
		t.Error("resolved id must be single-use")
	}

	_ = a.register("cu2", 1)
	a.drop("cu2")
	if a.resolve(computerResult{ID: "cu2", OK: true}) {
		t.Error("late report after drop must find no waiter")
	}

	// fanout=2: first failure absorbed, success resolves.
	ch3 := a.register("cu3", 2)
	if !a.resolve(computerResult{ID: "cu3", OK: false, Error: "disabled"}) {
		t.Fatal("first failure must be absorbed (true)")
	}
	select {
	case <-ch3:
		t.Fatal("one failure of two must not resolve")
	default:
	}
	a.resolve(computerResult{ID: "cu3", OK: true})
	select {
	case res := <-ch3:
		if !res.OK {
			t.Error("success must win over an earlier partial failure")
		}
	default:
		t.Fatal("success must resolve")
	}
}

func TestDispatchComputerCommand_GatesOnDesktop(t *testing.T) {
	s := computerTestServer()
	if _, err := s.dispatchComputerCommand(context.Background(), "screenshot", nil); err == nil || !strings.Contains(err.Error(), "데스크톱") {
		t.Fatalf("no desktop must error in Korean, got %v", err)
	}
	// A mobile-only subscriber must not count as a desktop.
	_, unsub := s.pushHub.Subscribe(nativepush.KindMobile)
	defer unsub()
	if _, err := s.dispatchComputerCommand(context.Background(), "screenshot", nil); err == nil {
		t.Fatal("mobile-only connection must not satisfy the desktop gate")
	}
}

func TestDispatchComputerCommand_AckRoundTrip(t *testing.T) {
	s := computerTestServer()
	frames, unsub := s.pushHub.Subscribe(nativepush.KindDesktop)
	defer unsub()

	type out struct {
		text string
		err  error
	}
	done := make(chan out, 1)
	go func() {
		text, err := s.dispatchComputerCommand(context.Background(), "click", map[string]string{"x": "10", "y": "20", "button": "left"})
		done <- out{text, err}
	}()

	frame := <-frames
	if frame.Kind != nativepush.PushKindComputer || frame.Data["action"] != "click" || frame.Data["x"] != "10" {
		t.Fatalf("unexpected frame: %+v", frame)
	}
	if frame.Ref == "" || frame.Title != "컴퓨터 조종" {
		t.Fatalf("frame must carry Ref + Korean title: %+v", frame)
	}
	if !s.resolveComputerResult(handlerchatminiapp.ComputerResult{ID: frame.Ref, OK: true}) {
		t.Fatal("report must find the waiter")
	}
	r := <-done
	if r.err != nil || !strings.Contains(r.text, "클릭 (10,20)") {
		t.Fatalf("ack text=%q err=%v", r.text, r.err)
	}
}

func TestDispatchComputerCommand_ReportedFailureAndCancel(t *testing.T) {
	s := computerTestServer()
	frames, unsub := s.pushHub.Subscribe(nativepush.KindDesktop)
	defer unsub()

	done := make(chan error, 1)
	go func() {
		_, err := s.dispatchComputerCommand(context.Background(), "type", map[string]string{"text": "hi"})
		done <- err
	}()
	frame := <-frames
	s.resolveComputerResult(handlerchatminiapp.ComputerResult{ID: frame.Ref, OK: false, Error: "설정에서 꺼짐"})
	if err := <-done; err == nil || !strings.Contains(err.Error(), "설정에서 꺼짐") {
		t.Fatalf("desktop failure detail must reach the tool: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done2 := make(chan error, 1)
	go func() {
		_, err := s.dispatchComputerCommand(ctx, "cursor", nil)
		done2 <- err
	}()
	<-frames
	cancel()
	select {
	case err := <-done2:
		if err == nil || !strings.Contains(err.Error(), "턴이 종료") {
			t.Fatalf("cancel must surface as turn-ended error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch must return promptly on ctx cancel")
	}
	if s.resolveComputerResult(handlerchatminiapp.ComputerResult{ID: "cu-unknown", OK: true}) {
		t.Fatal("unknown id must report unresolved")
	}
}
