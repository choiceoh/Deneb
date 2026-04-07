package typing

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"testing"
)

func TestResolveTypingMode(t *testing.T) {
	tests := []struct {
		name string
		ctx  TypingModeContext
		want TypingMode
	}{
		{
			name: "heartbeat returns never",
			ctx:  TypingModeContext{IsHeartbeat: true},
			want: TypingModeNever,
		},
		{
			name: "heartbeat policy returns never",
			ctx:  TypingModeContext{TypingPolicy: types.TypingPolicyHeartbeat},
			want: TypingModeNever,
		},
		{
			name: "system_event policy returns never",
			ctx:  TypingModeContext{TypingPolicy: types.TypingPolicySystemEvent},
			want: TypingModeNever,
		},
		{
			name: "suppress typing returns never",
			ctx:  TypingModeContext{SuppressTyping: true},
			want: TypingModeNever,
		},
		{
			name: "configured mode is used when set",
			ctx:  TypingModeContext{Configured: TypingModeThinking},
			want: TypingModeThinking,
		},
		{
			name: "direct message defaults to instant",
			ctx:  TypingModeContext{IsGroupChat: false},
			want: TypingModeInstant,
		},
		{
			name: "mentioned in group defaults to instant",
			ctx:  TypingModeContext{IsGroupChat: true, WasMentioned: true},
			want: TypingModeInstant,
		},
		{
			name: "unmentioned group defaults to message",
			ctx:  TypingModeContext{IsGroupChat: true, WasMentioned: false},
			want: DefaultGroupTypingMode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveTypingMode(tt.ctx)
			if got != tt.want {
				t.Errorf("ResolveTypingMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFullTypingSignaler_SignalRunStart(t *testing.T) {
	started := false
	tc := NewTypingController(TypingControllerConfig{
		OnStart: func() { started = true },
	})
	defer tc.Cleanup()

	// "instant" mode starts typing on run start.
	s := NewFullTypingSignaler(tc, TypingModeInstant, false)
	s.SignalRunStart()
	if !started {
		t.Error("expected typing to start on run start in instant mode")
	}
}

func TestFullTypingSignaler_SignalRunStart_Never(t *testing.T) {
	started := false
	tc := NewTypingController(TypingControllerConfig{
		OnStart: func() { started = true },
	})
	defer tc.Cleanup()

	s := NewFullTypingSignaler(tc, TypingModeNever, false)
	s.SignalRunStart()
	if started {
		t.Error("expected typing NOT to start in never mode")
	}
}

func TestFullTypingSignaler_SignalTextDelta_FiltersSilentReply(t *testing.T) {
	started := false
	tc := NewTypingController(TypingControllerConfig{
		OnStart: func() { started = true },
	})
	defer tc.Cleanup()

	s := NewFullTypingSignaler(tc, TypingModeInstant, false)
	s.SignalTextDelta("NO_REPLY")
	if started {
		t.Error("expected typing NOT to start for silent reply token")
	}
}

func TestFullTypingSignaler_SignalTextDelta_StartsOnRealText(t *testing.T) {
	started := false
	tc := NewTypingController(TypingControllerConfig{
		OnStart: func() { started = true },
	})
	defer tc.Cleanup()

	s := NewFullTypingSignaler(tc, TypingModeInstant, false)
	s.SignalTextDelta("Hello, world!")
	if !started {
		t.Error("expected typing to start on real text")
	}
}

func TestFullTypingSignaler_Disabled_Heartbeat(t *testing.T) {
	started := false
	tc := NewTypingController(TypingControllerConfig{
		OnStart: func() { started = true },
	})
	defer tc.Cleanup()

	s := NewFullTypingSignaler(tc, TypingModeInstant, true) // isHeartbeat=true
	s.SignalRunStart()
	s.SignalTextDelta("Hello")
	s.SignalToolStart()
	if started {
		t.Error("expected all signals to be no-ops for heartbeat")
	}
}
