// watch.go — "watch a video" extraction: representative frames + subtitles.
//
// This is the data-gathering half of the watch tool (the analysis half is the
// isolated vision call in pilot/vision.go). Given a YouTube URL or a local video
// file, it produces a WatchResult: a set of evenly-spaced JPEG frames plus the
// subtitle transcript, so the model can both SEE (frames) and READ/HEAR
// (subtitles) the video.
//
// Frame budgeting follows a duration-adaptive scale (denser for short clips,
// sparser for long ones) so the vision payload stays bounded regardless of
// length. An optional [start, end] window narrows analysis to one segment.
//
// Requires yt-dlp (YouTube download) and ffmpeg (frame extraction). Both are
// already project dependencies (see youtube.go / frames.go).
package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// WatchResult holds the frames and transcript extracted from a video for
// multimodal analysis.
type WatchResult struct {
	Title       string    // video title (YouTube) or file name (local)
	Channel     string    // uploader (YouTube only)
	DurationSec int       // total video length in seconds
	Source      string    // original URL or file path
	IsYouTube   bool      // true when extracted from a YouTube URL
	Frames      [][]byte  // JPEG-encoded frames, in timestamp order
	Timestamps  []float64 // wall-clock second of each frame (parallel to Frames)
	Transcript  string    // subtitle text (may be empty)
	Language    string    // subtitle language code
	StartSec    float64   // analyzed window start (0 = from beginning)
	EndSec      float64   // analyzed window end (0 = to end)
}

// WatchOptions configures a WatchVideo call.
type WatchOptions struct {
	// StartSec / EndSec optionally clip analysis to a [start, end] window
	// (seconds). Zero EndSec means "to the end". When set, frames are sampled
	// densely within the window instead of across the whole video.
	StartSec float64
	EndSec   float64

	// MaxFrames overrides the duration-adaptive frame count. Zero uses the
	// adaptive default (see selectWatchFrameCount).
	MaxFrames int
}

// Watch frame budgeting — denser sampling for short clips, sparse for long ones.
const (
	watchDur30s    = 30  // <= 30s
	watchDur1m     = 60  // <= 1m
	watchDur3m     = 180 // <= 3m
	watchDur10m    = 600 // <= 10m
	watchFrames30s = 30
	watchFrames1m  = 40
	watchFrames3m  = 60
	watchFrames10m = 80
	watchFramesMax = 100 // > 10m: sparse scan

	// watchFrameJPEGQuality is the ffmpeg JPEG quality (2=best..31=worst). A bit
	// lower than the inbound-media path (5) to keep 100-frame payloads bounded.
	watchFrameJPEGQuality = 6

	// watchVideoDownloadTimeout bounds the yt-dlp video download.
	watchVideoDownloadTimeout = 120 * time.Second
	// watchMaxVideoBytes caps a downloaded video file (200 MB) so a long 4K
	// video cannot exhaust disk. Frame extraction only needs a watchable copy.
	watchMaxVideoBytes = 200 * 1024 * 1024
)

// WatchVideo extracts frames + subtitles from a YouTube URL or local file.
func WatchVideo(ctx context.Context, source string, opts WatchOptions) (*WatchResult, error) {
	if IsYouTubeURL(source) {
		return watchYouTube(ctx, source, opts)
	}
	return watchLocalFile(ctx, source, opts)
}

// watchYouTube downloads a YouTube video, extracts frames, and pulls subtitles.
func watchYouTube(ctx context.Context, url string, opts WatchOptions) (*WatchResult, error) {
	ytdlpPath, err := exec.LookPath("yt-dlp")
	if err != nil {
		return nil, fmt.Errorf("yt-dlp not found: install with `pip install yt-dlp`")
	}

	tmpDir, err := os.MkdirTemp("", "deneb-watch-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Metadata (title, channel, duration) — reuse the youtube.go helper.
	meta, err := fetchYouTubeMetadata(ctx, ytdlpPath, url)
	if err != nil {
		return nil, fmt.Errorf("metadata fetch: %w", err)
	}

	result := &WatchResult{
		Title:       meta.Title,
		Channel:     meta.Channel,
		DurationSec: meta.Duration,
		Source:      url,
		IsYouTube:   true,
		StartSec:    opts.StartSec,
		EndSec:      opts.EndSec,
	}

	// Subtitles — reuse the youtube.go downloader (best-effort; frames still
	// carry the visual content if no captions exist).
	if transcript, lang, subErr := downloadSubtitles(ctx, ytdlpPath, url, tmpDir); subErr == nil {
		result.Transcript = transcript
		result.Language = lang
	}

	// Download a watchable copy. Prefer a compact MP4 (<=720p) to bound size and
	// keep ffmpeg seeking fast — we only need representative frames.
	videoPath, err := downloadYouTubeVideo(ctx, ytdlpPath, url, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("video download: %w", err)
	}

	frames, timestamps, err := extractFramesAtWindow(videoPath, meta.Duration, opts)
	if err != nil {
		return nil, fmt.Errorf("frame extraction: %w", err)
	}
	result.Frames = frames
	result.Timestamps = timestamps
	return result, nil
}

// watchLocalFile extracts frames + (optional) subtitles from a local video file.
func watchLocalFile(ctx context.Context, path string, opts WatchOptions) (*WatchResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("video file not found: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%q is a directory, not a video file", path)
	}
	if info.Size() > watchMaxVideoBytes {
		return nil, fmt.Errorf("video file too large (%d bytes; max %d)", info.Size(), watchMaxVideoBytes)
	}

	duration := probeDurationSec(ctx, path)
	result := &WatchResult{
		Title:       filepath.Base(path),
		DurationSec: duration,
		Source:      path,
		StartSec:    opts.StartSec,
		EndSec:      opts.EndSec,
	}

	frames, timestamps, err := extractFramesAtWindow(path, duration, opts)
	if err != nil {
		return nil, fmt.Errorf("frame extraction: %w", err)
	}
	result.Frames = frames
	result.Timestamps = timestamps
	return result, nil
}

