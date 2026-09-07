package llm

import "testing"

// The live defect this guards (srv4, 2026-09-05..08): the session's thinking
// LEVEL sets a 4096-token budget while each call site picks MaxTokens for the
// ANSWER alone. phone-event asked for 1536 and got 93 empty replies out of 94
// truncated turns — the model reasoned to the cap and never started writing.
func TestReconcileThinkingBudgetFitsOrDisables(t *testing.T) {
	cases := []struct {
		name       string
		maxTokens  int
		budget     int
		wantType   string
		wantBudget int
		wantNote   bool
	}{
		{"phone-event 1536 vs level-low 4096", 1536, 4096, "enabled", 1024, true},
		{"genesis 2048", 2048, 4096, "enabled", 1536, true},
		{"card titler 256 — too small to think at all", 256, 4096, "disabled", 0, true},
		{"exactly at the reserve boundary", 4608, 4096, "enabled", 4096, false},
		{"one token short of fitting", 4607, 4096, "enabled", 4095, true},
		{"roomy budget untouched", 16000, 4096, "enabled", 4096, false},
		{"no MaxTokens — nothing to reconcile", 0, 4096, "enabled", 4096, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := ChatRequest{
				Model:     "glm-5.3-flash",
				MaxTokens: tc.maxTokens,
				Thinking:  &ThinkingConfig{Type: "enabled", BudgetTokens: tc.budget},
			}
			note := req.ReconcileThinkingBudget()
			if (note != "") != tc.wantNote {
				t.Errorf("note = %q, wantNote = %v", note, tc.wantNote)
			}
			if req.Thinking.Type != tc.wantType {
				t.Fatalf("type = %q, want %q", req.Thinking.Type, tc.wantType)
			}
			if tc.wantType == "enabled" && req.Thinking.BudgetTokens != tc.wantBudget {
				t.Errorf("budget = %d, want %d", req.Thinking.BudgetTokens, tc.wantBudget)
			}
			// Whatever it decides, an enabled budget must leave room for an answer.
			if req.Thinking.Type == "enabled" && tc.maxTokens > 0 &&
				req.Thinking.BudgetTokens+answerReserveTokens > tc.maxTokens {
				t.Errorf("budget %d still crowds MaxTokens %d", req.Thinking.BudgetTokens, tc.maxTokens)
			}
		})
	}
}

func TestReconcileThinkingBudgetLeavesOtherShapesAlone(t *testing.T) {
	// disabled / adaptive / absent configs carry no budget to reconcile, and a
	// caller that never set MaxTokens keeps provider defaults.
	for _, th := range []*ThinkingConfig{nil, {Type: "disabled"}, {Type: "adaptive"}, {Type: "enabled"}} {
		req := ChatRequest{MaxTokens: 100, Thinking: th}
		if note := req.ReconcileThinkingBudget(); note != "" {
			t.Errorf("thinking %+v should not be reconciled, got %q", th, note)
		}
	}
}

// The shrink must not mutate a ThinkingConfig the caller still holds — roles
// share one config value across calls.
func TestReconcileThinkingBudgetDoesNotMutateSharedConfig(t *testing.T) {
	shared := &ThinkingConfig{Type: "enabled", BudgetTokens: 4096}
	req := ChatRequest{MaxTokens: 2048, Thinking: shared}
	req.ReconcileThinkingBudget()
	if shared.BudgetTokens != 4096 {
		t.Errorf("caller's config was mutated: budget = %d, want 4096", shared.BudgetTokens)
	}
}
