package nativesync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

func TestStoreAppendPull(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "native_sync.jsonl"))
	first, err := store.Append(AppendInput{
		Type:           TypeWorkFeedCreated,
		EntityID:       "wf_1",
		SessionKey:     "client:main",
		WorkFeedItemID: "wf_1",
		Payload:        map[string]any{"ok": true},
	})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	second, err := store.Append(AppendInput{Type: TypeTranscriptAppended, EntityID: "client:main"})
	if err != nil {
		t.Fatalf("append second: %v", err)
	}
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("seqs = %d/%d, want 1/2", first.Seq, second.Seq)
	}

	got, err := store.Pull(0, 1)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(got.Events) != 1 || got.Events[0].Seq != 1 || got.Cursor != 1 || !got.HasMore || got.LatestSeq != 2 {
		t.Fatalf("pull page = %+v", got)
	}
	var payload map[string]bool
	if err := json.Unmarshal(got.Events[0].Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if !payload["ok"] {
		t.Fatalf("payload ok missing: %+v", payload)
	}

	got, err = store.Pull(got.Cursor, 10)
	if err != nil {
		t.Fatalf("pull next: %v", err)
	}
	if len(got.Events) != 1 || got.Events[0].Seq != 2 || got.Cursor != 2 || got.HasMore {
		t.Fatalf("pull next = %+v", got)
	}
}

func TestStoreAppendRequiresType(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "native_sync.jsonl"))
	if _, err := store.Append(AppendInput{}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("append err = %v, want ErrInvalidEvent", err)
	}
}

func TestStorePullSkipsInvalidRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native_sync.jsonl")
	if err := jsonlAppendRaw(path, `{"seq":0,"type":"bad"}`+"\n"+`{"seq":3,"type":"workfeed.created"}`+"\n"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	store := NewStore(path)
	got, err := store.Pull(0, 10)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(got.Events) != 1 || got.Events[0].Seq != 3 {
		t.Fatalf("events = %+v", got.Events)
	}
}

func TestStoreAppendPrunesWhenOverCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native_sync.jsonl")

	// Seed the file past maxLogBytes with low seqs (well over keepEvents lines),
	// using a fat payload so the byte cap trips without needing millions of rows.
	bigPayload := json.RawMessage(`{"blob":"` + strings.Repeat("x", 4096) + `"}`)
	seedCount := keepEvents + 500
	var seeded []Event
	for i := 1; i <= seedCount; i++ {
		seeded = append(seeded, Event{
			Seq:         int64(i),
			Type:        TypeTranscriptAppended,
			TimestampMs: 1,
			Payload:     bigPayload,
		})
	}
	if err := jsonlstore.Snapshot(path, seeded); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	if stat, err := os.Stat(path); err != nil || stat.Size() <= int64(maxLogBytes) {
		t.Fatalf("seed must exceed maxLogBytes: size=%v err=%v", statSize(stat), err)
	}

	// The next Append should trigger pruneIfNeededLocked.
	store := NewStore(path)
	last, err := store.Append(AppendInput{Type: TypeWorkFeedCreated, EntityID: "wf_new"})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	// nextSeq is derived from the seeded max (seedCount), so the new event must
	// continue monotonically — pruning the head must not disturb seq assignment.
	if last.Seq != int64(seedCount+1) {
		t.Fatalf("new seq = %d, want %d", last.Seq, seedCount+1)
	}

	// File is now capped to keepEvents (it was keepEvents+500+1 before prune).
	remaining, err := jsonlstore.Load[Event](path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(remaining) != keepEvents {
		t.Fatalf("retained %d events, want %d", len(remaining), keepEvents)
	}
	// The retained tail must be the highest seqs, including the just-appended one,
	// and the oldest head rows must be gone.
	if remaining[len(remaining)-1].Seq != last.Seq {
		t.Fatalf("tail seq = %d, want %d", remaining[len(remaining)-1].Seq, last.Seq)
	}
	if remaining[0].Seq <= 1 {
		t.Fatalf("head seq = %d, oldest rows were not pruned", remaining[0].Seq)
	}

	// Pull must still return a coherent view over the pruned file.
	got, err := store.Pull(0, 0)
	if err != nil {
		t.Fatalf("pull after prune: %v", err)
	}
	if got.LatestSeq != last.Seq {
		t.Fatalf("LatestSeq = %d, want %d", got.LatestSeq, last.Seq)
	}
	if len(got.Events) != keepEvents {
		t.Fatalf("pull returned %d events, want %d", len(got.Events), keepEvents)
	}
}

func TestStoreAppendNoPruneUnderCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native_sync.jsonl")
	store := NewStore(path)
	for i := 0; i < 10; i++ {
		if _, err := store.Append(AppendInput{Type: TypeTranscriptAppended}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	remaining, err := jsonlstore.Load[Event](path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(remaining) != 10 {
		t.Fatalf("retained %d events, want 10 (no prune under cap)", len(remaining))
	}
}

func statSize(fi os.FileInfo) int64 {
	if fi == nil {
		return -1
	}
	return fi.Size()
}

func jsonlAppendRaw(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
