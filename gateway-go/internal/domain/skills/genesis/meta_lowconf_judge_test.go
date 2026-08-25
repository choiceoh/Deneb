package genesis

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// The judge decides only ties, and every way it can fail must land on the
// operator card rather than on an adoption — the fallback IS the old behavior.
func TestJudgeLowConfidenceFallsBackOnEveryFailure(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	cases := []struct {
		name  string
		judge func(context.Context, LowConfidenceCase) (LowConfidenceVerdict, error)
	}{{
		name:  "no judge wired",
		judge: nil,
	}, {
		name: "judge errors",
		judge: func(context.Context, LowConfidenceCase) (LowConfidenceVerdict, error) {
			return LowConfidenceVerdict{Adopt: true, Rationale: "채택"}, errors.New("boom")
		},
	}, {
		// A verdict with no reasoning cannot be audited later, and this is
		// precisely the decision that will need auditing.
		name: "adopt with no rationale",
		judge: func(context.Context, LowConfidenceCase) (LowConfidenceVerdict, error) {
			return LowConfidenceVerdict{Adopt: true, Rationale: "   "}, nil
		},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := &MetaEvolutionTask{LowConfidenceJudge: tc.judge}
			_, judged := task.judgeLowConfidence(context.Background(), logger,
				"evolve-system-prompt.md", "producer", "현행", "개정안", "margin 67→67", "이유")
			if judged {
				t.Fatalf("judged = true, want false (must fall back to operator)")
			}
		})
	}
}

func TestJudgeLowConfidenceReturnsBothVerdicts(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	for _, want := range []bool{true, false} {
		task := &MetaEvolutionTask{
			LowConfidenceJudge: func(_ context.Context, in LowConfidenceCase) (LowConfidenceVerdict, error) {
				if in.Artifact == "" || in.Margin == "" {
					t.Errorf("judge saw an incomplete case: %+v", in)
				}
				return LowConfidenceVerdict{Adopt: want, Rationale: "근거"}, nil
			},
		}
		got, judged := task.judgeLowConfidence(context.Background(), logger,
			"evolve-system-prompt.md", "producer", "현행", "개정안", "margin 67→67", "이유")
		if !judged {
			t.Fatalf("judged = false, want true")
		}
		if got.Adopt != want {
			t.Fatalf("adopt = %v, want %v", got.Adopt, want)
		}
	}
}

// The judge must not exist without a model — a nil client silently disabling
// the routing would look identical to "no ties occurred".
func TestNewLowConfidenceJudgeRequiresClientAndModel(t *testing.T) {
	if NewLowConfidenceJudge(nil, "some-model", nil) != nil {
		t.Error("nil client must yield no judge")
	}
	if NewLowConfidenceJudge(nil, "", nil) != nil {
		t.Error("empty model must yield no judge")
	}
}
