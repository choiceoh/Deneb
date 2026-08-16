package workfeed

import "strings"

const (
	// SourceSystemLog is an agent/runtime diagnostic card (model tuner, RSI
	// meta, telemetry). It belongs on the 로그 pivot, not the 업무 feed.
	SourceSystemLog     = "system_log"
	SourceGenesisMeta   = "genesis-meta"
	SourceGenesisLadder = "genesis-ladder"
)

// IsLogCard reports whether a work-feed item is a system/agent log, not a
// work card. The native 피드|결재|로그 pivot uses the same rule: log cards
// leave the 피드 list and land on 로그.
func IsLogCard(source, title string) bool {
	s := strings.TrimSpace(source)
	switch s {
	case SourceSystemLog, SourceGenesisMeta, SourceGenesisLadder:
		return true
	}
	if strings.HasPrefix(s, "genesis-") {
		return true
	}
	t := strings.TrimSpace(title)
	if t == "" {
		return false
	}
	if strings.Contains(t, "모델 튜너") ||
		strings.HasPrefix(t, "메타 개정") ||
		strings.Contains(t, "self-correction") ||
		strings.Contains(t, "자가개선 후보") {
		return true
	}
	modelish := strings.Contains(t, "GLM") || strings.Contains(t, "K3") || strings.Contains(t, "텔레메트리")
	healthish := strings.Contains(t, "지연") || strings.Contains(t, "오류") ||
		strings.Contains(t, "악화") || strings.Contains(t, "회귀")
	return modelish && healthish
}
