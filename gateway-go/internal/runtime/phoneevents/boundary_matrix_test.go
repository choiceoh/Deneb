package phoneevents

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func phoneEventTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestLoopbackRemoteBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, remote string
		want         bool
	}{
		{
			name:   "empty",
			remote: "",
			want:   false,
		},
		{
			name:   "ipv4 loopback port",
			remote: "127.0.0.1:1234",
			want:   true,
		},
		{
			name:   "ipv4 loopback no port",
			remote: "127.0.0.1",
			want:   true,
		},
		{
			name:   "ipv4 other loopback",
			remote: "127.0.0.2:80",
			want:   true,
		},
		{
			name:   "ipv4 last loopback",
			remote: "127.255.255.255:65535",
			want:   true,
		},
		{
			name:   "ipv6 loopback port",
			remote: "[::1]:1234",
			want:   true,
		},
		{
			name:   "ipv6 loopback no port",
			remote: "::1",
			want:   true,
		},
		{
			name:   "ipv6 expanded",
			remote: "0:0:0:0:0:0:0:1",
			want:   true,
		},
		{
			name:   "unspecified ipv4",
			remote: "0.0.0.0:1",
			want:   false,
		},
		{
			name:   "unspecified ipv6",
			remote: "[::]:1",
			want:   false,
		},
		{
			name:   "private 10",
			remote: "10.0.0.1:80",
			want:   false,
		},
		{
			name:   "private 172",
			remote: "172.16.0.1:80",
			want:   false,
		},
		{
			name:   "private 192",
			remote: "192.168.1.1:80",
			want:   false,
		},
		{
			name:   "public",
			remote: "8.8.8.8:53",
			want:   false,
		},
		{
			name:   "hostname localhost",
			remote: "localhost:80",
			want:   false,
		},
		{
			name:   "hostname",
			remote: "example.com:80",
			want:   false,
		},
		{
			name:   "malformed port",
			remote: "127.0.0.1:notaport",
			want:   true,
		},
		{
			name:   "missing host",
			remote: ":80",
			want:   false,
		},
		{
			name:   "space",
			remote: " ",
			want:   false,
		},
		{
			name:   "leading space",
			remote: " 127.0.0.1:80",
			want:   false,
		},
		{
			name:   "trailing space",
			remote: "127.0.0.1:80 ",
			want:   true,
		},
		{
			name:   "zone ipv6",
			remote: "[::1%lo]:80",
			want:   false,
		},
		{
			name:   "ipv4 mapped loopback",
			remote: "[::ffff:127.0.0.1]:80",
			want:   true,
		},
		{
			name:   "ipv4 mapped public",
			remote: "[::ffff:8.8.8.8]:80",
			want:   false,
		},
		{
			name:   "negative port",
			remote: "127.0.0.1:-1",
			want:   true,
		},
		{
			name:   "too high port",
			remote: "127.0.0.1:65536",
			want:   true,
		},
		{
			name:   "zero port",
			remote: "127.0.0.1:0",
			want:   true,
		},
		{
			name:   "colon only",
			remote: ":",
			want:   false,
		},
		{
			name:   "brackets only",
			remote: "[]:1",
			want:   false,
		},
		{
			name:   "url",
			remote: "http://127.0.0.1:80",
			want:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isLoopbackRemote(tc.remote); got != tc.want {
				t.Fatalf("isLoopbackRemote(%q)=%v want=%v", tc.remote, got, tc.want)
			}
		})
	}
}

func TestPhoneEventKindLabelBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, eventType, want string }{
		{
			name:      "empty",
			eventType: "",
			want:      "앱 알림",
		},
		{
			name:      "spaces",
			eventType: "   ",
			want:      "앱 알림",
		},
		{
			name:      "notification",
			eventType: "notification",
			want:      "앱 알림",
		},
		{
			name:      "notification upper",
			eventType: "NOTIFICATION",
			want:      "앱 알림",
		},
		{
			name:      "notification padded",
			eventType: " notification ",
			want:      "앱 알림",
		},
		{
			name:      "context",
			eventType: "context",
			want:      "상황 변화 (위치·네트워크 등)",
		},
		{
			name:      "context upper",
			eventType: "CONTEXT",
			want:      "상황 변화 (위치·네트워크 등)",
		},
		{
			name:      "clipboard",
			eventType: "clipboard",
			want:      "클립보드 캡처",
		},
		{
			name:      "clipboard upper",
			eventType: "CLIPBOARD",
			want:      "클립보드 캡처",
		},
		{
			name:      "usage",
			eventType: "usage",
			want:      "앱 사용 리듬",
		},
		{
			name:      "usage upper",
			eventType: "USAGE",
			want:      "앱 사용 리듬",
		},
		{
			name:      "sms",
			eventType: "sms",
			want:      "문자 메시지",
		},
		{
			name:      "sms upper",
			eventType: "SMS",
			want:      "문자 메시지",
		},
		{
			name:      "custom",
			eventType: "custom",
			want:      "custom",
		},
		{
			name:      "custom case",
			eventType: "Custom",
			want:      "Custom",
		},
		{
			name:      "custom padded",
			eventType: " custom ",
			want:      " custom ",
		},
		{
			name:      "korean",
			eventType: "알림",
			want:      "알림",
		},
		{
			name:      "emoji",
			eventType: "🚀",
			want:      "🚀",
		},
		{
			name:      "location update",
			eventType: "location_update",
			want:      "location_update",
		},
		{
			name:      "crash",
			eventType: "client_crash",
			want:      "client_crash",
		},
		{
			name:      "action result",
			eventType: "phone_action_result",
			want:      "phone_action_result",
		},
		{
			name:      "newline custom",
			eventType: "x\ny",
			want:      "x\ny",
		},
		{
			name:      "nul custom",
			eventType: "x\u0000y",
			want:      "x\u0000y",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := phoneEventKindLabel(tc.eventType); got != tc.want {
				t.Fatalf("label(%q)=%q want=%q", tc.eventType, got, tc.want)
			}
		})
	}
}

func TestNotificationLikeBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, eventType string
		want            bool
	}{
		{
			name:      "empty",
			eventType: "",
			want:      true,
		},
		{
			name:      "space",
			eventType: " ",
			want:      true,
		},
		{
			name:      "notification",
			eventType: "notification",
			want:      true,
		},
		{
			name:      "notification upper",
			eventType: "NOTIFICATION",
			want:      true,
		},
		{
			name:      "notification padded",
			eventType: " notification ",
			want:      true,
		},
		{
			name:      "sms",
			eventType: "sms",
			want:      true,
		},
		{
			name:      "free",
			eventType: "freeform",
			want:      true,
		},
		{
			name:      "heartbeat",
			eventType: "heartbeat",
			want:      true,
		},
		{
			name:      "location update",
			eventType: "location_update",
			want:      true,
		},
		{
			name:      "usage update",
			eventType: "usage_update",
			want:      true,
		},
		{
			name:      "client crash",
			eventType: "client_crash",
			want:      true,
		},
		{
			name:      "action result",
			eventType: "phone_action_result",
			want:      true,
		},
		{
			name:      "context",
			eventType: "context",
			want:      false,
		},
		{
			name:      "context upper",
			eventType: "CONTEXT",
			want:      false,
		},
		{
			name:      "context padded",
			eventType: " context ",
			want:      false,
		},
		{
			name:      "clipboard",
			eventType: "clipboard",
			want:      false,
		},
		{
			name:      "clipboard upper",
			eventType: "CLIPBOARD",
			want:      false,
		},
		{
			name:      "usage",
			eventType: "usage",
			want:      false,
		},
		{
			name:      "usage upper",
			eventType: "USAGE",
			want:      false,
		},
		{
			name:      "usage padded",
			eventType: "\tusage\n",
			want:      false,
		},
		{
			name:      "korean",
			eventType: "상황",
			want:      true,
		},
		{
			name:      "emoji",
			eventType: "🚀",
			want:      true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := notificationLikeEvent(tc.eventType); got != tc.want {
				t.Fatalf("notificationLikeEvent(%q)=%v want=%v", tc.eventType, got, tc.want)
			}
		})
	}
}

func TestPolledGmailBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, eventType, source string
		want                    bool
	}{
		{
			name:      "default gmail package",
			eventType: "",
			source:    "com.google.android.gm",
			want:      true,
		},
		{
			name:      "notification gmail package",
			eventType: "notification",
			source:    "com.google.android.gm",
			want:      true,
		},
		{
			name:      "upper type",
			eventType: "NOTIFICATION",
			source:    "com.google.android.gm",
			want:      true,
		},
		{
			name:      "padded type",
			eventType: " notification ",
			source:    "com.google.android.gm",
			want:      true,
		},
		{
			name:      "gmail lite",
			eventType: "notification",
			source:    "com.google.android.gm.lite",
			want:      true,
		},
		{
			name:      "gmail suffix",
			eventType: "notification",
			source:    "x.com.google.android.gm.extra",
			want:      false,
		},
		{
			name:      "gmail label",
			eventType: "notification",
			source:    "gmail",
			want:      true,
		},
		{
			name:      "gmail label upper",
			eventType: "notification",
			source:    "GMAIL",
			want:      true,
		},
		{
			name:      "gmail label padded",
			eventType: "notification",
			source:    " Gmail ",
			want:      true,
		},
		{
			name:      "empty source",
			eventType: "notification",
			source:    "",
			want:      false,
		},
		{
			name:      "gm near miss",
			eventType: "notification",
			source:    "com.google.android.gmx",
			want:      false,
		},
		{
			name:      "google mail near miss",
			eventType: "notification",
			source:    "com.google.android.mail",
			want:      false,
		},
		{
			name:      "outlook",
			eventType: "notification",
			source:    "com.microsoft.office.outlook",
			want:      false,
		},
		{
			name:      "samsung",
			eventType: "notification",
			source:    "com.samsung.android.email.provider",
			want:      false,
		},
		{
			name:      "context gmail",
			eventType: "context",
			source:    "com.google.android.gm",
			want:      false,
		},
		{
			name:      "clipboard gmail",
			eventType: "clipboard",
			source:    "com.google.android.gm",
			want:      false,
		},
		{
			name:      "usage gmail",
			eventType: "usage",
			source:    "com.google.android.gm",
			want:      false,
		},
		{
			name:      "sms gmail",
			eventType: "sms",
			source:    "com.google.android.gm",
			want:      false,
		},
		{
			name:      "custom gmail",
			eventType: "custom",
			source:    "com.google.android.gm",
			want:      false,
		},
		{
			name:      "space source",
			eventType: "notification",
			source:    "   ",
			want:      false,
		},
		{
			name:      "korean source",
			eventType: "notification",
			source:    "지메일",
			want:      false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isPolledGmailNotification(tc.eventType, tc.source); got != tc.want {
				t.Fatalf("gmail(%q,%q)=%v want=%v", tc.eventType, tc.source, got, tc.want)
			}
		})
	}
}

func TestPhoneEventGuidanceBoundaryMatrix(t *testing.T) {
	tests := []struct{ name, eventType, marker string }{
		{name: "context", eventType: "context", marker: "상황 변화 신호"},
		{name: "context upper", eventType: "CONTEXT", marker: "상황 변화 신호"},
		{name: "context padded", eventType: " context ", marker: "상황 변화 신호"},
		{name: "clipboard", eventType: "clipboard", marker: "복사(캡처)"},
		{name: "clipboard upper", eventType: "CLIPBOARD", marker: "복사(캡처)"},
		{name: "usage", eventType: "usage", marker: "앱 사용 패턴"},
		{name: "usage upper", eventType: "USAGE", marker: "앱 사용 패턴"},
		{name: "notification", eventType: "notification", marker: "알릴 가치"},
		{name: "sms", eventType: "sms", marker: "알릴 가치"},
		{name: "empty", eventType: "", marker: "알릴 가치"},
		{name: "custom", eventType: "custom", marker: "알릴 가치"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := phoneEventGuidance(tc.eventType)
			if !strings.Contains(got, tc.marker) {
				t.Errorf("guidance=%q missing %q", got, tc.marker)
			}
			if strings.Count(got, "%s") != 1 {
				t.Errorf("placeholder count=%d", strings.Count(got, "%s"))
			}
			formatted := fmt.Sprintf(got, "NO_REPLY")
			if strings.Contains(formatted, "%s") || !strings.Contains(formatted, "NO_REPLY") {
				t.Errorf("formatted guidance=%q", formatted)
			}
		})
	}
}

