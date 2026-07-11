package acp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionBindingServiceBindResolveReplaceAndUnbind(t *testing.T) {
	svc := NewSessionBindingService()
	if svc.IsDirty() {
		t.Fatal("new binding service starts dirty")
	}
	if got := svc.Resolve("telegram", "acct", "thread"); got != nil {
		t.Fatalf("Resolve on empty service = %#v", got)
	}

	first := svc.Bind(SessionBindParams{
		TargetSessionKey: "client:main:research",
		TargetKind:       "subagent",
		Channel:          "telegram",
		AccountID:        "acct",
		ConversationID:   "thread",
		Placement:        "current",
		Label:            "research",
		AgentID:          "agent-1",
		BoundBy:          "operator",
	})
	if first == nil || first.BindingID != "bind_1" || first.ConversationID != "thread" || first.TargetKey != "client:main:research" {
		t.Fatalf("first bind result = %#v", first)
	}
	if !svc.IsDirty() {
		t.Fatal("Bind did not mark service dirty")
	}
	resolved := svc.Resolve("telegram", "acct", "thread")
	if resolved == nil || resolved.BindingID != "bind_1" || resolved.TargetSessionKey != "client:main:research" || resolved.BoundBy != "operator" {
		t.Fatalf("resolved first binding = %#v", resolved)
	}

	// Rebinding the exact conversation is replacement, not fan-out. The old
	// binding ID must become invalid or an unbind of stale UI state could tear
	// down the newly installed route.
	second := svc.Bind(SessionBindParams{
		TargetSessionKey: "client:main:review",
		Channel:          "telegram",
		AccountID:        "acct",
		ConversationID:   "thread",
		BoundBy:          "admin",
	})
	if second.BindingID != "bind_2" {
		t.Fatalf("replacement ID = %q, want bind_2", second.BindingID)
	}
	if err := svc.Unbind(first.BindingID); err == nil {
		t.Fatal("stale replaced binding unexpectedly remained unbindable")
	}
	resolved = svc.Resolve("telegram", "acct", "thread")
	if resolved == nil || resolved.BindingID != second.BindingID || resolved.TargetSessionKey != "client:main:review" {
		t.Fatalf("resolved replacement = %#v", resolved)
	}

	if err := svc.Unbind(second.BindingID); err != nil {
		t.Fatalf("Unbind replacement: %v", err)
	}
	if got := svc.Resolve("telegram", "acct", "thread"); got != nil {
		t.Fatalf("Resolve after unbind = %#v", got)
	}
	if err := svc.Unbind(second.BindingID); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("second Unbind error = %v", err)
	}
}

func TestSessionBindingResolveDoesNotExposeMutableState(t *testing.T) {
	svc := NewSessionBindingService()
	result := svc.Bind(SessionBindParams{
		TargetSessionKey: "client:main",
		Channel:          "telegram",
		AccountID:        "default",
		ConversationID:   "42",
		BoundBy:          "owner",
	})

	resolved := svc.Resolve("telegram", "default", "42")
	if resolved == nil {
		t.Fatal("Resolve = nil")
	}
	resolved.TargetSessionKey = "attacker-controlled"
	resolved.BoundBy = "mutated"

	again := svc.Resolve("telegram", "default", "42")
	if again == nil {
		t.Fatal("second Resolve = nil")
	}
	if again.BindingID != result.BindingID || again.TargetSessionKey != "client:main" || again.BoundBy != "owner" {
		t.Fatalf("Resolve leaked internal pointer mutation: %#v", again)
	}
}

