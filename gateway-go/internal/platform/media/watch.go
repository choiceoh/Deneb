// watch.go — "watch a video" extraction: representative frames + subtitles.
//
// This is the data-gathering half of the watch tool (the analysis half is the
// isolated vision call in pilot/vision.go). Given a YouTube URL or a local video
// file, it produces a WatchResult: a set of representative JPEG frames plus the
// subtitle transcript, so the model can both SEE (frames) and READ/HEAR
// (subtitles) the video.
//
// Frame selection prefers scene-change detection (one ffmpeg scan pass): cuts
// and slide transitions land exactly on a frame instead of being straddled by
// an even grid. Videos with too few scene changes (talking heads, screen
// recordings) fall back to even spacing, as does any scan failure or timeout.
//
// Frame budgeting follows a duration-adaptive scale (denser for short clips,
// sparser for long ones) so the vision payload stays bounded regardless of
// length. An optional [start, end] window narrows analysis to one segment.
// After extraction, a near-duplicate pass drops held-slide frames so the
// budget is spent on visually distinct content.
//
// TranscriptOnly skips the video download + frame path entirely when captions
// (or ASR) are enough — the cheap summary mode.
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
	"regexp"
	"sort"
	"strings"
	"time"
)

// WatchResult holds the frames and transcript extracted from a video for
// multimodal analysis.
type WatchResult struct {
	Title       string           // video title (YouTube) or file name (local)
	Channel     string           // uploader (YouTube only)
	DurationSec int              // total video length in seconds
	Source      string           // original URL or file path
	IsYouTube   bool             // true when extracted from a YouTube URL
	Frames      [][]byte         // JPEG-encoded frames, in timestamp order
	Timestamps  []float64        // wall-clock second of each frame (parallel to Frames)
	Transcript  string           // subtitle text (may be empty)
	Language    string           // subtitle language code
	Chapters    []YouTubeChapter // section markers (native YouTube extraction only)
	StartSec    float64          // analyzed window start (0 = from beginning)
	EndSec      float64          // analyzed window end (0 = to end)
	IsLive      bool             // live or was-live stream (captions often missing)
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

	// TranscriptOnly skips video download and frame extraction. Returns
	// captions/ASR only — used for cheap summaries when visuals aren't needed.
	// YouTube only; local files have no caption track and return an error.
	TranscriptOnly bool
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
	// watchFrameExtractTimeout bounds one ffmpeg single-frame seek+decode. A
	// normal extraction takes well under a second; a hung ffmpeg must not be
	// able to stall the whole watch call.
	watchFrameExtractTimeout = 15 * time.Second
	// watchMaxVideoBytes caps a downloaded video file (200 MB) so a long 4K
	// video cannot exhaust disk. Frame extraction only needs a watchable copy.
	watchMaxVideoBytes = 200 * 1024 * 1024

	// Scene-change frame selection (single ffmpeg scan pass over the window).
	//
	// sceneChangeThreshold is ffmpeg's scene score cutoff (fraction of the frame
	// that changed, 0..1). 0.2 catches hard cuts and slide flips while ignoring
	// camera pans and talking-head motion.
	sceneChangeThreshold = "0.2"
	// sceneMinCandidates: below this many detected changes the video is treated
	// as static content (screen recording, talking head) where an even grid
	// covers better than a handful of cuts, so selection falls back to even
	// spacing.
	sceneMinCandidates = 8
	// sceneScanTimeout bounds the detection decode pass. On timeout selection
	// silently falls back to even spacing — never fails the watch.
	sceneScanTimeout = 90 * time.Second
	// sceneScanWidth downscales the decode before the scene filter: the score
	// is computed per-pixel, so a small luma plane is dramatically cheaper and
	// just as good at spotting cuts.
	sceneScanWidth = 320

	// watchCandidateOversample pulls extra timestamp candidates before the
	// near-dup pass so the final frame budget is spent on distinct frames
	// (held slides collapse instead of consuming slots).
	watchCandidateOversample = 2
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
	// probeYtDlp (not a bare LookPath) so a broken venv shim surfaces as an
	// actionable error instead of a confusing "metadata fetch" failure below.
	ytdlpPath, err := probeYtDlp(ctx)
	if err != nil {
		return nil, err
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
		IsLive:      meta.IsLive || meta.WasLive,
	}

	// Subtitles — reuse the youtube.go downloader (best-effort; frames still
	// carry the visual content if no captions exist). When captions are blocked
	// (429/no-JS) or absent, fall back to transcribing the audio via the local
	// ASR service so the analysis still has the spoken content — crucial when the
	// main model is text-only and can't read the frames.
	// Full-video captions only make sense when no time window was requested —
	// otherwise the (untimed) caption text would describe the whole video while
	// the frames (and a text-only fallback analysis) are from the window. For a
	// windowed request, transcribe just that window's audio so the transcript
	// matches the frames the analysis actually sees. A positive StartSec is a
	// window even without an explicit end (EndSec==0 means "to the end", and
	// frames are sampled from StartSec onward in that case).
	windowed := opts.StartSec > 0 || opts.EndSec > 0
	if !windowed {
		// Prefer the native innertube extraction so watch's transcript carries the
		// same chapter sections + timestamps as the web path (no subprocess). Fall
		// back to the yt-dlp caption downloader when native yields nothing.
		if yt := extractTranscriptNative(ctx, url); yt != nil && yt.HasTranscript() {
			result.Transcript = yt.Transcript
			result.Language = yt.Language
			result.Chapters = yt.Chapters
		} else if transcript, lang, subErr := downloadSubtitles(ctx, ytdlpPath, url, tmpDir); subErr == nil {
			result.Transcript = transcript
			result.Language = lang
		}
	}
	if result.Transcript == "" {
		if t, asrLang := transcriptViaASR(ctx, ytdlpPath, url, tmpDir, int(opts.StartSec), int(opts.EndSec), meta.Duration); t != "" {
			result.Transcript = t
			result.Language = asrLang
		}
	}

	// Cheap path: captions/ASR only — skip the video download + ffmpeg work.
	if opts.TranscriptOnly {
		return result, nil
	}

	// Download a watchable copy. Prefer a compact MP4 (<=720p) to bound size and
	// keep ffmpeg seeking fast — we only need representative frames.
	videoPath, err := downloadYouTubeVideo(ctx, ytdlpPath, url, tmpDir)
	if err != nil {
		if strings.TrimSpace(result.Transcript) != "" {
			return result, nil
		}
		return nil, fmt.Errorf("video download: %w", err)
	}

	frames, timestamps, err := extractFramesAtWindow(ctx, videoPath, meta.Duration, opts)
	if err != nil {
		if strings.TrimSpace(result.Transcript) != "" {
			return result, nil
		}
		return nil, fmt.Errorf("frame extraction: %w", err)
	}
	result.Frames = frames
	result.Timestamps = timestamps
	return result, nil
}

