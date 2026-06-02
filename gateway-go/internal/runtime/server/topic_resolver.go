package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
)

// topicResolver maps a delivery topic ID to a per-topic knowledge key,
// implementing chat.TopicResolver. It snapshots deneb.json topics.map at boot
// so per-turn lookups are pure in-memory map reads (prompt-cache Rule C: no
// per-request config reload).
type topicResolver struct {
	dir string
	m   map[string]string // source topic ID -> key
}

type topicConfigSnapshot struct {
	dir string
	m   map[string]string
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
	topicSnapshot := topicSnapshotFromConfig(tc)
	if topicSnapshot == nil {
		return nil
	}
	if logger != nil {
		logger.Info("topics: per-topic knowledge enabled", "topics", len(topicSnapshot.m), "dir", topicSnapshot.dir)
	}
	return &topicResolver{dir: topicSnapshot.dir, m: topicSnapshot.m}
}

// configuredTopicMap reloads topics.map for native-client discovery calls.
// Unlike topicResolver, this path is not used per model turn, so reflecting
// deneb.json edits without restart is more useful than holding a boot snapshot.
func configuredTopicMap() (map[string]string, error) {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || snapshot == nil || snapshot.Config.Topics == nil {
		return nil, err
	}
	topicSnapshot := topicSnapshotFromConfig(snapshot.Config.Topics)
	if topicSnapshot == nil {
		return nil, nil
	}
	return topicSnapshot.m, nil
}

func topicSnapshotFromConfig(tc *config.TopicsConfig) *topicConfigSnapshot {
	if tc == nil || len(tc.Map) == 0 {
		return nil
	}
	m := make(map[string]string, len(tc.Map))
	for sourceID, key := range tc.Map {
		m[sourceID] = key
	}
	return &topicConfigSnapshot{dir: tc.Dir, m: m}
}

// TopicKey returns the topic key for a delivery topic ID, or "" when unmapped.
// The default topic (empty ID) is normalized to "0".
func (r *topicResolver) TopicKey(threadID string) string {
	if threadID == "" {
		threadID = "0"
	}
	return r.m[threadID]
}

// Dir returns the configured knowledge directory (may be empty; the loader
// applies the "topics" default).
func (r *topicResolver) Dir() string { return r.dir }
