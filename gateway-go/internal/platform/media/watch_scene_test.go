package media

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseShowinfoTimes(t *testing.T) {
	out := []byte(`
[Parsed_showinfo_1 @ 0x5] n:   0 pts:      0 pts_time:0       duration_time:0.04
[Parsed_showinfo_1 @ 0x5] n:   1 pts:  51200 pts_time:2.048   duration_time:0.04
[Parsed_showinfo_1 @ 0x5] n:   2 pts: 102400 pts_time:4.1     duration_time:0.04
[Parsed_showinfo_1 @ 0x5] n:   3 pts: 102400 pts_time:4.1     duration_time:0.04
[Parsed_showinfo_1 @ 0x5] n:   4 pts:  51200 pts_time:2.048   duration_time:0.04
frame=  150 fps=0.0 q=-0.0 Lsize=N/A time=00:00:06.00 bitrate=N/A
`)
	got := parseShowinfoTimes(out)
	want := []float64{0, 2.048, 4.1} // adjacent and non-adjacent duplicates dropped
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Errorf("times[%d] = %f, want %f", i, got[i], want[i])
		}
	}
}

func TestParseShowinfoTimes_NoMatches(t *testing.T) {
	if got := parseShowinfoTimes([]byte("no frames here")); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestEvenSampleTimestampsPreservesEndpointsAndReturnsAllWhenUnderBudget(t *testing.T) {
	candidates := []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	t.Run("under budget returns as-is", func(t *testing.T) {
		got := evenSampleTimestamps(candidates, 20)
		if len(got) != len(candidates) {
			t.Errorf("got %d entries, want %d", len(got), len(candidates))
		}
	})

	t.Run("downsamples keeping first and last", func(t *testing.T) {
		got := evenSampleTimestamps(candidates, 4)
		if len(got) != 4 {
			t.Fatalf("got %v, want 4 entries", got)
		}
		if got[0] != 0 || got[len(got)-1] != 9 {
			t.Errorf("first/last not kept: %v", got)
		}
		for i := 1; i < len(got); i++ {
			if got[i] <= got[i-1] {
				t.Errorf("not strictly ascending: %v", got)
			}
		}
	})

	t.Run("budget of one", func(t *testing.T) {
		got := evenSampleTimestamps(candidates, 1)
		if len(got) != 1 || got[0] != 0 {
			t.Errorf("got %v, want [0]", got)
		}
	})

	t.Run("zero budget returns as-is", func(t *testing.T) {
		if got := evenSampleTimestamps(candidates, 0); len(got) != len(candidates) {
			t.Errorf("got %v, want all candidates", got)
		}
	})
}

func TestSelectSceneTimestamps_InvalidWindow(t *testing.T) {
	// end <= start: reject before any ffmpeg work.
	if got := selectSceneTimestamps(context.Background(), "nonexistent.mp4", 60, 10, 30, 20); got != nil {
		t.Errorf("expected nil for inverted window, got %v", got)
	}
}

func TestSelectSceneTimestamps_UnknownDuration(t *testing.T) {
	// duration 0 with no explicit end: skip the scan (it would decode to EOF)
	// and let even spacing's bounded unknown-duration grid handle it.
	if got := selectSceneTimestamps(context.Background(), "nonexistent.mp4", 0, 10, 0, 0); got != nil {
		t.Errorf("expected nil for unknown duration without explicit end, got %v", got)
	}
}

func TestSelectSceneTimestamps_MissingFile(t *testing.T) {
	// Scan failure degrades to nil (callers fall back to even spacing).
	if got := selectSceneTimestamps(context.Background(), "nonexistent.mp4", 60, 10, 0, 0); got != nil {
		t.Errorf("expected nil for missing file, got %v", got)
	}
}

// synthCutsVideo renders a video of solid-color segments (hard cut every
// segDur seconds) for scene-detection tests. Skips the test when ffmpeg is
// unavailable.
func synthCutsVideo(t *testing.T, colors []string, segDur int) string {
	t.Helper()
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not available")
	}
	out := filepath.Join(t.TempDir(), "cuts.mp4")
	args := []string{"-v", "error"}
	for _, c := range colors {
		args = append(args, "-f", "lavfi", "-i", fmt.Sprintf("color=%s:s=320x240:d=%d:r=25", c, segDur))
	}
	filter := ""
	for i := range colors {
		filter += fmt.Sprintf("[%d:v]", i)
	}
	filter += fmt.Sprintf("concat=n=%d:v=1:a=0[v]", len(colors))
	args = append(args, "-filter_complex", filter, "-map", "[v]", "-y", out)
	if msg, err := exec.Command(ffmpegPath, args...).CombinedOutput(); err != nil {
		t.Skipf("synthetic video render failed: %v: %s", err, msg)
	}
	return out
}

