package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// fixedTime returns a consistent time for deterministic test output.
func fixedTime() time.Time {
	return time.Date(2026, 3, 26, 14, 5, 9, 123_000_000, time.UTC)
}

func newTestRecord(level slog.Level, msg string, attrs ...slog.Attr) slog.Record {
	r := slog.NewRecord(fixedTime(), level, msg, 0)
	r.AddAttrs(attrs...)
	return r
}

func TestConsoleHandler_BasicFormat(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})

	r := newTestRecord(slog.LevelInfo, "server started",
		slog.String("addr", "127.0.0.1:8080"),
		slog.Int("port", 8080),
	)
	if err := h.Handle(context.TODO(), r); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	want := "14:05:09 INF │ server started addr=127.0.0.1:8080 port=8080\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestConsoleHandler_LevelAbbreviations(t *testing.T) {
	tests := []struct {
		level slog.Level
		want  string
	}{
		{slog.LevelDebug, "DBG"},
		{slog.LevelInfo, "INF"},
		{slog.LevelWarn, "WRN"},
		{slog.LevelError, "ERR"},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})
		r := newTestRecord(tt.level, "test")
		h.Handle(context.TODO(), r)

		if !strings.Contains(buf.String(), tt.want) {
			t.Errorf("level %v: output %q does not contain %q", tt.level, buf.String(), tt.want)
		}
	}
}

func TestConsoleHandler_ValueQuoting(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})

	r := newTestRecord(slog.LevelInfo, "test",
		slog.String("plain", "hello"),
		slog.String("spaces", "hello world"),
		slog.String("empty", ""),
		slog.String("equals", "a=b"),
		slog.String("quotes", `say "hi"`),
	)
	h.Handle(context.TODO(), r)

	got := buf.String()
	if !strings.Contains(got, "plain=hello") {
		t.Errorf("plain value not found in %q", got)
	}
	if !strings.Contains(got, `spaces="hello world"`) {
		t.Errorf("spaces value not quoted in %q", got)
	}
	if !strings.Contains(got, `empty=""`) {
		t.Errorf("empty value not quoted in %q", got)
	}
	if !strings.Contains(got, `equals="a=b"`) {
		t.Errorf("equals value not quoted in %q", got)
	}
	if !strings.Contains(got, `quotes="say \"hi\""`) {
		t.Errorf("quotes value not properly escaped in %q", got)
	}
}

func TestConsoleHandler_Enabled(t *testing.T) {
	h := NewConsoleHandler(nil, &ConsoleOptions{Level: slog.LevelWarn, Color: false})
	if h.Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("debug should not be enabled at warn level")
	}
	if h.Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("info should not be enabled at warn level")
	}
	if !h.Enabled(context.TODO(), slog.LevelWarn) {
		t.Error("warn should be enabled at warn level")
	}
	if !h.Enabled(context.TODO(), slog.LevelError) {
		t.Error("error should be enabled at warn level")
	}
}

func TestConsoleHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})
	h2 := h.WithAttrs([]slog.Attr{slog.String("component", "router")})

	r := newTestRecord(slog.LevelInfo, "request", slog.Int("ms", 42))
	h2.Handle(context.TODO(), r)

	got := buf.String()
	compIdx := strings.Index(got, "component=router")
	msIdx := strings.Index(got, "ms=42")
	if compIdx < 0 || msIdx < 0 {
		t.Fatalf("expected both attrs in %q", got)
	}
	if compIdx > msIdx {
		t.Errorf("pre-attr should appear before record attr: %q", got)
	}
}

func TestConsoleHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})
	h2 := h.WithGroup("http")

	r := newTestRecord(slog.LevelInfo, "request", slog.Int("status", 200))
	h2.Handle(context.TODO(), r)

	got := buf.String()
	if !strings.Contains(got, "http.status=200") {
		t.Errorf("group prefix not applied: %q", got)
	}
}

