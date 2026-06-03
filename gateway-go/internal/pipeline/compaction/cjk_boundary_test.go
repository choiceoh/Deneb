package compaction

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// TestTruncateOldToolResults_CJKRuneBoundary guards the rune (not byte)
// threshold of TruncateOldToolResults for Korean text. A completed Hangul
// syllable is one rune but three UTF-8 bytes, so a byte-based length check
// would stub content at a third of the intended size. Deneb is Korean-first,
// so an off-by-3x truncation would be a user-visible reliability bug.
//
// The function keeps content with <= minChars runes and stubs content with
// > minChars runes; this test pins both sides of that boundary using Hangul.
func TestTruncateOldToolResults_CJKRuneBoundary(t *testing.T) {
	const minChars = DefaultStubMinChars // 256

	// build wraps a tool_result with the given content in a sequence that has
	// enough assistant turns (2 > turnThreshold=1) for the result to sit before
	// the truncation cutoff.
	build := func(content string) []llm.Message {
		return []llm.Message{
			userToolResultMsg(t, "t1", content),
			llm.NewTextMessage("assistant", "turn one"),
			llm.NewTextMessage("assistant", "turn two"),
		}
	}

	hangul := func(n int) string { return strings.Repeat("가", n) } // 1 rune, 3 bytes each

	// Exactly minChars runes (= 3*minChars bytes) must be KEPT (<= minChars).
	// A byte-based check would see 768 bytes and wrongly stub this.
	atBoundary := build(hangul(minChars))
	if _, stubbed := TruncateOldToolResults(atBoundary, 1, minChars); stubbed != 0 {
		t.Errorf("exactly %d Hangul runes (=%d bytes) should be kept, but stubbed=%d (byte-based check?)",
			minChars, minChars*3, stubbed)
	}

	// One rune over the threshold must be STUBBED.
	over := build(hangul(minChars + 1))
	out, stubbed := TruncateOldToolResults(over, 1, minChars)
	if stubbed != 1 {
		t.Fatalf("%d Hangul runes should be stubbed, stubbed=%d", minChars+1, stubbed)
	}
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(out[0].Content, &blocks); err != nil {
		t.Fatalf("unmarshal stubbed message: %v", err)
	}
	if !strings.Contains(blocks[0].Content, "cleared to save context") {
		t.Errorf("over-threshold content should be replaced by the placeholder, got %q", blocks[0].Content)
	}

	// Mixed Hangul + ASCII at the exact boundary is also kept: rune counting
	// must treat each Hangul syllable and each ASCII char as one rune.
	mixed := build(hangul(minChars-10) + strings.Repeat("a", 10))
	if _, stubbed := TruncateOldToolResults(mixed, 1, minChars); stubbed != 0 {
		t.Errorf("mixed content of exactly %d runes should be kept, stubbed=%d", minChars, stubbed)
	}
}
