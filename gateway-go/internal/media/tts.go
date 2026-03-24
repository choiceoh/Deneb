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

// TtsAutoMode controls when TTS is applied.
type TtsAutoMode string

const (
	TtsAutoOff     TtsAutoMode = "off"
	TtsAutoAlways  TtsAutoMode = "always"
	TtsAutoInbound TtsAutoMode = "inbound"
	TtsAutoTagged  TtsAutoMode = "tagged"
)

// TtsProvider identifies a TTS provider.
type TtsProvider string

const (
	TtsProviderOpenAI     TtsProvider = "openai"
	TtsProviderElevenLabs TtsProvider = "elevenlabs"
	TtsProviderMicrosoft  TtsProvider = "microsoft"
)

// TtsConfig holds the resolved TTS configuration.
type TtsConfig struct {
	Auto           TtsAutoMode `json:"auto"`
	Provider       TtsProvider `json:"provider"`
	ProviderSource string      `json:"providerSource"` // "config" | "default"
	MaxTextLength  int         `json:"maxTextLength"`
	TimeoutMs      int         `json:"timeoutMs"`

	OpenAI     TtsOpenAIConfig     `json:"openai"`
	ElevenLabs TtsElevenLabsConfig `json:"elevenlabs"`
	Microsoft  TtsMicrosoftConfig  `json:"microsoft"`
}

