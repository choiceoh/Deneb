package media

import (
	"testing"
)

func TestSelectFrameCount(t *testing.T) {
	tests := []struct {
		duration int
		want     int
	}{
		{0, 1},
		{1, 1},
		{3, 1},
		{5, 3},
		{10, 3},
		{30, 4},
		{60, 4},
		{120, 6},
		{600, 6},
	}

	for _, tt := range tests {
		got := selectFrameCount(tt.duration)
		if got != tt.want {
			t.Errorf("selectFrameCount(%d) = %d, want %d", tt.duration, got, tt.want)
		}
	}
}

func TestSelectTimestamps(t *testing.T) {
	tests := []struct {
		duration int
		count    int
	}{
		{0, 1},
		{5, 1},
		{10, 3},
		{60, 4},
		{120, 6},
	}

	for _, tt := range tests {
		ts := selectTimestamps(tt.duration, tt.count)
		if len(ts) != tt.count {
			t.Errorf("selectTimestamps(%d, %d): got %d timestamps, want %d",
				tt.duration, tt.count, len(ts), tt.count)
			continue
		}

		// Verify timestamps are sorted and within bounds.
		for i, v := range ts {
			if v < 0 {
				t.Errorf("selectTimestamps(%d, %d)[%d] = %f, negative",
					tt.duration, tt.count, i, v)
			}
			if tt.duration > 0 && v > float64(tt.duration) {
				t.Errorf("selectTimestamps(%d, %d)[%d] = %f, exceeds duration",
					tt.duration, tt.count, i, v)
			}
			if i > 0 && v < ts[i-1] {
				t.Errorf("selectTimestamps(%d, %d): not sorted at index %d",
					tt.duration, tt.count, i)
			}
		}
	}
}
