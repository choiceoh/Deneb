package tools

import (
	"os/exec"
	"strings"
)

func firstAvailableBinary(candidates ...string) (string, bool) {
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path, true
		}
	}
	return "", false
}

func nonEmptyCommandLines(raw string) []string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "./")
		line = strings.TrimPrefix(line, ".\\")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
