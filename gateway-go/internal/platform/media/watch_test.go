package media

import "testing"

func TestSelectWatchFrameCount(t *testing.T) {
	tests := []struct {
		duration int
		want     int
	}{
		{0, watchFrames30s},    // unknown/zero → densest bucket
		{10, watchFrames30s},   // <= 30s
		{30, watchFrames30s},   // boundary
		{45, watchFrames1m},    // <= 1m
		{60, watchFrames1m},    // boundary
		{120, watchFrames3m},   // <= 3m
		{180, watchFrames3m},   // boundary
		{300, watchFrames10m},  // <= 10m
		{600, watchFrames10m},  // boundary
		{1200, watchFramesMax}, // > 10m
	}
	for _, tt := range tests {
		if got := selectWatchFrameCount(tt.duration); got != tt.want {
			t.Errorf("selectWatchFrameCount(%d) = %d, want %d", tt.duration, got, tt.want)
		}
	}
}

func TestSelectWatchTimestamps_WholeVideo(t *testing.T) {
	const duration, count = 600, 80
	ts := selectWatchTimestamps(duration, count, 0, 0)
	if len(ts) != count {
		t.Fatalf("got %d timestamps, want %d", len(ts), count)
	}
	for i, v := range ts {
		if v < 0 || v > float64(duration) {
			t.Errorf("timestamp[%d]=%f out of [0,%d]", i, v, duration)
		}
		if i > 0 && v < ts[i-1] {
			t.Errorf("timestamps not sorted at %d", i)
		}
	}
}

func TestSelectWatchTimestamps_Window(t *testing.T) {
	// A [start,end] window must keep every sample inside the window.
	const start, end, count = 100.0, 130.0, 30
	ts := selectWatchTimestamps(600, count, start, end)
	if len(ts) != count {
		t.Fatalf("got %d timestamps, want %d", len(ts), count)
	}
	for i, v := range ts {
		if v < start-0.001 || v > end+0.001 {
			t.Errorf("timestamp[%d]=%f outside window [%f,%f]", i, v, start, end)
		}
	}
}

func TestSelectWatchTimestamps_UnknownDuration(t *testing.T) {
	// Unknown duration (0) with no window must still produce `count` frames on a
	// 1s grid rather than panicking or returning nothing.
	const count = 30
	ts := selectWatchTimestamps(0, count, 0, 0)
	if len(ts) != count {
		t.Fatalf("got %d timestamps, want %d", len(ts), count)
	}
	for i, v := range ts {
		if v < 0 {
			t.Errorf("timestamp[%d]=%f negative", i, v)
		}
	}
}

func TestSelectWatchTimestamps_SingleFrame(t *testing.T) {
	ts := selectWatchTimestamps(100, 1, 0, 0)
	if len(ts) != 1 {
		t.Fatalf("got %d timestamps, want 1", len(ts))
	}
	if ts[0] <= 0 || ts[0] >= 100 {
		t.Errorf("single timestamp %f should be mid-video", ts[0])
	}
}
