package polaris

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	chattranscript "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/transcript"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

func TestBridgeExposesLegacyToolResultReceiptStore(t *testing.T) {
	t.Parallel()
	legacy := chattranscript.NewCachedTranscriptStore(chattranscript.NewMemoryTranscriptStore(), 0)
	bridge := NewBridge(legacy, newStrictTestStore(t), strictTestLogger())
	receipts := chatport.ResolveToolResultReceiptStore(bridge)
	if receipts == nil {
		t.Fatal("Polaris bridge did not expose legacy receipt store")
	}
	if err := receipts.AppendToolResultReceipt("client:bridge", chatport.ToolResultReceipt{
		ToolUseID: "tool-1", Content: "done",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := receipts.LoadToolResultReceipts("client:bridge")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "done" {
		t.Fatalf("bridge receipts = %+v", got)
	}
}

func TestStrictBridgeReturnsAppendFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		strict     bool
		wantErrSub string
	}{
		{name: "production remains best effort"},
		{name: "strict fails closed", strict: true, wantErrSub: "strict dual-write append failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			legacy := chattranscript.NewMemoryTranscriptStore()
			store := newStrictTestStore(t)
			const sessionKey = "append-failure"
			blockPolarisMessagePath(t, store, sessionKey, false)

			bridge := NewBridgeWithOptions(legacy, store, strictTestLogger(), BridgeOptions{
				StrictPersistence: tc.strict,
			})
			err := bridge.Append(sessionKey, toolport.NewTextChatMessage("user", "persist me", 1))
			assertStrictBridgeError(t, err, tc.wantErrSub)

			messages, total, loadErr := legacy.Load(sessionKey, 0)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if total != 1 || len(messages) != 1 {
				t.Fatalf("legacy append count = (%d, %d), want (1, 1)", total, len(messages))
			}
		})
	}
}

func TestStrictBridgeMigratesLegacyPrefixBeforeAppend(t *testing.T) {
	t.Parallel()

	legacy := chattranscript.NewMemoryTranscriptStore()
	const sessionKey = "append-after-legacy"
	if err := legacy.Append(sessionKey, toolport.NewTextChatMessage("user", "existing", 1)); err != nil {
		t.Fatal(err)
	}
	store := newStrictTestStore(t)
	bridge := NewBridgeWithOptions(legacy, store, strictTestLogger(), BridgeOptions{
		StrictPersistence: true,
	})
	if err := bridge.Append(sessionKey, toolport.NewTextChatMessage("assistant", "new tail", 2)); err != nil {
		t.Fatal(err)
	}

	messages, err := store.LoadMessages(sessionKey, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].TextContent() != "existing" || messages[1].TextContent() != "new tail" {
		t.Fatalf("Polaris message order = %+v, want existing then new tail", messages)
	}
}

func TestStrictBridgeReturnsDeleteFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		strict     bool
		wantErrSub string
	}{
		{name: "production remains best effort"},
		{name: "strict fails closed", strict: true, wantErrSub: "strict delete failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			legacy := chattranscript.NewMemoryTranscriptStore()
			const sessionKey = "delete-failure"
			if err := legacy.Append(sessionKey, toolport.NewTextChatMessage("user", "remove me", 1)); err != nil {
				t.Fatal(err)
			}
			store := newStrictTestStore(t)
			blockPolarisMessagePath(t, store, sessionKey, true)

			bridge := NewBridgeWithOptions(legacy, store, strictTestLogger(), BridgeOptions{
				StrictPersistence: tc.strict,
			})
			err := bridge.Delete(sessionKey)
			assertStrictBridgeError(t, err, tc.wantErrSub)

			_, total, loadErr := legacy.Load(sessionKey, 0)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if total != 0 {
				t.Fatalf("legacy message count after delete = %d, want 0", total)
			}
		})
	}
}

func TestStrictBridgeReturnsLazyMigrationFailureAndRetries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		invoke func(*Bridge, string) error
	}{
		{
			name: "Load",
			invoke: func(bridge *Bridge, sessionKey string) error {
				_, _, err := bridge.Load(sessionKey, 0)
				return err
			},
		},
		{
			name: "AssembleContext",
			invoke: func(bridge *Bridge, sessionKey string) error {
				_, err := bridge.AssembleContext(sessionKey, 8_192, 24, strictTestLogger())
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			legacy := chattranscript.NewMemoryTranscriptStore()
			const sessionKey = "migration-failure"
			if err := legacy.Append(sessionKey, toolport.NewTextChatMessage("user", "migrate me", 1)); err != nil {
				t.Fatal(err)
			}
			store := newStrictTestStore(t)
			blocker := blockPolarisMessagePath(t, store, sessionKey, false)
			bridge := NewBridgeWithOptions(legacy, store, strictTestLogger(), BridgeOptions{
				StrictPersistence: true,
			})

			err := tc.invoke(bridge, sessionKey)
			assertStrictBridgeError(t, err, "strict lazy migration failed")

			if err := os.Remove(blocker); err != nil {
				t.Fatalf("repair Polaris message path: %v", err)
			}
			if err := tc.invoke(bridge, sessionKey); err != nil {
				t.Fatalf("retry after repairing Polaris: %v", err)
			}
			count, err := store.MessageCount(sessionKey)
			if err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("Polaris message count after retry = %d, want 1", count)
			}
		})
	}
}

func TestDefaultBridgeKeepsBestEffortLazyMigration(t *testing.T) {
	t.Parallel()

	legacy := chattranscript.NewMemoryTranscriptStore()
	const sessionKey = "best-effort-migration"
	if err := legacy.Append(sessionKey, toolport.NewTextChatMessage("user", "legacy remains authoritative", 1)); err != nil {
		t.Fatal(err)
	}
	store := newStrictTestStore(t)
	blockPolarisMessagePath(t, store, sessionKey, false)
	bridge := NewBridge(legacy, store, strictTestLogger())

	messages, total, err := bridge.Load(sessionKey, 0)
	if err != nil {
		t.Fatalf("default bridge Load: %v", err)
	}
	if total != 1 || len(messages) != 1 {
		t.Fatalf("default bridge legacy load = (%d, %d), want (1, 1)", total, len(messages))
	}
}

func newStrictTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func blockPolarisMessagePath(t *testing.T, store *Store, sessionKey string, nonEmpty bool) string {
	t.Helper()
	path := store.messagesPath(sessionKey)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("block Polaris message path: %v", err)
	}
	if nonEmpty {
		if err := os.WriteFile(filepath.Join(path, "blocker"), []byte("x"), 0o600); err != nil {
			t.Fatalf("make Polaris message path non-empty: %v", err)
		}
	}
	return path
}

func assertStrictBridgeError(t *testing.T, err error, wantSubstring string) {
	t.Helper()
	if wantSubstring == "" {
		if err != nil {
			t.Fatalf("unexpected best-effort error: %v", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), wantSubstring) {
		t.Fatalf("error = %v, want substring %q", err, wantSubstring)
	}
}

func strictTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
