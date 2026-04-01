package shadow

import (
	"fmt"
	"strings"
	"time"
)

// ContextPrefetcher detects topic changes and prepares relevant context proactively.
type ContextPrefetcher struct {
	svc *Service

	// State (guarded by svc.mu).
	currentTopic    string
	topicStartedAt  int64
	prefetchedCtx   []PrefetchedContext
}

// PrefetchedContext is pre-gathered context ready for the main session.
type PrefetchedContext struct {
	Topic       string   `json:"topic"`
	Files       []string `json:"files,omitempty"`       // relevant file paths
	RecentDiffs string   `json:"recentDiffs,omitempty"` // summary of recent changes
	Notes       string   `json:"notes,omitempty"`       // context notes
	PreparedAt  int64    `json:"preparedAt"`            // unix ms
}

func newContextPrefetcher(svc *Service) *ContextPrefetcher {
	return &ContextPrefetcher{svc: svc}
}

// topicKeywords maps topic keywords to relevant code areas.
var topicKeywords = map[string][]string{
	"telegram":  {"internal/telegram/", "internal/server/inbound_telegram.go", "internal/server/server_chat.go"},
	"chat":      {"internal/chat/", "internal/llm/"},
	"memory":    {"internal/memory/", "internal/aurora/", "internal/unified/"},
	"cron":      {"internal/cron/", "internal/autonomous/"},
	"session":   {"internal/session/", "internal/server/session_manager.go"},
	"auth":      {"internal/auth/", "internal/provider/"},
	"vega":      {"internal/vega/", "core-rs/vega/"},
	"proto":     {"proto/", "pkg/protocol/"},
	"tool":      {"internal/chat/tools/", "internal/chat/toolreg/"},
	"ffi":       {"internal/ffi/", "core-rs/core/src/lib.rs"},
	"shadow":    {"internal/shadow/"},
	"deploy":    {"scripts/", "Makefile"},
	"test":      {"*_test.go"},
	"docs":      {"docs/"},
	"webhook":   {"internal/server/webhook_github.go"},
	"skill":     {"internal/skill/", "skills/"},
	"exec":      {"internal/process/", "internal/chat/tools/exec.go"},
	"코딩":      {"internal/telegram/"},
	"빌드":      {"Makefile", "scripts/"},
	"테스트":    {"*_test.go"},
	"배포":      {"scripts/release*", ".github/workflows/"},
	"메모리":    {"internal/memory/", "internal/aurora/"},
	"검색":      {"internal/vega/"},
}

// OnMessageForTopic analyzes a message for topic context and triggers prefetch
// when a topic change is detected.
func (cp *ContextPrefetcher) OnMessageForTopic(content string) {
	topic := detectTopic(content)
	if topic == "" {
		return
	}

	cp.svc.mu.Lock()
	oldTopic := cp.currentTopic
	if topic != oldTopic {
		cp.currentTopic = topic
		cp.topicStartedAt = time.Now().UnixMilli()

		// Record topic change.
		if len(cp.svc.topicHistory) >= maxTopicHistory {
			cp.svc.topicHistory = cp.svc.topicHistory[1:]
		}
		cp.svc.topicHistory = append(cp.svc.topicHistory, TopicChange{
			Topic: topic,
			Ts:    time.Now().UnixMilli(),
		})
	}
	cp.svc.mu.Unlock()

	if topic != oldTopic && oldTopic != "" {
		cp.svc.cfg.Logger.Info("shadow: topic change detected",
			"from", oldTopic,
			"to", topic,
		)
		go cp.prefetchForTopic(topic)
	}
}

// prefetchForTopic gathers relevant context for a detected topic.
func (cp *ContextPrefetcher) prefetchForTopic(topic string) {
	files, ok := topicKeywords[topic]
	if !ok {
		return
	}

	ctx := PrefetchedContext{
		Topic:      topic,
		Files:      files,
		Notes:      fmt.Sprintf("'%s' 관련 작업 감지. 관련 파일: %s", topic, strings.Join(files, ", ")),
		PreparedAt: time.Now().UnixMilli(),
	}

	cp.svc.mu.Lock()
	// Keep only last 5 prefetched contexts.
	if len(cp.prefetchedCtx) >= 5 {
		cp.prefetchedCtx = cp.prefetchedCtx[1:]
	}
	cp.prefetchedCtx = append(cp.prefetchedCtx, ctx)
	cp.svc.mu.Unlock()

	cp.svc.emit(ShadowEvent{Type: "context_prefetched", Payload: ctx})
}

// OnPRActivity prefetches context for files related to a PR.
// Called when a pull_request webhook arrives with an interesting action.
func (cp *ContextPrefetcher) OnPRActivity(pr map[string]any) {
	title, _ := pr["title"].(string)
	number, _ := pr["number"].(float64)
	url, _ := pr["html_url"].(string)

	// Use the PR title to detect a topic via the same keyword matching.
	topic := detectTopic(title)
	if topic == "" {
		topic = "pull_request"
	}

	files, _ := topicKeywords[topic]
	ctx := PrefetchedContext{
		Topic:      topic,
		Files:      files,
		Notes:      fmt.Sprintf("PR #%d '%s' 관련 컨텍스트 준비\n%s", int(number), truncate(title, 60), url),
		PreparedAt: time.Now().UnixMilli(),
	}

	cp.svc.mu.Lock()
	if len(cp.prefetchedCtx) >= 5 {
		cp.prefetchedCtx = cp.prefetchedCtx[1:]
	}
	cp.prefetchedCtx = append(cp.prefetchedCtx, ctx)
	cp.svc.mu.Unlock()

	cp.svc.emit(ShadowEvent{Type: "context_prefetched", Payload: ctx})
}

// GetPrefetchedContexts returns available prefetched contexts.
func (cp *ContextPrefetcher) GetPrefetchedContexts() []PrefetchedContext {
	cp.svc.mu.Lock()
	defer cp.svc.mu.Unlock()
	result := make([]PrefetchedContext, len(cp.prefetchedCtx))
	copy(result, cp.prefetchedCtx)
	return result
}

// detectTopic identifies the topic of a message based on keyword matching.
func detectTopic(content string) string {
	lower := strings.ToLower(content)
	bestTopic := ""
	bestScore := 0

	for topic := range topicKeywords {
		score := 0
		if strings.Contains(lower, strings.ToLower(topic)) {
			score = len(topic) // longer match = higher score
		}
		if score > bestScore {
			bestScore = score
			bestTopic = topic
		}
	}
	return bestTopic
}
