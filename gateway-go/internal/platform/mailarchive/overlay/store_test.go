package overlay_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive/overlay"
)

func TestStoreSavesLocatorsAndMutationsAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	store := overlay.NewStore(path)
	if got := store.Get("missing"); got != (overlay.MessageState{}) {
		t.Fatalf("missing state = %+v", got)
	}
	if store.Known("missing") {
		t.Fatal("missing id reported known")
	}
	if got := store.Snapshot(); got == nil || len(got) != 0 {
		t.Fatalf("initial snapshot = %#v, want empty non-nil map", got)
	}

	if err := store.RememberLocator("message-1", "INBOX", "42"); err != nil {
		t.Fatal(err)
	}
	located := store.Get("message-1")
	if located.Mailbox != "INBOX" || located.UID != "42" {
		t.Fatalf("locator = %+v", located)
	}
	if located.Read || located.Archived || located.Trashed || located.UpdatedAtMS != 0 {
		t.Fatalf("locator write mutated flags = %+v", located)
	}

	before := time.Now().UnixMilli()
	if err := store.MarkRead("message-1"); err != nil {
		t.Fatal(err)
	}
	read := store.Get("message-1")
	if !read.Read || read.Archived || read.Trashed {
		t.Fatalf("read state = %+v", read)
	}
	if read.UpdatedAtMS < before || read.UpdatedAtMS > time.Now().UnixMilli() {
		t.Fatalf("read update timestamp = %d", read.UpdatedAtMS)
	}
	if read.Mailbox != "INBOX" || read.UID != "42" {
		t.Fatalf("read lost locator = %+v", read)
	}

	if err := store.MarkArchived("message-1"); err != nil {
		t.Fatal(err)
	}
	archived := store.Get("message-1")
	if !archived.Read || !archived.Archived || archived.Trashed {
		t.Fatalf("archived state = %+v", archived)
	}

	if err := store.MarkTrashed("message-1"); err != nil {
		t.Fatal(err)
	}
	trashed := store.Get("message-1")
	if !trashed.Read || !trashed.Archived || !trashed.Trashed {
		t.Fatalf("trashed state = %+v", trashed)
	}

	reopened := overlay.NewStore(path)
	if got := reopened.Get("message-1"); got != trashed {
		t.Fatalf("reopened state = %+v, want %+v", got, trashed)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state file mode = %o, want 600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got&0o077 != 0 {
		t.Fatalf("state directory mode = %o", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) || !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatalf("state file is not newline-terminated JSON: %q", raw)
	}
}

