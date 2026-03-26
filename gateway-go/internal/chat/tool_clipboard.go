package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// clipEntry is a single clipboard item.
type clipEntry struct {
	Content   string `json:"content"`
	Label     string `json:"label,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

// clipStore is a thread-safe ring buffer for clipboard items.
type clipStore struct {
	mu      sync.RWMutex
	entries []clipEntry
	maxSize int
}

var (
	globalClip     *clipStore
	globalClipOnce sync.Once
)

func getClipStore() *clipStore {
	globalClipOnce.Do(func() {
		globalClip = &clipStore{
			entries: make([]clipEntry, 0, 32),
			maxSize: 32,
		}
	})
	return globalClip
}

func (s *clipStore) set(content, label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := clipEntry{
		Content:   content,
		Label:     label,
		CreatedAt: time.Now().Unix(),
	}
	s.entries = append(s.entries, entry)
	// Trim oldest if over capacity.
	if len(s.entries) > s.maxSize {
		s.entries = s.entries[len(s.entries)-s.maxSize:]
	}
}

// get returns the most recent clipboard entry.
func (s *clipStore) get() (clipEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.entries) == 0 {
		return clipEntry{}, false
	}
	return s.entries[len(s.entries)-1], true
}

func (s *clipStore) list() []clipEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]clipEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

func (s *clipStore) clear() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.entries)
	s.entries = s.entries[:0]
	return n
}

// clipboardToolSchema returns the JSON Schema for the clipboard tool.
func clipboardToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action: set, get, list, clear",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Content to store (for set action)",
			},
			"label": map[string]any{
				"type":        "string",
				"description": "Label for the clip (optional, for set action)",
			},
		},
		"required": []string{"action"},
	}
}

// toolClipboard implements the clipboard tool for temporary content sharing.
func toolClipboard() ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action  string `json:"action"`
			Content string `json:"content"`
			Label   string `json:"label"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid clipboard params: %w", err)
		}

		store := getClipStore()

		switch p.Action {
		case "set":
			if p.Content == "" {
				return "", fmt.Errorf("content is required for set")
			}
			store.set(p.Content, p.Label)
			label := p.Label
			if label == "" {
				label = "(unlabeled)"
			}
			return fmt.Sprintf("Clipboard set: %s (%d chars)", label, len(p.Content)), nil

		case "get":
			entry, ok := store.get()
			if !ok {
				return "Clipboard is empty.", nil
			}
			if entry.Label != "" {
				return fmt.Sprintf("[%s]\n%s", entry.Label, entry.Content), nil
			}
			return entry.Content, nil

		case "list":
			entries := store.list()
			if len(entries) == 0 {
				return "Clipboard is empty.", nil
			}
			var sb strings.Builder
			// Show newest first.
			for i := len(entries) - 1; i >= 0; i-- {
				e := entries[i]
				label := e.Label
				if label == "" {
					label = "(unlabeled)"
				}
				preview := e.Content
				if len(preview) > 80 {
					preview = preview[:80] + "..."
				}
				fmt.Fprintf(&sb, "%d. [%s] %s\n", len(entries)-i, label, preview)
			}
			return sb.String(), nil

		case "clear":
			n := store.clear()
			return fmt.Sprintf("Clipboard cleared (%d items removed).", n), nil

		default:
			return fmt.Sprintf("Unknown clipboard action: %q. Supported: set, get, list, clear.", p.Action), nil
		}
	}
}