// watchLocalFile extracts frames + (optional) subtitles from a local video file.
func watchLocalFile(ctx context.Context, path string, opts WatchOptions) (*WatchResult, error) {
	if opts.TranscriptOnly {
		return nil, fmt.Errorf("transcript-only watch requires a YouTube URL with captions (local files have no caption track)")
	}

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

	frames, timestamps, err := extractFramesAtWindow(ctx, path, duration, opts)
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
	cmd := exec.CommandContext(
		dlCtx, ytdlpPath,
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

// extractFramesAtWindow selects representative timestamps across the video (or
// the requested [start,end] window) and extracts a JPEG frame at each via
// ffmpeg. Scene-change timestamps are preferred; even spacing is the fallback.
// Candidates are oversampled, near-duplicates dropped, then thinned to the
// frame budget so held slides don't waste vision tokens.
func extractFramesAtWindow(ctx context.Context, videoPath string, duration int, opts WatchOptions) (frames [][]byte, timestamps []float64, err error) {
	count := opts.MaxFrames
	if count <= 0 {
		count = selectWatchFrameCount(duration)
	}
	candidateCount := count * watchCandidateOversample
	if candidateCount < count {
		candidateCount = count
	}

	stamps := selectSceneTimestamps(ctx, videoPath, duration, candidateCount, opts.StartSec, opts.EndSec)
	if len(stamps) == 0 {
		stamps = selectWatchTimestamps(duration, candidateCount, opts.StartSec, opts.EndSec)
	}
	frames, timestamps = extractFramesFromPath(ctx, videoPath, stamps)
	if len(frames) == 0 {
		return nil, nil, fmt.Errorf("no frames extracted (ffmpeg may be unavailable)")
	}
	frames, timestamps = dedupNearDuplicateFrames(frames, timestamps)
	frames, timestamps = downsampleFramePairs(frames, timestamps, count)
	if len(frames) == 0 {
		return nil, nil, fmt.Errorf("no frames remaining after near-duplicate filter")
	}
	return frames, timestamps, nil
}

// selectSceneTimestamps returns scene-change timestamps for the analyzed
// window, downsampled to the frame budget. Returns nil (fall back to even
// spacing) when the video has too few scene changes to represent it — static
// content is covered better by an even grid — or when the scan fails.
func selectSceneTimestamps(ctx context.Context, videoPath string, duration, count int, start, end float64) []float64 {
	lo := start
	if lo < 0 {
		lo = 0
	}
	hi := end
	if hi <= 0 || (duration > 0 && hi > float64(duration)) {
		hi = float64(duration)
	}
	// Unknown duration with no explicit end would mean decoding to EOF just to
	// discover the scenes — skip the scan and let even spacing (which handles
	// unknown duration with a bounded 1s grid) cover it.
	if hi <= lo {
		return nil
	}

	candidates := detectSceneChangeTimestamps(ctx, videoPath, lo, hi)
	if len(candidates) < sceneMinCandidates {
		return nil
	}
	return evenSampleTimestamps(candidates, count)
}

// detectSceneChangeTimestamps runs one ffmpeg decode pass over [lo, hi] (hi<=0
// means "to the end") and returns the timestamps whose scene score exceeds the
// threshold, plus the window's first frame as an anchor. Returns nil on any
// failure — callers degrade to even spacing.
func detectSceneChangeTimestamps(ctx context.Context, videoPath string, lo, hi float64) []float64 {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil
	}
	scanCtx, cancel := context.WithTimeout(ctx, sceneScanTimeout)
	defer cancel()

	// -ss before -i seeks fast but resets timestamps to 0, so parsed times are
	// shifted back by lo below. The first frame (n=0) is always selected as the
	// window anchor; showinfo prints one line per selected frame.
	args := []string{"-v", "info"}
	if lo > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.2f", lo))
	}
	if hi > lo {
		args = append(args, "-t", fmt.Sprintf("%.2f", hi-lo))
	}
	args = append(
		args,
		"-i", videoPath,
		"-an", "-sn",
		"-vf", fmt.Sprintf(
			"scale=w='min(%d,iw)':h=-2,select=eq(n\\,0)+gt(scene\\,%s),showinfo",
			sceneScanWidth, sceneChangeThreshold,
		),
		"-f", "null", "-",
	)
	// showinfo reports via the log stream (stderr).
	out, err := exec.CommandContext(scanCtx, ffmpegPath, args...).CombinedOutput()
	if err != nil {
		return nil
	}

	times := parseShowinfoTimes(out)
	for i := range times {
		times[i] += lo
	}
	return times
}

