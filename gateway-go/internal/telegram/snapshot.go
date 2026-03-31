package telegram

import "sync"

// AccountSnapshot represents the runtime state of the Telegram bot account.
type AccountSnapshot struct {
	AccountID         string `json:"accountId"`
	Name              string `json:"name,omitempty"`
	Enabled           bool   `json:"enabled"`
	Configured        bool   `json:"configured"`
	Linked            bool   `json:"linked"`
	Running           bool   `json:"running"`
	Connected         bool   `json:"connected"`
	RestartPending    bool   `json:"restartPending,omitempty"`
	ReconnectAttempts int    `json:"reconnectAttempts,omitempty"`
	LastConnectedAt   int64  `json:"lastConnectedAt,omitempty"`
	LastMessageAt     int64  `json:"lastMessageAt,omitempty"`
	LastEventAt       int64  `json:"lastEventAt,omitempty"`
	LastError         string `json:"lastError,omitempty"`
	LastStartAt       int64  `json:"lastStartAt,omitempty"`
	LastStopAt        int64  `json:"lastStopAt,omitempty"`
	LastInboundAt     int64  `json:"lastInboundAt,omitempty"`
	LastOutboundAt    int64  `json:"lastOutboundAt,omitempty"`
	Busy              bool   `json:"busy,omitempty"`
	ActiveRuns        int    `json:"activeRuns,omitempty"`
	LastRunActivityAt int64  `json:"lastRunActivityAt,omitempty"`
	Mode              string `json:"mode,omitempty"`
	TokenSource       string `json:"tokenSource,omitempty"`
	TokenStatus       string `json:"tokenStatus,omitempty"`
	WebhookURL        string `json:"webhookUrl,omitempty"`
	BaseURL           string `json:"baseUrl,omitempty"`
	Port              int    `json:"port,omitempty"`
	LastProbeAt       int64  `json:"lastProbeAt,omitempty"`
}

// ChannelSnapshot is the JSON payload returned by channels.status.
// Single-channel: always contains exactly one "telegram" entry.
type ChannelSnapshot struct {
	Channels map[string]AccountSnapshot `json:"channels"`
}

// SnapshotStore maintains the Telegram account snapshot in memory.
type SnapshotStore struct {
	mu   sync.RWMutex
	snap AccountSnapshot
}

// NewSnapshotStore creates an empty snapshot store.
func NewSnapshotStore() *SnapshotStore {
	return &SnapshotStore{}
}

// Update sets the current account snapshot.
func (s *SnapshotStore) Update(snap AccountSnapshot) {
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
}

// Get returns a copy of the current account snapshot.
func (s *SnapshotStore) Get() AccountSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

// Snapshot returns the full channel snapshot for the channels.status RPC.
func (s *SnapshotStore) Snapshot() ChannelSnapshot {
	snap := s.Get()
	return ChannelSnapshot{
		Channels: map[string]AccountSnapshot{"telegram": snap},
	}
}
