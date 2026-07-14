package chat

import (
	"strings"
	"testing"
)

func TestDeferredSubagentNotificationsReturnsEmptyAfterDrain(t *testing.T) {
	ch := make(chan string, 2)
	ch <- "child A done"
	ch <- "child B done"

	fn := deferredSubagentNotifications(ch)
	result := fn()

	// Should drain both notifications.
	if !strings.Contains(result, "child A done") || !strings.Contains(result, "child B done") {
		t.Errorf("should contain both notifications, got %q", result)
	}

	// Channel empty — should return empty.
	result = fn()
	if result != "" {
		t.Errorf("got %q, want empty when channel drained", result)
	}
}