func TestConsoleHandler_ColorStyling(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: true})

	r := newTestRecord(slog.LevelError, "fail", slog.String("id", "abc"))
	h.Handle(context.TODO(), r)

	got := buf.String()
	if !strings.Contains(got, ansiBoldRed) {
		t.Errorf("color mode should contain bold red for ERR: %q", got)
	}
	if !strings.Contains(got, ansiDim) {
		t.Errorf("timestamp should be dim: %q", got)
	}
	if !strings.Contains(got, ansiReset) {
		t.Errorf("should contain reset sequences: %q", got)
	}
}

func TestConsoleHandler_ColorLevelStyles(t *testing.T) {
	tests := []struct {
		level slog.Level
		want  string
	}{
		{slog.LevelDebug, ansiBoldCyn},
		{slog.LevelInfo, ansiDim},
		{slog.LevelWarn, ansiBoldYel},
		{slog.LevelError, ansiBoldRed},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: true})
		r := newTestRecord(tt.level, "test")
		h.Handle(context.TODO(), r)

		if !strings.Contains(buf.String(), tt.want) {
			t.Errorf("level %v: expected style %q in %q", tt.level, tt.want, buf.String())
		}
	}
}

func TestConsoleHandler_NoColorOutput(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})

	r := newTestRecord(slog.LevelError, "fail")
	h.Handle(context.TODO(), r)

	got := buf.String()
	if strings.Contains(got, "\033[") {
		t.Errorf("no-color mode should not contain ANSI sequences: %q", got)
	}
}

func TestConsoleHandler_ErrorAttrHighlight(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: true})

	r := newTestRecord(slog.LevelError, "failed",
		slog.String("id", "abc"),
		slog.String("error", "connection refused"),
	)
	h.Handle(context.TODO(), r)

	got := buf.String()
	if !strings.Contains(got, ansiItalic) {
		t.Errorf("error attr should use italic: %q", got)
	}
	errorIdx := strings.Index(got, "error=")
	if errorIdx < 0 {
		t.Fatalf("error attr not found in %q", got)
	}
	afterError := got[errorIdx:]
	redCount := strings.Count(afterError, ansiRed)
	if redCount < 1 {
		t.Errorf("error value should be red: %q", got)
	}
}

func TestConsoleHandler_PanicAttrHighlight(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: true})

	r := newTestRecord(slog.LevelError, "handler panic",
		slog.String("panic", "nil pointer"),
	)
	h.Handle(context.TODO(), r)

	got := buf.String()
	if !strings.Contains(got, ansiItalic) {
		t.Errorf("panic attr should use italic+red: %q", got)
	}
}

func TestConsoleHandler_BoolAttr(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})

	r := newTestRecord(slog.LevelDebug, "rpc",
		slog.String("method", "health"),
		slog.Bool("ok", true),
		slog.Int64("ms", 5),
	)
	h.Handle(context.TODO(), r)

	got := buf.String()
	want := "14:05:09 DBG │ rpc method=health ok=true ms=5\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestConsoleHandler_Timestamp(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})

	r := newTestRecord(slog.LevelInfo, "test")
	h.Handle(context.TODO(), r)

	got := buf.String()
	if !strings.HasPrefix(got, "14:05:09 ") {
		t.Errorf("expected second-precision timestamp, got %q", got)
	}
	// Should NOT have any fractional seconds.
	if strings.HasPrefix(got, "14:05:09.") {
		t.Errorf("timestamp should have no fractional seconds: %q", got)
	}
}

func TestConsoleHandler_ErrorMessageBoldRed(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: true})

	r := newTestRecord(slog.LevelError, "something broke")
	h.Handle(context.TODO(), r)

	got := buf.String()
	count := strings.Count(got, ansiBoldRed)
	if count < 2 {
		t.Errorf("ERR level should bold-red both level and message, got %d occurrences in %q", count, got)
	}
}