// downloadYouTubeVideo fetches a compact MP4 copy of a YouTube video into tmpDir
// and returns its path. Format selection prefers <=720p MP4 to bound size.
func downloadYouTubeVideo(ctx context.Context, ytdlpPath, url, tmpDir string) (string, error) {
	dlCtx, cancel := context.WithTimeout(ctx, watchVideoDownloadTimeout)
	defer cancel()

	outTemplate := filepath.Join(tmpDir, "video.%(ext)s")
	cmd := exec.CommandContext(dlCtx, ytdlpPath,
		"--no-warnings",
		"--no-playlist",
		// Prefer a progressive/merged MP4 at <=720p; fall back to best available.
		"-f", "best[height<=720][ext=mp4]/best[ext=mp4]/best",
		"--max-filesize", "200M",
		"-o", outTemplate,
		url,
	)
	cmd.Stderr = nil
	cmd.Stdout = nil
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("yt-dlp download failed: %w", err)
	}

	// Find the produced file (extension chosen by yt-dlp).
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		name := e.Name()
		if len(name) >= 6 && name[:6] == "video." {
			return filepath.Join(tmpDir, name), nil
		}
	}
	return "", fmt.Errorf("no video file produced by yt-dlp")
}

// extractFramesAtWindow selects adaptive timestamps across the video (or the
// requested [start,end] window) and extracts a JPEG frame at each via ffmpeg.
func extractFramesAtWindow(videoPath string, duration int, opts WatchOptions) (frames [][]byte, timestamps []float64, err error) {
	count := opts.MaxFrames
	if count <= 0 {
		count = selectWatchFrameCount(duration)
	}

	stamps := selectWatchTimestamps(duration, count, opts.StartSec, opts.EndSec)
	frames, timestamps = extractFramesFromPath(videoPath, stamps)
	if len(frames) == 0 {
		return nil, nil, fmt.Errorf("no frames extracted (ffmpeg may be unavailable)")
	}
	return frames, timestamps, nil
}

// extractFramesFromPath extracts one JPEG per timestamp from a video file on
// disk. Returns the frames and the timestamps that actually produced a frame
// (some seeks may fail near boundaries and are skipped).
func extractFramesFromPath(videoPath string, timestamps []float64) (frames [][]byte, kept []float64) {
	tmpDir, err := os.MkdirTemp("", "deneb-watch-frames-*")
	if err != nil {
		return nil, nil
	}
	defer os.RemoveAll(tmpDir)

	for i, ts := range timestamps {
		outPath := filepath.Join(tmpDir, fmt.Sprintf("frame_%03d.jpg", i))
		args := []string{
			"-ss", fmt.Sprintf("%.2f", ts),
			"-i", videoPath,
			"-vframes", "1",
			"-q:v", fmt.Sprintf("%d", watchFrameJPEGQuality),
			"-y",
			outPath,
		}
		cmd := exec.CommandContext(context.Background(), "ffmpeg", args...)
		cmd.Stderr = nil
		cmd.Stdout = nil
		if err := cmd.Run(); err != nil {
			continue
		}
		data, err := os.ReadFile(outPath)
		if err != nil || len(data) == 0 {
			continue
		}
		frames = append(frames, data)
		kept = append(kept, ts)
	}
	return frames, kept
}

// selectWatchFrameCount maps video duration to a frame budget (see spec scale).
func selectWatchFrameCount(duration int) int {
	switch {
	case duration <= watchDur30s:
		return watchFrames30s
	case duration <= watchDur1m:
		return watchFrames1m
	case duration <= watchDur3m:
		return watchFrames3m
	case duration <= watchDur10m:
		return watchFrames10m
	default:
		return watchFramesMax
	}
}

// selectWatchTimestamps returns `count` evenly-spaced timestamps across the
// analyzed window. When start/end are unset it spans the whole video; an unknown
// duration falls back to a fixed 1s grid so short clips still yield frames.
func selectWatchTimestamps(duration, count int, start, end float64) []float64 {
	if count < 1 {
		count = 1
	}

	lo := start
	hi := end
	if hi <= 0 || (duration > 0 && hi > float64(duration)) {
		if duration > 0 {
			hi = float64(duration)
		} else {
			// Unknown duration and no explicit end: sample a 1s grid.
			hi = float64(count)
		}
	}
	if lo < 0 {
		lo = 0
	}
	if hi <= lo {
		hi = lo + float64(count)
	}

	span := hi - lo
	timestamps := make([]float64, 0, count)
	if count == 1 {
		timestamps = append(timestamps, lo+span/2)
		return timestamps
	}
	// Inset slightly from both edges to avoid black frames at boundaries.
	offset := span * 0.02
	usable := span - 2*offset
	if usable <= 0 {
		usable = span
		offset = 0
	}
	for i := range count {
		ts := lo + offset + usable*float64(i)/float64(count-1)
		timestamps = append(timestamps, ts)
	}
	sort.Float64s(timestamps)
	return timestamps
}

// probeDurationSec returns a local video's duration in seconds via ffprobe.
// Returns 0 when ffprobe is unavailable or the probe fails (callers degrade to
// the unknown-duration sampling path).
func probeDurationSec(ctx context.Context, path string) int {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, ffprobe,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	var secs float64
	if _, err := fmt.Sscanf(string(out), "%f", &secs); err != nil {
		return 0
	}
	return int(secs)
}