func TestDetectSceneChangeTimestampsFindsCutsAndAnchorsAtWindowStart(t *testing.T) {
	// Hard cuts at t=2, 4, 6. Colors are picked for distinct luma — ffmpeg's
	// scene score works on the luma plane, so e.g. red→green (nearly equal Y)
	// would be invisible to it.
	video := synthCutsVideo(t, []string{"black", "white", "gray", "black"}, 2)

	got := detectSceneChangeTimestamps(context.Background(), video, 0, 0)
	if len(got) < 4 {
		t.Fatalf("expected anchor + 3 cuts, got %v", got)
	}
	if got[0] > 0.2 {
		t.Errorf("first timestamp should anchor the window start, got %v", got)
	}
	for _, cut := range []float64{2, 4, 6} {
		found := false
		for _, ts := range got {
			if math.Abs(ts-cut) < 0.5 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cut at %.0fs not detected: %v", cut, got)
		}
	}
}

func TestWatchVideoReturnsFramesAlignedWithSceneCuts(t *testing.T) {
	// 12 one-second segments → 11 hard cuts (>= sceneMinCandidates), so the
	// scene path is taken end-to-end: every extracted frame must sit on a cut
	// (integer second), not on an even grid across the duration.
	colors := []string{
		"black", "white", "gray", "black", "white", "gray",
		"black", "white", "gray", "black", "white", "gray",
	}
	video := synthCutsVideo(t, colors, 1)

	res, err := WatchVideo(context.Background(), video, WatchOptions{MaxFrames: 6})
	if err != nil {
		t.Fatalf("WatchVideo: %v", err)
	}
	if len(res.Frames) == 0 || len(res.Frames) != len(res.Timestamps) {
		t.Fatalf("frames/timestamps mismatch: %d/%d", len(res.Frames), len(res.Timestamps))
	}
	if len(res.Timestamps) > 6 {
		t.Errorf("frame budget exceeded: %v", res.Timestamps)
	}
	for _, ts := range res.Timestamps {
		if math.Abs(ts-math.Round(ts)) > 0.3 {
			t.Errorf("timestamp %f not aligned to a cut: %v", ts, res.Timestamps)
		}
	}
}

func TestDetectSceneChangeTimestampsShiftsCutsToAbsoluteTimeWithinWindow(t *testing.T) {
	video := synthCutsVideo(t, []string{"black", "white", "gray", "black"}, 2)

	// Scan [3, 8): only the cuts at t=4 and t=6 are inside; parsed times must
	// be shifted back into absolute video time.
	got := detectSceneChangeTimestamps(context.Background(), video, 3, 8)
	if len(got) < 3 {
		t.Fatalf("expected anchor + 2 cuts, got %v", got)
	}
	if math.Abs(got[0]-3) > 0.5 {
		t.Errorf("anchor should sit near window start 3s, got %v", got)
	}
	for _, cut := range []float64{4, 6} {
		found := false
		for _, ts := range got {
			if math.Abs(ts-cut) < 0.5 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cut at %.0fs not detected in window: %v", cut, got)
		}
	}
	for _, ts := range got {
		if ts < 2.5 || ts > 8.5 {
			t.Errorf("timestamp %f outside scanned window: %v", ts, got)
		}
	}
}