// TtsOpenAIConfig holds OpenAI TTS configuration.
type TtsOpenAIConfig struct {
	APIKey       string `json:"apiKey,omitempty"`
	BaseURL      string `json:"baseUrl,omitempty"`
	Model        string `json:"model"`
	Voice        string `json:"voice"`
	Speed        *float64 `json:"speed,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

// TtsElevenLabsConfig holds ElevenLabs TTS configuration.
type TtsElevenLabsConfig struct {
	APIKey  string `json:"apiKey,omitempty"`
	BaseURL string `json:"baseUrl,omitempty"`
	VoiceID string `json:"voiceId"`
	ModelID string `json:"modelId"`
}

// TtsMicrosoftConfig holds Microsoft TTS configuration.
type TtsMicrosoftConfig struct {
	APIKey string `json:"apiKey,omitempty"`
	Region string `json:"region"`
	Voice  string `json:"voice"`
}

// TtsUserPrefs represents user preferences from ~/.deneb/settings/tts.json.
type TtsUserPrefs struct {
	Tts *TtsUserPrefSection `json:"tts,omitempty"`
}

// TtsUserPrefSection is the nested TTS prefs section.
type TtsUserPrefSection struct {
	Auto      *TtsAutoMode `json:"auto,omitempty"`
	Enabled   *bool        `json:"enabled,omitempty"`
	Provider  *TtsProvider `json:"provider,omitempty"`
	MaxLength *int         `json:"maxLength,omitempty"`
}

// TtsStatus represents the current TTS status for RPC responses.
type TtsStatus struct {
	Enabled  bool        `json:"enabled"`
	Auto     TtsAutoMode `json:"auto"`
	Provider TtsProvider `json:"provider"`
}

// voiceBubbleChannels are channels that prefer voice-bubble audio format (Opus).
var voiceBubbleChannels = map[string]bool{
	"telegram": true, "feishu": true, "whatsapp": true,
}

// Default TTS settings.
const (
	defaultMaxTextLength = 4000
	defaultTtsTimeoutMs  = 30_000
	defaultOpenAIModel   = "tts-1"
	defaultOpenAIVoice   = "alloy"
	defaultELVoiceID     = "21m00Tcm4TlvDq8ikWAM"
	defaultELModelID     = "eleven_monolingual_v1"
	defaultMSRegion      = "eastus"
	defaultMSVoice       = "en-US-JennyNeural"
)

// TtsManager handles TTS configuration and status.
type TtsManager struct {
	mu       sync.RWMutex
	config   TtsConfig
	prefsDir string
	logger   *slog.Logger
}

// NewTtsManager creates a new TTS manager.
func NewTtsManager(prefsDir string, logger *slog.Logger) *TtsManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &TtsManager{
		config:   defaultTtsConfig(),
		prefsDir: prefsDir,
		logger:   logger,
	}
}

func defaultTtsConfig() TtsConfig {
	return TtsConfig{
		Auto:           TtsAutoOff,
		Provider:       TtsProviderOpenAI,
		ProviderSource: "default",
		MaxTextLength:  defaultMaxTextLength,
		TimeoutMs:      defaultTtsTimeoutMs,
		OpenAI: TtsOpenAIConfig{
			Model: defaultOpenAIModel,
			Voice: defaultOpenAIVoice,
		},
		ElevenLabs: TtsElevenLabsConfig{
			VoiceID: defaultELVoiceID,
			ModelID: defaultELModelID,
		},
		Microsoft: TtsMicrosoftConfig{
			Region: defaultMSRegion,
			Voice:  defaultMSVoice,
		},
	}
}

// ResolveConfig resolves TTS configuration from config + user prefs + env.
func (m *TtsManager) ResolveConfig() TtsConfig {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	// Overlay user prefs.
	prefs := m.loadUserPrefs()
	if prefs.Tts != nil {
		if prefs.Tts.Auto != nil {
			cfg.Auto = *prefs.Tts.Auto
		} else if prefs.Tts.Enabled != nil {
			if *prefs.Tts.Enabled {
				cfg.Auto = TtsAutoAlways
			} else {
				cfg.Auto = TtsAutoOff
			}
		}
		if prefs.Tts.Provider != nil {
			cfg.Provider = *prefs.Tts.Provider
			cfg.ProviderSource = "config"
		}
		if prefs.Tts.MaxLength != nil && *prefs.Tts.MaxLength > 0 {
			cfg.MaxTextLength = *prefs.Tts.MaxLength
		}
	}

	// Auto-select provider by API key availability.
	if cfg.ProviderSource == "default" {
		cfg.Provider = autoSelectProvider()
	}

	// Resolve API keys from env.
	if cfg.OpenAI.APIKey == "" {
		cfg.OpenAI.APIKey = os.Getenv("OPENAI_API_KEY")
	}
	if cfg.ElevenLabs.APIKey == "" {
		cfg.ElevenLabs.APIKey = firstNonEmpty(os.Getenv("ELEVENLABS_API_KEY"), os.Getenv("XI_API_KEY"))
	}
	if cfg.Microsoft.APIKey == "" {
		cfg.Microsoft.APIKey = os.Getenv("AZURE_SPEECH_KEY")
	}

	return cfg
}

// GetStatus returns the current TTS status.
func (m *TtsManager) GetStatus() TtsStatus {
	cfg := m.ResolveConfig()
	return TtsStatus{
		Enabled:  cfg.Auto != TtsAutoOff,
		Auto:     cfg.Auto,
		Provider: cfg.Provider,
	}
}

// SetAutoMode updates the TTS auto mode in user prefs.
func (m *TtsManager) SetAutoMode(mode TtsAutoMode) error {
	prefs := m.loadUserPrefs()
	if prefs.Tts == nil {
		prefs.Tts = &TtsUserPrefSection{}
	}
	prefs.Tts.Auto = &mode
	return m.saveUserPrefs(prefs)
}

// SetProvider updates the TTS provider in user prefs.
func (m *TtsManager) SetProvider(provider TtsProvider) error {
	prefs := m.loadUserPrefs()
	if prefs.Tts == nil {
		prefs.Tts = &TtsUserPrefSection{}
	}
	prefs.Tts.Provider = &provider
	return m.saveUserPrefs(prefs)
}

// IsProviderConfigured checks if a provider has valid credentials.
func (m *TtsManager) IsProviderConfigured(provider TtsProvider) bool {
	cfg := m.ResolveConfig()
	switch provider {
	case TtsProviderOpenAI:
		return cfg.OpenAI.APIKey != ""
	case TtsProviderElevenLabs:
		return cfg.ElevenLabs.APIKey != ""
	case TtsProviderMicrosoft:
		return cfg.Microsoft.APIKey != ""
	default:
		return false
	}
}

// ResolveProviderOrder returns the provider fallback order, starting with primary.
func (m *TtsManager) ResolveProviderOrder(primary TtsProvider) []TtsProvider {
	all := []TtsProvider{TtsProviderOpenAI, TtsProviderElevenLabs, TtsProviderMicrosoft}
	order := []TtsProvider{primary}
	for _, p := range all {
		if p != primary {
			order = append(order, p)
		}
	}
	return order
}

// OutputFormat returns the preferred audio format for a channel.
func OutputFormat(channelID string) (format string, ext string) {
	if voiceBubbleChannels[channelID] {
		return "opus", ".opus"
	}
	return "mp3", ".mp3"
}

// --- User prefs I/O ---

func (m *TtsManager) prefsPath() string {
	return filepath.Join(m.prefsDir, "tts.json")
}

func (m *TtsManager) loadUserPrefs() TtsUserPrefs {
	var prefs TtsUserPrefs
	data, err := os.ReadFile(m.prefsPath())
	if err != nil {
		return prefs
	}
	json.Unmarshal(data, &prefs)
	return prefs
}

func (m *TtsManager) saveUserPrefs(prefs TtsUserPrefs) error {
	if err := os.MkdirAll(m.prefsDir, 0o755); err != nil {
		return fmt.Errorf("tts: mkdir: %w", err)
	}

	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("tts: marshal prefs: %w", err)
	}

	// Atomic write: temp file + rename.
	tmp := fmt.Sprintf("%s/.tmp.%d.%d", m.prefsDir, time.Now().UnixNano(), os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("tts: write temp: %w", err)
	}
	if err := os.Rename(tmp, m.prefsPath()); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("tts: rename: %w", err)
	}
	return nil
}

func autoSelectProvider() TtsProvider {
	if os.Getenv("OPENAI_API_KEY") != "" {
		return TtsProviderOpenAI
	}
	if os.Getenv("ELEVENLABS_API_KEY") != "" || os.Getenv("XI_API_KEY") != "" {
		return TtsProviderElevenLabs
	}
	return TtsProviderMicrosoft
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
