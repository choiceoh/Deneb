package chat

import (
	"slices"
	"testing"
)

func TestDeferredSubagentNotificationsReturnsEmptyAfterDrain(t *testing.T) {
	ch := make(chan string, 2)
	ch <- "child A done"
	ch <- "child B done"

	fn := deferredSubagentNotifications(ch)
	result := fn()

	// Should drain both notifications, one entry each (they become separate
	// text blocks on the tool-results user message).
	if want := []string{"child A done", "child B done"}; !slices.Equal(result, want) {
		t.Errorf("drained notices = %v, want %v", result, want)
	}

	// Channel empty — should return no notices.
	if result = fn(); len(result) != 0 {
		t.Errorf("got %v, want none when channel drained", result)
	}
}

func TestDeferredSubagentNotificationsNilChannel(t *testing.T) {
	if fn := deferredSubagentNotifications(nil); fn != nil {
		t.Fatal("nil channel must return nil (executor skips the hook)")
	}
}