func TestNewHandlerRejectsNonLoopbackAndGuardsChatUnavailable(t *testing.T) {
	logger := phoneEventTestLogger()
	h := New(Config{Logger: logger})
	if h == nil {
		t.Fatal("New returned nil")
	}
	if h.shutdownContext == nil {
		t.Fatal("default shutdown context is nil")
	}
	if h.logger != logger {
		t.Error("logger not retained")
	}
	for _, tc := range []struct {
		name, remote, body string
		want               int
		contains           string
	}{
		{name: "public rejected first", remote: "8.8.8.8:1", body: `{}`, want: 403, contains: "localhost only"},
		{name: "hostname rejected", remote: "localhost:1", body: `{}`, want: 403, contains: "localhost only"},
		{name: "loopback unavailable", remote: "127.0.0.1:1", body: `{}`, want: 503, contains: "chat handler unavailable"},
		{name: "ipv6 unavailable", remote: "[::1]:1", body: `{}`, want: 503, contains: "chat handler unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/event/ingest", strings.NewReader(tc.body))
			req.RemoteAddr = tc.remote
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.contains) {
				t.Errorf("body=%q", rec.Body.String())
			}
			if rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type=%q", rec.Header().Get("Content-Type"))
			}
		})
	}
}

func TestPhoneEventPathsReturnExpectedConstants(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", dir)
	for _, tc := range []struct{ name, got, base string }{
		{name: "heartbeat", got: phoneHeartbeatPath(), base: "phone-heartbeat"},
		{name: "location", got: phoneLocationPath(), base: "phone-location.json"},
		{name: "usage", got: phoneUsagePath(), base: "phone-usage.txt"},
		{name: "crash", got: phoneCrashPath(), base: "client-crashes.log"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.HasPrefix(tc.got, dir) || !strings.HasSuffix(tc.got, tc.base) {
				t.Errorf("path=%q", tc.got)
			}
		})
	}
	if phoneEventMaxTokens != 1536 {
		t.Errorf("max tokens=%d", phoneEventMaxTokens)
	}
	if phoneEventTurnDeadline != 4*time.Minute {
		t.Errorf("deadline=%s", phoneEventTurnDeadline)
	}
	if phoneEventSessionPrefix != "phone-event" {
		t.Errorf("prefix=%q", phoneEventSessionPrefix)
	}
	if maxClientCrashLogBytes != 256*1024 {
		t.Errorf("crash cap=%d", maxClientCrashLogBytes)
	}
}

func TestPhoneEventPureBoundariesConcurrent(t *testing.T) {
	const workers = 128
	const iterations = 100
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if !isLoopbackRemote("127.0.0.1:1234") {
					errs <- fmt.Errorf("loopback worker=%d", worker)
					return
				}
				if phoneEventKindLabel("USAGE") != "앱 사용 리듬" {
					errs <- fmt.Errorf("label worker=%d", worker)
					return
				}
				if notificationLikeEvent(" context ") {
					errs <- fmt.Errorf("notification worker=%d", worker)
					return
				}
				if !isPolledGmailNotification("notification", "GMAIL") {
					errs <- fmt.Errorf("gmail worker=%d", worker)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	_ = context.Background()
}
