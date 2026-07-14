package server

import (
	"os"
	"path/filepath"
	"testing"
)

// autoBackfillEnabled gates the one-shot historical-mail seeding: on by default,
// but off without archive creds, off when DENEB_MAIL_AUTOBACKFILL=0, and off once
// a completed-pass marker exists (so it runs exactly once, not every boot).
func TestAutoBackfillEnabledReturnsTrueOnlyWhenCredsPresentAndUnmarked(t *testing.T) {
	marker := filepath.Join(t.TempDir(), mailAutoBackfillMarker)

	t.Run("runs when no marker + creds present", func(t *testing.T) {
		t.Setenv("DENEB_MAIL_AUTOBACKFILL", "")
		if !autoBackfillEnabled(marker, "user", "pass") {
			t.Error("expected enabled with creds and no marker")
		}
	})

	t.Run("skips without archive creds", func(t *testing.T) {
		t.Setenv("DENEB_MAIL_AUTOBACKFILL", "")
		if autoBackfillEnabled(marker, "", "pass") {
			t.Error("no user → skip")
		}
		if autoBackfillEnabled(marker, "user", "") {
			t.Error("no pass → skip")
		}
	})

	t.Run("skips when opted out", func(t *testing.T) {
		t.Setenv("DENEB_MAIL_AUTOBACKFILL", "0")
		if autoBackfillEnabled(marker, "user", "pass") {
			t.Error("DENEB_MAIL_AUTOBACKFILL=0 → skip")
		}
	})

	t.Run("skips once a pass completed (marker present)", func(t *testing.T) {
		t.Setenv("DENEB_MAIL_AUTOBACKFILL", "")
		if err := os.WriteFile(marker, []byte("2026-07-09T00:00:00Z\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if autoBackfillEnabled(marker, "user", "pass") {
			t.Error("completed-pass marker → skip (must run exactly once)")
		}
	})
}
