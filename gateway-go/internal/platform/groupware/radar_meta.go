package groupware

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// RadarStateFileName is the radar persistence file under the state dir.
const RadarStateFileName = "groupware_radar_state.json"

// RadarDocMeta is read-only enrichment for letters/briefings (age + escalation).
type RadarDocMeta struct {
	AgeHours        int    `json:"age_hours,omitempty"`
	EscalationLevel int    `json:"escalation_level,omitempty"`
	StaleLabel      string `json:"stale_label,omitempty"`
}

// DefaultRadarStatePath returns {stateDir}/groupware_radar_state.json.
func DefaultRadarStatePath() string {
	return filepath.Join(config.ResolveStateDir(), RadarStateFileName)
}

// LoadRadarDocMetaIndex reads radar state and returns docID → age/escalation meta.
// Missing or unreadable state yields an empty map (never an error for callers
// that treat enrichment as best-effort).
func LoadRadarDocMetaIndex(statePath string, now time.Time) map[string]RadarDocMeta {
	out := make(map[string]RadarDocMeta)
	path := strings.TrimSpace(statePath)
	if path == "" {
		return out
	}
	state, err := loadRadarState(path)
	if err != nil || len(state.Docs) == 0 {
		return out
	}
	nowMs := now.UnixMilli()
	for id, doc := range state.Docs {
		id = strings.TrimSpace(id)
		if id == "" || doc.FirstSeenAt <= 0 {
			continue
		}
		age := time.Duration(nowMs-doc.FirstSeenAt) * time.Millisecond
		if age < 0 {
			age = 0
		}
		level := doc.EscalationLevel
		if level <= 0 {
			level = radarEscalationLevel(age)
		}
		meta := RadarDocMeta{
			AgeHours:        int(age / time.Hour),
			EscalationLevel: level,
		}
		if level > 0 {
			meta.StaleLabel = RadarStaleLabel(level, age)
		} else if age >= time.Hour {
			meta.StaleLabel = fmt.Sprintf("%d시간째", meta.AgeHours)
			if meta.AgeHours < 1 {
				meta.StaleLabel = "1시간 미만"
			}
		}
		out[id] = meta
	}
	return out
}

// RadarStaleLabel matches the radar escalation phrasing used on workfeed cards.
func RadarStaleLabel(level int, age time.Duration) string {
	if level >= RadarEscalationLevelTwentyFour {
		return "24시간 이상 방치"
	}
	if level >= RadarEscalationLevelFourHours {
		hours := int(age.Round(time.Hour) / time.Hour)
		if hours < 4 {
			hours = 4
		}
		return fmt.Sprintf("%d시간째 방치", hours)
	}
	return ""
}
