package compaction

import (
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
// Shares the stubPlaceholder constant and firstToolResultContent helper with
// truncate_old_tool_results_test.go.
func TestTruncateOldToolResults_CJKRuneBoundary(t *testing.T) {
	const minChars = DefaultStubMinChars // 256

	// build wraps a tool_result with the given content in a sequence that has
	// enough assistant turns (2 > turnThreshold=1) for the result to sit before
	// the truncation cutoff.
	build := func(content string) []llm.Message {
		return []llm.Message{
			userToolResultMsg(t, "t1", content),
			assistantMsg(t, "a1"),
			assistantMsg(t, "a2"),
		}
	}

	hangul := func(n int) string { return strings.Repeat("가", n) } // 1 rune, 3 bytes each

	// Exactly minChars runes (= 3*minChars bytes) must be KEPT (<= minChars).
	// A byte-based check would see 768 bytes and wrongly stub this.
	if _, stubbed := TruncateOldToolResults(build(hangul(minChars)), 1, minChars); stubbed != 0 {
		t.Errorf("exactly %d Hangul runes (=%d bytes) should be kept, but stubbed=%d (byte-based check?)",
			minChars, minChars*3, stubbed)
	}

	// One rune over the threshold must be STUBBED with the placeholder.
	out, stubbed := TruncateOldToolResults(build(hangul(minChars+1)), 1, minChars)
	if stubbed != 1 {
		t.Fatalf("%d Hangul runes should be stubbed, stubbed=%d", minChars+1, stubbed)
	}
	if got := firstToolResultContent(t, out[0].Content); got != stubPlaceholder {
		t.Errorf("over-threshold content = %q, want stub placeholder %q", got, stubPlaceholder)
	}

	// Mixed Hangul + ASCII at the exact boundary is also kept: rune counting
	// must treat each Hangul syllable and each ASCII char as one rune.
	mixed := hangul(minChars-10) + strings.Repeat("a", 10)
	if _, stubbed := TruncateOldToolResults(build(mixed), 1, minChars); stubbed != 0 {
		t.Errorf("mixed content of exactly %d runes should be kept, stubbed=%d", minChars, stubbed)
	}
}
