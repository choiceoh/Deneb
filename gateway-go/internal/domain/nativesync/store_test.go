package nativesync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
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

func jsonlAppendRaw(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
