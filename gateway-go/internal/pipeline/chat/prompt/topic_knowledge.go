package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// maxTopicKnowledgeChars caps a single topic knowledge file. It is set higher
// than context files' 8K cap (context_files.go:34) because per-topic knowledge
// is meant to be rich — e.g. a coding topic's maintenance knowledge
// (architecture, conventions, recurring pitfalls). The content lands in the
// Static (cached) block, so the cost is paid once per topic and reused across
// that topic's turns. The cap is an upper bound, not a target: a lean topic
// (e.g. a work topic) stays small simply by keeping its .md short, so a large
// coding topic never inflates an unrelated topic. ~24K chars is roughly 6K
// tokens (more for Korean-heavy text).
const maxTopicKnowledgeChars = 24_000

// defaultTopicDir is the workspace-relative directory holding <topicKey>.md
// files when TopicsConfig.Dir is empty.
const defaultTopicDir = "topics"

// TopicKnowledge holds resolved per-forum-topic knowledge for the system
// prompt. Key/Content/Hash are all empty when there is no injection (no topic
// mapped, unsafe key, or missing/empty file).
type TopicKnowledge struct {
	Key     string // topic key (e.g. "coding"); "" = no injection
	Content string // file body (trimmed, truncated); "" = no injection
	Hash    string // sha256 hex (12 chars) of Content; "" when Content==""
}

// LoadTopicKnowledge reads <dir>/<topicKey>.md and freezes the result per
// session, mirroring LoadContextFiles' frozen-snapshot semantics: the first
// call for a sessionKey caches the value and every later call returns it
// unchanged. This keeps the Static cache key byte-stable for the session even
// if the file is edited mid-session (edits take effect from the next session).
//
// It never returns an error: an empty topicKey, an unsafe key, or a
// missing/empty/unreadable file all yield an empty TopicKnowledge (no
// injection) so a misconfigured topic degrades to "no extra knowledge" rather
// than failing the agent run.
func LoadTopicKnowledge(workspaceDir, dir, topicKey, sessionKey string) TopicKnowledge {
	if topicKey == "" {
		return TopicKnowledge{}
	}

	// Frozen snapshot: return the session's first-loaded value.
	if sessionKey != "" {
		if frozen, ok := Cache.TopicSnapshot(sessionKey); ok {
			return frozen
		}
	}

	tk := loadTopicKnowledgeFromDisk(workspaceDir, dir, topicKey)

	// Freeze for this session (including empty results, so a missing file is
	// not re-stat'd every turn).
	if sessionKey != "" {
		Cache.SetTopicSnapshot(sessionKey, tk)
	}
	return tk
}

// loadTopicKnowledgeFromDisk performs the actual file read + truncation + hash.
func loadTopicKnowledgeFromDisk(workspaceDir, dir, topicKey string) TopicKnowledge {
	// Reject path traversal — topicKey is config-owned but must never escape
	// the knowledge dir.
	if strings.ContainsAny(topicKey, `/\`) || strings.Contains(topicKey, "..") {
		return TopicKnowledge{}
	}

	knowledgeDir := dir
	if knowledgeDir == "" {
		knowledgeDir = defaultTopicDir
	}
	if !filepath.IsAbs(knowledgeDir) {
		knowledgeDir = filepath.Join(workspaceDir, knowledgeDir)
	}
	path := filepath.Join(knowledgeDir, topicKey+".md")

	data, err := os.ReadFile(path)
	if err != nil {
		return TopicKnowledge{}
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return TopicKnowledge{}
	}
	if len(content) > maxTopicKnowledgeChars {
		// truncateContent (context_files.go) is UTF-8 safe (no mid-rune cut).
		content = strings.TrimSpace(truncateContent(content, maxTopicKnowledgeChars))
	}

	sum := sha256.Sum256([]byte(content))
	return TopicKnowledge{
		Key:     topicKey,
		Content: content,
		Hash:    hex.EncodeToString(sum[:])[:12],
	}
}
