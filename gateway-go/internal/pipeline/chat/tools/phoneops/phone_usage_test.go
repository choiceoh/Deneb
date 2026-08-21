package phoneops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadCachedPhoneUsage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", dir)
	path := filepath.Join(dir, "phone-usage.txt")

	if _, ok := readCachedPhoneUsage(phoneUsageMaxAge); ok {
		t.Fatal("expected miss when the cache file is absent")
	}

	payload := "지난 6시간 앱 사용: 카카오톡 42분 · Chrome 18분"
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := readCachedPhoneUsage(phoneUsageMaxAge)
	if !ok {
		t.Fatal("expected a hit for a fresh cache")
	}
	if !strings.Contains(got, "카카오톡 42분") || !strings.Contains(got, "앱 보고 사용 리듬") {
		t.Errorf("formatted usage is missing the payload or label: %q", got)
	}

	old := time.Now().Add(-2 * phoneUsageMaxAge)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, ok := readCachedPhoneUsage(phoneUsageMaxAge); ok {
		t.Error("expected a miss for a stale cache")
	}
}
