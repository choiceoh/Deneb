package channel

import (
	"encoding/json"
	"testing"
)

func TestResolveThreadBindingIdleTimeoutMs(t *testing.T) {
	t.Run("defaults to 24 hours", func(t *testing.T) {
		got := ResolveThreadBindingIdleTimeoutMs(nil, nil)
		want := int64(24 * 60 * 60 * 1000)
		if got != want {
			t.Errorf("got %d, want %d", got, want)
		}
	})

	t.Run("channel hours override", func(t *testing.T) {
		h := 2.0
		got := ResolveThreadBindingIdleTimeoutMs(&h, nil)
		want := int64(2 * 60 * 60 * 1000)
		if got != want {
			t.Errorf("got %d, want %d", got, want)
		}
	})

	t.Run("session hours fallback", func(t *testing.T) {
		h := 8.0
		got := ResolveThreadBindingIdleTimeoutMs(nil, &h)
		want := int64(8 * 60 * 60 * 1000)
		if got != want {
			t.Errorf("got %d, want %d", got, want)
		}
	})

	t.Run("channel hours take priority", func(t *testing.T) {
		ch := 3.0
		sh := 12.0
		got := ResolveThreadBindingIdleTimeoutMs(&ch, &sh)
		want := int64(3 * 60 * 60 * 1000)
		if got != want {
			t.Errorf("got %d, want %d", got, want)
		}
	})
}

func TestResolveThreadBindingsEnabled(t *testing.T) {
	t.Run("default true", func(t *testing.T) {
		if !ResolveThreadBindingsEnabled(nil, nil) {
			t.Error("expected true by default")
		}
	})

	t.Run("channel override false", func(t *testing.T) {
		f := false
		if ResolveThreadBindingsEnabled(&f, nil) {
			t.Error("expected false when channel says false")
		}
	})

	t.Run("session fallback", func(t *testing.T) {
		f := false
		if ResolveThreadBindingsEnabled(nil, &f) {
			t.Error("expected false when session says false")
		}
	})
}

func TestResolveThreadBindingSpawnPolicy(t *testing.T) {
	t.Run("default for non-discord", func(t *testing.T) {
		policy := ResolveThreadBindingSpawnPolicy(nil, nil, "telegram", "default", SpawnKindSubagent)
		if !policy.Enabled {
			t.Error("expected enabled by default")
		}
		if !policy.SpawnEnabled {
			t.Error("expected spawn enabled for non-discord")
		}
	})

	t.Run("default for discord disables spawn", func(t *testing.T) {
		policy := ResolveThreadBindingSpawnPolicy(nil, nil, "discord", "default", SpawnKindSubagent)
		if !policy.Enabled {
			t.Error("expected enabled by default")
		}
		if policy.SpawnEnabled {
			t.Error("expected spawn disabled for discord by default")
		}
	})

	t.Run("config override for discord", func(t *testing.T) {
		channels := json.RawMessage(`{
			"discord": {
				"threadBindings": {
					"spawnSubagentSessions": true
				}
			}
		}`)
		policy := ResolveThreadBindingSpawnPolicy(channels, nil, "discord", "default", SpawnKindSubagent)
		if !policy.SpawnEnabled {
			t.Error("expected spawn enabled from config override")
		}
	})
}

func TestFormatThreadBindingDisabledError(t *testing.T) {
	msg := FormatThreadBindingDisabledError("discord", "default")
	if msg == "" {
		t.Error("expected non-empty error message")
	}

	msg = FormatThreadBindingDisabledError("telegram", "default")
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}
