package server

import (
	"strings"
	"testing"
)

func TestPhoneActionData(t *testing.T) {
	got := phoneActionData("open_url", map[string]string{"url": "https://example.io"})
	if got["action"] != "open_url" {
		t.Fatalf("action = %q, want open_url", got["action"])
	}
	if got["url"] != "https://example.io" {
		t.Fatalf("url = %q, want passthrough", got["url"])
	}
	// nil args must not panic and still carries the action.
	if d := phoneActionData("photo", nil); d["action"] != "photo" {
		t.Fatalf("photo data = %v, want action=photo", d)
	}
}

func TestPhoneActionNotice(t *testing.T) {
	cases := []struct {
		action      string
		args        map[string]string
		wantTitle   string
		wantBodyHas string // substring the body must contain (skipped when empty)
	}{
		{"open_url", map[string]string{"url": "https://a.io"}, "링크 열기", "https://a.io"},
		{"open_app", map[string]string{"package": "com.kakao.talk"}, "앱 열기", "com.kakao.talk"},
		{"dial", map[string]string{"number": "010-1234-5678"}, "전화 걸기", "010-1234-5678"},
		{"message", map[string]string{"to": "김대표", "text": "안녕하세요"}, "메시지 보내기", "김대표"},
		{"share", map[string]string{"text": "공유할 내용"}, "공유", "공유할 내용"},
		{"photo", nil, "카메라 열기", ""},
		{"unknown_action", nil, "폰 동작", "unknown_action"},
	}
	for _, c := range cases {
		title, body := phoneActionNotice(c.action, c.args)
		if title != c.wantTitle {
			t.Errorf("%s: title = %q, want %q", c.action, title, c.wantTitle)
		}
		if body == "" {
			t.Errorf("%s: body is empty (FCM notification body must be non-empty)", c.action)
		}
		if c.wantBodyHas != "" && !strings.Contains(body, c.wantBodyHas) {
			t.Errorf("%s: body = %q, must contain %q", c.action, body, c.wantBodyHas)
		}
	}
}
