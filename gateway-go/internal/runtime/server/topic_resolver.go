package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
)

// topicResolver maps a Telegram forum threadID to a per-topic knowledge key,
// implementing chat.TopicResolver. It snapshots deneb.json topics.map at boot
// so per-turn lookups are pure in-memory map reads (prompt-cache Rule C: no
// per-request config reload).
type topicResolver struct {
	dir string
	m   map[string]string // threadID → key
	rev map[string]string // key → threadID (reverse of m; last write wins on dup keys)
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
	rev := make(map[string]string, len(tc.Map))
	for threadID, key := range tc.Map {
		m[threadID] = key
		rev[key] = threadID
	}
	if logger != nil {
		logger.Info("topics: per-topic knowledge enabled", "topics", len(m), "dir", tc.Dir)
	}
	return &topicResolver{dir: tc.Dir, m: m, rev: rev}
}

// TopicKey returns the topic key for a threadID, or "" when unmapped. The
// General topic (empty threadID) is normalized to "0".
func (r *topicResolver) TopicKey(threadID string) string {
	if threadID == "" {
		threadID = "0"
	}
	return r.m[threadID]
}

// ThreadIDForKey returns the forum threadID configured for a topic key, with
// ok=false when the key is unmapped. The reverse of TopicKey.
func (r *topicResolver) ThreadIDForKey(key string) (string, bool) {
	threadID, ok := r.rev[key]
	return threadID, ok
}

// Dir returns the configured knowledge directory (may be empty; the loader
// applies the "topics" default).
func (r *topicResolver) Dir() string { return r.dir }

// topicEntriesFromConfig snapshots deneb.json topics.map into a flat list for
// miniapp.topics.list, so the native client can render one topic switch per
// configured topic. Returns a fresh slice each call (the list handler sorts it
// in place) and nil when topics are unconfigured. Mirrors newTopicResolver's
// config source; topics are boot-static, so a per-call read is harmless and
// stays consistent with the threadID→key map injection uses.
func topicEntriesFromConfig() []handlerminiapp.KnowledgeTopic {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || snapshot == nil || snapshot.Config.Topics == nil {
		return nil
	}
	m := snapshot.Config.Topics.Map
	if len(m) == 0 {
		return nil
	}
	out := make([]handlerminiapp.KnowledgeTopic, 0, len(m))
	for threadID, key := range m {
		out = append(out, handlerminiapp.KnowledgeTopic{Key: key, ThreadID: threadID})
	}
	return out
}