func TestConsoleHandler_InfoMessageBold(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: true})

	r := newTestRecord(slog.LevelInfo, "started")
	h.Handle(context.TODO(), r)

	got := buf.String()
	if !strings.Contains(got, ansiBold) {
		t.Errorf("INF message should be bold: %q", got)
	}
}

// --- New tests for modernized features ---

func TestConsoleHandler_Separator(t *testing.T) {
	// No-color mode should contain the │ separator.
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})
	r := newTestRecord(slog.LevelInfo, "test")
	h.Handle(context.TODO(), r)

	got := buf.String()
	if !strings.Contains(got, " │ ") {
		t.Errorf("expected │ separator in no-color output: %q", got)
	}

	// Color mode should also contain │.
	buf.Reset()
	h2 := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: true})
	h2.Handle(context.TODO(), r)

	got = buf.String()
	if !strings.Contains(got, "│") {
		t.Errorf("expected │ separator in color output: %q", got)
	}
}

func TestConsoleHandler_PkgTag(t *testing.T) {
	// pkg preAttr should render as [server] tag, not pkg=server.
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})
	h2 := h.WithAttrs([]slog.Attr{slog.String("pkg", "server")})

	r := newTestRecord(slog.LevelInfo, "request", slog.Int("status", 200))
	h2.Handle(context.TODO(), r)

	got := buf.String()
	want := "14:05:09 INF │ [server] request status=200\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	// Must NOT contain pkg=server.
	if strings.Contains(got, "pkg=server") {
		t.Errorf("pkg should be rendered as [server] tag, not key=value: %q", got)
	}
}

func TestConsoleHandler_PkgTagInline(t *testing.T) {
	// pkg as a record attr (not preAttr) should also render as tag.
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})

	r := newTestRecord(slog.LevelInfo, "request",
		slog.String("pkg", "media"),
		slog.Int("status", 200),
	)
	h.Handle(context.TODO(), r)

	got := buf.String()
	if !strings.Contains(got, "[media]") {
		t.Errorf("inline pkg should render as [media] tag: %q", got)
	}
	if strings.Contains(got, "pkg=media") {
		t.Errorf("pkg should not appear as key=value: %q", got)
	}
}

func TestConsoleHandler_PkgTagColor(t *testing.T) {
	// Color mode should use dim cyan for pkg tag.
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: true})
	h2 := h.WithAttrs([]slog.Attr{slog.String("pkg", "server")})

	r := newTestRecord(slog.LevelInfo, "request")
	h2.Handle(context.TODO(), r)

	got := buf.String()
	if !strings.Contains(got, ansiDimCyn) {
		t.Errorf("pkg tag should use dim cyan in color mode: %q", got)
	}
	if !strings.Contains(got, "[server]") {
		t.Errorf("pkg tag should contain [server]: %q", got)
	}
}

func TestConsoleHandler_NoPkgTag(t *testing.T) {
	// Without pkg attr, separator goes directly to message.
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})

	r := newTestRecord(slog.LevelInfo, "hello")
	h.Handle(context.TODO(), r)

	got := buf.String()
	want := "14:05:09 INF │ hello\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestConsoleHandler_DurationFormatting(t *testing.T) {
	tests := []struct {
		dur  time.Duration
		want string
	}{
		{150 * time.Microsecond, "150µs"},
		{42 * time.Millisecond, "42ms"},
		{1234 * time.Millisecond, "1.2s"},
		{9999 * time.Millisecond, "9.9s"},
		{90 * time.Second, "1m30s"},
		{time.Hour + 30*time.Minute, "1h30m0s"},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})
		r := newTestRecord(slog.LevelInfo, "op", slog.Duration("elapsed", tt.dur))
		h.Handle(context.TODO(), r)

		got := buf.String()
		wantAttr := "elapsed=" + tt.want
		if !strings.Contains(got, wantAttr) {
			t.Errorf("duration %v: expected %q in %q", tt.dur, wantAttr, got)
		}
	}
}
