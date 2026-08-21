package phoneops

import (
	"fmt"
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
	data, age, ok := readFreshPhoneCache(phoneUsageCachePath(), maxAge)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("앱 보고 사용 리듬 (약 %d분 전):\n%s", int(age.Minutes()), strings.TrimSpace(string(data))), true
}