func TestStoreRememberLocatorsBatchesAndPreservesFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := overlay.NewStore(path)
	if err := store.RememberLocator("one", "Old", "1"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkArchived("one"); err != nil {
		t.Fatal(err)
	}
	if err := store.RememberLocators(map[string]overlay.MessageState{
		"one": {Mailbox: "INBOX", UID: "10"},
		"two": {Mailbox: "Sent", UID: "20"},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := overlay.NewStore(path).Snapshot()
	if one := snapshot["one"]; !one.Archived || !one.Read || one.Mailbox != "INBOX" || one.UID != "10" {
		t.Fatalf("one = %+v", one)
	}
	if two := snapshot["two"]; two.Mailbox != "Sent" || two.UID != "20" {
		t.Fatalf("two = %+v", two)
	}
}

func TestStoreReadsExistingStateSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy := []byte(`{
  "messages": {
    "legacy-message": {
      "read": true,
      "archived": true,
      "mailbox": "Gmail",
      "uid": "91",
      "updatedAtMs": 1234
    }
  }
}
`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	store := overlay.NewStore(path)
	got := store.Get("legacy-message")
	if !got.Read || !got.Archived || got.Trashed || got.Mailbox != "Gmail" || got.UID != "91" || got.UpdatedAtMS != 1234 {
		t.Fatalf("legacy state = %+v", got)
	}
	if err := store.MarkTrashed("legacy-message"); err != nil {
		t.Fatal(err)
	}
	got = overlay.NewStore(path).Get("legacy-message")
	if !got.Read || !got.Archived || !got.Trashed || got.Mailbox != "Gmail" || got.UID != "91" || got.UpdatedAtMS <= 1234 {
		t.Fatalf("mutated legacy state = %+v", got)
	}
}

func TestStoreInvalidInputsAreNoOpsAndCorruptStateRecovers(t *testing.T) {
	var nilStore *overlay.Store
	if got := nilStore.Get("id"); got != (overlay.MessageState{}) {
		t.Fatalf("nil get = %+v", got)
	}
	if nilStore.Known("id") {
		t.Fatal("nil store reported known")
	}
	if got := nilStore.Snapshot(); got != nil {
		t.Fatalf("nil snapshot = %#v", got)
	}
	for name, action := range map[string]func() error{
		"remember": func() error { return nilStore.RememberLocator("id", "INBOX", "1") },
		"read":     func() error { return nilStore.MarkRead("id") },
		"archive":  func() error { return nilStore.MarkArchived("id") },
		"trash":    func() error { return nilStore.MarkTrashed("id") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(); err != nil {
				t.Fatal(err)
			}
		})
	}

	path := filepath.Join(t.TempDir(), "state.json")
	store := overlay.NewStore(path)
	for name, action := range map[string]func() error{
		"empty locator id":      func() error { return store.RememberLocator("", "INBOX", "1") },
		"empty locator mailbox": func() error { return store.RememberLocator("id", "", "1") },
		"empty locator uid":     func() error { return store.RememberLocator("id", "INBOX", "") },
		"empty read id":         func() error { return store.MarkRead("") },
		"empty archive id":      func() error { return store.MarkArchived("") },
		"empty trash id":        func() error { return store.MarkTrashed("") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(); err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid no-op created state file: %v", err)
	}

	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot(); len(got) != 0 {
		t.Fatalf("corrupt file snapshot = %+v", got)
	}
	if err := store.MarkRead("recovered"); err != nil {
		t.Fatal(err)
	}
	if !store.Get("recovered").Read {
		t.Fatal("store did not recover from invalid JSON")
	}
}

func TestStoreSnapshotReturnsIndependentCopy(t *testing.T) {
	store := overlay.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.RememberLocator("id", "INBOX", "9"); err != nil {
		t.Fatal(err)
	}
	first := store.Snapshot()
	first["id"] = overlay.MessageState{Mailbox: "tampered", UID: "0"}
	first["injected"] = overlay.MessageState{Read: true}
	second := store.Snapshot()
	if got := second["id"]; got.Mailbox != "INBOX" || got.UID != "9" {
		t.Fatalf("snapshot mutation leaked into store: %+v", got)
	}
	if _, exists := second["injected"]; exists {
		t.Fatal("injected snapshot entry leaked into store")
	}
}

func TestStoreInstancesForSamePathPreserveConcurrentUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	const workers = 16
	const messagesPerWorker = 20
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			store := overlay.NewStore(path)
			for i := range messagesPerWorker {
				id := fmt.Sprintf("message-%02d-%02d", worker, i)
				if err := store.RememberLocator(id, "INBOX", fmt.Sprintf("%d", worker*messagesPerWorker+i)); err != nil {
					t.Errorf("remember %s: %v", id, err)
					return
				}
				if err := store.MarkRead(id); err != nil {
					t.Errorf("read %s: %v", id, err)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	snapshot := overlay.NewStore(path).Snapshot()
	want := workers * messagesPerWorker
	if len(snapshot) != want {
		t.Fatalf("snapshot size = %d, want %d", len(snapshot), want)
	}
	for id, state := range snapshot {
		if !state.Read || state.Mailbox != "INBOX" || state.UID == "" {
			t.Fatalf("incomplete state %s: %+v", id, state)
		}
	}
}

func TestStoreReturnsFilesystemFailures(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := overlay.NewStore(filepath.Join(blocker, "state.json"))
	if err := store.MarkRead("id"); err == nil {
		t.Fatal("write through regular-file parent succeeded")
	}
}
