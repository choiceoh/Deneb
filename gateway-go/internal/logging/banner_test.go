package logging

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPrintBanner_NoColor(t *testing.T) {
	var buf bytes.Buffer
	info := BannerInfo{
		Version:       "3.25.0",
		Addr:          "127.0.0.1:18789",
		LocalAIStatus: "online",
	}
	PrintBanner(&buf, info, false)

	got := buf.String()
	for _, want := range []string{
		"deneb gateway",
		"3.25.0",
		"127.0.0.1:18789",
		"localai",
		"online",
		"ready.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("banner missing %q in:\n%s", want, got)
		}
	}

	// Should not contain ANSI sequences in no-color mode.
	if strings.Contains(got, "\033[") {
		t.Errorf("no-color banner contains ANSI sequences:\n%s", got)
	}
}

func TestPrintBanner_WithColor(t *testing.T) {
	var buf bytes.Buffer
	info := BannerInfo{
		Version:       "3.25.0",
		Addr:          "127.0.0.1:18789",
		RustFFI:       false,
		LocalAIStatus: "offline",
	}
	PrintBanner(&buf, info, true)

	got := buf.String()
	if !strings.Contains(got, "\033[") {
		t.Errorf("color banner should contain ANSI sequences:\n%s", got)
	}
	if !strings.Contains(got, "offline") {
		t.Errorf("localai should show offline:\n%s", got)
	}
}

func TestPrintBanner_DaemonMode(t *testing.T) {
	var buf bytes.Buffer
	info := BannerInfo{
		Version: "3.25.0",
		Addr:    "127.0.0.1:18789",
		PID:     12345,
	}
	PrintBanner(&buf, info, false)

	got := buf.String()
	if !strings.Contains(got, "12345") {
		t.Errorf("daemon mode banner should show PID:\n%s", got)
	}
}

func TestPrintShutdown(t *testing.T) {
	var buf bytes.Buffer
	PrintShutdown(&buf, 2*time.Hour+14*time.Minute, false)

	got := buf.String()
	if !strings.Contains(got, "deneb gateway stopped") {
		t.Errorf("shutdown missing identity:\n%s", got)
	}
	if !strings.Contains(got, "2h 14m") {
		t.Errorf("shutdown missing uptime:\n%s", got)
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{5*time.Minute + 30*time.Second, "5m 30s"},
		{2 * time.Hour, "2h"},
		{2*time.Hour + 14*time.Minute, "2h 14m"},
	}
	for _, tt := range tests {
		got := formatUptime(tt.d)
		if got != tt.want {
			t.Errorf("formatUptime(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
