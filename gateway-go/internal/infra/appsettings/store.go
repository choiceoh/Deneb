// Package appsettings persists user-mutable runtime state that must survive
// gateway restarts but does not belong in the deployment config
// (~/.deneb/deneb.json). Today this holds only the "active home" — the chat
// that owns conversation after the user migrates from the 1:1 bot chat into
// a Forum supergroup via /use-forum. The store is atomic-write JSON in
// ~/.deneb/app-settings.json with a tmp+rename to survive crashes mid-save.
package appsettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const defaultFile = "app-settings.json"

// ActiveHome identifies the chat that owns the bot's conversation. ChatID==0
// means "no migration yet — accept any chat" (legacy 1:1 behavior, the
// installed default). Type is kept alongside for diagnostics and future
// routing tweaks; it does not gate behavior today.
type ActiveHome struct {
	ChatID int64  `json:"chatId,omitempty"`
	Type   string `json:"type,omitempty"`
}

// IsSet reports whether a migration has occurred. Inbound checks against
// IsSet() rather than ChatID!=0 so a future "reset to default" path can
// re-zero the struct without rebuilding a comparison.
func (h ActiveHome) IsSet() bool { return h.ChatID != 0 }

// Settings is the persisted root.
type Settings struct {
	ActiveHome ActiveHome `json:"activeHome,omitempty"`
}

// Store is a thread-safe Settings persistence layer backed by a single JSON
// file. It is safe to share across goroutines.
type Store struct {
	path string
	mu   sync.RWMutex
	s    Settings
}

// NewStore opens (or creates) the settings file under dir. A missing file is
// not an error — it produces an empty Settings.
func NewStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("appsettings: dir is required")
	}
	st := &Store{path: filepath.Join(dir, defaultFile)}
	if err := st.load(); err != nil {
		return nil, err
	}
	return st, nil
}

func (st *Store) load() error {
	data, err := os.ReadFile(st.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read app settings: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &st.s); err != nil {
		return fmt.Errorf("parse app settings: %w", err)
	}
	return nil
}

// ActiveHome returns a copy of the current active home. Empty struct means
// no migration has occurred.
func (st *Store) ActiveHome() ActiveHome {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.s.ActiveHome
}

// SetActiveHome updates the active home and persists immediately. The write
// is atomic via tmp+rename so a crash mid-save leaves the prior settings
// intact rather than producing a truncated file. Primitive signature is
// intentional — the chat package's AppSettings interface uses primitives so
// it stays free of any infra import.
func (st *Store) SetActiveHome(chatID int64, chatType string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.ActiveHome = ActiveHome{ChatID: chatID, Type: chatType}
	return st.saveLocked()
}

func (st *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}
	data, err := json.MarshalIndent(st.s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	if err := os.Rename(tmp, st.path); err != nil {
		// Best-effort cleanup of the orphan tmp; the rename failure is the
		// real error worth surfacing.
		_ = os.Remove(tmp)
		return fmt.Errorf("rename settings: %w", err)
	}
	return nil
}
