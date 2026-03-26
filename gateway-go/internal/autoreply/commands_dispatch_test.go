package autoreply

import "testing"

func TestCommandDispatcher_ResetDetection(t *testing.T) {
	// Register a handler that catches /new.
	handler := func(params HandleCommandsFullParams, allowText bool) *CommandHandlerFullResult {
		if IsResetCommand(params.Command.CommandBodyNormalized) {
			return &CommandHandlerFullResult{
				Reply:          &ReplyPayload{Text: "Session reset."},
				ShouldContinue: false,
			}
		}
		return nil
	}
	d := NewCommandDispatcher([]CommandHandlerFull{handler}, nil)

	// Authorized /new.
	result := d.DispatchCommands(HandleCommandsFullParams{
		Ctx: &MsgContext{Body: "/new"},
		Command: CommandContextFull{
			CommandBodyNormalized: "/new",
			IsAuthorizedSender:    true,
		},
	})
	if result.ShouldContinue {
		t.Error("expected shouldContinue=false for /new command")
	}
}

func TestCommandDispatcher_UnauthorizedReset(t *testing.T) {
	d := NewCommandDispatcher(nil, nil)

	result := d.DispatchCommands(HandleCommandsFullParams{
		Ctx: &MsgContext{Body: "/reset"},
		Command: CommandContextFull{
			CommandBodyNormalized: "/reset",
			IsAuthorizedSender:    false,
			SenderID:              "unknown-user",
		},
	})
	if result.ShouldContinue {
		t.Error("expected shouldContinue=false for unauthorized reset")
	}
	if result.Reply != nil {
		t.Error("expected nil reply for unauthorized reset")
	}
}

func TestCommandDispatcher_ResetWithTail(t *testing.T) {
	d := NewCommandDispatcher(nil, nil)

	ctx := &MsgContext{Body: "/new what's the weather?"}
	result := d.DispatchCommands(HandleCommandsFullParams{
		Ctx: ctx,
		Command: CommandContextFull{
			CommandBodyNormalized: "/new what's the weather?",
			IsAuthorizedSender:    true,
		},
	})
	if result.ShouldContinue {
		t.Error("expected shouldContinue=false for /new with tail")
	}
	// Context should be rewritten with tail text.
	if ctx.Body != "what's the weather?" {
		t.Errorf("Body = %q, want 'what's the weather?'", ctx.Body)
	}
	if ctx.BodyForAgent != "what's the weather?" {
		t.Errorf("BodyForAgent = %q, want 'what's the weather?'", ctx.BodyForAgent)
	}
}

func TestCommandDispatcher_HandlerMatch(t *testing.T) {
	handlerCalled := false
	handler := func(params HandleCommandsFullParams, allowText bool) *CommandHandlerFullResult {
		if params.Command.CommandBodyNormalized == "/status" {
			handlerCalled = true
			return &CommandHandlerFullResult{
				Reply:          &ReplyPayload{Text: "ok"},
				ShouldContinue: false,
			}
		}
		return nil
	}

	d := NewCommandDispatcher([]CommandHandlerFull{handler}, nil)
	result := d.DispatchCommands(HandleCommandsFullParams{
		Ctx: &MsgContext{Body: "/status"},
		Command: CommandContextFull{
			CommandBodyNormalized: "/status",
			IsAuthorizedSender:    true,
		},
	})

	if !handlerCalled {
		t.Error("expected handler to be called")
	}
	if result.ShouldContinue {
		t.Error("expected shouldContinue=false")
	}
	if result.Reply == nil || result.Reply.Text != "ok" {
		t.Error("expected reply text 'ok'")
	}
}

func TestCommandDispatcher_NoMatchContinues(t *testing.T) {
	handler := func(params HandleCommandsFullParams, allowText bool) *CommandHandlerFullResult {
		return nil // does not match
	}

	d := NewCommandDispatcher([]CommandHandlerFull{handler}, nil)
	result := d.DispatchCommands(HandleCommandsFullParams{
		Ctx: &MsgContext{Body: "hello world"},
		Command: CommandContextFull{
			CommandBodyNormalized: "hello world",
			IsAuthorizedSender:    true,
		},
	})

	if !result.ShouldContinue {
		t.Error("expected shouldContinue=true when no handler matches")
	}
}

func TestCommandDispatcher_SendPolicyDeny(t *testing.T) {
	d := NewCommandDispatcher(nil, nil)
	d.SetSendPolicyFunc(func(sessionKey, channel, chatType string) string {
		return "deny"
	})

	result := d.DispatchCommands(HandleCommandsFullParams{
		Ctx: &MsgContext{Body: "hello"},
		Command: CommandContextFull{
			CommandBodyNormalized: "hello",
			IsAuthorizedSender:    true,
		},
		SessionKey: "session-1",
	})

	if result.ShouldContinue {
		t.Error("expected shouldContinue=false when send policy denies")
	}
}

func TestIsResetCommand(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"/new", true},
		{"/reset", true},
		{"/new hello", true},
		{"/reset world", true},
		{"/newuser", false},
		{"hello", false},
		{"", false},
	}
	for _, tt := range tests {
		got := IsResetCommand(tt.text)
		if got != tt.want {
			t.Errorf("IsResetCommand(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestParseResetCommand(t *testing.T) {
	action, tail := ParseResetCommand("/new what's up?")
	if action != ResetActionNew {
		t.Errorf("action = %q, want 'new'", action)
	}
	if tail != "what's up?" {
		t.Errorf("tail = %q, want 'what's up?'", tail)
	}

	action, tail = ParseResetCommand("/reset")
	if action != ResetActionReset {
		t.Errorf("action = %q, want 'reset'", action)
	}
	if tail != "" {
		t.Errorf("tail = %q, want empty", tail)
	}

	action, _ = ParseResetCommand("hello")
	if action != "" {
		t.Errorf("action = %q, want empty for non-reset", action)
	}
}
