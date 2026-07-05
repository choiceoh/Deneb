package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// phoneUsageMaxAge bounds how stale the app-usage digest may be before phone_read
// requests a fresh sync_state. The client summarizes a six-hour window.
const phoneUsageMaxAge = 8 * time.Hour

func phoneUsageCachePath() string {
	return filepath.Join(config.ResolveStateDir(), "phone-usage.txt")
}

func readCachedPhoneUsage(maxAge time.Duration) (string, bool) {
	path := phoneUsageCachePath()
	data, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	age := time.Since(info.ModTime())
	if age > maxAge {
		return "", false
	}
	return fmt.Sprintf("앱 보고 사용 리듬 (약 %d분 전):\n%s", int(age.Minutes()), strings.TrimSpace(string(data))), true
}
