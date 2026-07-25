package runtimeops

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

func TestBoundaryPhoneActionAllowlistNormalization(t *testing.T) {
	valid := []string{"open_url", "open_app", "share", "message", "dial", "photo", "notify", "speak", "clipboard", "sync_state", "alarm", "timer"}
	for _, action := range valid {
		for _, raw := range []string{action, strings.ToUpper(action), " \t" + action + "\n"} {
			t.Run(raw, func(t *testing.T) {
				if !isPhoneAction(raw) {
					t.Fatalf("isPhoneAction(%q) = false", raw)
				}
			})
		}
	}
	for _, raw := range []string{"", " ", "exec", "shell", "open", "open-url", "timer!", "camera", "sms", "OPEN URL"} {
		t.Run("invalid_"+raw, func(t *testing.T) {
			if isPhoneAction(raw) {
				t.Fatalf("isPhoneAction(%q) = true", raw)
			}
		})
	}
}

func TestBoundaryBuildPhoneActionSuccessMatrix(t *testing.T) {
	tests := []struct {
		name string
		in   phoneWriteParams
		act  string
		args map[string]string
	}{
		{name: "open url", in: phoneWriteParams{To: " OPEN_URL ", Target: " https://example.test/a?q=1 "}, act: "open_url", args: map[string]string{"url": "https://example.test/a?q=1"}},
		{name: "open app", in: phoneWriteParams{To: "open_app", Target: " com.example.app "}, act: "open_app", args: map[string]string{"package": "com.example.app"}},
		{name: "share", in: phoneWriteParams{To: "share", Text: " share text ", Target: " recipient "}, act: "share", args: map[string]string{"text": " share text ", "to": "recipient"}},
		{name: "message", in: phoneWriteParams{To: "message", Text: " hello ", Target: " +8210 "}, act: "message", args: map[string]string{"text": " hello ", "to": "+8210"}},
		{name: "dial", in: phoneWriteParams{To: "dial", Target: " +82 10 1234 5678 "}, act: "dial", args: map[string]string{"number": "+82 10 1234 5678"}},
		{name: "photo", in: phoneWriteParams{To: "photo"}, act: "photo", args: map[string]string{}},
		{name: "notify default title", in: phoneWriteParams{To: "notify", Text: " notice "}, act: "notify", args: map[string]string{"title": "Deneb", "text": "notice"}},
		{name: "notify custom title", in: phoneWriteParams{To: "notify", Title: " Alert ", Text: " notice "}, act: "notify", args: map[string]string{"title": "Alert", "text": "notice"}},
		{name: "speak", in: phoneWriteParams{To: "speak", Text: " 말하기 "}, act: "speak", args: map[string]string{"text": "말하기"}},
		{name: "clipboard", in: phoneWriteParams{To: "clipboard", Text: " copy "}, act: "clipboard", args: map[string]string{"text": "copy"}},
		{name: "sync state", in: phoneWriteParams{To: "sync_state"}, act: "sync_state", args: map[string]string{}},
		{name: "alarm", in: phoneWriteParams{To: "alarm", Target: "07:05", Text: " wake "}, act: "alarm", args: map[string]string{"hour": "7", "minute": "5", "label": "wake"}},
		{name: "timer", in: phoneWriteParams{To: "timer", Target: "1m30s", Text: " break "}, act: "timer", args: map[string]string{"seconds": "90", "label": "break"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			act, args, err := buildPhoneAction(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if act != tt.act || !reflect.DeepEqual(args, tt.args) {
				t.Fatalf("buildPhoneAction = (%q,%#v), want (%q,%#v)", act, args, tt.act, tt.args)
			}
		})
	}
}

func TestBoundaryBuildPhoneActionValidationFailures(t *testing.T) {
	tests := []struct {
		name string
		in   phoneWriteParams
		want string
	}{
		{name: "unknown", in: phoneWriteParams{To: "exec"}, want: "not allowed"},
		{name: "empty action", in: phoneWriteParams{}, want: "not allowed"},
		{name: "relative URL", in: phoneWriteParams{To: "open_url", Target: "/relative"}, want: "valid absolute url"},
		{name: "empty URL", in: phoneWriteParams{To: "open_url"}, want: "valid absolute url"},
		{name: "empty app", in: phoneWriteParams{To: "open_app"}, want: "needs target"},
		{name: "empty share", in: phoneWriteParams{To: "share", Text: " "}, want: "needs text"},
		{name: "empty message", in: phoneWriteParams{To: "message"}, want: "needs text"},
		{name: "empty dial", in: phoneWriteParams{To: "dial", Target: " "}, want: "needs target"},
		{name: "empty notify", in: phoneWriteParams{To: "notify", Text: " "}, want: "needs text"},
		{name: "empty speak", in: phoneWriteParams{To: "speak"}, want: "needs text"},
		{name: "empty clipboard", in: phoneWriteParams{To: "clipboard"}, want: "needs text"},
		{name: "bad alarm", in: phoneWriteParams{To: "alarm", Target: "25:00"}, want: "out of range"},
		{name: "bad timer", in: phoneWriteParams{To: "timer", Target: "90"}, want: "explicit unit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, args, err := buildPhoneAction(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("buildPhoneAction = (%q,%#v,%v), want error containing %q", action, args, err, tt.want)
			}
			if action != "" || args != nil {
				t.Fatalf("failure returned partial action: %q %#v", action, args)
			}
		})
	}
}

func TestBoundaryDispatchPhoneActionOutcomeMatrix(t *testing.T) {
	wantFailure := errors.New("device rejected")
	tests := []struct {
		name      string
		params    phoneWriteParams
		send      tooldeps.PhoneActionFunc
		wantText  string
		wantErr   string
		wantCalls int
	}{
		{name: "nil sender", params: phoneWriteParams{To: "photo"}, send: nil, wantErr: "unavailable", wantCalls: 0},
		{name: "confirmed", params: phoneWriteParams{To: "photo"}, send: func(context.Context, string, map[string]string) error { return nil }, wantText: "launched on device: photo", wantCalls: 1},
		{name: "unconfirmed", params: phoneWriteParams{To: "timer", Target: "1s"}, send: func(context.Context, string, map[string]string) error {
			return fmt.Errorf("wait: %w", tooldeps.ErrPhoneActionUnconfirmed)
		}, wantText: "Do NOT retry", wantCalls: 1},
		{name: "real failure", params: phoneWriteParams{To: "photo"}, send: func(context.Context, string, map[string]string) error { return wantFailure }, wantErr: "device rejected", wantCalls: 1},
		{name: "validation before sender", params: phoneWriteParams{To: "timer", Target: "90"}, send: func(context.Context, string, map[string]string) error { return nil }, wantErr: "explicit unit", wantCalls: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			send := tt.send
			if send != nil {
				original := send
				send = func(ctx context.Context, action string, args map[string]string) error {
					calls++
					return original(ctx, action, args)
				}
			}
			got, err := dispatchPhoneAction(context.Background(), send, tt.params)
			if calls != tt.wantCalls {
				t.Fatalf("sender calls = %d, want %d", calls, tt.wantCalls)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("result = %q, %v, want error %q", got, err, tt.wantErr)
				}
				return
			}
			if err != nil || !strings.Contains(got, tt.wantText) {
				t.Fatalf("result = %q, %v, want text %q", got, err, tt.wantText)
			}
		})
	}
}
