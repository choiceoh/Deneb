package server

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

func sessionModelsStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".deneb", "session-models.json"), nil
}

func loadSessionModels(path string) map[string]string {
	return loadSessionLabels(path)
}

func saveSessionModels(path string, models map[string]string) error {
	return saveSessionLabels(path, models)
}

// snapshotSessionModels collects per-conversation model overrides for restorable
// sessions. Empty Model means "follow the global default" and is not stored.
func snapshotSessionModels(sessions []*session.Session) map[string]string {
	out := map[string]string{}
	for _, s := range sessions {
		if s == nil || strings.TrimSpace(s.Model) == "" {
			continue
		}
		if _, ok := session.RestorableTranscriptChannel(s.Key); !ok {
			continue
		}
		out[s.Key] = s.Model
	}
	return out
}
