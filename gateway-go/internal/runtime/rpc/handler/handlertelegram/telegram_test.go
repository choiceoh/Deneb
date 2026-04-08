package handlertelegram

import (
	"testing"
)

func TestLifecycleMethods_nilDeps(t *testing.T) {
	m := LifecycleMethods(LifecycleDeps{})
	if m != nil {
		t.Fatal("expected nil for nil TelegramPlugin")
	}
}