func TestSessionBindingListAndSnapshotPreserveColonConversationIDs(t *testing.T) {
	svc := NewSessionBindingService()
	svc.Bind(SessionBindParams{
		TargetSessionKey: "client:main",
		Channel:          "discord",
		AccountID:        "work",
		ConversationID:   "guild:channel:thread",
		BoundBy:          "user-1",
	})
	svc.Bind(SessionBindParams{
		TargetSessionKey: "client:main",
		Channel:          "telegram",
		AccountID:        "personal",
		ConversationID:   "9001",
		BoundBy:          "user-2",
	})
	svc.Bind(SessionBindParams{
		TargetSessionKey: "client:other",
		Channel:          "slack",
		AccountID:        "corp",
		ConversationID:   "C123",
	})

	listed := svc.ListForSession("client:main")
	if len(listed) != 2 {
		t.Fatalf("ListForSession = %#v, want 2", listed)
	}
	sort.Slice(listed, func(i, j int) bool { return listed[i].Channel < listed[j].Channel })
	if listed[0].Channel != "discord" || listed[0].AccountID != "work" || listed[0].ConversationID != "guild:channel:thread" {
		t.Fatalf("colon conversation parsed incorrectly: %#v", listed[0])
	}
	if listed[0].TargetKey != "client:main" {
		t.Fatalf("target key = %q", listed[0].TargetKey)
	}
	if got := svc.ListForSession("missing"); len(got) != 0 {
		t.Fatalf("missing session bindings = %#v", got)
	}

	snapshot := svc.Snapshot()
	if len(snapshot) != 3 {
		t.Fatalf("Snapshot length = %d, want 3", len(snapshot))
	}
	var foundColon bool
	for _, entry := range snapshot {
		if entry.Channel == "discord" {
			foundColon = true
			if entry.AccountID != "work" || entry.ConversationID != "guild:channel:thread" || entry.BoundBy != "user-1" {
				t.Fatalf("discord snapshot = %#v", entry)
			}
		}
	}
	if !foundColon {
		t.Fatal("discord binding absent from snapshot")
	}
}

func TestSessionBindingRestoreReplacesStaleStateAndContinuesIDs(t *testing.T) {
	svc := NewSessionBindingService()
	svc.Bind(SessionBindParams{
		TargetSessionKey: "stale",
		Channel:          "old",
		AccountID:        "old",
		ConversationID:   "old",
	})
	svc.MarkClean()

	entries := []StoredBinding{
		{Channel: "telegram", AccountID: "a", ConversationID: "1", TargetSessionKey: "s1", BoundBy: "u1"},
		{Channel: "slack", AccountID: "b", ConversationID: "2", TargetSessionKey: "s2", BoundBy: "u2"},
	}
	svc.RestoreAll(entries)
	if !svc.IsDirty() {
		t.Fatal("RestoreAll did not mark service dirty")
	}
	if got := svc.Resolve("old", "old", "old"); got != nil {
		t.Fatalf("stale binding survived restore: %#v", got)
	}
	if got := svc.Resolve("telegram", "a", "1"); got == nil || got.BindingID != "bind_1" || got.TargetSessionKey != "s1" {
		t.Fatalf("restored first binding = %#v", got)
	}
	if got := svc.Resolve("slack", "b", "2"); got == nil || got.BindingID != "bind_2" || got.TargetSessionKey != "s2" {
		t.Fatalf("restored second binding = %#v", got)
	}

	next := svc.Bind(SessionBindParams{
		TargetSessionKey: "s3",
		Channel:          "discord",
		AccountID:        "c",
		ConversationID:   "3",
	})
	if next.BindingID != "bind_3" {
		t.Fatalf("post-restore binding ID = %q, want bind_3", next.BindingID)
	}

	svc.RestoreAll(nil)
	if got := svc.Snapshot(); len(got) != 0 {
		t.Fatalf("RestoreAll(nil) did not clear state: %#v", got)
	}
	clearedNext := svc.Bind(SessionBindParams{
		TargetSessionKey: "fresh",
		Channel:          "telegram",
		ConversationID:   "fresh",
	})
	if clearedNext.BindingID != "bind_1" {
		t.Fatalf("ID after clear = %q, want bind_1", clearedNext.BindingID)
	}
}

