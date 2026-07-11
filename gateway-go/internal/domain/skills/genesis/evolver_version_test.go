package genesis

import "testing"

func TestBumpPatchVersion(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"0.1.0", "0.1.1"},
		{"1.2.3", "1.2.4"},
		{"0.0.0", "0.0.1"},
		{"bad", "0.1.1"},
		{"1.0", "1.0.1"},
		{"2.3.x", "2.3.1"},
		{"  4.5.6  ", "4.5.7"},
	}
	for _, tt := range tests {
		if got := bumpPatchVersion(tt.input); got != tt.want {
			t.Errorf("bumpPatchVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
