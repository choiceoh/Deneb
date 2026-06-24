// Package media — YouTube transcript extraction and metadata.
//
// The primary path is native (youtube_native.go): it calls YouTube's internal
// innertube API directly over HTTP for captions + metadata, so the common
// "summarize this link" case needs no external tooling. yt-dlp remains the
// fallback for caption-less videos (audio→ASR) and for the audio/video download
// paths (signature deciphering is left to yt-dlp). When a YouTube link is
// detected, the transcript and metadata are extracted and fed to the LLM.
package media

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/cliprobe"
)

// YouTubeResult holds the extracted transcript and metadata from a YouTube video.
type YouTubeResult struct {
	Title       string `json:"title"`
	Channel     string `json:"channel"`
	Duration    string `json:"duration"`     // human-readable (e.g., "12:34")
	DurationSec int    `json:"duration_sec"` // seconds
	UploadDate  string `json:"upload_date"`  // YYYYMMDD
	ViewCount   int64  `json:"view_count"`
	Description string `json:"description"`
	Transcript  string `json:"transcript"` // full subtitle text (plain, deduped)
	Language    string `json:"language"`   // subtitle language code
	URL         string `json:"url"`

	// Deep metadata — populated by the native innertube path (youtube_native.go).
	// The yt-dlp fallback leaves these zero-valued.
	ChannelID         string              `json:"channel_id,omitempty"`
	ChannelURL        string              `json:"channel_url,omitempty"`
	Category          string              `json:"category,omitempty"`
	Keywords          []string            `json:"keywords,omitempty"` // creator tags
	IsLive            bool                `json:"is_live,omitempty"`
	Thumbnail         string              `json:"thumbnail,omitempty"` // largest thumbnail URL
	Chapters          []YouTubeChapter    `json:"chapters,omitempty"`  // section markers
	AvailableCaptions []string            `json:"available_captions,omitempty"`
	Segments          []TranscriptSegment `json:"segments,omitempty"` // timestamped caption cues
}

// TranscriptSegment is one timestamped caption cue (start offset + text). The
// native path preserves these so the transcript can be rendered with timestamps
// for citation ("at 12:34 he says X") without losing the plain Transcript.
type TranscriptSegment struct {
	StartSec int    `json:"start_sec"`
	Text     string `json:"text"`
}

// YouTubeChapter is a named section marker within the video.
type YouTubeChapter struct {
	StartSec int    `json:"start_sec"`
	Title    string `json:"title"`
}

// youtubeURLPattern matches YouTube video URLs.
var youtubeURLPattern = regexp.MustCompile(
	`(?i)(?:https?://)?(?:www\.)?(?:youtube\.com/(?:watch\?v=|shorts/|live/)|youtu\.be/)([a-zA-Z0-9_-]{11})`,
)

// IsYouTubeURL returns true if the text contains a YouTube video URL.
func IsYouTubeURL(text string) bool {
	return youtubeURLPattern.MatchString(text)
}

// ExtractYouTubeURLs extracts all YouTube video URLs from text.
func ExtractYouTubeURLs(text string) []string {
	matches := youtubeURLPattern.FindAllString(text, 5)
	// Deduplicate.
	seen := make(map[string]struct{}, len(matches))
	var urls []string
	for _, u := range matches {
		if _, ok := seen[u]; !ok {
			seen[u] = struct{}{}
			urls = append(urls, u)
		}
	}
	return urls
}