// showinfoPtsTimeRe pulls the presentation timestamp out of ffmpeg showinfo
// log lines ("... pts_time:12.345 ...").
var showinfoPtsTimeRe = regexp.MustCompile(`pts_time:([0-9]+(?:\.[0-9]+)?)`)

// parseShowinfoTimes extracts the pts_time values from ffmpeg showinfo output,
// sorted ascending with duplicates dropped.
func parseShowinfoTimes(out []byte) []float64 {
	matches := showinfoPtsTimeRe.FindAllSubmatch(out, -1)
	times := make([]float64, 0, len(matches))
	for _, m := range matches {
		var ts float64
		if _, err := fmt.Sscanf(string(m[1]), "%f", &ts); err != nil {
			continue
		}
		times = append(times, ts)
	}
	sort.Float64s(times)
	deduped := times[:0]
	for _, ts := range times {
		if n := len(deduped); n > 0 && deduped[n-1] == ts {
			continue
		}
		deduped = append(deduped, ts)
	}
	return deduped
}

// evenSampleTimestamps downsamples candidates to at most n entries, always
// keeping the first and last so the window's opening scene and final state
// both survive.
func evenSampleTimestamps(candidates []float64, n int) []float64 {
	if n <= 0 || len(candidates) <= n {
		return candidates
	}
	if n == 1 {
		return candidates[:1]
	}
	sampled := make([]float64, 0, n)
	last := len(candidates) - 1
	for i := range n {
		idx := i * last / (n - 1)
		if k := len(sampled); k > 0 && sampled[k-1] == candidates[idx] {
			continue
		}
		sampled = append(sampled, candidates[idx])
	}
	return sampled
}

// extractFramesFromPath extracts one JPEG per timestamp from a video file on
// disk. Returns the frames and the timestamps that actually produced a frame
// (some seeks may fail near boundaries and are skipped). Each ffmpeg run is
// bounded by watchFrameExtractTimeout, and a cancelled ctx stops the loop —
// a cancelled turn must not keep spawning up to ~200 oversampled extractions.
func extractFramesFromPath(ctx context.Context, videoPath string, timestamps []float64) (frames [][]byte, kept []float64) {
	tmpDir, err := os.MkdirTemp("", "deneb-watch-frames-*")
	if err != nil {
		return nil, nil
	}
	defer os.RemoveAll(tmpDir)

	for i, ts := range timestamps {
		if ctx.Err() != nil {
			break
		}
		outPath := filepath.Join(tmpDir, fmt.Sprintf("frame_%03d.jpg", i))
		args := []string{
			"-ss", fmt.Sprintf("%.2f", ts),
			"-i", videoPath,
			"-vframes", "1",
			"-q:v", fmt.Sprintf("%d", watchFrameJPEGQuality),
			"-y",
			outPath,
		}
		frameCtx, cancel := context.WithTimeout(ctx, watchFrameExtractTimeout)
		cmd := exec.CommandContext(frameCtx, "ffmpeg", args...)
		cmd.Stderr = nil
		cmd.Stdout = nil
		runErr := cmd.Run()
		cancel()
		if runErr != nil {
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
	cmd := exec.CommandContext(
		probeCtx, ffprobe,
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
