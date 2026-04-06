// loop_detector.go — Detects stuck tool-call loops in the agent executor.
//
// Three detection patterns (inspired by OpenClaw's tool-loop-detection.ts):
//   - generic_repeat: same tool+params called N times consecutively
//   - ping_pong: alternating between two tool calls with no progress
//   - global_circuit_breaker: total repeated calls across all patterns
//
// When triggered, returns a system message to inject into the conversation
// that breaks the loop by forcing the agent to change approach.
package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// LoopDetector tracks tool call patterns to detect stuck loops.
type LoopDetector struct {
	// Recent tool call fingerprints (tool name + input hash).
	history []string
	// Counts of consecutive identical calls.
	repeatCount int
	lastFingerprint string
	// Total repeated calls across the session.
	totalRepeats int
}

// LoopDetectorThresholds configures when loop detection triggers.
type LoopDetectorThresholds struct {
	RepeatWarning  int // consecutive identical calls before warning (default: 3)
	RepeatCritical int // consecutive identical calls before blocking (default: 5)
	CircuitBreaker int // total repeated calls before session block (default: 15)
}

// DefaultLoopThresholds returns sensible defaults for loop detection.
func DefaultLoopThresholds() LoopDetectorThresholds {
	return LoopDetectorThresholds{
		RepeatWarning:  3,
		RepeatCritical: 5,
		CircuitBreaker: 15,
	}
}

// LoopVerdict is the result of checking a tool call against loop patterns.
type LoopVerdict struct {
	Level   string // "ok", "warning", "critical", "blocked"
	Message string // system message to inject (empty if ok)
}

// NewLoopDetector creates a new loop detector.
func NewLoopDetector() *LoopDetector {
	return &LoopDetector{}
}

// Check records a tool call and returns a verdict.
func (d *LoopDetector) Check(toolName string, input []byte, thresholds LoopDetectorThresholds) LoopVerdict {
	fp := fingerprint(toolName, input)

	// Track consecutive repeats.
	if fp == d.lastFingerprint {
		d.repeatCount++
		d.totalRepeats++
	} else {
		d.repeatCount = 1
		d.lastFingerprint = fp
	}

	// Track history for ping-pong detection (keep last 6).
	d.history = append(d.history, fp)
	if len(d.history) > 6 {
		d.history = d.history[len(d.history)-6:]
	}

	// Check global circuit breaker first.
	if d.totalRepeats >= thresholds.CircuitBreaker {
		return LoopVerdict{
			Level: "blocked",
			Message: fmt.Sprintf(
				"[System: ⛔ 도구 호출 루프 감지 — 반복 호출이 %d회에 도달했습니다. "+
					"동일한 접근법을 반복하는 것은 다른 결과를 가져오지 않습니다. "+
					"완전히 다른 전략을 사용하세요. "+
					"현재까지의 결과를 바탕으로 사용자에게 상황을 설명하고 대안을 제시하세요.]",
				d.totalRepeats),
		}
	}

	// Check consecutive repeat.
	if d.repeatCount >= thresholds.RepeatCritical {
		return LoopVerdict{
			Level: "critical",
			Message: fmt.Sprintf(
				"[System: ⚠️ 도구 루프 감지 — %s을(를) %d회 연속 동일 파라미터로 호출했습니다. "+
					"반복을 멈추고 다른 접근법을 시도하세요: "+
					"(1) 다른 도구 사용 (2) 파라미터 변경 (3) 문제를 분해하여 다른 각도에서 접근 "+
					"(4) 사용자에게 현재 상황 보고]",
				toolName, d.repeatCount),
		}
	}

	if d.repeatCount >= thresholds.RepeatWarning {
		return LoopVerdict{
			Level: "warning",
			Message: fmt.Sprintf(
				"[System: 주의 — %s을(를) %d회 연속 동일하게 호출 중입니다. "+
					"이 접근법이 효과가 없다면 다른 방법을 고려하세요.]",
				toolName, d.repeatCount),
		}
	}

	// Check ping-pong pattern (A-B-A-B).
	if d.isPingPong() {
		d.totalRepeats += 2
		return LoopVerdict{
			Level: "warning",
			Message: "[System: 주의 — 두 도구 호출이 교대로 반복되고 있습니다 (ping-pong 패턴). " +
				"진전이 없다면 이 루프를 중단하고 근본 원인을 분석하세요.]",
		}
	}

	return LoopVerdict{Level: "ok"}
}

// isPingPong detects the A-B-A-B alternating pattern.
func (d *LoopDetector) isPingPong() bool {
	h := d.history
	if len(h) < 4 {
		return false
	}
	tail := h[len(h)-4:]
	return tail[0] == tail[2] && tail[1] == tail[3] && tail[0] != tail[1]
}

// fingerprint creates a compact identifier for a tool call.
func fingerprint(toolName string, input []byte) string {
	h := sha256.Sum256(input)
	return toolName + ":" + hex.EncodeToString(h[:8])
}
