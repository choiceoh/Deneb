package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type sessionFocusStore struct {
	SessionKey string `json:"sessionKey"`
}

func sessionFocusStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".deneb", "session-focus.json"), nil
}

func loadSessionFocus(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var store sessionFocusStore
	if err := json.Unmarshal(data, &store); err != nil {
		return ""
	}
	return strings.TrimSpace(store.SessionKey)
}

func saveSessionFocus(path, key string) error {
	data, err := json.MarshalIndent(sessionFocusStore{SessionKey: strings.TrimSpace(key)}, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Server) currentSessionFocus() string {
	if s == nil || s.SessionManager == nil {
		return ""
	}
	s.sessionExtrasMu.Lock()
	defer s.sessionExtrasMu.Unlock()
	return s.sessionFocusKey
}

func (s *Server) setSessionFocus(key string) error {
	if s == nil || s.SessionManager == nil {
		return nil
	}
	key = strings.TrimSpace(key)
	s.sessionExtrasMu.Lock()
	s.sessionFocusKey = key
	s.sessionExtrasMu.Unlock()
	path, err := sessionFocusStorePath()
	if err != nil {
		return err
	}
	return saveSessionFocus(path, key)
}

func (s *Server) restoreSessionFocus() {
	if s == nil || s.SessionManager == nil {
		return
	}
	path, err := sessionFocusStorePath()
	if err != nil {
		return
	}
	key := loadSessionFocus(path)
	s.sessionExtrasMu.Lock()
	s.sessionFocusKey = key
	s.sessionExtrasMu.Unlock()
}

func (s *Server) forgetSessionExtras(key string) {
	key = strings.TrimSpace(key)
	if key == "" || s == nil || s.SessionManager == nil {
		return
	}
	s.sessionExtrasMu.Lock()
	if s.forgottenSessionKeys == nil {
		s.forgottenSessionKeys = map[string]struct{}{}
	}
	s.forgottenSessionKeys[key] = struct{}{}
	if s.sessionFocusKey == key {
		s.sessionFocusKey = ""
	}
	s.sessionExtrasMu.Unlock()

	dropStoredSessionModel(key)
	dropStoredSessionListPin(key)
	dropStoredSessionLabel(key)
	dropStoredSessionPin(key)
	if path, err := sessionFocusStorePath(); err == nil && loadSessionFocus(path) == key {
		_ = saveSessionFocus(path, "")
	}
}

func (s *Server) isForgottenSession(key string) bool {
	if s == nil || s.SessionManager == nil {
		return false
	}
	s.sessionExtrasMu.Lock()
	defer s.sessionExtrasMu.Unlock()
	_, ok := s.forgottenSessionKeys[key]
	return ok
}

func dropForgottenKeys(src map[string]string, forgotten func(string) bool) map[string]string {
	if len(src) == 0 {
		return src
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		if forgotten != nil && forgotten(k) {
			continue
		}
		out[k] = v
	}
	return out
}

func dropForgottenPins(src map[string]bool, forgotten func(string) bool) map[string]bool {
	if len(src) == 0 {
		return src
	}
	out := make(map[string]bool, len(src))
	for k := range src {
		if forgotten != nil && forgotten(k) {
			continue
		}
		out[k] = true
	}
	return out
}
