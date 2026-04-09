package chat

import (
	"log/slog"
	"testing"
)

func TestAssembleContextRequiresBridge(t *testing.T) {
	// assembleContext must reject non-Bridge stores.
	store := &nonBridgeStore{}
	cfg := DefaultContextConfig()
	_, err := assembleContext(store, "test", cfg, slog.Default())
	if err == nil {
		t.Fatal("expected error for non-Bridge store")
	}
}

// nonBridgeStore is a minimal TranscriptStore that is NOT a polaris.Bridge.
type nonBridgeStore struct{}

func (s *nonBridgeStore) Load(string, int) ([]ChatMessage, int, error) { return nil, 0, nil }
func (s *nonBridgeStore) Append(string, ChatMessage) error             { return nil }
func (s *nonBridgeStore) Delete(string) error                          { return nil }
func (s *nonBridgeStore) ListKeys() ([]string, error)                  { return nil, nil }
func (s *nonBridgeStore) Search(string, int) ([]SearchResult, error)   { return nil, nil }
func (s *nonBridgeStore) CloneRecent(string, string, int) error        { return nil }
