package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
)

// topicResolver maps a Telegram forum threadID to a per-topic knowledge key,
// implementing chat.TopicResolver. It snapshots deneb.json topics.map at boot
// so per-turn lookups are pure in-memory map reads (prompt-cache Rule C: no
// per-request config reload).
type topicResolver struct {
	dir string
	m   map[string]string // threadID → key
}

// newTopicResolver reads topics config from deneb.json once. It returns nil
// when topics are unconfigured or the map is empty, so the chat handler leaves
// deps.topicResolver nil and skips per-topic injection entirely.
func newTopicResolver(logger *slog.Logger) chat.TopicResolver {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || snapshot == nil || snapshot.Config.Topics == nil {
		return nil
	}
	tc := snapshot.Config.Topics
	if len(tc.Map) == 0 {
		return nil
	}
	m := make(map[string]string, len(tc.Map))
	for threadID, key := range tc.Map {
		m[threadID] = key
	}
	if logger != nil {
		logger.Info("topics: per-topic knowledge enabled", "topics", len(m), "dir", tc.Dir)
	}
	return &topicResolver{dir: tc.Dir, m: m}
}

// TopicKey returns the topic key for a threadID, or "" when unmapped. The
// General topic (empty threadID) is normalized to "0".
func (r *topicResolver) TopicKey(threadID string) string {
	if threadID == "" {
		threadID = "0"
	}
	return r.m[threadID]
}

// Dir returns the configured knowledge directory (may be empty; the loader
// applies the "topics" default).
func (r *topicResolver) Dir() string { return r.dir }
