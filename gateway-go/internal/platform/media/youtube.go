// Package media — YouTube transcript extraction and metadata via yt-dlp.
//
// Uses yt-dlp to download subtitles/auto-captions and video metadata from
// YouTube URLs. Designed for the single-user Telegram workflow: when a
// YouTube link is detected, the transcript and metadata are extracted and
// fed to the LLM for analysis.
package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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
	Transcript  string `json:"transcript"` // full subtitle text
	Language    string `json:"language"`   // subtitle language code
	URL         string `json:"url"`
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

// ExtractYouTubeTranscript uses yt-dlp to download subtitles and metadata.
// Prefers manual subtitles in ko/en; falls back to auto-generated captions.
//
// Requires yt-dlp to be installed (`pip install yt-dlp` or system package).
// Returns an error if yt-dlp is not found.
func ExtractYouTubeTranscript(ctx context.Context, videoURL string) (*YouTubeResult, error) {
	// Check that yt-dlp is available.
	ytdlpPath, err := exec.LookPath("yt-dlp")
	if err != nil {
		return nil, fmt.Errorf("yt-dlp not found: install with `pip install yt-dlp`")
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

	// Step 2: Download subtitles.
	transcript, lang, err := downloadSubtitles(ctx, ytdlpPath, videoURL, tmpDir)
	if err != nil {
		// Transcript extraction failed; return metadata-only result.
		result.Transcript = "(자막을 추출할 수 없습니다)"
		return result, nil
	}

	result.Transcript = transcript
	result.Language = lang
	return result, nil
}

// FormatYouTubeResult formats the extraction result as a readable string
// suitable for inclusion in an LLM prompt.
func FormatYouTubeResult(r *YouTubeResult) string {
	var b strings.Builder
	b.WriteString("## YouTube 비디오 정보\n\n")

	if r.Title != "" {
		fmt.Fprintf(&b, "**제목:** %s\n", r.Title)
	}
	if r.Channel != "" {
		fmt.Fprintf(&b, "**채널:** %s\n", r.Channel)
	}
	if r.Duration != "" {
		fmt.Fprintf(&b, "**길이:** %s\n", r.Duration)
	}
	if r.UploadDate != "" {
		fmt.Fprintf(&b, "**업로드:** %s\n", formatUploadDate(r.UploadDate))
	}
	if r.ViewCount > 0 {
		fmt.Fprintf(&b, "**조회수:** %s\n", formatViewCount(r.ViewCount))
	}
	if r.URL != "" {
		fmt.Fprintf(&b, "**URL:** %s\n", r.URL)
	}

	if r.Description != "" {
		fmt.Fprintf(&b, "\n### 설명\n%s\n", r.Description)
	}

	if r.Transcript != "" && r.Transcript != "(자막을 추출할 수 없습니다)" {
		lang := r.Language
		if lang == "" {
			lang = "unknown"
		}
		fmt.Fprintf(&b, "\n### 자막 (%s)\n\n%s\n", lang, r.Transcript)
	} else {
		b.WriteString("\n(자막 없음)\n")
	}

	return b.String()
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

	cmd := exec.CommandContext(cmdCtx, ytdlpPath,
		"--dump-json",
		"--no-download",
		"--no-warnings",
		"--no-playlist",
		videoURL,
	)
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
func downloadSubtitles(ctx context.Context, ytdlpPath, videoURL, tmpDir string) (string, string, error) {
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

// tryDownloadSubs attempts to download subtitles for a specific language.
func tryDownloadSubs(ctx context.Context, ytdlpPath, videoURL, tmpDir, lang string, auto bool) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	outTemplate := filepath.Join(tmpDir, "subs")
	args := []string{
		"--no-download",
		"--no-warnings",
		"--no-playlist",
		"--convert-subs", "vtt",
		"-o", outTemplate,
	}

	if auto {
		args = append(args, "--write-auto-subs")
		if lang != "" {
			args = append(args, "--sub-langs", lang)
		}
	} else {
		args = append(args, "--write-subs")
		if lang != "" {
			args = append(args, "--sub-langs", lang)
		}
	}

	args = append(args, videoURL)

	cmd := exec.CommandContext(cmdCtx, ytdlpPath, args...)
	cmd.Stderr = nil
	cmd.Stdout = nil
	_ = cmd.Run() // yt-dlp may exit non-zero if no subs found; that's OK.

	// Find the downloaded subtitle file.
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return "", err
	}

	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".vtt") || strings.HasSuffix(name, ".srt") {
			data, err := os.ReadFile(filepath.Join(tmpDir, name))
			if err != nil {
				continue
			}
			text := cleanSubtitleText(string(data))
			if text != "" {
				return text, nil
			}
		}
	}

	return "", fmt.Errorf("no subtitle file produced")
}

// cleanSubtitleText strips VTT/SRT headers, timestamps, and formatting tags,
// returning plain text with deduplicated lines.
func cleanSubtitleText(raw string) string {
	lines := strings.Split(raw, "\n")
	var result []string
	var prevLine string

	// Patterns to skip.
	timestampRe := regexp.MustCompile(`^\d{2}:\d{2}[:\.]`)
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
	return len(s) > 0
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
