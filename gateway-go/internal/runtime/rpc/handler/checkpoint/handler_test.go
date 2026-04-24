package checkpoint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestMethodsReturnsThreeHandlers(t *testing.T) {
	m := Methods(Deps{Root: t.TempDir()})
	for _, name := range []string{"checkpoint.list", "checkpoint.restore", "checkpoint.diff"} {
		if m[name] == nil {
			t.Errorf("missing handler %s", name)
		}
	}
}

func TestDisabledRootReturnsUnavailable(t *testing.T) {
	m := Methods(Deps{Root: ""})
	resp := m["checkpoint.list"](context.Background(), &protocol.RequestFrame{
		ID:     "r1",
		Params: mustMarshal(t, ListParams{SessionKey: "k"}),
	})
	if resp == nil || resp.Error == nil {
		t.Fatalf("expected error, got %+v", resp)
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %q, want %q", resp.Error.Code, protocol.ErrUnavailable)
	}
}

func TestListRequiresSessionKey(t *testing.T) {
	m := Methods(Deps{Root: t.TempDir()})
	resp := m["checkpoint.list"](context.Background(), &protocol.RequestFrame{
		ID:     "r1",
		Params: mustMarshal(t, ListParams{}),
	})
	if resp == nil || resp.Error == nil {
		t.Fatalf("expected error, got %+v", resp)
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %q, want MISSING_PARAM", resp.Error.Code)
	}
}

func TestRoundtripListRestoreDiff(t *testing.T) {
	root := t.TempDir()
	sessionKey := "telegram:999"

	// Seed a snapshot via the real pkg/checkpoint API so the on-disk layout
	// matches what the RPC handler expects to find.
	mgr := checkpoint.New(root, sessionKey)
	workdir := t.TempDir()
	target := filepath.Join(workdir, "f.txt")
	if err := os.WriteFile(target, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := mgr.Snapshot(context.Background(), target, "fs_write")
	if err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	if err := os.WriteFile(target, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}

	methods := Methods(Deps{Root: root})

	// --- list ---
	resp := methods["checkpoint.list"](context.Background(), &protocol.RequestFrame{
		ID:     "r-list",
		Params: mustMarshal(t, ListParams{SessionKey: sessionKey}),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("list error: %+v", resp)
	}
	var listOut ListResult
	if err := json.Unmarshal(resp.Payload, &listOut); err != nil {
		t.Fatalf("list unmarshal: %v", err)
	}
	if listOut.Total != 1 || len(listOut.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %+v", listOut)
	}
	if listOut.Snapshots[0].ID != snap.ID {
		t.Errorf("list snap id = %q, want %q", listOut.Snapshots[0].ID, snap.ID)
	}
	if listOut.Snapshots[0].Path != target {
		t.Errorf("list path = %q, want %q", listOut.Snapshots[0].Path, target)
	}

	// --- diff ---
	resp = methods["checkpoint.diff"](context.Background(), &protocol.RequestFrame{
		ID:     "r-diff",
		Params: mustMarshal(t, DiffParams{SessionKey: sessionKey, ID: snap.ID}),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("diff error: %+v", resp)
	}
	var diffOut DiffResult
	if err := json.Unmarshal(resp.Payload, &diffOut); err != nil {
		t.Fatalf("diff unmarshal: %v", err)
	}
	if diffOut.ID != snap.ID {
		t.Errorf("diff id = %q, want %q", diffOut.ID, snap.ID)
	}
	if diffOut.Path != target {
		t.Errorf("diff path = %q, want %q", diffOut.Path, target)
	}
	if !strings.Contains(diffOut.Diff, "before") || !strings.Contains(diffOut.Diff, "after") {
		t.Errorf("diff missing before/after content: %s", diffOut.Diff)
	}

	// --- diff with missing id ---
	resp = methods["checkpoint.diff"](context.Background(), &protocol.RequestFrame{
		ID:     "r-diff-404",
		Params: mustMarshal(t, DiffParams{SessionKey: sessionKey, ID: "nope"}),
	})
	if resp == nil || resp.Error == nil {
		t.Fatalf("expected 404 diff error, got %+v", resp)
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("code = %q, want NOT_FOUND", resp.Error.Code)
	}

	// --- restore ---
	resp = methods["checkpoint.restore"](context.Background(), &protocol.RequestFrame{
		ID:     "r-restore",
		Params: mustMarshal(t, RestoreParams{SessionKey: sessionKey, ID: snap.ID}),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("restore error: %+v", resp)
	}
	var restoreOut RestoreResult
	if err := json.Unmarshal(resp.Payload, &restoreOut); err != nil {
		t.Fatalf("restore unmarshal: %v", err)
	}
	if restoreOut.Restored == nil || restoreOut.Restored.ID != snap.ID {
		t.Errorf("restore out = %+v, want snap id %q", restoreOut, snap.ID)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "before" {
		t.Errorf("restore did not bring back content; got %q", string(data))
	}

	// --- restore with missing id ---
	resp = methods["checkpoint.restore"](context.Background(), &protocol.RequestFrame{
		ID:     "r-restore-404",
		Params: mustMarshal(t, RestoreParams{SessionKey: sessionKey, ID: "nope"}),
	})
	if resp == nil || resp.Error == nil {
		t.Fatalf("expected 404 restore error, got %+v", resp)
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("code = %q, want NOT_FOUND", resp.Error.Code)
	}
}

func TestListEmptySession(t *testing.T) {
	methods := Methods(Deps{Root: t.TempDir()})
	resp := methods["checkpoint.list"](context.Background(), &protocol.RequestFrame{
		ID:     "r-empty",
		Params: mustMarshal(t, ListParams{SessionKey: "untouched"}),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("expected OK for empty session, got %+v", resp)
	}
	var out ListResult
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatal(err)
	}
	if out.Total != 0 || len(out.Snapshots) != 0 {
		t.Errorf("expected empty list, got %+v", out)
	}
}

func TestListLimit(t *testing.T) {
	root := t.TempDir()
	sessionKey := "s"
	mgr := checkpoint.New(root, sessionKey)
	for i := range 4 {
		p := filepath.Join(t.TempDir(), "a.txt")
		if err := os.WriteFile(p, []byte{byte('a' + i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := mgr.Snapshot(context.Background(), p, "fs"); err != nil {
			t.Fatal(err)
		}
	}

	methods := Methods(Deps{Root: root})
	resp := methods["checkpoint.list"](context.Background(), &protocol.RequestFrame{
		ID:     "r-lim",
		Params: mustMarshal(t, ListParams{SessionKey: sessionKey, Limit: 2}),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("limit list error: %+v", resp)
	}
	var out ListResult
	_ = json.Unmarshal(resp.Payload, &out)
	if out.Total != 2 || len(out.Snapshots) != 2 {
		t.Errorf("expected 2 snapshots under limit=2, got %+v", out)
	}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
