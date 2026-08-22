package artifact

import (
	"strings"
	"testing"
)

// A chunk that answered "근거 없음" was read but supplied no evidence, so the
// reducer must not be able to cite into it. Counting its interval as citable
// let a fabricated [L<n>] inside that range pass as grounded.
func TestSpillAskRejectsReducedCitationIntoNoEvidenceChunk(t *testing.T) {
	store, ctx, id := spillWithLines(t, 4000) // several chunks
	var firstChunk bool
	rec := &askRecorder{reply: func(user string) (string, error) {
		if strings.Contains(user, "부분 답변") {
			// Cite a line inside the SECOND chunk — read, but it answered
			// "no evidence", so nothing there was ever evidenced.
			return "합쳐진 답 [L1000]", nil
		}
		if !firstChunk {
			firstChunk = true
			return stubAnswer(user, "구간 답"), nil // the only chunk with evidence
		}
		return "이 구간에는 근거 없음", nil
	}}
	fn := ToolSpilloverRead(store, rec.fn())

	out := callSpill(ctx, t, fn, map[string]any{"spill_id": id, "question": "무엇?"})

	if strings.Contains(out, "합쳐진 답") {
		t.Errorf("reducer cited into a chunk that supplied no evidence:\n%s", out)
	}
}
