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

// waitForReactionCount polls until reactions has at least n entries or the
// timeout elapses. Returns a snapshot of reactions and whether the target
// count was reached. Polling avoids the flakiness of fixed time.Sleep gaps
// under heavy load (especially with -race), where a 10ms debounce timer
// can slip past a 30ms wait window and cause intermediate states to be
// missed or reordered.
func waitForReactionCount(mu *sync.Mutex, reactions *[]string, n int, timeout time.Duration) ([]string, bool) {
	deadline := time.Now().Add(timeout)
	for {
		mu.Lock()
		got := append([]string(nil), (*reactions)...)
		mu.Unlock()
		if len(got) >= n {
			return got, true
		}
		if time.Now().After(deadline) {
			return got, false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestStatusReactionController_PreparingRecallingProgression verifies the
// new prep-phase emoji sequence (Queued → Preparing → Recalling → Thinking)
// transitions through every state. This is the visibility win the prep
// phase emits exist for: the user must see distinct emojis for "loading
// context" and "searching memory", not just 👀 frozen for two seconds.
func TestStatusReactionController_PreparingRecallingProgression(t *testing.T) {
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

	// Each setter is allowed to flush before the next is called — otherwise
	// the debounce timer would replace the pending emoji and we'd see only
	// the last transition. Polling tolerates scheduler jitter that breaks
	// fixed-sleep approaches.
	steps := []struct {
		name string
		fire func()
	}{
		{"Queued", c.SetQueued},
		{"Preparing", c.SetPreparing},
		{"Recalling", c.SetRecalling},
		{"Thinking", c.SetThinking},
	}
	for i, step := range steps {
		step.fire()
		if _, ok := waitForReactionCount(&mu, &reactions, i+1, time.Second); !ok {
			mu.Lock()
			got := append([]string(nil), reactions...)
			mu.Unlock()
			t.Fatalf("after %s: expected %d reactions, got %d (%v)", step.name, i+1, len(got), got)
		}
	}

	emojis := DefaultStatusEmojis()
	mu.Lock()
	got := append([]string(nil), reactions...)
	mu.Unlock()
	want := []string{emojis.Queued, emojis.Preparing, emojis.Recalling, emojis.Thinking}
	if len(got) != len(want) {
		t.Fatalf("reaction count = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("reactions[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestStatusReactionController_PreparingIsNotTerminal verifies that
// SetPreparing/SetRecalling are non-terminal: subsequent SetThinking/SetDone
// must still apply. Without this, an over-eager debounce or finished flag
// would freeze the controller on the prep emoji and the user would never
// see done/error.
func TestStatusReactionController_PreparingIsNotTerminal(t *testing.T) {
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

	c.SetPreparing()
	if _, ok := waitForReactionCount(&mu, &reactions, 1, time.Second); !ok {
		t.Fatal("expected at least one reaction after SetPreparing")
	}
	c.SetDone()
	got, ok := waitForReactionCount(&mu, &reactions, 2, time.Second)
	if !ok {
		t.Fatalf("expected at least two reactions after SetDone, got %d (%v)", len(got), got)
	}

	emojis := DefaultStatusEmojis()
	last := got[len(got)-1]
	if last != emojis.Done {
		t.Errorf("last reaction = %q, want %q (done after preparing)", last, emojis.Done)
	}
}
