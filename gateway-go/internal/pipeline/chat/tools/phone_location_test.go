package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReadCachedPhoneLocation exercises the freshness window that decides whether
// phone_read answers from the native client's pushed cache or falls back to a live
// Termux read.
func TestReadCachedPhoneLocation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", dir)
	path := filepath.Join(dir, "phone-location.json")

	// Absent cache → miss (caller does a live read).
	if _, ok := readCachedPhoneLocation(phoneLocationMaxAge); ok {
		t.Fatal("expected miss when the cache file is absent")
	}

	// Fresh cache → hit, carrying the verbatim payload + a Korean label.
	payload := `{"latitude":37.5012,"longitude":127.0396,"accuracy":18.0}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := readCachedPhoneLocation(phoneLocationMaxAge)
	if !ok {
		t.Fatal("expected a hit for a fresh cache")
	}
	if !strings.Contains(got, "37.5012") || !strings.Contains(got, "앱 보고 위치") {
		t.Errorf("formatted location is missing the payload or label: %q", got)
	}

	// Stale cache → miss. Push the mtime well past the freshness window.
	old := time.Now().Add(-2 * phoneLocationMaxAge)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, ok := readCachedPhoneLocation(phoneLocationMaxAge); ok {
		t.Error("expected a miss for a stale cache")
	}

	// Empty/whitespace cache → miss.
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readCachedPhoneLocation(phoneLocationMaxAge); ok {
		t.Error("expected a miss for an empty cache")
	}
}
