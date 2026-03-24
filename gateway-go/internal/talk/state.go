// Package talk manages the talk mode state and configuration.
//
// This ports the TypeScript talk system (src/gateway/server-methods/messaging/talk.ts)
// to Go, providing talk mode toggle and configuration retrieval.
package talk

import (
	"sync"
	"time"
)

// Config holds the talk mode configuration snapshot.
type Config struct {
	Talk    *TalkSettings    `json:"talk,omitempty"`
	Session *SessionSettings `json:"session,omitempty"`
	UI      *UISettings      `json:"ui,omitempty"`
}

// TalkSettings holds talk-specific configuration.
type TalkSettings struct {
	Enabled       bool   `json:"enabled"`
	WakeWord      string `json:"wakeWord,omitempty"`
	Voice         string `json:"voice,omitempty"`
	Speed         string `json:"speed,omitempty"`
	AutoSend      bool   `json:"autoSend,omitempty"`
	ListenTimeout int    `json:"listenTimeout,omitempty"`
}

// SessionSettings holds the active session info for talk mode.
type SessionSettings struct {
	MainKey string `json:"mainKey,omitempty"`
}

// UISettings holds UI-related settings for talk mode.
type UISettings struct {
	SeamColor string `json:"seamColor,omitempty"`
}

// ModeResult holds the result of a talk.mode toggle.
type ModeResult struct {
	Enabled bool   `json:"enabled"`
	Phase   string `json:"phase"`
	Ts      int64  `json:"ts"`
}

// State manages talk mode state.
type State struct {
	mu      sync.RWMutex
	enabled bool
	phase   string
	config  Config
}

// NewState creates a new talk state manager.
func NewState() *State {
	return &State{}
}

// GetConfig returns the current talk configuration.
func (s *State) GetConfig(includeSecrets bool) *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := s.config
	if cp.Talk == nil {
		cp.Talk = &TalkSettings{Enabled: s.enabled}
	} else {
		cp.Talk.Enabled = s.enabled
	}
	if !includeSecrets {
		// Strip any secret fields if needed.
	}
	return &cp
}

// SetMode toggles talk mode on/off with an optional phase.
func (s *State) SetMode(enabled bool, phase string) *ModeResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enabled = enabled
	if phase != "" {
		s.phase = phase
	}
	if !enabled {
		s.phase = ""
	}

	return &ModeResult{
		Enabled: s.enabled,
		Phase:   s.phase,
		Ts:      time.Now().UnixMilli(),
	}
}

// SetConfig updates the talk configuration.
func (s *State) SetConfig(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
}
