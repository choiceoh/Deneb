package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// fixedTime returns a consistent time for deterministic test output.
func fixedTime() time.Time {
	return time.Date(2026, 3, 26, 14, 5, 9, 0, time.UTC)
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
	if err := h.Handle(nil, r); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	want := "14:05:09 INF server started addr=127.0.0.1:8080 port=8080\n"
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
		h.Handle(nil, r)

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
	h.Handle(nil, r)

	got := buf.String()
	// Plain value: no quotes.
	if !strings.Contains(got, "plain=hello") {
		t.Errorf("plain value not found in %q", got)
	}
	// Value with spaces: quoted.
	if !strings.Contains(got, `spaces="hello world"`) {
		t.Errorf("spaces value not quoted in %q", got)
	}
	// Empty value: quoted.
	if !strings.Contains(got, `empty=""`) {
		t.Errorf("empty value not quoted in %q", got)
	}
	// Value with equals: quoted.
	if !strings.Contains(got, `equals="a=b"`) {
		t.Errorf("equals value not quoted in %q", got)
	}
	// Value with quotes: escaped and quoted.
	if !strings.Contains(got, `quotes="say \"hi\""`) {
		t.Errorf("quotes value not properly escaped in %q", got)
	}
}

func TestConsoleHandler_Enabled(t *testing.T) {
	h := NewConsoleHandler(nil, &ConsoleOptions{Level: slog.LevelWarn, Color: false})
	if h.Enabled(nil, slog.LevelDebug) {
		t.Error("debug should not be enabled at warn level")
	}
	if h.Enabled(nil, slog.LevelInfo) {
		t.Error("info should not be enabled at warn level")
	}
	if !h.Enabled(nil, slog.LevelWarn) {
		t.Error("warn should be enabled at warn level")
	}
	if !h.Enabled(nil, slog.LevelError) {
		t.Error("error should be enabled at warn level")
	}
}

func TestConsoleHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})
	h2 := h.WithAttrs([]slog.Attr{slog.String("pkg", "server")})

	r := newTestRecord(slog.LevelInfo, "request", slog.Int("ms", 42))
	h2.Handle(nil, r)

	got := buf.String()
	// Pre-attr should appear before record attrs.
	pkgIdx := strings.Index(got, "pkg=server")
	msIdx := strings.Index(got, "ms=42")
	if pkgIdx < 0 || msIdx < 0 {
		t.Fatalf("expected both attrs in %q", got)
	}
	if pkgIdx > msIdx {
		t.Errorf("pre-attr should appear before record attr: %q", got)
	}
}

func TestConsoleHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})
	h2 := h.WithGroup("http")

	r := newTestRecord(slog.LevelInfo, "request", slog.Int("status", 200))
	h2.Handle(nil, r)

	got := buf.String()
	if !strings.Contains(got, "http.status=200") {
		t.Errorf("group prefix not applied: %q", got)
	}
}

func TestConsoleHandler_ColorOutput(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: true})

	r := newTestRecord(slog.LevelError, "fail")
	h.Handle(nil, r)

	got := buf.String()
	if !strings.Contains(got, colorRed) {
		t.Errorf("color mode should contain ANSI red: %q", got)
	}
	if !strings.Contains(got, colorReset) {
		t.Errorf("color mode should contain ANSI reset: %q", got)
	}
}

func TestConsoleHandler_NoColorOutput(t *testing.T) {
	var buf bytes.Buffer
	h := NewConsoleHandler(&buf, &ConsoleOptions{Level: slog.LevelDebug, Color: false})

	r := newTestRecord(slog.LevelError, "fail")
	h.Handle(nil, r)

	got := buf.String()
	if strings.Contains(got, "\033[") {
		t.Errorf("no-color mode should not contain ANSI sequences: %q", got)
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
	h.Handle(nil, r)

	got := buf.String()
	want := "14:05:09 DBG rpc method=health ok=true ms=5\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
