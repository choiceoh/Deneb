package phoneevents

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

type judgmentErrorRunner struct {
	err error
}

func (r judgmentErrorRunner) ChatReady() bool { return true }

func (r judgmentErrorRunner) RunSync(context.Context, chatport.SyncRequest) (*chatport.SyncResult, error) {
	return nil, r.err
}

// The usage event type must skip the notification-specific tiny gate (its ads/OTP
// classifier doesn't fit a usage digest) and run the full default-silence judgment,
// like context/clipboard. notification/sms keep the gate.
func TestNotificationLikeEventReturnsExpectedPerType(t *testing.T) {
	cases := map[string]bool{
		"notification": true,
		"sms":          true,
		"":             true, // defaults to notification
		"freeform":     true,
		"context":      false,
		"clipboard":    false,
		"usage":        false,
		"USAGE":        false, // case-insensitive
	}
	for typ, want := range cases {
		if got := notificationLikeEvent(typ); got != want {
			t.Errorf("notificationLikeEvent(%q) = %v, want %v", typ, got, want)
		}
	}
}

// Gmail notifications are suppressed (gmail-poll already posts the mail_report
// card), but ONLY Gmail — other mail apps have no poll coverage so they must still
// run. And only notification-type events: a Gmail "source" on a clipboard/context
// event isn't a notification and must pass through.
func TestIsPolledGmailNotificationIgnoresOnlyGmailSource(t *testing.T) {
	type tc struct {
		eventType string
		source    string
		want      bool
	}
	cases := []tc{
		// Gmail package (deneb-notification-watch forwards the package name) → skip.
		{"notification", "com.google.android.gm", true},
		{"notification", "com.google.android.gm.lite", true},
		{"", "com.google.android.gm", true}, // empty type defaults to notification
		{"notification", "Gmail", true},     // display-label fallback
		{"notification", "GMAIL", true},     // case-insensitive
		// Other mail apps: NOT covered by gmail-poll → must still surface.
		{"notification", "com.microsoft.office.outlook", false},
		{"notification", "com.samsung.android.email.provider", false},
		// Non-mail notifications run normally.
		{"notification", "com.kakao.talk", false},
		{"notification", "(미상)", false},
		{"notification", "", false},
		// Non-notification types pass through even from the Gmail app.
		{"clipboard", "com.google.android.gm", false},
		{"context", "com.google.android.gm", false},
		{"location_update", "com.google.android.gm", false},
		{"usage_update", "com.google.android.gm", false},
	}
	for _, c := range cases {
		if got := isPolledGmailNotification(c.eventType, c.source); got != c.want {
			t.Errorf("isPolledGmailNotification(%q, %q) = %v, want %v", c.eventType, c.source, got, c.want)
		}
	}
}

// Toss notifications are suppressed (the operator does not want that app in the
// work feed), but ONLY the Toss super-app — 토스뱅크/토스증권 keep their labels
// and must still run. Non-notification types pass through even from Toss.
func TestIsIgnoredTossNotificationIgnoresOnlyTossSource(t *testing.T) {
	type tc struct {
		eventType string
		source    string
		want      bool
	}
	cases := []tc{
		{"notification", "토스", true},
		{"notification", "Toss", true},
		{"notification", "TOSS", true},
		{"", "토스", true},
		{"notification", "viva.republica.toss", true},
		{"notification", "viva.republica.toss.internal", true},
		{"notification", " 토스 ", true},
		{"notification", "토스뱅크", false},
		{"notification", "토스증권", false},
		{"notification", "com.kakao.talk", false},
		{"notification", "Gmail", false},
		{"notification", "", false},
		{"clipboard", "토스", false},
		{"context", "viva.republica.toss", false},
		{"location_update", "토스", false},
	}
	for _, c := range cases {
		if got := isIgnoredTossNotification(c.eventType, c.source); got != c.want {
			t.Errorf("isIgnoredTossNotification(%q, %q) = %v, want %v", c.eventType, c.source, got, c.want)
		}
	}
}

// The usage type carries its own label + guidance, and the guidance embeds the
// NO_REPLY placeholder so its default-silence branch can be filled by the caller.
func TestUsageEventLabelAndGuidanceFormatPlaceholder(t *testing.T) {
	if got := phoneEventKindLabel("usage"); got != "앱 사용 리듬" {
		t.Errorf("phoneEventKindLabel(usage) = %q", got)
	}
	guidance := phoneEventGuidance("usage")
	if !strings.Contains(guidance, "%s") {
		t.Errorf("usage guidance missing NO_REPLY placeholder: %q", guidance)
	}
}

