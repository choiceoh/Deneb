package phoneops

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// phoneCallLogMaxAge bounds how stale the recent-call digest may be before
// phone_read asks for a fresh sync_state. Longer than the usage window: a call
// that happened is a fact that does not decay, so a slightly old digest is still
// correct — the age is reported alongside so the agent can judge for itself.
const phoneCallLogMaxAge = 12 * time.Hour

func phoneCallLogCachePath() string {
	return filepath.Join(config.ResolveStateDir(), "phone-calllog.txt")
}

func readCachedPhoneCallLog(maxAge time.Duration) (string, bool) {
	data, age, ok := readFreshPhoneCache(phoneCallLogCachePath(), maxAge)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("앱 보고 최근 통화 (약 %d분 전 기준):\n%s", int(age.Minutes()), strings.TrimSpace(string(data))), true
}
