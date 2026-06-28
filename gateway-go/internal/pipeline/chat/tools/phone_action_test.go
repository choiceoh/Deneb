package tools

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildPhoneAction_Valid(t *testing.T) {
	cases := []struct {
		p      phoneWriteParams
		action string
		args   map[string]string
	}{
		{phoneWriteParams{To: "open_url", Target: "https://x.com"}, "open_url", map[string]string{"url": "https://x.com"}},
		{phoneWriteParams{To: "open_app", Target: "com.kakao.talk"}, "open_app", map[string]string{"package": "com.kakao.talk"}},
		{phoneWriteParams{To: "message", Target: "01012345678", Text: "안녕"}, "message", map[string]string{"text": "안녕", "to": "01012345678"}},
		{phoneWriteParams{To: "SHARE", Text: "hi"}, "share", map[string]string{"text": "hi"}}, // case-insensitive, recipient optional
		{phoneWriteParams{To: "dial", Target: "119"}, "dial", map[string]string{"number": "119"}},
		{phoneWriteParams{To: "photo"}, "photo", map[string]string{}},
	}
	for _, c := range cases {
		action, args, err := buildPhoneAction(c.p)
		if err != nil {
			t.Errorf("%+v: unexpected error %v", c.p, err)
			continue
		}
		if action != c.action || !reflect.DeepEqual(args, c.args) {
			t.Errorf("%+v → (%q, %v), want (%q, %v)", c.p, action, args, c.action, c.args)
		}
	}
}

func TestBuildPhoneAction_Rejected(t *testing.T) {
	bad := []phoneWriteParams{
		{To: "wipe_phone", Text: "x"},       // not in the allowlist
		{To: "open_url", Target: "notaurl"}, // not an absolute url
		{To: "open_url", Target: ""},        // empty target
		{To: "dial"},                        // missing number
		{To: "message", Target: "x"},        // missing text
	}
	for _, p := range bad {
		if _, _, err := buildPhoneAction(p); err == nil {
			t.Errorf("%+v should be rejected", p)
		}
	}
}

func TestDispatchPhoneAction(t *testing.T) {
	ctx := context.Background()

	// nil sender → reported unavailable, never a silent drop.
	if _, err := dispatchPhoneAction(ctx, nil, phoneWriteParams{To: "open_url", Target: "https://x.com"}); err == nil {
		t.Error("nil sender must report unavailable")
	}

	// A wired sender receives exactly the built command.
	var gotAction string
	var gotArgs map[string]string
	send := func(_ context.Context, action string, args map[string]string) error {
		gotAction, gotArgs = action, args
		return nil
	}
	out, err := dispatchPhoneAction(ctx, send, phoneWriteParams{To: "open_url", Target: "https://x.com"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if gotAction != "open_url" || gotArgs["url"] != "https://x.com" {
		t.Errorf("sender got (%q, %v)", gotAction, gotArgs)
	}
	if out == "" {
		t.Error("expected a result string")
	}

	// Allowlist is enforced before the sender is ever called.
	called := false
	guard := func(context.Context, string, map[string]string) error { called = true; return nil }
	if _, err := dispatchPhoneAction(ctx, guard, phoneWriteParams{To: "format_disk"}); err == nil {
		t.Error("disallowed action must error")
	}
	if called {
		t.Error("sender must not be called for a disallowed action")
	}
}