func TestRecordPhoneUsageWritesTrimmedPayload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", dir)

	payload := "지난 6시간 앱 사용: 카카오톡 42분"
	recordPhoneUsage(nil, "  "+payload+"\n")

	data, err := os.ReadFile(filepath.Join(dir, "phone-usage.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != payload {
		t.Fatalf("usage cache = %q, want %q", got, payload)
	}
}

func TestProcessJudgmentLogsModelCircuitOpenAsDebugSkip(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	runner := judgmentErrorRunner{
		err: errors.New(modelCircuitOpenErrorText),
	}
	h := New(Config{
		ChatHandler: runner,
		Logger:      logger,
	})

	delivered, err := h.processJudgment(context.Background(), "context", "native", "office wifi connected")
	if err == nil || !strings.Contains(err.Error(), modelCircuitOpenErrorText) {
		t.Fatalf("error = %v, want circuit-open error", err)
	}
	if delivered {
		t.Fatal("circuit-open judgment must not report delivery")
	}
	got := logs.String()
	if strings.Contains(got, "phone-event judgment turn failed") || strings.Contains(got, "level=ERROR") {
		t.Fatalf("circuit-open skip must not be logged as a failure:\n%s", got)
	}
	for _, want := range []string{"level=DEBUG", "phone-event judgment turn skipped", "reason=model_circuit_open"} {
		if !strings.Contains(got, want) {
			t.Fatalf("debug skip log missing %q:\n%s", want, got)
		}
	}
}

func TestProcessJudgmentLogsOrdinaryRunSyncErrorAsFailure(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	runner := judgmentErrorRunner{
		err: errors.New("provider 500"),
	}
	h := New(Config{
		ChatHandler: runner,
		Logger:      logger,
	})

	delivered, err := h.processJudgment(context.Background(), "context", "native", "office wifi connected")
	if err == nil || !strings.Contains(err.Error(), "provider 500") {
		t.Fatalf("error = %v, want provider error", err)
	}
	if delivered {
		t.Fatal("failed judgment must not report delivery")
	}
	got := logs.String()
	if !strings.Contains(got, "level=ERROR") || !strings.Contains(got, "phone-event judgment turn failed") {
		t.Fatalf("ordinary run error must stay logged as a failure:\n%s", got)
	}
	if strings.Contains(got, "phone-event judgment turn skipped") {
		t.Fatalf("ordinary run error must not use the circuit-open skip log:\n%s", got)
	}
}

// A native-client crash report is appended to the bounded crash log with its
// device source and full stack — the server-side record the app otherwise lacks.
// Consecutive reports accumulate as separate entries; blank text is a no-op.
func TestRecordClientCrashCreatesEntriesIgnoresBlank(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", dir)
	path := filepath.Join(dir, "client-crashes.log")

	recordClientCrash(nil, "  ", "  ") // blank → no file
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("blank crash must not create the log: %v", err)
	}

	stack := "java.lang.NullPointerException: boom\n\tat ai.deneb.Foo.bar(Foo.kt:42)"
	recordClientCrash(nil, "Samsung SM-S928N", stack)
	recordClientCrash(nil, "dev", "second crash\n\tat ai.deneb.Baz.qux(Baz.kt:7)")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"NullPointerException", "Foo.kt:42", "Samsung SM-S928N", "second crash", "Baz.kt:7"} {
		if !strings.Contains(got, want) {
			t.Fatalf("crash log missing %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "===== "); n != 2 {
		t.Fatalf("expected 2 entries, got %d:\n%s", n, got)
	}
}

// The crash log is bounded so a crash loop can't grow it without limit, and after
// trimming it must still begin at a clean entry marker (never mid-stack).
func TestRecordClientCrash_BoundedTruncatesAtEntryMarker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", dir)

	big := strings.Repeat("x", 50*1024)
	for i := 0; i < 20; i++ {
		recordClientCrash(nil, "dev", big)
	}
	data, err := os.ReadFile(filepath.Join(dir, "client-crashes.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > maxClientCrashLogBytes {
		t.Fatalf("crash log not bounded: %d > %d", len(data), maxClientCrashLogBytes)
	}
	if len(data) == 0 || !strings.HasPrefix(string(data), "===== ") {
		t.Fatalf("bounded log must start at an entry marker, got head: %.40q", string(data))
	}
}
