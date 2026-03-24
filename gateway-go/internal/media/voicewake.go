package media

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// VoiceWakeConfig holds voice wake trigger configuration.
type VoiceWakeConfig struct {
	Triggers    []string `json:"triggers"`
	UpdatedAtMs int64    `json:"updatedAtMs"`
}

// DefaultTriggers are the default voice wake trigger words.
var DefaultTriggers = []string{"deneb", "claude", "computer"}

// VoiceWakeManager manages voice wake trigger configuration.
type VoiceWakeManager struct {
	mu        sync.RWMutex
	configDir string
	logger    *slog.Logger
}

// NewVoiceWakeManager creates a new voice wake manager.
func NewVoiceWakeManager(configDir string, logger *slog.Logger) *VoiceWakeManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &VoiceWakeManager{
		configDir: configDir,
		logger:    logger,
	}
}

func (m *VoiceWakeManager) configPath() string {
	return filepath.Join(m.configDir, "voicewake.json")
}

// Get returns the current voice wake configuration.
func (m *VoiceWakeManager) Get() (*VoiceWakeConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &VoiceWakeConfig{Triggers: DefaultTriggers}, nil
		}
		return nil, fmt.Errorf("voicewake: read: %w", err)
	}

	var cfg VoiceWakeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("voicewake: parse: %w", err)
	}
	if len(cfg.Triggers) == 0 {
		cfg.Triggers = DefaultTriggers
	}
	return &cfg, nil
}

// Set updates the voice wake triggers.
// Returns the updated config.
func (m *VoiceWakeManager) Set(triggers []string) (*VoiceWakeConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Sanitize: trim whitespace, filter empty.
	var clean []string
	for _, t := range triggers {
		s := strings.TrimSpace(t)
		if s != "" {
			clean = append(clean, s)
		}
	}
	if len(clean) == 0 {
		clean = DefaultTriggers
	}

	cfg := &VoiceWakeConfig{
		Triggers:    clean,
		UpdatedAtMs: time.Now().UnixMilli(),
	}

	if err := os.MkdirAll(m.configDir, 0o755); err != nil {
		return nil, fmt.Errorf("voicewake: mkdir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("voicewake: marshal: %w", err)
	}

	// Atomic write.
	tmp := fmt.Sprintf("%s/.tmp.%d.%d", m.configDir, time.Now().UnixNano(), os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return nil, fmt.Errorf("voicewake: write: %w", err)
	}
	if err := os.Rename(tmp, m.configPath()); err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("voicewake: rename: %w", err)
	}

	m.logger.Info("voicewake triggers updated", "triggers", clean)
	return cfg, nil
}
