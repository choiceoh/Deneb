package coremarkdown

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestDetectFences_Simple(t *testing.T) {
	spans := DetectFences("hello\n```js\ncode\n```\nend")
	if len(spans) != 1 {
		t.Fatalf("got %d, want 1 span", len(spans))
	}
	if spans[0].OpenLine != "```js" {
		t.Errorf("openLine = %q", spans[0].OpenLine)
	}
	if spans[0].Marker != "```" {
		t.Errorf("marker = %q", spans[0].Marker)
	}
}

func TestDetectFences_Tilde(t *testing.T) {
	spans := DetectFences("~~~\ncode\n~~~")
	if len(spans) != 1 {
		t.Fatalf("got %d, want 1 span", len(spans))
	}
	if spans[0].Marker != "~~~" {
		t.Errorf("marker = %q", spans[0].Marker)
	}
}

func TestDetectFences_Unclosed(t *testing.T) {
	input := "```\ncode\nno close"
	spans := DetectFences(input)
	if len(spans) != 1 {
		t.Fatalf("got %d, want 1 span", len(spans))
	}
	if spans[0].End != len(input) {
		t.Errorf("unclosed fence end=%d, want %d", spans[0].End, len(input))
	}
}

func TestDetectFences_Indented(t *testing.T) {
	spans := DetectFences("   ```\ncode\n   ```")
	if len(spans) != 1 {
		t.Fatalf("got %d, want 1 span", len(spans))
	}
	if spans[0].Indent != "   " {
		t.Errorf("indent = %q", spans[0].Indent)
	}
}

func TestDetectFences_TooMuchIndent(t *testing.T) {
	spans := DetectFences("    ```python\n    code\n    ```")
	if len(spans) != 0 {
		t.Errorf("4-space indent should not match, got %d spans", len(spans))
	}
}

func TestDetectFences_NoFences(t *testing.T) {
	spans := DetectFences("just text\nno fences here")
	if len(spans) != 0 {
		t.Errorf("got %d, want 0 spans", len(spans))
	}
}

func TestDetectFences_MultipleFences(t *testing.T) {
	spans := DetectFences("```\na\n```\n\n```\nb\n```")
	if len(spans) != 2 {
		t.Errorf("got %d, want 2 spans", len(spans))
	}
}

func TestDetectFences_ClosingNeedsSameChar(t *testing.T) {
	// Open with ``` but try to close with ~~~ — should not close.
	spans := DetectFences("```\ncode\n~~~\nmore\n```")
	if len(spans) != 1 {
		t.Fatalf("got %d, want 1 span", len(spans))
	}
	if spans[0].Marker != "```" {
		t.Errorf("marker = %q", spans[0].Marker)
	}
}

func TestDetectFences_ClosingNeedsEnoughMarkers(t *testing.T) {
	// Open with ```` (4) — closing ``` (3) should not close.
	spans := DetectFences("````\ncode\n```\nstill open\n````")
	if len(spans) != 1 {
		t.Fatalf("got %d, want 1 span", len(spans))
	}
}

func TestDetectFences_Empty(t *testing.T) {
	spans := DetectFences("")
	if spans != nil {
		t.Errorf("got %v, want nil", spans)
	}
}

func TestDetectFences_JSON(t *testing.T) {
	spans := DetectFences("```python\nprint('hi')\n```")
	data := testutil.Must(json.Marshal(spans))
	// Verify JSON field names match Rust output.
	var parsed []map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 {
		t.Fatalf("got %d, want 1", len(parsed))
	}
	fields := parsed[0]
	for _, key := range []string{"start", "end", "openLine", "marker", "indent"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("missing JSON field %q", key)
		}
	}
}

// ---------------------------------------------------------------------------
// matchFenceLine unit tests
// ---------------------------------------------------------------------------

func TestMatchFenceLine_Backtick(t *testing.T) {
	indent, marker, rest := matchFenceLine("```python")
	if indent != "" || marker != "```" || rest != "python" {
		t.Errorf("got indent=%q marker=%q rest=%q", indent, marker, rest)
	}
}

func TestMatchFenceLine_Indented(t *testing.T) {
	indent, marker, rest := matchFenceLine("  ~~~")
	if indent != "  " || marker != "~~~" || rest != "" {
		t.Errorf("got indent=%q marker=%q rest=%q", indent, marker, rest)
	}
}

func TestMatchFenceLine_MaxIndent(t *testing.T) {
	indent, marker, _ := matchFenceLine("   ```")
	if indent != "   " || marker != "```" {
		t.Errorf("3-space indent: indent=%q marker=%q", indent, marker)
	}
}

func TestMatchFenceLine_TooMuchIndent(t *testing.T) {
	_, marker, _ := matchFenceLine("    ```")
	if marker != "" {
		t.Error("4+ spaces should not match")
	}
}

func TestMatchFenceLine_TooFewMarkers(t *testing.T) {
	_, marker, _ := matchFenceLine("``")
	if marker != "" {
		t.Error("2 backticks should not match")
	}
}

func TestMatchFenceLine_NotFence(t *testing.T) {
	_, marker, _ := matchFenceLine("hello world")
	if marker != "" {
		t.Error("plain text should not match")
	}
}

func TestMatchFenceLine_LongMarker(t *testing.T) {
	_, marker, _ := matchFenceLine("``````")
	if marker != "``````" {
		t.Errorf("got %q, want 6 backticks", marker)
	}
}

// ---------------------------------------------------------------------------
// Spoiler preprocessing unit tests
// ---------------------------------------------------------------------------

func TestPreprocessSpoilers_NoDelimiters(t *testing.T) {
	result := preprocessSpoilers("hello world")
	if result != "hello world" {
		t.Errorf("got %q, want passthrough", result)
	}
}

func TestPreprocessSpoilers_SingleDelimiter(t *testing.T) {
	result := preprocessSpoilers("hello || world")
	if result != "hello || world" {
		t.Errorf("got %q, want passthrough for single ||", result)
	}
}

func TestPreprocessSpoilers_BasicPair(t *testing.T) {
	result := preprocessSpoilers("||secret||")
	if !containsSentinel(result) {
		t.Error("expected sentinels in result")
	}
	if indexOf(result, "||") >= 0 {
		t.Error("original || should be replaced")
	}
}

func TestPreprocessSpoilers_Unicode(t *testing.T) {
	result := preprocessSpoilers("||안녕하세요||")
	if indexOf(result, "안녕하세요") < 0 {
		t.Error("Korean text should be preserved")
	}
	if !containsSentinel(result) {
		t.Error("expected sentinels")
	}
}

func TestPreprocessSpoilers_OddDelimiters(t *testing.T) {
	result := preprocessSpoilers("||a|| ||b")
	openCount := 0
	closeCount := 0
	remaining := result
	for {
		idx := indexOf(remaining, sentinelOpen)
		if idx < 0 {
			break
		}
		openCount++
		remaining = remaining[idx+len(sentinelOpen):]
	}
	remaining = result
	for {
		idx := indexOf(remaining, sentinelClose)
		if idx < 0 {
			break
		}
		closeCount++
		remaining = remaining[idx+len(sentinelClose):]
	}
	if openCount != 1 || closeCount != 1 {
		t.Errorf("got %d + %d, want 1 open + 1 close", openCount, closeCount)
	}
}
