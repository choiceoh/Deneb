package autoreply

import (
	"strings"
	"testing"
)

func TestResolveMemoryFlushSettings_defaults(t *testing.T) {
	s := ResolveMemoryFlushSettings(nil)
	if s == nil {
		t.Fatal("expected non-nil settings")
	}
	if !s.Enabled {
		t.Error("expected enabled=true")
	}
	if s.SoftThresholdTokens != DefaultMemoryFlushSoftTokens {
		t.Errorf("expected softTokens=%d, got %d", DefaultMemoryFlushSoftTokens, s.SoftThresholdTokens)
	}
	if s.ForceFlushTranscriptBytes != DefaultMemoryFlushForceTranscriptBytes {
		t.Errorf("expected forceBytes=%d, got %d", DefaultMemoryFlushForceTranscriptBytes, s.ForceFlushTranscriptBytes)
	}
	if !strings.Contains(s.Prompt, "Pre-compaction memory flush") {
		t.Error("expected default prompt")
	}
	if !strings.Contains(s.Prompt, SilentReplyToken) {
		t.Error("expected NO_REPLY hint in prompt")
	}
	if !strings.Contains(s.Prompt, "APPEND new content only") {
		t.Error("expected append hint in prompt")
	}
}

func TestResolveMemoryFlushSettings_custom(t *testing.T) {
	s := ResolveMemoryFlushSettings(&MemoryFlushConfig{
		SoftThresholdTokens: 8000,
		Prompt:              "Custom flush prompt.",
	})
	if s.SoftThresholdTokens != 8000 {
		t.Errorf("expected softTokens=8000, got %d", s.SoftThresholdTokens)
	}
	if !strings.Contains(s.Prompt, "Custom flush prompt") {
		t.Error("expected custom prompt content")
	}
	// Safety hints should still be present.
	if !strings.Contains(s.Prompt, "APPEND new content only") {
		t.Error("expected safety hint appended to custom prompt")
	}
}

func TestResolveMemoryFlushRelativePath(t *testing.T) {
	// 2024-01-15 00:00:00 UTC
	nowMs := int64(1705276800000)
	path := ResolveMemoryFlushRelativePath(nowMs, "UTC")
	if path != "memory/2024-01-15.md" {
		t.Errorf("expected memory/2024-01-15.md, got %s", path)
	}
}

func TestResolveMemoryFlushPromptForRun(t *testing.T) {
	prompt := "Flush memory to YYYY-MM-DD.md"
	nowMs := int64(1705276800000) // 2024-01-15 UTC
	result := ResolveMemoryFlushPromptForRun(prompt, nowMs, "UTC")
	if !strings.Contains(result, "2024-01-15") {
		t.Errorf("expected date stamp in result, got: %s", result)
	}
	if !strings.Contains(result, "Current time:") {
		t.Errorf("expected Current time: in result, got: %s", result)
	}
}

func TestShouldRunMemoryFlush(t *testing.T) {
	// Below threshold: should not flush.
	if ShouldRunMemoryFlush(ShouldRunMemoryFlushParams{
		TotalTokens:         1000,
		ContextWindowTokens: 200000,
		ReserveTokensFloor:  8192,
		SoftThresholdTokens: 4000,
	}) {
		t.Error("expected no flush below threshold")
	}

	// Above threshold: should flush.
	if !ShouldRunMemoryFlush(ShouldRunMemoryFlushParams{
		TotalTokens:         190000,
		ContextWindowTokens: 200000,
		ReserveTokensFloor:  8192,
		SoftThresholdTokens: 4000,
	}) {
		t.Error("expected flush above threshold")
	}

	// Already flushed for this compaction.
	if ShouldRunMemoryFlush(ShouldRunMemoryFlushParams{
		TotalTokens:                190000,
		ContextWindowTokens:        200000,
		ReserveTokensFloor:         8192,
		SoftThresholdTokens:        4000,
		CompactionCount:            3,
		MemoryFlushCompactionCount: 3,
		HasMemoryFlushCount:        true,
	}) {
		t.Error("expected no flush when already flushed for current compaction")
	}

	// Zero tokens: should not flush.
	if ShouldRunMemoryFlush(ShouldRunMemoryFlushParams{
		TotalTokens:         0,
		ContextWindowTokens: 200000,
	}) {
		t.Error("expected no flush with zero tokens")
	}
}

func TestShouldForceMemoryFlushByTranscriptSize(t *testing.T) {
	// Below byte threshold.
	if ShouldForceMemoryFlushByTranscriptSize(ShouldRunMemoryFlushParams{
		TranscriptBytes:           1024 * 1024, // 1 MB
		ForceFlushTranscriptBytes: 2 * 1024 * 1024,
	}) {
		t.Error("expected no force flush below byte threshold")
	}

	// Above byte threshold.
	if !ShouldForceMemoryFlushByTranscriptSize(ShouldRunMemoryFlushParams{
		TranscriptBytes:           3 * 1024 * 1024, // 3 MB
		ForceFlushTranscriptBytes: 2 * 1024 * 1024,
	}) {
		t.Error("expected force flush above byte threshold")
	}

	// Already flushed.
	if ShouldForceMemoryFlushByTranscriptSize(ShouldRunMemoryFlushParams{
		TranscriptBytes:            3 * 1024 * 1024,
		ForceFlushTranscriptBytes:  2 * 1024 * 1024,
		CompactionCount:            2,
		MemoryFlushCompactionCount: 2,
		HasMemoryFlushCount:        true,
	}) {
		t.Error("expected no force flush when already flushed")
	}

	// Disabled (0 threshold).
	if ShouldForceMemoryFlushByTranscriptSize(ShouldRunMemoryFlushParams{
		TranscriptBytes:           3 * 1024 * 1024,
		ForceFlushTranscriptBytes: 0,
	}) {
		t.Error("expected no force flush when disabled")
	}
}

func TestHasAlreadyFlushedForCurrentCompaction(t *testing.T) {
	if !HasAlreadyFlushedForCurrentCompaction(3, 3) {
		t.Error("expected true when counts match")
	}
	if HasAlreadyFlushedForCurrentCompaction(3, 2) {
		t.Error("expected false when counts differ")
	}
}
