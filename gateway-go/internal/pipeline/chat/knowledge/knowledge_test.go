package knowledge

import (
	"testing"
	"unicode/utf8"
)

func TestFormatTier1_NilStore(t *testing.T) {
	result := FormatTier1(nil, 0.8)
	if result != "" {
		t.Errorf("expected empty for nil store, got: %q", result)
	}
}

func TestTruncateRunes(t *testing.T) {
	s := "가나다라마바사"
	result := truncateRunes(s, 3)
	if utf8.RuneCountInString(result) > 6 { // 3 runes + "..."
		t.Errorf("expected truncated, got: %q", result)
	}
	if result != "가나다..." {
		t.Errorf("expected 가나다..., got: %q", result)
	}
}
