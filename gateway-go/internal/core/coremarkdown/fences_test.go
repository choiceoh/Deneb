package coremarkdown

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestDetectFencesParsesSingleBacktickSpan(t *testing.T) {
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

func TestDetectFencesParsesTildeMarker(t *testing.T) {
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

func TestDetectFencesPreservesIndentPrefix(t *testing.T) {
	spans := DetectFences("   ```\ncode\n   ```")
	if len(spans) != 1 {
		t.Fatalf("got %d, want 1 span", len(spans))
	}
	if spans[0].Indent != "   " {
		t.Errorf("indent = %q", spans[0].Indent)
	}
}

func TestDetectFencesRejectsFourSpaceIndent(t *testing.T) {
	spans := DetectFences("    ```python\n    code\n    ```")
	if len(spans) != 0 {
		t.Errorf("4-space indent should not match, got %d spans", len(spans))
	}
}

func TestDetectFencesParsesMultipleSeparateSpans(t *testing.T) {
	spans := DetectFences("```\na\n```\n\n```\nb\n```")
	if len(spans) != 2 {
		t.Errorf("got %d, want 2 spans", len(spans))
	}
}

func TestDetectFencesIgnoresMismatchedCloserChar(t *testing.T) {
	// Open with ``` but try to close with ~~~ — should not close.
	spans := DetectFences("```\ncode\n~~~\nmore\n```")
	if len(spans) != 1 {
		t.Fatalf("got %d, want 1 span", len(spans))
	}
	if spans[0].Marker != "```" {
		t.Errorf("marker = %q", spans[0].Marker)
	}
}

func TestDetectFencesIgnoresShortClosingMarkerCount(t *testing.T) {
	// Open with ```` (4) — closing ``` (3) should not close.
	spans := DetectFences("````\ncode\n```\nstill open\n````")
	if len(spans) != 1 {
		t.Fatalf("got %d, want 1 span", len(spans))
	}
}

func TestDetectFencesEncodesSpansWithRustCompatibleFieldNames(t *testing.T) {
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

func TestMatchFenceLineParsesBacktickComponents(t *testing.T) {
	indent, marker, rest := matchFenceLine("```python")
	if indent != "" || marker != "```" || rest != "python" {
		t.Errorf("got indent=%q marker=%q rest=%q", indent, marker, rest)
	}
}

func TestMatchFenceLineParsesIndentedComponents(t *testing.T) {
	indent, marker, rest := matchFenceLine("  ~~~")
	if indent != "  " || marker != "~~~" || rest != "" {
		t.Errorf("got indent=%q marker=%q rest=%q", indent, marker, rest)
	}
}
