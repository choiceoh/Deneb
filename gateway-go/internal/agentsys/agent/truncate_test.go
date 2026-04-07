package agent

import (
	"strings"
	"testing"
)

func TestTruncateHeadTail_BelowLimit(t *testing.T) {
	content := strings.Repeat("a", 100)
	got := TruncateHeadTail(content, 200, "")
	if got != content {
		t.Error("content below limit should be returned unchanged")
	}
}

func TestTruncateHeadTail_ExactLimit(t *testing.T) {
	content := strings.Repeat("b", 200)
	got := TruncateHeadTail(content, 200, "")
	if got != content {
		t.Error("content at exact limit should be returned unchanged")
	}
}

func TestTruncateHeadTail_AboveLimit(t *testing.T) {
	// Build content with identifiable head and tail.
	head := strings.Repeat("H", 500)
	middle := strings.Repeat("M", 1000)
	tail := strings.Repeat("T", 500)
	content := head + middle + tail

	got := TruncateHeadTail(content, 1000, "")

	// Head portion should be preserved.
	if !strings.HasPrefix(got, head) {
		t.Error("head should be preserved")
	}
	// Tail portion should be preserved.
	if !strings.HasSuffix(got, tail) {
		t.Error("tail should be preserved")
	}
	// Middle should be replaced with marker.
	if !strings.Contains(got, "lines truncated") {
		t.Error("should contain truncation marker")
	}
	// Should NOT contain full middle.
	if strings.Contains(got, strings.Repeat("M", 100)) {
		t.Error("middle content should be discarded")
	}
}

func TestTruncateHeadTail_WithSpillID(t *testing.T) {
	content := strings.Repeat("x", 2000)
	got := TruncateHeadTail(content, 1000, "sp_abc123")

	if !strings.Contains(got, `read_spillover("sp_abc123")`) {
		t.Error("should contain read_spillover reference with spill ID")
	}
}

func TestTruncateHeadTail_WithoutSpillID(t *testing.T) {
	content := strings.Repeat("y", 2000)
	got := TruncateHeadTail(content, 1000, "")

	if strings.Contains(got, "read_spillover") {
		t.Error("should not contain read_spillover when no spill ID")
	}
	if !strings.Contains(got, "lines truncated") {
		t.Error("should contain truncation marker")
	}
}

func TestTruncateHeadTail_LineCount(t *testing.T) {
	// Create content with known line structure.
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = strings.Repeat("x", 20) // 20 chars per line
	}
	content := strings.Join(lines, "\n") // ~2100 chars total

	got := TruncateHeadTail(content, 1000, "")

	// Marker should report truncated line count.
	if !strings.Contains(got, "lines truncated") {
		t.Error("should contain line count in marker")
	}
}

func TestTruncateHeadTail_DefaultMaxOutput(t *testing.T) {
	// Verify the constant is sane.
	if DefaultMaxOutput < 1024 {
		t.Errorf("DefaultMaxOutput too small: %d", DefaultMaxOutput)
	}
	if DefaultMaxOutput > 256*1024 {
		t.Errorf("DefaultMaxOutput too large: %d", DefaultMaxOutput)
	}
}
