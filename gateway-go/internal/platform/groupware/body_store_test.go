package groupware

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApprovalBodyStore_RoundTripAndTTL(t *testing.T) {
	dir := t.TempDir()
	store := &ApprovalBodyStore{dir: dir, ttl: time.Hour}
	if err := store.Save("doc-1", "본문"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := store.Load("doc-1"); got != "본문" {
		t.Fatalf("Load = %q", got)
	}

	expired := &ApprovalBodyStore{dir: dir, ttl: time.Nanosecond}
	// Rewrite with old stamp via direct file so Load sees expiry.
	recPath := filepath.Join(dir, sanitizeApprovalCacheFilename("doc-2")+".json")
	old := ApprovalBodyRecord{DocID: "doc-2", Body: "stale", SavedAt: time.Now().UTC().Add(-time.Hour)}
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := expired.Load("doc-2"); got != "" {
		t.Fatalf("expired Load = %q, want empty", got)
	}
}
