package artifact

import "testing"

func TestNormalizeWatchDetail(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", watchDetailTranscript, false}, // default is transcript (captions only; frames is opt-in)
		{"frames", watchDetailFrames, false},
		{"FRAMES", watchDetailFrames, false},
		{"transcript", watchDetailTranscript, false},
		{" Transcript ", watchDetailTranscript, false},
		{"efficient", "", true},
		{"token-burner", "", true},
	}
	for _, tt := range tests {
		got, err := normalizeWatchDetail(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("normalizeWatchDetail(%q) err=nil, want error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeWatchDetail(%q) unexpected err: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("normalizeWatchDetail(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
