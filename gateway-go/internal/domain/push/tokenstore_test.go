package push

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(filepath.Join(t.TempDir(), "push_tokens.json"))
	clock := int64(1000)
	s.now = func() int64 { clock += 10; return clock }
	return s
}

func TestStore_RegisterDedupAndRefresh(t *testing.T) {
	s := newTestStore(t)

	if n, err := s.Register("tok-a", "android"); err != nil || n != 1 {
		t.Fatalf("register a: n=%d err=%v", n, err)
	}
	if n, err := s.Register("tok-b", "ios"); err != nil || n != 2 {
		t.Fatalf("register b: n=%d err=%v", n, err)
	}
	// Re-registering an existing token must not grow the set.
	if n, err := s.Register("tok-a", "android"); err != nil || n != 2 {
		t.Fatalf("re-register a: n=%d err=%v (want dedup to 2)", n, err)
	}

	toks := s.Tokens()
	if len(toks) != 2 {
		t.Fatalf("len = %d, want 2", len(toks))
	}
	// LastSeen should advance on the refresh while RegisteredAt is preserved.
	for _, tok := range toks {
		if tok.Token == "tok-a" && tok.LastSeenMs <= tok.RegisteredAtMs {
			t.Errorf("tok-a LastSeen %d not advanced past RegisteredAt %d", tok.LastSeenMs, tok.RegisteredAtMs)
		}
	}
}

func TestStore_RegisterEmptyTokenRejected(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Register("   ", "android"); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestStore_Unregister(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Register("tok-a", "android")
	_, _ = s.Register("tok-b", "ios")

	if n, err := s.Unregister("tok-a"); err != nil || n != 1 {
		t.Fatalf("unregister: n=%d err=%v", n, err)
	}
	// Unregistering an absent token is a no-op, not an error.
	if n, err := s.Unregister("tok-missing"); err != nil || n != 1 {
		t.Fatalf("unregister missing: n=%d err=%v", n, err)
	}
}

func TestStore_Prune(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Register("tok-a", "android")
	_, _ = s.Register("tok-b", "ios")
	_, _ = s.Register("tok-c", "desktop")

	removed, err := s.Prune([]string{"tok-a", "tok-c", "tok-absent"})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	if got := s.Tokens(); len(got) != 1 || got[0].Token != "tok-b" {
		t.Errorf("after prune = %#v, want only tok-b", got)
	}
}

func TestStore_PersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "push_tokens.json")

	s1 := NewStore(path)
	if _, err := s1.Register("tok-a", "android"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := s1.Register("tok-b", "ios"); err != nil {
		t.Fatalf("register: %v", err)
	}

	// File is written atomically; perms must be 0600 (tokens are sensitive).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}

	// A fresh store over the same file recovers the set.
	s2 := NewStore(path)
	if got := s2.Tokens(); len(got) != 2 {
		t.Fatalf("reloaded len = %d, want 2", len(got))
	}
}

func TestStore_CorruptFileStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "push_tokens.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := NewStore(path)
	if got := s.Tokens(); len(got) != 0 {
		t.Fatalf("corrupt file should yield empty store, got %d", len(got))
	}
	// And a subsequent write must overwrite the corruption cleanly.
	if _, err := s.Register("tok-a", "android"); err != nil {
		t.Fatalf("register after corrupt: %v", err)
	}
	raw, _ := os.ReadFile(path)
	var list []DeviceToken
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("file not valid JSON after write: %v", err)
	}
}