// ExtractYouTubeTranscript extracts subtitles and metadata for a YouTube video.
// Prefers manual subtitles in ko/en; falls back to auto-generated captions.
//
// It tries the native innertube path first (no external dependency; see
// youtube_native.go) and only short-circuits when that yields an actual
// transcript. Otherwise it falls back to yt-dlp, which adds the audio→ASR
// fallback for caption-less videos. As a result, caption videos work even when
// yt-dlp is absent or broken; only the ASR path strictly needs yt-dlp.
func ExtractYouTubeTranscript(ctx context.Context, videoURL string) (*YouTubeResult, error) {
	// Native-first: direct innertube API. Strictly faster (no Python subprocess)
	// and dependency-free for the common "summarize this link" case.
	native := extractTranscriptNativeBounded(ctx, videoURL)
	if native != nil && native.HasTranscript() {
		return native, nil
	}

	// Probe that yt-dlp is not just present but actually runnable. A bare
	// LookPath passes for a broken venv shim (the standard casualty of a system
	// Python upgrade), which then explodes at the first real invocation with a
	// confusing "metadata fetch" error. The probe classifies missing vs broken
	// and surfaces an operator-actionable repair hint.
	ytdlpPath, err := probeYtDlp(ctx)
	if err != nil {
		// yt-dlp unavailable. If the native path at least retrieved metadata,
		// return that (transcript marked missing) instead of failing outright —
		// a caption-less video still yields a useful info card with no tooling.
		if native != nil {
			if native.Transcript == "" {
				native.Transcript = noTranscriptMarker
			}
			return native, nil
		}
		return nil, err
	}

	tmpDir, err := os.MkdirTemp("", "deneb-yt-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Step 1: Fetch metadata as JSON.
	meta, err := fetchYouTubeMetadata(ctx, ytdlpPath, videoURL)
	if err != nil {
		return nil, fmt.Errorf("metadata fetch: %w", err)
	}

	result := &YouTubeResult{
		Title:       meta.Title,
		Channel:     meta.Channel,
		DurationSec: meta.Duration,
		Duration:    formatDuration(meta.Duration),
		UploadDate:  meta.UploadDate,
		ViewCount:   meta.ViewCount,
		Description: truncateString(meta.Description, 1000),
		URL:         videoURL,
	}

	// Step 2: Download subtitles. When ASR is usable and the caller's deadline is
	// tight (the web fetch path caps this at 90s), bound the caption probes so they
	// can't consume the whole budget — leaving a reserve for the ASR fallback. The
	// ASR call below still uses the original ctx, so it gets that reserved time.
	// Gate the reserve on actual ASR readiness: if the sidecar is down there is no
	// fallback to reserve for, so captions must get the whole deadline.
	subCtx := ctx
	if asrUsable(ctx) {
		if dl, ok := ctx.Deadline(); ok {
			subDeadline := dl.Add(-asrReserveBudget)
			if time.Until(subDeadline) >= minSubtitleBudget {
				var cancel context.CancelFunc
				subCtx, cancel = context.WithDeadline(ctx, subDeadline)
				defer cancel()
			}
		}
	}
	transcript, lang, err := downloadSubtitles(subCtx, ytdlpPath, videoURL, tmpDir)
	if err != nil {
		// No captions (or YouTube blocked them) — fall back to transcribing the
		// audio with the local ASR service when one is wired. This is what makes
		// caption-less videos (and the intermittent 429 caption block) analyzable
		// without a multimodal model.
		if t, asrLang := transcriptViaASR(ctx, ytdlpPath, videoURL, tmpDir, 0, 0, meta.Duration); t != "" {
			result.Transcript = t
			result.Language = asrLang
			return result, nil
		}
		result.Transcript = noTranscriptMarker
		return result, nil
	}

	result.Transcript = transcript
	result.Language = lang
	return result, nil
}

// minNativeBudget is the floor below which the native attempt is skipped when a
// tight deadline must reserve time for the ASR fallback. Native usually finishes
// in 1-3s, so a sub-5s window isn't worth risking the caption→ASR reserve.
const minNativeBudget = 5 * time.Second

// extractTranscriptNativeBounded runs the native path, but caps its time when the
// audio→ASR fallback is live AND the caller's deadline is tight (the web fetch
// path caps at 90s). A native call can spend up to ~30s on its two HTTP timeouts;
// if it ran unbounded and returned no transcript, it would shrink the remaining
// deadline below the subtitle phase's ASR reserve threshold, after which the
// yt-dlp caption probes would run unbounded and starve transcriptViaASR. Bounding
// native to (remaining − asrReserveBudget − minSubtitleBudget) keeps that reserve
// intact; when ASR isn't the fallback (no reserve needed) native gets full time.
func extractTranscriptNativeBounded(ctx context.Context, videoURL string) *YouTubeResult {
	if asrUsable(ctx) {
		if dl, ok := ctx.Deadline(); ok {
			budget := time.Until(dl) - asrReserveBudget - minSubtitleBudget
			if budget < minNativeBudget {
				return nil // too tight; defer to yt-dlp, which owns the reserve logic
			}
			nctx, cancel := context.WithTimeout(ctx, budget)
			defer cancel()
			return extractTranscriptNative(nctx, videoURL)
		}
	}
	return extractTranscriptNative(ctx, videoURL)
}

// ytDlpRepairHint is the remediation surfaced when yt-dlp is on PATH but won't
// run (broken shim / missing interpreter). The pip reinstall is the usual fix
// after a system Python upgrade orphans the entry-point script.
const ytDlpRepairHint = "pip install --force-reinstall yt-dlp"

// probeYtDlp verifies yt-dlp is present AND executable, returning its resolved
// path. On a missing or broken tool it returns an error carrying the operator
// repair hint (and logs at Error for a broken install, since the user's request
// silently degrades to "no transcript"). This replaces a bare exec.LookPath,
// which cannot tell a working install from a broken shim.
func probeYtDlp(ctx context.Context) (string, error) {
	r := cliprobe.Probe(ctx, "yt-dlp", ytDlpRepairHint)
	switch r.Status {
	case cliprobe.StatusOK:
		return r.Path, nil
	case cliprobe.StatusBroken:
		// On PATH but unrunnable: the operator needs to know, because every
		// YouTube fetch will otherwise fail opaquely.
		slog.Error("yt-dlp is installed but not runnable",
			"path", r.Path, "error", r.Err, "fix", r.Hint)
		return "", fmt.Errorf("yt-dlp found but not runnable (%s): %w", r.Hint, r.Err)
	default: // StatusMissing
		return "", fmt.Errorf("yt-dlp not found: %s", r.Hint)
	}
}

// AudioTranscriber, when set, transcribes a local audio file to text. It is the
// injection point for the chat layer's ASR service (VibeVoice-ASR) so this
// platform package stays free of any chat/tools dependency. Wired once at
// startup; nil disables the audio-ASR fallback (captions-only behavior).
var AudioTranscriber func(ctx context.Context, audioPath string) (string, error)

// AudioTranscriberReady, when set, reports whether the ASR sidecar is actually
// reachable. transcriptViaASR consults it BEFORE downloading any audio so that a
// deployment whose sidecar is down doesn't burn the web/watch budget downloading
// audio only for the localhost ASR request to fail. nil means "assume ready"
// (the wire sets a cached health probe).
var AudioTranscriberReady func(ctx context.Context) bool

// asrUsable reports whether the audio→ASR fallback can actually run: a transcriber
// is wired and (when a readiness probe is wired) the sidecar is reachable. Used
// both to gate the fallback itself and to decide whether to reserve caption-phase
// time for it — if ASR can't run, captions must get the whole deadline.
func asrUsable(ctx context.Context) bool {
	return AudioTranscriber != nil && (AudioTranscriberReady == nil || AudioTranscriberReady(ctx))
}

// asrAudioCapSec bounds how much of a video's audio is transcribed via ASR, so a
// long video can't blow the agent turn deadline (ASR runs ~0.5-0.7x real-time).
// Overridable via DENEB_YT_ASR_CAP_SEC. Videos shorter than the cap transcribe whole.
func asrAudioCapSec() int {
	if v := strings.TrimSpace(os.Getenv("DENEB_YT_ASR_CAP_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 600
}

// ASR cost model used to keep the audio→ASR fallback inside the caller's deadline.
const (
	asrRealtimeFactor = 0.7 // VibeVoice transcription time ≈ audioSeconds * this
	asrMinViableSec   = 20  // not worth transcribing a clip shorter than this
)

// asrOverheadBudget is download + request headroom subtracted from the remaining
// deadline before deriving how much audio can be transcribed in time.
const asrOverheadBudget = 20 * time.Second

// asrReserveBudget is the slice of a tight caller deadline kept aside for the ASR
// fallback so slow/blocked caption probes can't consume the whole budget first.
// minSubtitleBudget is the floor the caption phase must still get for the reserve
// to apply (a generous deadline leaves both phases ample time).
const (
	asrReserveBudget  = 60 * time.Second
	minSubtitleBudget = 20 * time.Second
)

// transcriptViaASR downloads the requested audio span and runs it through the wired
// AudioTranscriber. The span is an explicit [startSec,endSec] window (watch) or, when
// endSec<=startSec, a from-startSec prefix capped at DENEB_YT_ASR_CAP_SEC. The span is
// further shrunk to fit the caller's context deadline (the web fetch path is bounded to
// 90s), and ASR is skipped entirely when too little time remains — so this never blocks
// past the caller's budget only to return nothing. videoDurSec (0 = unknown) lets it
// mark the transcript partial when the covered span is less than the whole video, so
// downstream summaries don't imply full coverage. Returns ("","") on any failure.
func transcriptViaASR(ctx context.Context, ytdlpPath, videoURL, tmpDir string, startSec, endSec, videoDurSec int) (text, lang string) {
	// Skip before any download when ASR is unwired or the sidecar is unreachable —
	// otherwise a no-ASR deployment wastes the deadline downloading audio for a
	// doomed request.
	if !asrUsable(ctx) {
		return "", ""
	}

	// Requested span length.
	reqLen := asrAudioCapSec()
	if endSec > startSec {
		reqLen = endSec - startSec
	}
	segLen := reqLen

	// Deadline-aware shrink: only transcribe what can finish before ctx expires.
	if dl, ok := ctx.Deadline(); ok {
		usable := time.Until(dl) - asrOverheadBudget
		maxSeg := int(usable.Seconds() / asrRealtimeFactor)
		if maxSeg < asrMinViableSec {
			return "", "" // not enough remaining time for a useful transcription
		}
		if maxSeg < segLen {
			segLen = maxSeg
		}
	}

	audioPath, err := DownloadYouTubeAudio(ctx, ytdlpPath, videoURL, tmpDir, startSec, startSec+segLen)
	if err != nil || audioPath == "" {
		return "", ""
	}
	t, err := AudioTranscriber(ctx, audioPath)
	if err != nil || strings.TrimSpace(t) == "" {
		return "", ""
	}

	// Mark partiality (covered span < whole video) so summaries are honest.
	covEnd := startSec + segLen
	partial := startSec > 0 || (videoDurSec > 0 && covEnd < videoDurSec)
	if partial {
		note := fmt.Sprintf("[참고: 아래 전사는 영상 전체가 아니라 %s–%s 구간만 다룹니다. 요약·결론이 영상 전체를 대표하지 않을 수 있습니다.]\n\n",
			formatDuration(startSec), formatDuration(covEnd))
		return note + t, fmt.Sprintf("asr (%s–%s)", formatDuration(startSec), formatDuration(covEnd))
	}
	return t, "asr"
}

// DownloadYouTubeAudio downloads the [startSec,endSec] audio span (endSec<=startSec
// means from startSec to end-of-video) and re-encodes it to 16 kHz mono WAV in tmpDir,
// returning the path. WAV is used because the ASR sidecar decodes it natively — a
// trimmed m4a/aac decoded to an empty waveform and crashed the server (HTTP 500).
// Requires ffmpeg (extract-audio + section trim).
func DownloadYouTubeAudio(ctx context.Context, ytdlpPath, videoURL, tmpDir string, startSec, endSec int) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	outTemplate := filepath.Join(tmpDir, "audio.%(ext)s")
	args := []string{
		"--no-warnings", "--no-playlist",
		"-f", "bestaudio/best",
		"--extract-audio", "--audio-format", "wav",
		"--postprocessor-args", "ffmpeg:-ar 16000 -ac 1",
		"-o", outTemplate,
	}
	args = append(args, ytPlayerClientArgs()...)
	if endSec > startSec {
		args = append(args, "--download-sections", fmt.Sprintf("*%d-%d", startSec, endSec), "--force-keyframes-at-cuts")
	} else if startSec > 0 {
		args = append(args, "--download-sections", fmt.Sprintf("*%d-inf", startSec), "--force-keyframes-at-cuts")
	}
	args = append(args, videoURL)

	cmd := exec.CommandContext(cmdCtx, ytdlpPath, args...)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("yt-dlp audio download failed: %w", err)
	}
	wavPath := filepath.Join(tmpDir, "audio.wav")
	if _, err := os.Stat(wavPath); err == nil {
		return wavPath, nil
	}
	// Fallback: whatever audio.* the postprocessor produced.
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "audio.") {
			return filepath.Join(tmpDir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("no audio file produced")
}

// ytPlayerClientArgs is an operator escape hatch: when DENEB_YTDLP_PLAYER_CLIENT
// is set it forces specific YouTube player clients (e.g. if a future YouTube
// change makes the default client fail). It is EMPTY by default on purpose —
// forcing player_client=android was measured to break audio-format availability
// ("Requested format is not available") while giving no caption-download benefit
// over the default client. Robustness instead comes from the 429 retry and the
// ASR fallback.
func ytPlayerClientArgs() []string {
	pc := strings.TrimSpace(os.Getenv("DENEB_YTDLP_PLAYER_CLIENT"))
	if pc == "" {
		return nil
	}
	return []string{"--extractor-args", "youtube:player_client=" + pc}
}

// noTranscriptMarker is the sentinel transcript value set when subtitle
// extraction fails but metadata was retrieved.
const noTranscriptMarker = "(자막을 추출할 수 없습니다)"

// HasTranscript reports whether the result carries usable subtitle text.
func (r *YouTubeResult) HasTranscript() bool {
	return r.Transcript != "" && r.Transcript != noTranscriptMarker
}

// FormatYouTubeMeta formats only the video metadata (title, channel, duration,
// upload date, view count, URL, description) — without the transcript. Callers
// that summarize the transcript separately reuse this for the header.
func FormatYouTubeMeta(r *YouTubeResult) string {
	var b strings.Builder
	b.WriteString("## YouTube 비디오 정보\n\n")

	if r.Title != "" {
		fmt.Fprintf(&b, "**제목:** %s\n", r.Title)
	}
	if r.Channel != "" {
		if r.ChannelURL != "" {
			fmt.Fprintf(&b, "**채널:** %s (%s)\n", r.Channel, r.ChannelURL)
		} else {
			fmt.Fprintf(&b, "**채널:** %s\n", r.Channel)
		}
	}
	if r.Duration != "" {
		live := ""
		if r.IsLive {
			live = " (라이브)"
		}
		fmt.Fprintf(&b, "**길이:** %s%s\n", r.Duration, live)
	}
	if r.UploadDate != "" {
		fmt.Fprintf(&b, "**업로드:** %s\n", formatUploadDate(r.UploadDate))
	}
	if r.ViewCount > 0 {
		fmt.Fprintf(&b, "**조회수:** %s\n", formatViewCount(r.ViewCount))
	}
	if r.Category != "" {
		fmt.Fprintf(&b, "**카테고리:** %s\n", r.Category)
	}
	if len(r.Keywords) > 0 {
		tags := r.Keywords
		if len(tags) > 15 { // cap noisy tag lists
			tags = tags[:15]
		}
		fmt.Fprintf(&b, "**태그:** %s\n", strings.Join(tags, ", "))
	}
	if r.URL != "" {
		fmt.Fprintf(&b, "**URL:** %s\n", r.URL)
	}
	if len(r.AvailableCaptions) > 0 {
		fmt.Fprintf(&b, "**제공 자막:** %s\n", strings.Join(r.AvailableCaptions, ", "))
	}

	if len(r.Chapters) > 0 {
		b.WriteString("\n### 챕터\n")
		for _, c := range r.Chapters {
			fmt.Fprintf(&b, "- %s  %s\n", formatDuration(c.StartSec), c.Title)
		}
	}

	if r.Description != "" {
		fmt.Fprintf(&b, "\n### 설명\n%s\n", r.Description)
	}

	return b.String()
}

// FormatYouTubeResult formats the extraction result as a readable string
// suitable for inclusion in an LLM prompt (metadata + full transcript).
func FormatYouTubeResult(r *YouTubeResult) string {
	var b strings.Builder
	b.WriteString(FormatYouTubeMeta(r))

	if r.HasTranscript() {
		lang := r.Language
		if lang == "" {
			lang = "unknown"
		}
		body := r.Transcript
		// Prefer the timestamped rendering when caption cues are available — it
		// lets the model cite "at mm:ss …" and anchor quotes precisely.
		if ts := formatTimestampedTranscript(r.Segments); ts != "" {
			body = ts
		}
		fmt.Fprintf(&b, "\n### 자막 (%s)\n\n%s\n", lang, body)
	} else {
		b.WriteString("\n(자막 없음)\n")
	}

	return b.String()
}

// formatTimestampedTranscript renders caption cues into lines, each prefixed with
// a [m:ss] marker emitted at most once per timestampBucketSec — so the transcript
// stays citeable without a marker per (often sub-second) cue. The marker shows the
// actual start of the cue that opens each bucket. Returns "" for no segments.
func formatTimestampedTranscript(segs []TranscriptSegment) string {
	const timestampBucketSec = 30
	const maxChars = 60000 // guard against pathological lengths (plain text is capped separately)

	var lines []string
	var cur strings.Builder
	nextMark, total := 0, 0
	var prev string
	flush := func() {
		if cur.Len() > 0 {
			lines = append(lines, strings.TrimSpace(cur.String()))
			total += cur.Len()
			cur.Reset()
		}
	}
	for _, s := range segs {
		line := strings.TrimSpace(s.Text)
		if line == "" || line == prev {
			continue
		}
		prev = line
		if s.StartSec >= nextMark {
			flush()
			fmt.Fprintf(&cur, "[%s] ", formatClock(s.StartSec))
			nextMark = (s.StartSec/timestampBucketSec)*timestampBucketSec + timestampBucketSec
		}
		cur.WriteString(line)
		cur.WriteByte(' ')
		if total+cur.Len() > maxChars {
			flush()
			lines = append(lines, "[...자막이 잘렸습니다]")
			return strings.Join(lines, "\n")
		}
	}
	flush()
	return strings.Join(lines, "\n")
}

// formatClock renders seconds as "m:ss" or "h:mm:ss". Unlike formatDuration it
// renders 0 as "0:00" (formatDuration returns "" for non-positive input, which
// is wrong for a transcript timestamp at the very start of a video).
func formatClock(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// ytMetadata holds the subset of yt-dlp JSON metadata we use.
type ytMetadata struct {
	Title       string `json:"title"`
	Channel     string `json:"channel"`
	Duration    int    `json:"duration"`
	UploadDate  string `json:"upload_date"`
	ViewCount   int64  `json:"view_count"`
	Description string `json:"description"`
}

// fetchYouTubeMetadata calls yt-dlp --dump-json to get video metadata.
func fetchYouTubeMetadata(ctx context.Context, ytdlpPath, videoURL string) (*ytMetadata, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	metaArgs := []string{"--dump-json", "--no-download", "--no-warnings", "--no-playlist"}
	metaArgs = append(metaArgs, ytPlayerClientArgs()...)
	metaArgs = append(metaArgs, videoURL)
	cmd := exec.CommandContext(cmdCtx, ytdlpPath, metaArgs...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp --dump-json failed: %w", err)
	}

	var meta ytMetadata
	if err := json.Unmarshal(out, &meta); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}
	return &meta, nil
}

// downloadSubtitles downloads subtitles in preferred language order and returns
// the transcript text and language code.
// Preference: ko manual → en manual → ko auto → en auto → any auto.
func downloadSubtitles(ctx context.Context, ytdlpPath, videoURL, tmpDir string) (string, string, error) { //nolint:gocritic // unnamedResult — naming would shadow local vars
	// Try manual subtitles first (ko, then en).
	for _, lang := range []string{"ko", "en"} {
		text, err := tryDownloadSubs(ctx, ytdlpPath, videoURL, tmpDir, lang, false)
		if err == nil && text != "" {
			return text, lang, nil
		}
	}

	// Try auto-generated captions (ko, en, then any).
	for _, lang := range []string{"ko", "en"} {
		text, err := tryDownloadSubs(ctx, ytdlpPath, videoURL, tmpDir, lang, true)
		if err == nil && text != "" {
			return text, lang + " (auto)", nil
		}
	}

	// Last resort: any auto-generated caption.
	text, err := tryDownloadSubs(ctx, ytdlpPath, videoURL, tmpDir, "", true)
	if err == nil && text != "" {
		return text, "auto", nil
	}

	return "", "", fmt.Errorf("no subtitles available")
}

// tryDownloadSubs attempts to download subtitles for a specific language. YouTube
// intermittently rate-limits caption downloads (HTTP 429); on that signal it
// retries once after a short backoff, which clears most transient failures.
func tryDownloadSubs(ctx context.Context, ytdlpPath, videoURL, tmpDir, lang string, auto bool) (string, error) {
	outTemplate := filepath.Join(tmpDir, "subs")
	args := []string{
		"--no-download",
		"--no-warnings",
		"--no-playlist",
		"--convert-subs", "vtt",
		"-o", outTemplate,
	}
	args = append(args, ytPlayerClientArgs()...)
	if auto {
		args = append(args, "--write-auto-subs")
	} else {
		args = append(args, "--write-subs")
	}
	if lang != "" {
		args = append(args, "--sub-langs", lang)
	}
	args = append(args, videoURL)

	for attempt := 0; attempt < 2; attempt++ {
		text, rateLimited := runSubsAttempt(ctx, ytdlpPath, args, tmpDir)
		if text != "" {
			return text, nil
		}
		if !rateLimited {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second): // back off, then retry once
		}
	}
	return "", fmt.Errorf("no subtitle file produced")
}

// runSubsAttempt runs one yt-dlp subtitle pass and scans tmpDir for the result,
// reporting whether the failure looked like a 429 rate-limit (worth a retry).
func runSubsAttempt(ctx context.Context, ytdlpPath string, args []string, tmpDir string) (text string, rateLimited bool) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, ytdlpPath, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	_ = cmd.Run() // yt-dlp may exit non-zero when no subs exist; that's expected.

	entries, err := os.ReadDir(tmpDir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if strings.HasSuffix(name, ".vtt") || strings.HasSuffix(name, ".srt") {
				data, readErr := os.ReadFile(filepath.Join(tmpDir, name))
				if readErr != nil {
					continue
				}
				if t := cleanSubtitleText(string(data)); t != "" {
					return t, false
				}
			}
		}
	}
	errOut := stderr.String()
	return "", strings.Contains(errOut, "429") || strings.Contains(errOut, "Too Many Requests")
}

