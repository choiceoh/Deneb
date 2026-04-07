package directives

import "testing"

func TestSkipDirectiveArgPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"  : arg", 4},
		{":arg", 1},
		{"  arg", 2},
		{"arg", 0},
		{"", 0},
		{"   ", 3},
		{" : ", 3},
	}
	for _, tt := range tests {
		got := SkipDirectiveArgPrefix(tt.input)
		if got != tt.want {
			t.Errorf("SkipDirectiveArgPrefix(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestTakeDirectiveToken(t *testing.T) {
	tests := []struct {
		input     string
		start     int
		wantToken string
		wantNext  int
	}{
		{"hello world", 0, "hello", 6},
		{"hello world", 6, "world", 11},
		{"  hello  ", 0, "hello", 9},
		{"", 0, "", 0},
		{"   ", 0, "", 3},
		{"host=sandbox security=full", 0, "host=sandbox", 13},
		{"host=sandbox security=full", 13, "security=full", 26},
	}
	for _, tt := range tests {
		gotToken, gotNext := TakeDirectiveToken(tt.input, tt.start)
		if gotToken != tt.wantToken || gotNext != tt.wantNext {
			t.Errorf("TakeDirectiveToken(%q, %d) = (%q, %d), want (%q, %d)",
				tt.input, tt.start, gotToken, gotNext, tt.wantToken, tt.wantNext)
		}
	}
}
