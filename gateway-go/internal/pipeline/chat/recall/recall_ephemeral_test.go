package recall

import "testing"

// The gate reads two independent things: whether the turn persists
// (EphemeralUser) and whether the user asked for memory off (SkipRecall). Only
// the second is the user's own choice, so only it wins unconditionally.
func TestRecallSuppressedSeparatesPersistenceFromRecall(t *testing.T) {
	tests := []struct {
		name   string
		params Params
		want   bool
	}{{
		name:   "ordinary turn runs",
		params: Params{},
		want:   false,
	}, {
		name:   "ephemeral turn is suppressed by default",
		params: Params{EphemeralUser: true},
		want:   true,
	}, {
		name:   "ephemeral turn that declares a subject runs",
		params: Params{EphemeralUser: true, AllowRecall: true},
		want:   false,
	}, {
		name:   "the user's memory-off toggle beats AllowRecall",
		params: Params{EphemeralUser: true, AllowRecall: true, SkipRecall: true},
		want:   true,
	}, {
		name:   "the user's memory-off toggle beats a normal turn",
		params: Params{SkipRecall: true},
		want:   true,
	}, {
		name:   "AllowRecall alone changes nothing for a persisted turn",
		params: Params{AllowRecall: true},
		want:   false,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.params.recallSuppressed(); got != tt.want {
				t.Fatalf("recallSuppressed() = %v, want %v", got, tt.want)
			}
		})
	}
}
