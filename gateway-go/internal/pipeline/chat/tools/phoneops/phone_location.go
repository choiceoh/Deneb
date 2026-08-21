package phoneops

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// phoneLocationMaxAge bounds how stale the cached native location may be before
// phone_read asks the native app to refresh (sync_state push). The native client
// pushes its location periodically / on geofence transitions, so a recent cache is
// the common case; beyond this window we'd rather request a fresh push than answer
// with a location that may be far out of date.
const phoneLocationMaxAge = 30 * time.Minute

// phoneLocationCachePath is the native client's last-known-location cache, written by
// the gateway when the app pushes `miniapp.event.ingest` type=location_update. The
// payload is stored verbatim (the FusedLocationProvider JSON) and the file mtime is
// the freshness signal — same convention as the phone heartbeat marker.
func phoneLocationCachePath() string {
	return filepath.Join(config.ResolveStateDir(), "phone-location.json")
}

// readCachedPhoneLocation returns the native client's last-known location when the
// cache is fresher than maxAge, plus a human "N분 전" age. Returns ok=false when the
// cache is missing, empty, or stale — the caller then does a live read.
func readCachedPhoneLocation(maxAge time.Duration) (string, bool) {
	data, age, ok := readFreshPhoneCache(phoneLocationCachePath(), maxAge)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("앱 보고 위치 (약 %d분 전):\n%s", int(age.Minutes()), strings.TrimSpace(string(data))), true
}

// readCachedPhoneBattery extracts the battery object the native client embeds
// in the same location_update payload ({"latitude":…, "battery":{…}}). ok=false
// when the cache is missing/stale or the payload predates the battery field
// (older app build) — the caller then requests a sync_state refresh.
func readCachedPhoneBattery(maxAge time.Duration) (string, bool) {
	data, age, ok := readFreshPhoneCache(phoneLocationCachePath(), maxAge)
	if !ok {
		return "", false
	}
	var fix struct {
		Battery json.RawMessage `json:"battery"`
	}
	if json.Unmarshal(data, &fix) != nil || len(fix.Battery) == 0 || string(fix.Battery) == "null" {
		return "", false
	}
	return fmt.Sprintf("앱 보고 배터리 (약 %d분 전): %s", int(age.Minutes()), string(fix.Battery)), true
}

func readFreshPhoneCache(path string, maxAge time.Duration) ([]byte, time.Duration, bool) {
	data, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return nil, 0, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, false
	}
	age := time.Since(info.ModTime())
	if age > maxAge {
		return nil, 0, false
	}
	return data, age, true
}
