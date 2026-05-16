package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHandleBtwCommand_NoDeps(t *testing.T) {
	res, err := handleBtwCommand(CommandContext{
		Command: "btw",
		Args:    &CommandArgs{Raw: "what is 2+2?"},
		// Deps intentionally nil — service unavailable path.
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError || !res.SkipAgent {
		t.Errorf("expected IsError + SkipAgent for missing deps, got %+v", res)
	}
	if !strings.Contains(res.Reply, "사용할 수 없") {
		t.Errorf("expected unavailability message, got %q", res.Reply)
	}
}

func TestHandleBtwCommand_NoDepsBtwFn(t *testing.T) {
	res, err := handleBtwCommand(CommandContext{
		Command: "btw",
		Args:    &CommandArgs{Raw: "what is 2+2?"},
		Deps:    &CommandDeps{}, // Deps present but BtwFn nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError when BtwFn is nil, got %+v", res)
	}
}

func TestHandleBtwCommand_NoArgsShowsUsage(t *testing.T) {
	called := false
	res, err := handleBtwCommand(CommandContext{
		Command: "btw",
		Args:    &CommandArgs{Raw: "   "}, // whitespace only
		Deps: &CommandDeps{
			BtwFn: func(_ context.Context, _, _ string) (string, error) {
				called = true
				return "should not be called", nil
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Errorf("BtwFn should not be invoked when question is empty")
	}
	if !res.SkipAgent {
		t.Errorf("expected SkipAgent=true for usage reply")
	}
	if res.IsError {
		t.Errorf("usage reply is not an error condition")
	}
	if !strings.Contains(res.Reply, "/btw") {
		t.Errorf("expected usage hint to mention /btw, got %q", res.Reply)
	}
}

func TestHandleBtwCommand_HappyPath(t *testing.T) {
	var gotSession, gotQuestion string
	res, err := handleBtwCommand(CommandContext{
		Command:    "btw",
		Args:       &CommandArgs{Raw: "환율 얼마야?"},
		SessionKey: "telegram:42",
		Deps: &CommandDeps{
			BtwFn: func(_ context.Context, sk, q string) (string, error) {
				gotSession, gotQuestion = sk, q
				return "1330원입니다.\n\n— BTW", nil
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotSession != "telegram:42" {
		t.Errorf("session key not forwarded: got %q", gotSession)
	}
	if gotQuestion != "환율 얼마야?" {
		t.Errorf("question not forwarded verbatim: got %q", gotQuestion)
	}
	if res.IsError {
		t.Errorf("happy path should not set IsError")
	}
	if !res.SkipAgent {
		t.Errorf("BTW handler must always SkipAgent — answer is the reply, no agent passthrough")
	}
	if !strings.Contains(res.Reply, "1330원") {
		t.Errorf("expected answer text in reply, got %q", res.Reply)
	}
}

func TestHandleBtwCommand_BtwFnError(t *testing.T) {
	res, err := handleBtwCommand(CommandContext{
		Command: "btw",
		Args:    &CommandArgs{Raw: "hello"},
		Deps: &CommandDeps{
			BtwFn: func(_ context.Context, _, _ string) (string, error) {
				return "", errors.New("backend down")
			},
		},
	})
	if err != nil {
		t.Fatalf("handler should not propagate BtwFn errors as Go errors, got %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError when BtwFn fails")
	}
	if !strings.Contains(res.Reply, "backend down") {
		t.Errorf("expected error reason in reply, got %q", res.Reply)
	}
}

func TestHandleBtwCommand_EmptyAnswer(t *testing.T) {
	res, _ := handleBtwCommand(CommandContext{
		Command: "btw",
		Args:    &CommandArgs{Raw: "hello"},
		Deps: &CommandDeps{
			BtwFn: func(_ context.Context, _, _ string) (string, error) {
				return "   ", nil // whitespace = empty
			},
		},
	})
	if !res.IsError {
		t.Errorf("empty answer should surface as error so user sees something failed")
	}
}

// TestCommandRouter_DispatchBtw — wire-level check that /btw is registered
// in the default router so a future refactor that drops the registration
// fails loudly instead of silently regressing the feature back to orphan
// status (see "by the way 기능이 있나?" investigation).
func TestCommandRouter_DispatchBtw(t *testing.T) {
	r := NewCommandRouter(NewCommandRegistry(BuiltinChatCommands()))
	if !r.HasHandler("btw") {
		t.Fatal("router should have a 'btw' handler registered")
	}
}
