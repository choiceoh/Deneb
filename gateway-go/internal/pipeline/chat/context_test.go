package chat

import "testing"

// DENEB_MEMORY_TOKEN_BUDGET lets a latency-sensitive deployment shrink the
// context budget (decode speed on the local bandwidth-bound serve degrades
// sharply with input size). Invalid or headroom-less values must fall back to
// the default rather than underflow effectiveContextBudget's memory-minus-
// system arithmetic.
func TestDefaultContextConfigBudgetOverride(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want uint64
	}{
		{"unset keeps default", "", defaultMemoryTokenBudget},
		{"valid override applies", "60000", 60_000},
		{"non-numeric ignored", "fast", defaultMemoryTokenBudget},
		{"negative ignored", "-1", defaultMemoryTokenBudget},
		{"below system+headroom ignored", "30001", defaultMemoryTokenBudget},
		{"exactly system+headroom applies", "34096", 34_096},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DENEB_MEMORY_TOKEN_BUDGET", tc.env)
			cfg := DefaultContextConfig()
			if cfg.MemoryTokenBudget != tc.want {
				t.Errorf("MemoryTokenBudget = %d, want %d", cfg.MemoryTokenBudget, tc.want)
			}
			if cfg.SystemPromptBudget != defaultSystemPromptBudget {
				t.Errorf("SystemPromptBudget changed: %d", cfg.SystemPromptBudget)
			}
		})
	}
}
