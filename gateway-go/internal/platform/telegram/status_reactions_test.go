package telegram

import (
	"sync"
	"testing"
	"time"
)

func TestResolveToolEmoji(t *testing.T) {
	emojis := DefaultStatusEmojis()
	tests := []struct {
		name     string
		toolName string
		want     string
	}{
		{"empty", "", emojis.Tool},
		{"web", "web", emojis.Web},
		{"web_search_legacy", "web_search", emojis.Web},
		{"web_fetch_legacy", "web_fetch", emojis.Web},
		{"browser", "browser", emojis.Web},
		{"exec", "exec", emojis.Coding},
		{"read", "read", emojis.Coding},
		{"write", "write", emojis.Coding},
		{"edit", "edit", emojis.Coding},
		{"bash", "bash", emojis.Coding},
		{"process", "process", emojis.Coding},
		{"unknown", "kv", emojis.Tool},
		{"case insensitive", "WEB_SEARCH", emojis.Web},
		{"whitespace", "  exec  ", emojis.Coding},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveToolEmoji(tt.toolName, emojis)
			if got != tt.want {
				t.Errorf("ResolveToolEmoji(%q) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestStatusReactionController_BasicFlow(t *testing.T) {
	var mu sync.Mutex
	var reactions []string

	c := NewStatusReactionController(StatusReactionControllerParams{
		Enabled:      true,
		InitialEmoji: "👀",
		SetReaction: func(emoji string) error {
			mu.Lock()
			reactions = append(reactions, emoji)
			mu.Unlock()
			return nil
		},
		Timing: &StatusReactionTiming{DebounceMs: 10, StallSoftMs: 100_000, StallHardMs: 200_000},
	})
	defer c.Close()

	c.SetQueued()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(reactions) == 0 {
		mu.Unlock()
		t.Fatal("expected at least one reaction after SetQueued")
	}
	if reactions[0] != "👀" {
		t.Errorf("first reaction = %q, want 👀", reactions[0])
	}
	mu.Unlock()
}

func TestStatusReactionController_DoneIsTerminal(t *testing.T) {
	var mu sync.Mutex
	var reactions []string

	c := NewStatusReactionController(StatusReactionControllerParams{
		Enabled:      true,
		InitialEmoji: "👀",
		SetReaction: func(emoji string) error {
			mu.Lock()
			reactions = append(reactions, emoji)
			mu.Unlock()
			return nil
		},
		Timing: &StatusReactionTiming{DebounceMs: 10, StallSoftMs: 100_000, StallHardMs: 200_000},
	})
	defer c.Close()

	c.SetDone()
	time.Sleep(50 * time.Millisecond)

	// After done, further updates should be ignored.
	c.SetThinking()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	last := reactions[len(reactions)-1]
	mu.Unlock()

	emojis := DefaultStatusEmojis()
	if last != emojis.Done {
		t.Errorf("last reaction = %q, want %q (done)", last, emojis.Done)
	}
}

// TestStatusReactionController_ClearIsTerminal verifies that SetClear
// removes any reaction (sets ""), is terminal (subsequent updates are
// ignored), and can be used as a graceful "this run was superseded"
// finisher — distinct from SetDone (👍) and SetError (😱).
func TestStatusReactionController_ClearIsTerminal(t *testing.T) {
	var mu sync.Mutex
	var reactions []string

	c := NewStatusReactionController(StatusReactionControllerParams{
		Enabled:      true,
		InitialEmoji: "👀",
		SetReaction: func(emoji string) error {
			mu.Lock()
			reactions = append(reactions, emoji)
			mu.Unlock()
			return nil
		},
		Timing: &StatusReactionTiming{DebounceMs: 10, StallSoftMs: 100_000, StallHardMs: 200_000},
	})
	defer c.Close()

	c.SetClear()
	time.Sleep(50 * time.Millisecond)

	// After clear, further updates must be ignored (terminal state).
	c.SetThinking()
	c.SetTool("exec")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	last := reactions[len(reactions)-1]
	mu.Unlock()

	if last != "" {
		t.Errorf("last reaction = %q, want \"\" (cleared)", last)
	}
}
