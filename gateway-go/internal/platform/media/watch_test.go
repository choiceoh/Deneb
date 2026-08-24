package media

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSelectWatchFrameCountReturnsBucketedCountsByDuration(t *testing.T) {
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

func TestSelectWatchTimestampsReturnsSortedTimestampsAcrossWholeVideo(t *testing.T) {
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

func TestSelectWatchTimestampsKeepsSamplesWithinWindow(t *testing.T) {
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

func TestExtractFramesFromPathStopsOnCancelledContext(t *testing.T) {
	// A cancelled turn must not keep spawning per-timestamp ffmpeg runs: the
	// loop checks ctx before each extraction and bails out with nothing.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	frames, kept := extractFramesFromPath(ctx, "nonexistent.mp4", []float64{1, 2, 3})
	if len(frames) != 0 || len(kept) != 0 {
		t.Fatalf("expected no frames after cancellation, got %d frames / %d timestamps", len(frames), len(kept))
	}
}

// An extraction killed by the per-frame bound means ffmpeg HUNG on that seek —
// and an input that hangs one seek hangs them all. The batch must stop after
// the first bounded failure instead of paying the timeout once per remaining
// timestamp (up to ~200 oversampled candidates ≈ 50 minutes at the 15s bound).
func TestExtractFramesFromPathAbortsBatchAfterTimedOutExtraction(t *testing.T) {
	prevRunner, prevTimeout := runFrameExtraction, watchFrameExtractTimeout
	runFrameExtraction = func(ctx context.Context, _ []string) error {
		<-ctx.Done() // hang until the per-frame bound kills us
		return ctx.Err()
	}
	watchFrameExtractTimeout = 20 * time.Millisecond
	t.Cleanup(func() {
		runFrameExtraction = prevRunner
		watchFrameExtractTimeout = prevTimeout
	})

	frames, kept := extractFramesFromPath(context.Background(), "hung.mp4", []float64{1, 2, 3})
	if len(frames) != 0 || len(kept) != 0 {
		t.Fatalf("expected no frames from a hung input, got %d frames / %d timestamps", len(frames), len(kept))
	}
}

// A runner that fails on its OWN (ffmpeg exits nonzero, well inside the bound)
// is a per-seek boundary miss, not a hang: the loop must keep extracting the
// remaining timestamps.
func TestExtractFramesFromPathContinuesAfterOwnExitFailure(t *testing.T) {
	prevRunner := runFrameExtraction
	calls := 0
	runFrameExtraction = func(_ context.Context, _ []string) error {
		calls++
		return errors.New("exit status 1")
	}
	t.Cleanup(func() { runFrameExtraction = prevRunner })

	frames, kept := extractFramesFromPath(context.Background(), "video.mp4", []float64{1, 2, 3})
	if calls != 3 {
		t.Fatalf("extraction ran %d times, want 3 (own-exit failure must not abort the batch)", calls)
	}
	if len(frames) != 0 || len(kept) != 0 {
		t.Fatalf("expected no frames from an all-failing runner, got %d/%d", len(frames), len(kept))
	}
}

func TestSelectWatchTimestampsReturnsMidVideoTimestampForSingleFrame(t *testing.T) {
	ts := selectWatchTimestamps(100, 1, 0, 0)
	if len(ts) != 1 {
		t.Fatalf("got %d timestamps, want 1", len(ts))
	}
	if ts[0] <= 0 || ts[0] >= 100 {
		t.Errorf("single timestamp %f should be mid-video", ts[0])
	}
}