func TestSessionBindingConcurrentReadersAndWriters(t *testing.T) {
	svc := NewSessionBindingService()
	const workers = 24
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			channel := fmt.Sprintf("channel-%d", i%4)
			account := fmt.Sprintf("account-%d", i%3)
			conversation := fmt.Sprintf("conversation-%d", i)
			result := svc.Bind(SessionBindParams{
				TargetSessionKey: fmt.Sprintf("session-%d", i%5),
				Channel:          channel,
				AccountID:        account,
				ConversationID:   conversation,
			})
			if got := svc.Resolve(channel, account, conversation); got == nil || got.BindingID != result.BindingID {
				t.Errorf("worker %d resolve = %#v, result = %#v", i, got, result)
			}
			_ = svc.ListForSession(fmt.Sprintf("session-%d", i%5))
			_ = svc.Snapshot()
		}(i)
	}
	close(start)
	wg.Wait()
	if got := len(svc.Snapshot()); got != workers {
		t.Fatalf("concurrent snapshot length = %d, want %d", got, workers)
	}
}

func TestDefaultACPStorePaths(t *testing.T) {
	home := filepath.Join("home", "operator")
	if got, want := DefaultBindingStorePath(home), filepath.Join(home, ".deneb", "acp", "bindings.json"); got != want {
		t.Fatalf("binding path = %q, want %q", got, want)
	}
	if got, want := DefaultRegistryStorePath(home), filepath.Join(home, ".deneb", "acp", "agents.json"); got != want {
		t.Fatalf("registry path = %q, want %q", got, want)
	}
	if filepath.IsAbs(DefaultBindingStorePath("")) {
		t.Fatalf("empty-home path unexpectedly absolute: %q", DefaultBindingStorePath(""))
	}
}

