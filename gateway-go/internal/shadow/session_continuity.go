package shadow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SessionContinuity saves conversation context snapshots so sessions can
// resume seamlessly after gateway restarts.
type SessionContinuity struct {
	svc *Service

	// State (guarded by svc.mu).
	lastSnapshot    *ContinuitySnapshot
	lastSnapshotAt  int64 // unix ms
	recentMessages  []continuityMessage
}

// ContinuitySnapshot captures the state needed to resume a session.
type ContinuitySnapshot struct {
	SessionKey      string   `json:"sessionKey"`
	Topic           string   `json:"topic"`           // current conversation topic
	RecentSummary   string   `json:"recentSummary"`   // brief summary of recent conversation
	PendingTasks    []string `json:"pendingTasks"`     // task descriptions still pending
	LastUserMessage string   `json:"lastUserMessage"`  // last user message for context
	SavedAt         int64    `json:"savedAt"`          // unix ms
}

type continuityMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Ts      int64  `json:"ts"`
}

func newSessionContinuity(svc *Service) *SessionContinuity {
	return &SessionContinuity{svc: svc}
}

// OnMessage records messages for continuity snapshots.
func (sc *SessionContinuity) OnMessage(msg json.RawMessage) {
	var parsed struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(msg, &parsed); err != nil {
		return
	}
	if parsed.Content == "" {
		return
	}

	sc.svc.mu.Lock()
	sc.recentMessages = append(sc.recentMessages, continuityMessage{
		Role:    parsed.Role,
		Content: parsed.Content,
		Ts:      time.Now().UnixMilli(),
	})
	// Keep last 20 messages.
	if len(sc.recentMessages) > 20 {
		sc.recentMessages = sc.recentMessages[len(sc.recentMessages)-20:]
	}
	sc.svc.mu.Unlock()

	// Auto-save snapshot every 5 minutes.
	sc.svc.mu.Lock()
	shouldSave := time.Now().UnixMilli()-sc.lastSnapshotAt > 5*60*1000
	sc.svc.mu.Unlock()
	if shouldSave {
		sc.SaveSnapshot()
	}
}

// SaveSnapshot persists the current continuity state to disk.
func (sc *SessionContinuity) SaveSnapshot() {
	sc.svc.mu.Lock()
	msgs := make([]continuityMessage, len(sc.recentMessages))
	copy(msgs, sc.recentMessages)
	tasks := make([]TrackedTask, len(sc.svc.pendingTasks))
	copy(tasks, sc.svc.pendingTasks)
	topic := ""
	if sc.svc.contextPrefetcher != nil {
		topic = sc.svc.contextPrefetcher.currentTopic
	}
	sc.svc.mu.Unlock()

	// Build summary from recent messages.
	var summary strings.Builder
	var lastUserMsg string
	for _, m := range msgs {
		if m.Role == "user" {
			lastUserMsg = truncate(m.Content, 200)
		}
		summary.WriteString(fmt.Sprintf("[%s] %s\n", m.Role, truncate(m.Content, 100)))
	}

	var pendingDescs []string
	for _, t := range tasks {
		if t.Status == "pending" {
			pendingDescs = append(pendingDescs, truncate(t.Content, 80))
		}
	}

	snapshot := &ContinuitySnapshot{
		SessionKey:      sc.svc.cfg.MainSessionKey,
		Topic:           topic,
		RecentSummary:   truncate(summary.String(), 2000),
		PendingTasks:    pendingDescs,
		LastUserMessage: lastUserMsg,
		SavedAt:         time.Now().UnixMilli(),
	}

	// Persist to disk.
	if err := sc.persistSnapshot(snapshot); err != nil {
		sc.svc.cfg.Logger.Warn("shadow: failed to save continuity snapshot", "error", err)
		return
	}

	sc.svc.mu.Lock()
	sc.lastSnapshot = snapshot
	sc.lastSnapshotAt = time.Now().UnixMilli()
	sc.svc.mu.Unlock()

	sc.svc.cfg.Logger.Debug("shadow: continuity snapshot saved")
}

// LoadSnapshot reads the last saved continuity snapshot from disk.
func (sc *SessionContinuity) LoadSnapshot() *ContinuitySnapshot {
	path := sc.snapshotPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var snapshot ContinuitySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil
	}
	return &snapshot
}

// GetResumeSummary returns a Korean-language summary for injecting into a new session.
func (sc *SessionContinuity) GetResumeSummary() string {
	snapshot := sc.LoadSnapshot()
	if snapshot == nil {
		return ""
	}

	// Only use snapshots less than 24 hours old.
	if time.Now().UnixMilli()-snapshot.SavedAt > 24*60*60*1000 {
		return ""
	}

	var parts []string
	if snapshot.Topic != "" {
		parts = append(parts, fmt.Sprintf("마지막 주제: %s", snapshot.Topic))
	}
	if snapshot.LastUserMessage != "" {
		parts = append(parts, fmt.Sprintf("마지막 요청: %s", snapshot.LastUserMessage))
	}
	if len(snapshot.PendingTasks) > 0 {
		parts = append(parts, fmt.Sprintf("대기 중 작업 %d건:", len(snapshot.PendingTasks)))
		for i, t := range snapshot.PendingTasks {
			if i >= 3 {
				parts = append(parts, fmt.Sprintf("  ... 외 %d건", len(snapshot.PendingTasks)-3))
				break
			}
			parts = append(parts, fmt.Sprintf("  %d. %s", i+1, t))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return "이전 세션 컨텍스트:\n" + strings.Join(parts, "\n")
}

func (sc *SessionContinuity) persistSnapshot(snapshot *ContinuitySnapshot) error {
	path := sc.snapshotPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (sc *SessionContinuity) snapshotPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".deneb", "shadow", "continuity.json")
}