// cleanSubtitleText strips VTT/SRT headers, timestamps, and formatting tags,
// returning plain text with deduplicated lines.
func cleanSubtitleText(raw string) string {
	lines := strings.Split(raw, "\n")
	var result []string
	var prevLine string

	// Patterns to skip.
	timestampRe := regexp.MustCompile(`^\d{2}:\d{2}[:.]`)
	tagRe := regexp.MustCompile(`<[^>]+>`)
	positionRe := regexp.MustCompile(`(?i)^(WEBVTT|Kind:|Language:|NOTE\b)`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip VTT/SRT headers and metadata.
		if positionRe.MatchString(line) {
			continue
		}
		// Skip timestamp lines.
		if timestampRe.MatchString(line) {
			continue
		}
		// Skip numeric cue IDs (SRT format).
		if isNumericLine(line) {
			continue
		}
		// Strip HTML-like tags.
		line = tagRe.ReplaceAllString(line, "")
		line = strings.TrimSpace(line)
		if line == "" || line == prevLine {
			continue
		}
		result = append(result, line)
		prevLine = line
	}

	text := strings.Join(result, "\n")

	// Truncate very long transcripts (cap at ~30K chars to stay within LLM context).
	const maxTranscriptChars = 50000
	if len(text) > maxTranscriptChars {
		text = text[:maxTranscriptChars] + "\n\n[...자막이 50,000자에서 잘렸습니다]"
	}

	return text
}

// isNumericLine returns true if the line is a pure number (SRT cue index).
func isNumericLine(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// formatDuration converts seconds to "HH:MM:SS" or "MM:SS".
func formatDuration(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// formatUploadDate converts "YYYYMMDD" to "YYYY-MM-DD".
func formatUploadDate(d string) string {
	if len(d) == 8 {
		return d[:4] + "-" + d[4:6] + "-" + d[6:8]
	}
	return d
}

// formatViewCount formats a view count with Korean number suffixes.
func formatViewCount(n int64) string {
	switch {
	case n >= 100_000_000:
		return fmt.Sprintf("%d억회", n/100_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1f만회", float64(n)/10_000)
	default:
		return fmt.Sprintf("%d회", n)
	}
}

// truncateString truncates a string to maxLen characters.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