func TestBindingStoreMissingMalformedAndPartialIO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "bindings.json")
	store := NewBindingStore(path)

	got, err := store.Load()
	if err != nil || got != nil {
		t.Fatalf("missing Load = (%#v, %v), want (nil,nil)", got, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"bindings":[`), 0o600); err != nil {
		t.Fatalf("write malformed fixture: %v", err)
	}
	if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "parse binding store") {
		t.Fatalf("malformed Load error = %v", err)
	}

	// Reading a directory exercises the non-ENOENT partial-I/O branch without
	// relying on Unix permission bits (tests often run as root in CI).
	directoryStore := NewBindingStore(dir)
	if _, err := directoryStore.Load(); err == nil || !strings.Contains(err.Error(), "read binding store") {
		t.Fatalf("directory Load error = %v", err)
	}

	blockedParent := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(blockedParent, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write blocked parent: %v", err)
	}
	blocked := NewBindingStore(filepath.Join(blockedParent, "bindings.json"))
	if err := blocked.Save([]StoredBinding{{Channel: "x"}}); err == nil || !strings.Contains(err.Error(), "save binding store") {
		t.Fatalf("blocked Save error = %v", err)
	}
}

func TestBindingStoreRoundTripNilEncodingAndStableRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "bindings.json")
	store := NewBindingStore(path)
	if err := store.Save(nil); err != nil {
		t.Fatalf("Save(nil): %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("store JSON lacks terminal newline: %q", data)
	}
	var disk BindingStoreFile
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("decode store: %v", err)
	}
	if disk.Version != 1 || disk.Bindings == nil || len(disk.Bindings) != 0 {
		t.Fatalf("nil binding encoding = %#v", disk)
	}

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat before stable save: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := store.Save(nil); err != nil {
		t.Fatalf("second Save(nil): %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat after stable save: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("unchanged save rewrote file: before=%s after=%s", before.ModTime(), after.ModTime())
	}

	want := []StoredBinding{
		{Channel: "telegram", AccountID: "default", ConversationID: "1", TargetSessionKey: "client:main", BoundBy: "owner"},
		{Channel: "discord", AccountID: "work", ConversationID: "guild:thread", TargetSessionKey: "client:other"},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save(bindings): %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load round-trip: %v", err)
	}
	if !reflect.DeepEqual(loaded, want) {
		t.Fatalf("round-trip = %#v, want %#v", loaded, want)
	}
}

func TestBindingStoreSyncDirtyLifecycleAndFailureRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bindings.json")
	store := NewBindingStore(path)
	svc := NewSessionBindingService()

	// A clean service must not create a store file.
	if err := store.SyncFromService(svc); err != nil {
		t.Fatalf("clean SyncFromService: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clean sync created file or unexpected stat error: %v", err)
	}

	svc.Bind(SessionBindParams{
		TargetSessionKey: "client:main",
		Channel:          "telegram",
		AccountID:        "default",
		ConversationID:   "1",
	})
	if err := store.SyncFromService(svc); err != nil {
		t.Fatalf("dirty SyncFromService: %v", err)
	}
	if svc.IsDirty() {
		t.Fatal("successful sync did not mark service clean")
	}

	// A failed atomic write must preserve dirty=true so a later retry is not
	// silently skipped.
	blockedParent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blockedParent, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocked parent: %v", err)
	}
	failedStore := NewBindingStore(filepath.Join(blockedParent, "bindings.json"))
	svc.Bind(SessionBindParams{
		TargetSessionKey: "client:other",
		Channel:          "slack",
		AccountID:        "work",
		ConversationID:   "2",
	})
	if err := failedStore.SyncFromService(svc); err == nil {
		t.Fatal("blocked SyncFromService unexpectedly succeeded")
	}
	if !svc.IsDirty() {
		t.Fatal("failed sync cleared dirty flag")
	}
}

func TestBindingStoreRestoreIsTransactionalOnLoadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bindings.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write malformed store: %v", err)
	}
	svc := NewSessionBindingService()
	svc.Bind(SessionBindParams{
		TargetSessionKey: "existing",
		Channel:          "telegram",
		ConversationID:   "1",
	})
	svc.MarkClean()

	if err := NewBindingStore(path).RestoreToService(svc); err == nil {
		t.Fatal("RestoreToService malformed store succeeded")
	}
	if got := svc.Resolve("telegram", "", "1"); got == nil || got.TargetSessionKey != "existing" {
		t.Fatalf("failed restore mutated existing service: %#v", got)
	}
	if svc.IsDirty() {
		t.Fatal("failed restore changed dirty state")
	}
}

func TestBindingStoreRestoreMissingAndPresent(t *testing.T) {
	dir := t.TempDir()
	missingSvc := NewSessionBindingService()
	missingSvc.Bind(SessionBindParams{TargetSessionKey: "keep", Channel: "x", ConversationID: "1"})
	missingSvc.MarkClean()
	if err := NewBindingStore(filepath.Join(dir, "missing.json")).RestoreToService(missingSvc); err != nil {
		t.Fatalf("restore missing: %v", err)
	}
	if got := missingSvc.Resolve("x", "", "1"); got == nil || got.TargetSessionKey != "keep" {
		t.Fatalf("missing restore cleared existing state: %#v", got)
	}

	want := []StoredBinding{{
		Channel: "telegram", AccountID: "a", ConversationID: "2", TargetSessionKey: "restored", BoundBy: "owner",
	}}
	path := filepath.Join(dir, "present.json")
	store := NewBindingStore(path)
	if err := store.Save(want); err != nil {
		t.Fatalf("seed binding store: %v", err)
	}
	svc := NewSessionBindingService()
	if err := store.RestoreToService(svc); err != nil {
		t.Fatalf("restore present: %v", err)
	}
	got := svc.Resolve("telegram", "a", "2")
	if got == nil || got.TargetSessionKey != "restored" || got.BoundBy != "owner" {
		t.Fatalf("restored binding = %#v", got)
	}
}

func TestRegistryStoreMissingMalformedAndPartialIO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "agents.json")
	store := NewRegistryStore(path)
	got, err := store.Load()
	if err != nil || got != nil {
		t.Fatalf("missing Load = (%#v,%v)", got, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"agents":`), 0o600); err != nil {
		t.Fatalf("write malformed registry: %v", err)
	}
	if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "parse registry store") {
		t.Fatalf("malformed Load error = %v", err)
	}
	if _, err := NewRegistryStore(dir).Load(); err == nil || !strings.Contains(err.Error(), "read registry store") {
		t.Fatalf("directory Load error = %v", err)
	}

	blockedParent := filepath.Join(dir, "regular")
	if err := os.WriteFile(blockedParent, []byte("x"), 0o600); err != nil {
		t.Fatalf("write regular parent: %v", err)
	}
	if err := NewRegistryStore(filepath.Join(blockedParent, "agents.json")).Save(nil); err == nil || !strings.Contains(err.Error(), "save registry store") {
		t.Fatalf("blocked Save error = %v", err)
	}
}

func TestRegistryStoreRoundTripFilteringAndRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "agents.json")
	store := NewRegistryStore(path)
	if err := store.Save(nil); err != nil {
		t.Fatalf("Save(nil): %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var empty RegistryStoreFile
	if err := json.Unmarshal(data, &empty); err != nil {
		t.Fatalf("decode empty store: %v", err)
	}
	if empty.Version != 1 || empty.Agents == nil || len(empty.Agents) != 0 {
		t.Fatalf("empty registry encoding = %#v", empty)
	}

	r := NewACPRegistry()
	r.Register(ACPAgent{ID: "idle", ParentID: "p", Role: "idle-role", Status: "idle", SessionKey: "acp:p:idle", SpawnedAt: 1})
	r.Register(ACPAgent{ID: "running", ParentID: "p", Role: "run-role", Status: "running", SessionKey: "acp:p:running", SpawnedAt: 2})
	r.Register(ACPAgent{ID: "done", ParentID: "p", Role: "done-role", Status: "done", SessionKey: "acp:p:done", SpawnedAt: 3})
	r.Register(ACPAgent{ID: "failed", ParentID: "p", Status: "failed", SessionKey: "acp:p:failed", SpawnedAt: 4})
	r.Register(ACPAgent{ID: "killed", ParentID: "p", Status: "killed", SessionKey: "acp:p:killed", SpawnedAt: 5})

	if err := store.SyncFromRegistry(r); err != nil {
		t.Fatalf("SyncFromRegistry: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ids := make([]string, 0, len(loaded))
	for _, a := range loaded {
		ids = append(ids, a.ID)
	}
	sort.Strings(ids)
	if want := []string{"done", "idle", "running"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("persisted IDs = %#v, want %#v", ids, want)
	}

	restoredRegistry := NewACPRegistry()
	restored, err := store.RestoreToRegistry(restoredRegistry)
	if err != nil {
		t.Fatalf("RestoreToRegistry: %v", err)
	}
	if restored != 3 {
		t.Fatalf("restored count = %d, want 3", restored)
	}
	for _, id := range ids {
		a := restoredRegistry.Get(id)
		if a == nil {
			t.Errorf("restored registry missing %q", id)
			continue
		}
		if a.Status != "idle" {
			t.Errorf("restored %q status = %q, want idle", id, a.Status)
		}
		if a.ParentID != "p" || a.SessionKey == "" || a.SpawnedAt == 0 {
			t.Errorf("restored %q lost metadata: %#v", id, a)
		}
	}
}

func TestRegistryStoreRestoreErrorLeavesRegistryUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("write malformed registry: %v", err)
	}
	r := NewACPRegistry()
	r.Register(ACPAgent{ID: "existing", Status: "running"})

	restored, err := NewRegistryStore(path).RestoreToRegistry(r)
	if err == nil || restored != 0 {
		t.Fatalf("RestoreToRegistry malformed = (%d,%v)", restored, err)
	}
	if got := r.Get("existing"); got == nil || got.Status != "running" {
		t.Fatalf("failed restore mutated registry: %#v", got)
	}
}

func TestRegistryStoreConcurrentSaveAndLoadSerializesFileAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	store := NewRegistryStore(path)
	const workers = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			agents := []ACPAgent{{
				ID:         fmt.Sprintf("agent-%d", i),
				Status:     "running",
				SessionKey: fmt.Sprintf("acp:p:agent-%d", i),
			}}
			if err := store.Save(agents); err != nil {
				t.Errorf("Save worker %d: %v", i, err)
				return
			}
			if _, err := store.Load(); err != nil {
				t.Errorf("Load worker %d: %v", i, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("final Load: %v", err)
	}
	if len(loaded) != 1 || !strings.HasPrefix(loaded[0].ID, "agent-") {
		t.Fatalf("final store contains torn data: %#v", loaded)
	}
}
