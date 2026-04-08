package chunk

import (
	"strings"
	"testing"
)

func TestChunkText(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		if got := Text("", 100); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("text within limit", func(t *testing.T) {
		got := Text("hello", 100)
		if len(got) != 1 || got[0] != "hello" {
			t.Errorf("got %v, want single chunk", got)
		}
	})

	t.Run("text exceeding limit splits on whitespace", func(t *testing.T) {
		text := "hello world foo bar"
		got := Text(text, 12)
		if len(got) < 2 {
			t.Errorf("got %v, want multiple chunks", got)
		}
		for _, chunk := range got {
			if len(chunk) > 12 {
				t.Errorf("chunk %q exceeds limit 12", chunk)
			}
		}
	})

	t.Run("prefers newline breaks", func(t *testing.T) {
		text := "line one\nline two\nline three"
		got := Text(text, 18)
		if len(got) < 2 {
			t.Errorf("got %v, want multiple chunks", got)
		}
		// First chunk should break at a newline.
		if strings.Contains(got[0], "\n") && len(got[0]) > 18 {
			t.Errorf("first chunk too long: %q", got[0])
		}
	})

	t.Run("zero limit returns whole text", func(t *testing.T) {
		got := Text("hello", 0)
		if len(got) != 1 || got[0] != "hello" {
			t.Errorf("got %v, want single chunk with zero limit", got)
		}
	})
}

func TestChunkByParagraph(t *testing.T) {
	t.Run("no paragraphs", func(t *testing.T) {
		got := ByParagraph("single paragraph", 100, true)
		if len(got) != 1 {
			t.Errorf("got %d, want 1 chunk", len(got))
		}
	})

	t.Run("splits on blank lines", func(t *testing.T) {
		text := "paragraph one\n\nparagraph two\n\nparagraph three"
		got := ByParagraph(text, 100, true)
		if len(got) != 3 {
			t.Errorf("got %d: %v, want 3 chunks", len(got), got)
		}
	})

	t.Run("long paragraph with split enabled", func(t *testing.T) {
		text := "para1\n\n" + strings.Repeat("a", 50)
		got := ByParagraph(text, 20, true)
		if len(got) < 3 {
			t.Errorf("got %d, want at least 3 chunks", len(got))
		}
	})

	t.Run("long paragraph with split disabled", func(t *testing.T) {
		text := "para1\n\n" + strings.Repeat("a", 50)
		got := ByParagraph(text, 20, false)
		// Should not split the long paragraph.
		if len(got) != 2 {
			t.Errorf("got %d, want 2 chunks (no split)", len(got))
		}
	})
}

func TestChunkByNewline(t *testing.T) {
	t.Run("simple lines", func(t *testing.T) {
		text := "line1\nline2\nline3"
		got := ByNewline(text, 100, true, true)
		if len(got) != 3 {
			t.Errorf("got %d: %v, want 3 chunks", len(got), got)
		}
	})

	t.Run("blank lines folded", func(t *testing.T) {
		text := "line1\n\n\nline2"
		got := ByNewline(text, 100, true, true)
		if len(got) != 2 {
			t.Errorf("got %d: %v, want 2 chunks", len(got), got)
		}
		// Second chunk should have leading newlines.
		if !strings.HasPrefix(got[1], "\n") {
			t.Errorf("expected leading newlines in second chunk: %q", got[1])
		}
	})
}

func TestChunkTextWithMode(t *testing.T) {
	text := "para1\n\npara2"

	t.Run("length mode", func(t *testing.T) {
		got := TextWithMode(text, 100, ModeLength)
		if len(got) != 1 {
			t.Errorf("length mode should return 1 chunk for short text, got %d", len(got))
		}
	})

	t.Run("newline mode splits paragraphs", func(t *testing.T) {
		got := TextWithMode(text, 100, ModeNewline)
		if len(got) != 2 {
			t.Errorf("newline mode should split on paragraphs, got %d: %v", len(got), got)
		}
	})
}

func TestResolveTextChunkLimit(t *testing.T) {
	if got := ResolveTextLimit(0, 0); got != DefaultLimit {
		t.Errorf("got %d, want default %d", got, DefaultLimit)
	}
	if got := ResolveTextLimit(2000, 0); got != 2000 {
		t.Errorf("got %d, want provider limit 2000", got)
	}
	if got := ResolveTextLimit(0, 3000); got != 3000 {
		t.Errorf("got %d, want fallback 3000", got)
	}
}
