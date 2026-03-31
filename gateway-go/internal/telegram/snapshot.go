package telegram

import "sync"

// AccountSnapshot represents the full runtime state of a channel account.
// Mirrors proto/channel.proto ChannelAccountSnapshot and the TypeScript
// ChannelAccountSnapshot type in src/channels/plugins/types.ts.
//
// The Go gateway maintains this snapshot for RPC handlers such as
// channels.status to return accurate state.
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

// ChannelSnapshot holds the aggregated account snapshots for all channels.
// This is the aggregated snapshot payload returned by channels.status.
type ChannelSnapshot struct {
	// Channels maps channel ID to its primary account snapshot.
	Channels map[string]AccountSnapshot `json:"channels"`
	// ChannelAccounts maps channel ID to account ID to account snapshot
	// (for multi-account channels).
	ChannelAccounts map[string]map[string]AccountSnapshot `json:"channelAccounts,omitempty"`
}

// SnapshotStore maintains an in-memory channel snapshot that can be
// updated incrementally and synced to the Plugin Host.
type SnapshotStore struct {
	mu       sync.RWMutex
	channels map[string]AccountSnapshot
	accounts map[string]map[string]AccountSnapshot
}

// NewSnapshotStore creates an empty snapshot store.
func NewSnapshotStore() *SnapshotStore {
	return &SnapshotStore{
		channels: make(map[string]AccountSnapshot),
		accounts: make(map[string]map[string]AccountSnapshot),
	}
}

// Update sets the snapshot for a channel account.
func (s *SnapshotStore) Update(channelID string, snap AccountSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channels[channelID] = snap
}

// UpdateAccount sets the snapshot for a specific account within a channel.
func (s *SnapshotStore) UpdateAccount(channelID, accountID string, snap AccountSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accounts[channelID] == nil {
		s.accounts[channelID] = make(map[string]AccountSnapshot)
	}
	s.accounts[channelID][accountID] = snap
}

// Snapshot returns a copy of the full channel snapshot.
func (s *SnapshotStore) Snapshot() ChannelSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	channels := make(map[string]AccountSnapshot, len(s.channels))
	for k, v := range s.channels {
		channels[k] = v
	}
	accounts := make(map[string]map[string]AccountSnapshot, len(s.accounts))
	for chID, accts := range s.accounts {
		acctsCopy := make(map[string]AccountSnapshot, len(accts))
		for aID, v := range accts {
			acctsCopy[aID] = v
		}
		accounts[chID] = acctsCopy
	}
	return ChannelSnapshot{Channels: channels, ChannelAccounts: accounts}
}
