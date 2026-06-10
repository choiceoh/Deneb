package chat

import "testing"

// skipCompactionBudget guards assembleMessages against history-suppression
// sentinel budgets. Production symptom before the guard: every skill-review
// fork run logged "polaris: compaction failed to reduce below budget
// tokensBefore=5390 tokensAfter=5390 budget=1" after burning every compaction
// tier on a budget the protected current turn alone exceeds.
func TestSkipCompactionBudget(t *testing.T) {
	cases := []struct {
		name   string
		budget int
		want   bool
	}{
		{"zero keeps legacy no-budget behavior", 0, false},
		{"negative treated as unset", -1, false},
		{"review-fork sentinel (MaxHistoryTokens=1)", 1, true},
		{"just below floor", minCompactionBudget - 1, true},
		{"at floor", minCompactionBudget, false},
		{"boot budget", 30_000, false},
		{"default-scale budget", 140_000, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := skipCompactionBudget(tc.budget); got != tc.want {
				t.Fatalf("skipCompactionBudget(%d) = %v, want %v", tc.budget, got, tc.want)
			}
		})
	}
}
