package autoreply

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/typing"
)

func TestResolveTypingMode(t *testing.T) {
	tests := []struct {
		name string
		ctx  typing.TypingModeContext
		want typing.TypingMode
	}{
		{
			name: "heartbeat returns never",
			ctx:  typing.TypingModeContext{IsHeartbeat: true},
			want: typing.TypingModeNever,
		},
		{
			name: "heartbeat policy returns never",
			ctx:  typing.TypingModeContext{TypingPolicy: types.TypingPolicyHeartbeat},
			want: typing.TypingModeNever,
		},
		{
			name: "system_event policy returns never",
			ctx:  typing.TypingModeContext{TypingPolicy: types.TypingPolicySystemEvent},
			want: typing.TypingModeNever,
		},
		{
			name: "internal_webchat policy returns never",
			ctx:  typing.TypingModeContext{TypingPolicy: types.TypingPolicyInternalWeb},
			want: typing.TypingModeNever,
		},
		{
			name: "suppress typing returns never",
			ctx:  typing.TypingModeContext{SuppressTyping: true},
			want: typing.TypingModeNever,
		},
		{
			name: "configured mode is used when set",
			ctx:  typing.TypingModeContext{Configured: typing.TypingModeThinking},
			want: typing.TypingModeThinking,
		},
		{
			name: "direct message defaults to instant",
			ctx:  typing.TypingModeContext{IsGroupChat: false},
			want: typing.TypingModeInstant,
		},
		{
			name: "mentioned in group defaults to instant",
			ctx:  typing.TypingModeContext{IsGroupChat: true, WasMentioned: true},
			want: typing.TypingModeInstant,
		},
		{
			name: "unmentioned group defaults to message",
			ctx:  typing.TypingModeContext{IsGroupChat: true, WasMentioned: false},
			want: typing.DefaultGroupTypingMode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := typing.ResolveTypingMode(tt.ctx)
			if got != tt.want {
				t.Errorf("ResolveTypingMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFullTypingSignaler_SignalRunStart(t *testing.T) {
	started := false
	tc := typing.NewTypingController(typing.TypingControllerConfig{
		OnStart: func() { started = true },
	})
	defer tc.Cleanup()

	// "instant" mode starts typing on run start.
	s := typing.NewFullTypingSignaler(tc, typing.TypingModeInstant, false)
	s.SignalRunStart()
	if !started {
		t.Error("expected typing to start on run start in instant mode")
	}
}

func TestFullTypingSignaler_SignalRunStart_Never(t *testing.T) {
	started := false
	tc := typing.NewTypingController(typing.TypingControllerConfig{
		OnStart: func() { started = true },
	})
	defer tc.Cleanup()

	s := typing.NewFullTypingSignaler(tc, typing.TypingModeNever, false)
	s.SignalRunStart()
	if started {
		t.Error("expected typing NOT to start in never mode")
	}
}

func TestFullTypingSignaler_SignalTextDelta_FiltersSilentReply(t *testing.T) {
	started := false
	tc := typing.NewTypingController(typing.TypingControllerConfig{
		OnStart: func() { started = true },
	})
	defer tc.Cleanup()

	s := typing.NewFullTypingSignaler(tc, typing.TypingModeInstant, false)
	s.SignalTextDelta("NO_REPLY")
	if started {
		t.Error("expected typing NOT to start for silent reply token")
	}
}

func TestFullTypingSignaler_SignalTextDelta_StartsOnRealText(t *testing.T) {
	started := false
	tc := typing.NewTypingController(typing.TypingControllerConfig{
		OnStart: func() { started = true },
	})
	defer tc.Cleanup()

	s := typing.NewFullTypingSignaler(tc, typing.TypingModeInstant, false)
	s.SignalTextDelta("Hello, world!")
	if !started {
		t.Error("expected typing to start on real text")
	}
}

func TestFullTypingSignaler_Disabled_Heartbeat(t *testing.T) {
	started := false
	tc := typing.NewTypingController(typing.TypingControllerConfig{
		OnStart: func() { started = true },
	})
	defer tc.Cleanup()

	s := typing.NewFullTypingSignaler(tc, typing.TypingModeInstant, true) // isHeartbeat=true
	s.SignalRunStart()
	s.SignalTextDelta("Hello")
	s.SignalToolStart()
	if started {
		t.Error("expected all signals to be no-ops for heartbeat")
	}
}
