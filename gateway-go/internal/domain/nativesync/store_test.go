package nativesync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

func TestStoreAppendPullReturnsPagedEvents(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "native_sync.jsonl"))
	first, err := store.Append(AppendInput{
		Type:           TypeWorkFeedCreated,
		EntityID:       "wf_1",
		SessionKey:     "client:main",
		WorkFeedItemID: "wf_1",
		Payload:        mustJSON(t, map[string]any{"ok": true}),
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

func TestStoreAppendRejectsMissingType(t *testing.T) {
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

func TestStoreAppendPreservesAllEventsUnderCap(t *testing.T) {
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

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
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

func TestStorePullFlagsTruncatedCursorBelowRetainedWindow(t *testing.T) {
	// Post-prune retention shape: the head (seqs 1..500) is gone and the file
	// starts mid-stream. Seeded directly because tripping a real prune needs
	// >maxLogBytes of rows (see TestStoreAppendPrunesWhenOverCap) — Pull's
	// truncation predicate only cares that seqs are contiguous.
	path := filepath.Join(t.TempDir(), "native_sync.jsonl")
	var b strings.Builder
	for seq := 501; seq <= 505; seq++ {
		b.WriteString(`{"seq":` + strconv.Itoa(seq) + `,"type":"workfeed.created","timestampMs":1}` + "\n")
	}
	if err := jsonlAppendRaw(path, b.String()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	store := NewStore(path)

	// Cursor inside the pruned head: the (200, 501] range was rotated away
	// unseen — the client must be told the delta has a hole.
	got, err := store.Pull(200, 10)
	if err != nil {
		t.Fatalf("pull below window: %v", err)
	}
	if !got.Truncated {
		t.Fatalf("pull(cursor=200) truncated = false, want true")
	}
	if len(got.Events) != 5 || got.Events[0].Seq != 501 {
		t.Fatalf("events = %+v, want retained window 501..505", got.Events)
	}

	// Cursor exactly at the last pruned seq: nothing unseen was lost.
	got, err = store.Pull(500, 10)
	if err != nil {
		t.Fatalf("pull at window edge: %v", err)
	}
	if got.Truncated {
		t.Fatalf("pull(cursor=500) truncated = true, want false")
	}

	// Fresh-client baseline pull (cursor 0) is not a gap.
	got, err = store.Pull(0, 10)
	if err != nil {
		t.Fatalf("pull fresh: %v", err)
	}
	if got.Truncated {
		t.Fatalf("pull(cursor=0) truncated = true, want false")
	}

	// Cursor at head: nothing retained past it, no gap.
	got, err = store.Pull(505, 10)
	if err != nil {
		t.Fatalf("pull at head: %v", err)
	}
	if got.Truncated {
		t.Fatalf("pull(cursor=505) truncated = true, want false")
	}
}
