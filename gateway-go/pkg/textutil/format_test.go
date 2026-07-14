package textutil

import "testing"

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{bytes: 7, want: "7 B"},
		{bytes: 1536, want: "1.5 KB"},
		{bytes: 2 * 1024 * 1024, want: "2.0 MB"},
	}
	for _, test := range tests {
		if got := FormatBytes(test.bytes); got != test.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", test.bytes, got, test.want)
		}
	}
}

func TestGroupThousandsFormatsCommaSeparators(t *testing.T) {
	tests := map[string]string{
		"1":       "1",
		"999":     "999",
		"1000":    "1,000",
		"13786":   "13,786",
		"1234567": "1,234,567",
	}
	for input, want := range tests {
		if got := GroupThousands(input); got != want {
			t.Errorf("GroupThousands(%q) = %q, want %q", input, got, want)
		}
	}
}
