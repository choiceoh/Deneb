package media

import (
	"fmt"
	"strings"
)

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
		// Prefer the richest rendering the data supports: chapter-segmented (when
		// the video has chapters) > flat timestamped (caption cues) > plain text.
		// Both timestamped forms let the model cite "at mm:ss …" precisely.
		if ch := formatChapteredTranscript(r.Chapters, r.Segments); ch != "" {
			body = ch
		} else if ts := formatTimestampedTranscript(r.Segments); ts != "" {
			body = ts
		}
		fmt.Fprintf(&b, "\n### 자막 (%s)\n\n%s\n", lang, body)
	} else if r.IsLive {
		b.WriteString("\n라이브라 자막 트랙이 없습니다. 오디오 전사를 시도했지만 받지 못했어요 — 방송이 끝나면 다시 주시거나, 보고 싶은 구간(초)을 지정해 주세요.\n")
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

// formatChapteredTranscript groups caption cues under their chapter headers, so
// the transcript reads as "#### [m:ss] Chapter\n<timestamped lines>" per section.
// A segment belongs to the latest chapter whose start is <= the segment start.
// Segments are assumed chronological (timedtext document order) and chapters
// sorted (parseChaptersFromDescription guarantees non-decreasing starts). Returns
// "" when there are no chapters or no segments. Bounded overall to avoid bloat.
func formatChapteredTranscript(chapters []YouTubeChapter, segs []TranscriptSegment) string {
	if len(chapters) == 0 || len(segs) == 0 {
		return ""
	}
	const maxTotalChars = 80000

	// Bucket each segment into its chapter in a single pass (both lists sorted).
	buckets := make([][]TranscriptSegment, len(chapters))
	ci := 0
	for _, s := range segs {
		for ci+1 < len(chapters) && s.StartSec >= chapters[ci+1].StartSec {
			ci++
		}
		buckets[ci] = append(buckets[ci], s)
	}

	var b strings.Builder
	for i, ch := range chapters {
		body := formatTimestampedTranscript(buckets[i])
		if body == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "#### [%s] %s\n%s", formatClock(ch.StartSec), ch.Title, body)
		if b.Len() > maxTotalChars {
			b.WriteString("\n\n[...자막이 잘렸습니다]")
			break
		}
	}
	return b.String()
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
