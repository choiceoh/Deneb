package autoreply

import "testing"

func TestExtractQueueDirective_NoDirective(t *testing.T) {
	result := ExtractQueueDirective("hello world")
	if result.HasDirective {
		t.Fatal("expected no directive")
	}
	if result.Cleaned != "hello world" {
		t.Fatalf("unexpected cleaned: %q", result.Cleaned)
	}
}

func TestExtractQueueDirective_BasicMode(t *testing.T) {
	result := ExtractQueueDirective("/queue auto")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if result.QueueMode != QueueModeAuto {
		t.Fatalf("expected QueueModeAuto, got %q", result.QueueMode)
	}
}

func TestExtractQueueDirective_Off(t *testing.T) {
	result := ExtractQueueDirective("/queue off")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if result.QueueMode != QueueModeOff {
		t.Fatalf("expected QueueModeOff, got %q", result.QueueMode)
	}
}

func TestExtractQueueDirective_Reset(t *testing.T) {
	result := ExtractQueueDirective("/queue reset")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if !result.QueueReset {
		t.Fatal("expected QueueReset")
	}
}

func TestExtractQueueDirective_WithOptions(t *testing.T) {
	result := ExtractQueueDirective("/queue auto debounce=500 cap=10 drop=oldest")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if result.QueueMode != QueueModeAuto {
		t.Fatalf("expected auto, got %q", result.QueueMode)
	}
	if result.DebounceMs == nil || *result.DebounceMs != 500 {
		t.Fatalf("expected debounce=500, got %v", result.DebounceMs)
	}
	if result.Cap == nil || *result.Cap != 10 {
		t.Fatalf("expected cap=10, got %v", result.Cap)
	}
	if result.DropPolicy != QueueDropOldest {
		t.Fatalf("expected drop=oldest, got %q", result.DropPolicy)
	}
	if !result.HasOptions {
		t.Fatal("expected HasOptions")
	}
}

func TestExtractQueueDirective_ColonSyntax(t *testing.T) {
	result := ExtractQueueDirective("/queue auto debounce:1000")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if result.DebounceMs == nil || *result.DebounceMs != 1000 {
		t.Fatalf("expected debounce=1000 (colon syntax), got %v", result.DebounceMs)
	}
}

func TestExtractQueueDirective_DurationSuffix(t *testing.T) {
	result := ExtractQueueDirective("/queue auto debounce=2s")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if result.DebounceMs == nil || *result.DebounceMs != 2000 {
		t.Fatalf("expected debounce=2000 (2s), got %v", result.DebounceMs)
	}
}

func TestExtractQueueDirective_MsSuffix(t *testing.T) {
	result := ExtractQueueDirective("/queue auto debounce=500ms")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if result.DebounceMs == nil || *result.DebounceMs != 500 {
		t.Fatalf("expected debounce=500 (500ms), got %v", result.DebounceMs)
	}
}

func TestExtractQueueDirective_InlineText(t *testing.T) {
	result := ExtractQueueDirective("do stuff /queue manual hello")
	if !result.HasDirective {
		t.Fatal("expected directive")
	}
	if result.QueueMode != QueueModeManual {
		t.Fatalf("expected manual, got %q", result.QueueMode)
	}
	if result.Cleaned != "do stuff hello" {
		t.Fatalf("unexpected cleaned: %q", result.Cleaned)
	}
}

func TestExtractQueueDirective_Empty(t *testing.T) {
	result := ExtractQueueDirective("")
	if result.HasDirective {
		t.Fatal("expected no directive")
	}
}

func TestNormalizeQueueMode(t *testing.T) {
	tests := []struct {
		input string
		want  QueueMode
	}{
		{"auto", QueueModeAuto},
		{"AUTO", QueueModeAuto},
		{"manual", QueueModeManual},
		{"off", QueueModeOff},
		{"disable", QueueModeOff},
		{"unknown", ""},
	}
	for _, tt := range tests {
		got := NormalizeQueueMode(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeQueueMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeQueueDropPolicy(t *testing.T) {
	tests := []struct {
		input string
		want  QueueDropPolicy
	}{
		{"oldest", QueueDropOldest},
		{"old", QueueDropOldest},
		{"newest", QueueDropNewest},
		{"new", QueueDropNewest},
		{"unknown", ""},
	}
	for _, tt := range tests {
		got := NormalizeQueueDropPolicy(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeQueueDropPolicy(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
