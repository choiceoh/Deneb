package workfeed

import "strings"

const (
	// SourceSystemLog is an agent/runtime diagnostic card (model tuner, RSI
	// meta, telemetry). It belongs on the 로그 pivot, not the 업무 feed.
	SourceSystemLog     = "system_log"
	SourceGenesisMeta   = "genesis-meta"
	SourceGenesisLadder = "genesis-ladder"
)

// IsLogSource reports whether source is a system/agent log lane. The native
// 피드|결재|로그 pivot uses this field only — producers stamp the source at
// write time instead of asking the client to guess from the title.
func IsLogSource(source string) bool {
	s := strings.TrimSpace(source)
	switch s {
	case SourceSystemLog, SourceGenesisMeta, SourceGenesisLadder:
		return true
	}
	return strings.HasPrefix(s, "genesis-")
}

// IsLogCard reports whether a work-feed item is a system/agent log.
// Title is ignored; keep the argument so existing call sites compile.
func IsLogCard(source, _ string) bool {
	return IsLogSource(source)
}

// rewriteLegacyLogSource lifts pre-#4513 diagnostic cards that were stored as
// proactive onto system_log. New producers stamp the source themselves; this
// only heals cards already on disk so the client can stay source-only.
func rewriteLegacyLogSource(item Item) Item {
	if IsLogSource(item.Source) {
		return item
	}
	if item.Source == SourceProactive && legacyProactiveLogTitle(item.Title) {
		item.Source = SourceSystemLog
	}
	return item
}

func legacyProactiveLogTitle(title string) bool {
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
