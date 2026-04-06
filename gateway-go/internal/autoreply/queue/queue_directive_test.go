package queue

import (
	"testing"
)

func TestExtractQueueDirective_NoDirective(t *testing.T) {
	d := ExtractQueueDirective("hello world")
	if d.HasDirective {
		t.Error("expected no directive")
	}
	if d.Cleaned != "hello world" {
		t.Errorf("expected cleaned 'hello world', got %q", d.Cleaned)
	}
}

func TestExtractQueueDirective_EmptyInput(t *testing.T) {
	d := ExtractQueueDirective("")
	if d.HasDirective {
		t.Error("expected no directive for empty input")
	}
}

func TestExtractQueueDirective_ModeIgnored(t *testing.T) {
	// Mode tokens are silently ignored (always collect).
	d := ExtractQueueDirective("/queue steer")
	if !d.HasDirective {
		t.Fatal("expected directive")
	}
	// Mode is not captured since it's always collect.
}

func TestExtractQueueDirective_WithMessage(t *testing.T) {
	d := ExtractQueueDirective("Please do this /queue collect and then this")
	if !d.HasDirective {
		t.Fatal("expected directive")
	}
	if d.Cleaned == "" {
		t.Error("expected non-empty cleaned text")
	}
}

func TestExtractQueueDirective_Reset(t *testing.T) {
	d := ExtractQueueDirective("/queue reset")
	if !d.HasDirective {
		t.Fatal("expected directive")
	}
	if !d.QueueReset {
		t.Error("expected QueueReset=true")
	}
}

func TestExtractQueueDirective_WithOptions(t *testing.T) {
	// drop:old is silently ignored (always summarize).
	d := ExtractQueueDirective("/queue collect debounce:2000 cap:5 drop:old")
	if !d.HasDirective {
		t.Fatal("expected directive")
	}
	if d.DebounceMs != 2000 {
		t.Errorf("expected debounce 2000, got %d", d.DebounceMs)
	}
	if d.Cap != 5 {
		t.Errorf("expected cap 5, got %d", d.Cap)
	}
	if !d.HasOptions {
		t.Error("expected HasOptions=true")
	}
}

func TestExtractQueueDirective_DebounceGoDuration(t *testing.T) {
	d := ExtractQueueDirective("/queue collect debounce:3s")
	if d.DebounceMs != 3000 {
		t.Errorf("expected debounce 3000ms from '3s', got %d", d.DebounceMs)
	}
}

func TestParseQueueDebounce(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"1000", 1000},
		{"0", 0},
		{"5s", 5000},
		{"500ms", 500},
		{"1m", 60000},
		{"invalid", 0},
		{"-5", 0},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := parseQueueDebounce(tc.input)
			if got != tc.want {
				t.Errorf("parseQueueDebounce(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseQueueCap(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"10", 10},
		{"1", 1},
		{"0", 0},
		{"-1", 0},
		{"abc", 0},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := parseQueueCap(tc.input)
			if got != tc.want {
				t.Errorf("parseQueueCap(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestSkipDirectiveArgPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{" hello", 1},
		{": hello", 2},
		{"  :  hello", 5},
		{"\t:\thello", 3},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := skipDirectiveArgPrefix(tc.input)
			if got != tc.want {
				t.Errorf("skipDirectiveArgPrefix(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestTakeDirectiveToken(t *testing.T) {
	tests := []struct {
		input string
		start int
		token string
		next  int
	}{
		{"hello world", 0, "hello", 5},
		{"hello world", 5, "world", 11},
		{"  hello", 0, "hello", 7},
		{"", 0, "", 0},
		{"hello\nworld", 0, "hello", 5},
		{"hello\nworld", 5, "", 5}, // stops at newline
	}

	for _, tc := range tests {
		token, next := takeDirectiveToken(tc.input, tc.start)
		if token != tc.token || next != tc.next {
			t.Errorf("takeDirectiveToken(%q, %d) = (%q, %d), want (%q, %d)",
				tc.input, tc.start, token, next, tc.token, tc.next)
		}
	}
}

func TestCompactSpaces(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello  world", "hello world"},
		{"  hello  ", " hello "},
		{"no-change", "no-change"},
		{"tab\t\there", "tab here"},
		{"", ""},
	}

	for _, tc := range tests {
		got := compactSpaces(tc.input)
		if got != tc.want {
			t.Errorf("compactSpaces(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
