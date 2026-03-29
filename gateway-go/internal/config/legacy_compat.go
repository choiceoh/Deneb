package config

import "path/filepath"

// Legacy directory and config file names from previous product branding.
//
// To retire a legacy name: remove its entry here, delete the matching test
// row in paths_test.go, and let the compiler surface every remaining consumer.
var (
	legacyStateDirnames   = []string{".clawdbot", ".moldbot", ".moltbot"}
	legacyConfigFilenames = []string{"clawdbot.json", "moldbot.json", "moltbot.json"}
)

// findLegacyStateDir returns the first existing legacy state directory under
// home, scanning legacyStateDirnames in order. Returns "" if none exist.
func findLegacyStateDir(home string) string {
	for _, name := range legacyStateDirnames {
		candidate := filepath.Join(home, name)
		if dirExists(candidate) {
			return candidate
		}
	}
	return ""
}

// findLegacyConfigFile returns the first existing legacy config file in
// stateDir, scanning legacyConfigFilenames in order. Returns "" if none exist.
func findLegacyConfigFile(stateDir string) string {
	for _, name := range legacyConfigFilenames {
		candidate := filepath.Join(stateDir, name)
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}
