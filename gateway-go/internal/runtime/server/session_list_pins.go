package server

import (
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

func sessionListPinsStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".deneb", "session-list-pins.json"), nil
}

func loadSessionListPins(path string) map[string]bool {
	return loadSessionPins(path)
}

func saveSessionListPins(path string, pins map[string]bool) error {
	return saveSessionPins(path, pins)
}

// snapshotSessionListPins collects restorable conversations the user pinned
// to the top of the drawer. Distinct from snapshotSessionPins (rename lock).
func snapshotSessionListPins(sessions []*session.Session) map[string]bool {
	out := map[string]bool{}
	for _, s := range sessions {
		if s == nil || !s.Pinned {
			continue
		}
		if _, ok := session.RestorableTranscriptChannel(s.Key); !ok {
			continue
		}
		out[s.Key] = true
	}
	return out
}

func dropStoredSessionListPin(key string) {
	path, err := sessionListPinsStorePath()
	if err != nil {
		return
	}
	pins := loadSessionListPins(path)
	if !pins[key] {
		return
	}
	delete(pins, key)
	_ = saveSessionListPins(path, pins)
}
