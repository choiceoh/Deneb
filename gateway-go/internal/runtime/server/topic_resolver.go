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
		// The native client and any threadId-less delivery (cron, heartbeat)
		// resolve to "0" (the 업무 home; see TopicKey). If the map has no "0"
		// entry, topic knowledge is silently dead for all of them — exactly what
		// a stale telegram-era map (numeric thread IDs, no "0") caused. Warn so
		// the misconfiguration doesn't go unnoticed again.
		if _, ok := topicSnapshot.m["0"]; !ok {
			logger.Warn(`topics.map has no "0" key — native client / threadId-less delivery (cron, heartbeat) receives NO topic knowledge; likely a stale telegram-era map`,
				"keys", topicKeys(topicSnapshot.m))
		}
	}
	return &topicResolver{dir: topicSnapshot.dir, m: topicSnapshot.m}
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

// topicKeys returns the map's source-ID keys for diagnostic logging.
func topicKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
