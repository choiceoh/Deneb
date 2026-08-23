package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestFormatYouTubeResultExplainsLiveWithoutCaptions(t *testing.T) {
	got := FormatYouTubeResult(&YouTubeResult{Title: "Talk", IsLive: true})
	if !strings.Contains(got, "라이브라 자막 트랙이 없습니다") {
		t.Fatalf("live talk notice missing:\n%s", got)
	}
	if strings.Contains(got, "(자막 없음)") {
		t.Fatalf("generic marker leaked:\n%s", got)
	}
}

func TestYoutubeResultFromMetaMarksWasLive(t *testing.T) {
	got := youtubeResultFromMeta(&ytMetadata{Title: "Talk", WasLive: true, Duration: 3540}, "https://youtu.be/L8UDNXubKFs")
	if !got.IsLive || got.Title != "Talk" || got.DurationSec != 3540 {
		t.Fatalf("got %+v", got)
	}
}

func TestYouTubeResultHasTranscriptContract(t *testing.T) {
	tests := []struct {
		name       string
		transcript string
		want       bool
	}{
		{name: "empty", transcript: "", want: false},
		{name: "sentinel", transcript: noTranscriptMarker, want: false},
		{name: "plain", transcript: "hello", want: true},
		{name: "spaces are currently content", transcript: "   ", want: true},
		{name: "sentinel with spaces differs", transcript: " " + noTranscriptMarker, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &YouTubeResult{Transcript: tt.transcript}
			if got := result.HasTranscript(); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatYouTubeMetaFullContract(t *testing.T) {
	keywords := make([]string, 18)
	for i := range keywords {
		keywords[i] = fmt.Sprintf("tag-%02d", i+1)
	}
	result := &YouTubeResult{
		Title:             "Quarterly Review",
		Channel:           "Deneb Channel",
		ChannelURL:        "https://youtube.com/@deneb",
		Duration:          "1:02:03",
		UploadDate:        "20260711",
		ViewCount:         123456,
		Category:          "Business",
		Keywords:          keywords,
		IsLive:            true,
		URL:               "https://youtu.be/abcdefghijk",
		AvailableCaptions: []string{"ko", "en (auto)"},
		Chapters: []YouTubeChapter{
			{StartSec: 0, Title: "Intro"},
			{StartSec: 65, Title: "Main"},
		},
		Description: "Detailed description",
	}
	got := FormatYouTubeMeta(result)
	for _, want := range []string{
		"## YouTube 비디오 정보",
		"**제목:** Quarterly Review",
		"**채널:** Deneb Channel (https://youtube.com/@deneb)",
		"**길이:** 1:02:03 (라이브)",
		"**업로드:** 2026-07-11",
		"**조회수:** 12.3만회",
		"**카테고리:** Business",
		"**URL:** https://youtu.be/abcdefghijk",
		"**제공 자막:** ko, en (auto)",
		"### 챕터",
		"-   Intro",
		"- 1:05  Main",
		"### 설명",
		"Detailed description",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("metadata missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "tag-15") {
		t.Fatal("fifteenth keyword missing")
	}
	if strings.Contains(got, "tag-16") || strings.Contains(got, "tag-18") {
		t.Fatal("keywords beyond cap leaked")
	}
}

func TestFormatYouTubeMetaSparseContract(t *testing.T) {
	got := FormatYouTubeMeta(&YouTubeResult{})
	if got != "## YouTube 비디오 정보\n\n" {
		t.Fatalf("zero metadata = %q", got)
	}
	result := &YouTubeResult{Channel: "Channel"}
	got = FormatYouTubeMeta(result)
	if !strings.Contains(got, "**채널:** Channel\n") || strings.Contains(got, "()") {
		t.Fatalf("channel without URL = %q", got)
	}
	result = &YouTubeResult{Duration: "2:00"}
	got = FormatYouTubeMeta(result)
	if !strings.Contains(got, "**길이:** 2:00\n") || strings.Contains(got, "라이브") {
		t.Fatalf("non-live duration = %q", got)
	}
	result = &YouTubeResult{ViewCount: -1}
	got = FormatYouTubeMeta(result)
	if strings.Contains(got, "조회수") {
		t.Fatalf("negative view count rendered: %q", got)
	}
}

func TestFormatYouTubeResultTranscriptModes(t *testing.T) {
	t.Run("no transcript", func(t *testing.T) {
		for _, transcript := range []string{"", noTranscriptMarker} {
			got := FormatYouTubeResult(&YouTubeResult{Title: "Video", Transcript: transcript})
			if !strings.Contains(got, "(자막 없음)") || strings.Contains(got, "### 자막 (") {
				t.Fatalf("result = %q", got)
			}
		}
	})

	t.Run("plain transcript and unknown language", func(t *testing.T) {
		got := FormatYouTubeResult(&YouTubeResult{Transcript: "plain transcript"})
		if !strings.Contains(got, "### 자막 (unknown)") || !strings.Contains(got, "plain transcript") {
			t.Fatalf("result = %q", got)
		}
	})

	t.Run("timestamped segments replace plain body", func(t *testing.T) {
		got := FormatYouTubeResult(&YouTubeResult{
			Transcript: "plain fallback",
			Language:   "ko",
			Segments: []TranscriptSegment{
				{StartSec: 0, Text: "first"},
				{StartSec: 35, Text: "second"},
			},
		})
		if !strings.Contains(got, "[0:00] first") || !strings.Contains(got, "[0:35] second") {
			t.Fatalf("timestamped result = %q", got)
		}
		if strings.Contains(got, "plain fallback") {
			t.Fatal("plain fallback leaked despite segments")
		}
	})

	t.Run("chapters take precedence over flat timestamps", func(t *testing.T) {
		got := FormatYouTubeResult(&YouTubeResult{
			Transcript: "plain fallback",
			Language:   "en",
			Chapters: []YouTubeChapter{
				{StartSec: 0, Title: "Intro"},
				{StartSec: 60, Title: "Main"},
			},
			Segments: []TranscriptSegment{
				{StartSec: 0, Text: "first"},
				{StartSec: 65, Text: "second"},
			},
		})
		if !strings.Contains(got, "#### [0:00] Intro") || !strings.Contains(got, "#### [1:00] Main") {
			t.Fatalf("chaptered result = %q", got)
		}
	})
}

func TestFormatTimestampedTranscriptBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		segments []TranscriptSegment
		want     string
	}{
		{name: "nil", segments: nil, want: ""},
		{name: "blank skipped", segments: []TranscriptSegment{{StartSec: 0, Text: "  "}}, want: ""},
		{name: "negative clamps marker clock", segments: []TranscriptSegment{{StartSec: -5, Text: "before"}}, want: "before"},
		{name: "within bucket single line", segments: []TranscriptSegment{{StartSec: 0, Text: "a"}, {StartSec: 10, Text: "b"}, {StartSec: 29, Text: "c"}}, want: "[0:00] a b c"},
		{name: "boundary starts new line", segments: []TranscriptSegment{{StartSec: 0, Text: "a"}, {StartSec: 30, Text: "b"}}, want: "[0:00] a\n[0:30] b"},
		{name: "late first marker", segments: []TranscriptSegment{{StartSec: 65, Text: "late"}}, want: "[1:05] late"},
		{name: "consecutive duplicate skipped", segments: []TranscriptSegment{{StartSec: 0, Text: "same"}, {StartSec: 5, Text: " same "}, {StartSec: 10, Text: "next"}}, want: "[0:00] same next"},
		{name: "nonconsecutive duplicate retained", segments: []TranscriptSegment{{StartSec: 0, Text: "same"}, {StartSec: 5, Text: "other"}, {StartSec: 10, Text: "same"}}, want: "[0:00] same other same"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTimestampedTranscript(tt.segments); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatTimestampedTranscriptCapsPathologicalText(t *testing.T) {
	segments := make([]TranscriptSegment, 0, 100)
	for i := 0; i < 100; i++ {
		segments = append(segments, TranscriptSegment{StartSec: i * 30, Text: fmt.Sprintf("%03d-%s", i, strings.Repeat("x", 900))})
	}
	got := formatTimestampedTranscript(segments)
	if !strings.Contains(got, "[...자막이 잘렸습니다]") {
		t.Fatal("truncation marker missing")
	}
	if len(got) > 62_000 {
		t.Fatalf("timestamped output = %d bytes, expected bounded", len(got))
	}
}

func TestFormatChapteredTranscriptAssignmentContract(t *testing.T) {
	chapters := []YouTubeChapter{
		{StartSec: 0, Title: "Intro"},
		{StartSec: 60, Title: "Main"},
		{StartSec: 120, Title: "Outro"},
	}
	segments := []TranscriptSegment{
		{StartSec: 0, Text: "start"},
		{StartSec: 59, Text: "end intro"},
		{StartSec: 60, Text: "start main"},
		{StartSec: 119, Text: "end main"},
		{StartSec: 120, Text: "start outro"},
	}
	got := formatChapteredTranscript(chapters, segments)
	introStart := strings.Index(got, "#### [0:00] Intro")
	mainStart := strings.Index(got, "#### [1:00] Main")
	outroStart := strings.Index(got, "#### [2:00] Outro")
	if introStart < 0 || mainStart < 0 || outroStart < 0 || !(introStart < mainStart && mainStart < outroStart) {
		t.Fatalf("chapter order:\n%s", got)
	}
	if strings.Index(got, "start main") < mainStart || strings.Index(got, "start main") > outroStart {
		t.Fatalf("boundary segment assigned incorrectly:\n%s", got)
	}
	if strings.Index(got, "start outro") < outroStart {
		t.Fatalf("outro segment assigned incorrectly:\n%s", got)
	}
	if got := formatChapteredTranscript(nil, segments); got != "" {
		t.Fatalf("nil chapters = %q", got)
	}
	if got := formatChapteredTranscript(chapters, nil); got != "" {
		t.Fatalf("nil segments = %q", got)
	}
}

func TestFormatClockAndDurationBoundaryContract(t *testing.T) {
	tests := []struct {
		seconds      int
		wantClock    string
		wantDuration string
	}{
		{seconds: -1, wantClock: "0:00", wantDuration: ""},
		{seconds: 0, wantClock: "0:00", wantDuration: ""},
		{seconds: 1, wantClock: "0:01", wantDuration: "0:01"},
		{seconds: 59, wantClock: "0:59", wantDuration: "0:59"},
		{seconds: 60, wantClock: "1:00", wantDuration: "1:00"},
		{seconds: 61, wantClock: "1:01", wantDuration: "1:01"},
		{seconds: 3599, wantClock: "59:59", wantDuration: "59:59"},
		{seconds: 3600, wantClock: "1:00:00", wantDuration: "1:00:00"},
		{seconds: 3661, wantClock: "1:01:01", wantDuration: "1:01:01"},
		{seconds: 36_000, wantClock: "10:00:00", wantDuration: "10:00:00"},
	}
	for _, tt := range tests {
		if got := formatClock(tt.seconds); got != tt.wantClock {
			t.Errorf("formatClock(%d) = %q, want %q", tt.seconds, got, tt.wantClock)
		}
		if got := formatDuration(tt.seconds); got != tt.wantDuration {
			t.Errorf("formatDuration(%d) = %q, want %q", tt.seconds, got, tt.wantDuration)
		}
	}
}

func TestFormatUploadDateContract(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "20260711", want: "2026-07-11"},
		{in: "12345678", want: "1234-56-78"},
		{in: "2026-07-11", want: "2026-07-11"},
		{in: "2026071", want: "2026071"},
		{in: "202607110", want: "202607110"},
	} {
		if got := formatUploadDate(tt.in); got != tt.want {
			t.Errorf("formatUploadDate(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatViewCountBoundaries(t *testing.T) {
	for _, tt := range []struct {
		views int64
		want  string
	}{
		{views: -1, want: "-1회"},
		{views: 0, want: "0회"},
		{views: 1, want: "1회"},
		{views: 9_999, want: "9999회"},
		{views: 10_000, want: "1.0만회"},
		{views: 15_000, want: "1.5만회"},
		{views: 99_999_999, want: "10000.0만회"},
		{views: 100_000_000, want: "1억회"},
		{views: 250_000_000, want: "2억회"},
	} {
		if got := formatViewCount(tt.views); got != tt.want {
			t.Errorf("formatViewCount(%d) = %q, want %q", tt.views, got, tt.want)
		}
	}
}

func TestTruncateStringUnicodeContract(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{name: "empty", input: "", max: 3, want: ""},
		{name: "under ascii", input: "abc", max: 5, want: "abc"},
		{name: "exact ascii", input: "abc", max: 3, want: "abc"},
		{name: "over ascii", input: "abcdef", max: 3, want: "abc..."},
		{name: "under unicode", input: "가나다", max: 3, want: "가나다"},
		{name: "over unicode", input: "가나다라", max: 3, want: "가나다..."},
		{name: "emoji", input: "😀😃😄😁", max: 2, want: "😀😃..."},
		{name: "zero nonempty", input: "abc", max: 0, want: "..."},
		{name: "negative nonempty", input: "abc", max: -1, want: "..."},
		{name: "zero empty", input: "", max: 0, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.max)
			if got != tt.want || !utf8.ValidString(got) {
				t.Fatalf("got %q, want %q valid=%v", got, tt.want, utf8.ValidString(got))
			}
		})
	}
}

func TestCleanSubtitleTextContractMatrix(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "headers", raw: "WEBVTT\nKind: captions\nLanguage: en\nNOTE generated\n", want: ""},
		{name: "srt cue", raw: "1\n00:00:00,000 --> 00:00:01,000\nHello\n", want: "Hello"},
		{name: "vtt cue", raw: "00:00:00.000 --> 00:00:01.000\nHello\n", want: "Hello"},
		{name: "tags stripped", raw: "<c.green>Hello</c> <b>world</b>", want: "Hello world"},
		{name: "blank after tags", raw: "<c></c>\ntext", want: "text"},
		{name: "consecutive duplicates", raw: "same\nsame\nnext", want: "same\nnext"},
		{name: "nonconsecutive duplicates retained", raw: "same\nother\nsame", want: "same\nother\nsame"},
		{name: "numeric content discarded as cue id", raw: "123\ntext", want: "text"},
		{name: "mixed alphanumeric retained", raw: "123a", want: "123a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanSubtitleText(tt.raw); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanSubtitleTextTruncatesAtUnicodeRuneCap(t *testing.T) {
	input := strings.Repeat("가", 50_001)
	got := cleanSubtitleText(input)
	if !utf8.ValidString(got) {
		t.Fatal("truncated subtitle is invalid UTF-8")
	}
	if !strings.HasSuffix(got, "[...자막이 50,000자에서 잘렸습니다]") {
		t.Fatal("truncation marker missing")
	}
	prefix := strings.SplitN(got, "\n\n", 2)[0]
	if len([]rune(prefix)) != 50_000 {
		t.Fatalf("prefix runes = %d, want 50000", len([]rune(prefix)))
	}
}

func TestNumericLineContract(t *testing.T) {
	for input, want := range map[string]bool{
		"":    false,
		"0":   true,
		"123": true,
		"001": true,
		" 1":  false,
		"1 ":  false,
		"-1":  false,
		"1.0": false,
		"１２３": false,
		"1a":  false,
	} {
		if got := isNumericLine(input); got != want {
			t.Errorf("isNumericLine(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestYouTubeURLExtractionContract(t *testing.T) {
	id1, id2 := "abcdefghijk", "ABCDEFGHIJK"
	text := strings.Join([]string{
		"watch https://www.youtube.com/watch?v=" + id1,
		"duplicate https://www.youtube.com/watch?v=" + id1,
		"short https://youtu.be/" + id2,
		"bare youtube.com/shorts/12345678901",
		"live https://youtube.com/live/zyxwvutsrqp",
		"extra https://youtu.be/11111111111",
		"capped https://youtu.be/22222222222",
	}, " ")
	got := ExtractYouTubeURLs(text)
	want := []string{
		"https://www.youtube.com/watch?v=" + id1,
		"https://youtu.be/" + id2,
		"youtube.com/shorts/12345678901",
		"https://youtube.com/live/zyxwvutsrqp",
		"https://youtu.be/11111111111",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("urls = %#v, want %#v", got, want)
	}
	for _, url := range want {
		if !IsYouTubeURL(url) {
			t.Errorf("URL not recognized: %s", url)
		}
	}
	for _, notURL := range []string{"", "youtube.com/channel/abcdefghijk", "youtu.be/short", "https://example.com/watch?v=abcdefghijk"} {
		if IsYouTubeURL(notURL) {
			t.Errorf("non-video recognized: %q", notURL)
		}
	}
}

func TestVideoIDValidationContract(t *testing.T) {
	for _, valid := range []string{"abcdefghijk", "ABCDEFGHIJK", "12345678901", "abc_def-123", "-----------", "___________"} {
		if !isVideoID(valid) {
			t.Errorf("valid ID rejected: %q", valid)
		}
		if got := extractVideoID(valid); got != valid {
			t.Errorf("extractVideoID(%q) = %q", valid, got)
		}
	}
	for _, invalid := range []string{"", "abc defghij", "abcdefghij!", "한글abcdefgh", "abcdefghij/"} {
		if isVideoID(invalid) {
			t.Errorf("invalid chars accepted: %q", invalid)
		}
		if len(invalid) == 11 && extractVideoID(invalid) != "" {
			t.Errorf("invalid bare ID extracted: %q", invalid)
		}
	}
}

func TestInnertubeKeyAndVersionUseEnvOverrideWhenSet(t *testing.T) {
	t.Setenv("DENEB_YT_INNERTUBE_KEY", "")
	t.Setenv("DENEB_YT_INNERTUBE_CLIENT_VERSION", "")
	if got := innertubeKey(); got != innertubeWebKey {
		t.Fatalf("default key = %q", got)
	}
	if got := innertubeVersion(); got != innertubeClientVersion {
		t.Fatalf("default version = %q", got)
	}
	t.Setenv("DENEB_YT_INNERTUBE_KEY", "  override-key  ")
	t.Setenv("DENEB_YT_INNERTUBE_CLIENT_VERSION", "  version-1  ")
	if got := innertubeKey(); got != "override-key" {
		t.Fatalf("override key = %q", got)
	}
	if got := innertubeVersion(); got != "version-1" {
		t.Fatalf("override version = %q", got)
	}
}

func TestChannelURLContractAdditional(t *testing.T) {
	for _, tt := range []struct {
		name    string
		id      string
		profile string
		want    string
	}{
		{name: "https profile", id: "UC1", profile: "https://youtube.com/@name", want: "https://youtube.com/@name"},
		{name: "http profile", id: "UC1", profile: "http://youtube.com/@name", want: "http://youtube.com/@name"},
		{name: "relative slash", id: "UC1", profile: "/@name", want: "https://www.youtube.com/@name"},
		{name: "relative no slash", id: "UC1", profile: "@name", want: "https://www.youtube.com@name"},
		{name: "trim profile", id: "UC1", profile: "  /@name  ", want: "https://www.youtube.com/@name"},
		{name: "id fallback", id: "UC1", want: "https://www.youtube.com/channel/UC1"},
		{name: "empty", want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := channelURL(tt.id, tt.profile); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBestThumbnailContractAdditional(t *testing.T) {
	type thumb = struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	tests := []struct {
		name   string
		thumbs []thumb
		want   string
	}{
		{name: "nil", want: ""},
		{name: "one", thumbs: []thumb{{URL: "a", Width: 10, Height: 10}}, want: "a"},
		{name: "largest area", thumbs: []thumb{{URL: "wide", Width: 100, Height: 20}, {URL: "square", Width: 50, Height: 50}}, want: "square"},
		{name: "empty URL skipped", thumbs: []thumb{{URL: "a", Width: 10, Height: 10}, {URL: "", Width: 100, Height: 100}}, want: "a"},
		{name: "tie keeps first", thumbs: []thumb{{URL: "first", Width: 20, Height: 10}, {URL: "second", Width: 10, Height: 20}}, want: "first"},
		{name: "zero dimensions accepted", thumbs: []thumb{{URL: "zero", Width: 0, Height: 0}}, want: "zero"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bestThumbnail(tt.thumbs); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCaptionLabelAndSelectionContract(t *testing.T) {
	tracks := []captionTrack{
		{BaseURL: "ko-manual", LanguageCode: "ko-KR"},
		{BaseURL: "en-manual", LanguageCode: "en-US"},
		{BaseURL: "ko-auto", LanguageCode: "ko", Kind: "asr"},
		{BaseURL: "ja-auto", LanguageCode: "ja", Kind: "asr"},
		{BaseURL: "unknown", LanguageCode: ""},
	}
	labels := availableCaptionLabels(tracks)
	wantLabels := []string{"ko-KR", "en-US", "ko (auto)", "ja (auto)", "unknown"}
	if !reflect.DeepEqual(labels, wantLabels) {
		t.Fatalf("labels = %v, want %v", labels, wantLabels)
	}
	url, label := selectCaptionTrack(tracks)
	if url != "ko-manual" || label != "ko-KR" {
		t.Fatalf("selected = %q/%q", url, label)
	}
	for input, want := range map[string]string{
		"en-US": "en", " EN-gb ": "en", "ko": "ko", "zh-Hant-TW": "zh", "": "",
	} {
		if got := baseLang(input); got != want {
			t.Errorf("baseLang(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTimedTextSegmentsContractAdditional(t *testing.T) {
	doc := timedTextDoc{}
	doc.Texts = append(
		doc.Texts,
		struct {
			Start   string `xml:"start,attr"`
			Dur     string `xml:"dur,attr"`
			Content string `xml:",chardata"`
		}{Start: "1.9", Content: " A &amp; B "},
		struct {
			Start   string `xml:"start,attr"`
			Dur     string `xml:"dur,attr"`
			Content string `xml:",chardata"`
		}{Start: "bad", Content: "bad time"},
		struct {
			Start   string `xml:"start,attr"`
			Dur     string `xml:"dur,attr"`
			Content string `xml:",chardata"`
		}{Start: "3", Content: "   "},
	)
	doc.Paras = append(
		doc.Paras,
		srv3Para{TMs: "2500", Content: " paragraph "},
		srv3Para{TMs: "bad", Content: "bad ms"},
	)
	got := doc.segments()
	want := []TranscriptSegment{
		{StartSec: 1, Text: "A & B"},
		{StartSec: 0, Text: "bad time"},
		{StartSec: 2, Text: "paragraph"},
		{StartSec: 0, Text: "bad ms"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("segments = %+v, want %+v", got, want)
	}
}

func TestSegmentsToPlainTextContractAdditional(t *testing.T) {
	segments := []TranscriptSegment{
		{StartSec: 0, Text: "first"},
		{StartSec: 1, Text: ""},
		{StartSec: 2, Text: "first"},
		{StartSec: 3, Text: "second"},
	}
	if got := segmentsToPlainText(segments); got != "first\nsecond" {
		t.Fatalf("plain = %q", got)
	}
	if got := segmentsToPlainText(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
}

func TestChapterParsingContractAdditional(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        []YouTubeChapter
	}{
		{
			name:        "bullets and punctuation",
			description: "- 0:00 - Intro\n* 1:30 Main\n• (2:45) Outro",
			want:        []YouTubeChapter{{StartSec: 0, Title: "Intro"}, {StartSec: 90, Title: "Main"}, {StartSec: 165, Title: "Outro"}},
		},
		{
			name:        "hour timestamps",
			description: "0:00 Intro\n1:00:00 Long section\n1:30:00 End",
			want:        []YouTubeChapter{{StartSec: 0, Title: "Intro"}, {StartSec: 3600, Title: "Long section"}, {StartSec: 5400, Title: "End"}},
		},
		{name: "fewer than three", description: "0:00 Intro\n1:00 End", want: nil},
		{name: "first not zero", description: "0:01 Intro\n1:00 Main\n2:00 End", want: nil},
		{name: "decreasing", description: "0:00 Intro\n2:00 Main\n1:00 End", want: nil},
		{name: "no matches", description: "plain description", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseChaptersFromDescription(tt.description); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTimestampParsingContractAdditional(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  int
	}{
		{input: "0:00", want: 0},
		{input: "00:01", want: 1},
		{input: "1:00", want: 60},
		{input: "1:02:03", want: 3723},
		{input: "10:20:30", want: 37230},
		{input: "bad", want: 0},
		{input: "1:bad", want: 60},
		{input: "", want: 0},
	} {
		if got := parseTimestampToSec(tt.input); got != tt.want {
			t.Errorf("parseTimestampToSec(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeUploadDateContractAdditional(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  string
	}{
		{input: "2026-07-11", want: "20260711"},
		{input: "2026-07-11T10:20:30Z", want: "20260711"},
		{input: "20260711", want: "20260711"},
		{input: "date 2026/07/11", want: "20260711"},
		{input: "123456789", want: "12345678"},
		{input: "2026-07", want: ""},
		{input: "", want: ""},
	} {
		if got := normalizeUploadDate(tt.input); got != tt.want {
			t.Errorf("normalizeUploadDate(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFirstNonEmptyContractAdditional(t *testing.T) {
	if got := firstNonEmpty("", "  ", "value", "later"); got != "value" {
		t.Fatalf("got %q", got)
	}
	if got := firstNonEmpty("  spaced  ", "later"); got != "  spaced  " {
		t.Fatalf("original spacing was not preserved: %q", got)
	}
	if got := firstNonEmpty(); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestYTPlayerClientArgsContract(t *testing.T) {
	t.Setenv("DENEB_YTDLP_PLAYER_CLIENT", "")
	if got := ytPlayerClientArgs(); got != nil {
		t.Fatalf("empty args = %v", got)
	}
	t.Setenv("DENEB_YTDLP_PLAYER_CLIENT", "  web,android  ")
	want := []string{"--extractor-args", "youtube:player_client=web,android"}
	if got := ytPlayerClientArgs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestASRAudioCapContract(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
		want  int
	}{
		{name: "empty", value: "", want: 600},
		{name: "spaces", value: "  ", want: 600},
		{name: "valid", value: "120", want: 120},
		{name: "trimmed", value: " 45 ", want: 45},
		{name: "zero", value: "0", want: 600},
		{name: "negative", value: "-1", want: 600},
		{name: "invalid", value: "abc", want: 600},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DENEB_YT_ASR_CAP_SEC", tt.value)
			if got := asrAudioCapSec(); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestASRUsableContract(t *testing.T) {
	oldTranscriber, oldReady := AudioTranscriber, AudioTranscriberReady
	t.Cleanup(func() {
		AudioTranscriber = oldTranscriber
		AudioTranscriberReady = oldReady
	})
	ctx := context.Background()
	AudioTranscriber = nil
	AudioTranscriberReady = nil
	if asrUsable(ctx) {
		t.Fatal("nil transcriber reported usable")
	}
	AudioTranscriber = func(context.Context, string) (string, error) { return "text", nil }
	if !asrUsable(ctx) {
		t.Fatal("wired transcriber without probe reported unusable")
	}
	AudioTranscriberReady = func(context.Context) bool { return false }
	if asrUsable(ctx) {
		t.Fatal("failed readiness probe reported usable")
	}
	AudioTranscriberReady = func(context.Context) bool { return true }
	if !asrUsable(ctx) {
		t.Fatal("ready transcriber reported unusable")
	}
}

func TestSelectFrameCountBoundaryValuesMapToExpectedBuckets(t *testing.T) {
	for _, tt := range []struct {
		duration int
		want     int
	}{
		{duration: -1, want: 1},
		{duration: 0, want: 1},
		{duration: 3, want: 1},
		{duration: 4, want: 3},
		{duration: 10, want: 3},
		{duration: 11, want: 4},
		{duration: 60, want: 4},
		{duration: 61, want: 6},
	} {
		if got := selectFrameCount(tt.duration); got != tt.want {
			t.Errorf("selectFrameCount(%d) = %d, want %d", tt.duration, got, tt.want)
		}
	}
}

func TestSelectTimestampsContractAdditional(t *testing.T) {
	tests := []struct {
		name     string
		duration int
		count    int
		want     []float64
	}{
		{name: "unknown", duration: 0, count: 4, want: []float64{0.5}},
		{name: "negative", duration: -1, count: 4, want: []float64{0.5}},
		{name: "one", duration: 10, count: 1, want: []float64{5}},
		{name: "two short", duration: 2, count: 2, want: []float64{0.5, 1.5}},
		{name: "two medium", duration: 10, count: 2, want: []float64{0.5, 9.5}},
		{name: "two long offset capped", duration: 100, count: 2, want: []float64{2, 98}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectTimestamps(tt.duration, tt.count)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if math.Abs(got[i]-tt.want[i]) > 1e-9 {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestWatchTimestampWindowContractAdditional(t *testing.T) {
	tests := []struct {
		name     string
		duration int
		count    int
		start    float64
		end      float64
		wantLen  int
		min      float64
		max      float64
	}{
		{name: "count zero becomes one", duration: 10, count: 0, wantLen: 1, min: 0, max: 10},
		{name: "negative start clamps", duration: 10, count: 3, start: -5, wantLen: 3, min: 0, max: 10},
		{name: "end over duration clamps", duration: 10, count: 3, start: 2, end: 20, wantLen: 3, min: 2, max: 10},
		{name: "explicit window", duration: 100, count: 5, start: 20, end: 30, wantLen: 5, min: 20, max: 30},
		{name: "inverted expands from start", duration: 100, count: 4, start: 30, end: 20, wantLen: 4, min: 30, max: 34},
		{name: "unknown duration", duration: 0, count: 4, wantLen: 4, min: 0, max: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectWatchTimestamps(tt.duration, tt.count, tt.start, tt.end)
			if len(got) != tt.wantLen {
				t.Fatalf("len = %d, want %d: %v", len(got), tt.wantLen, got)
			}
			for i, ts := range got {
				if ts < tt.min-1e-9 || ts > tt.max+1e-9 {
					t.Errorf("timestamp %f outside %f..%f", ts, tt.min, tt.max)
				}
				if i > 0 && ts < got[i-1] {
					t.Errorf("timestamps not sorted: %v", got)
				}
			}
		})
	}
}

func TestEvenSampleTimestampsContractAdditional(t *testing.T) {
	candidates := []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for _, tt := range []struct {
		name string
		n    int
		want []float64
	}{
		{name: "negative unchanged", n: -1, want: candidates},
		{name: "zero unchanged", n: 0, want: candidates},
		{name: "one", n: 1, want: []float64{0}},
		{name: "two endpoints", n: 2, want: []float64{0, 10}},
		{name: "three", n: 3, want: []float64{0, 5, 10}},
		{name: "over unchanged", n: 20, want: candidates},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := evenSampleTimestamps(candidates, tt.n); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseShowinfoTimesAdditional(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"pts_time:5.5",
		"pts_time:1",
		"noise pts_time:3.25 other",
		"pts_time:1",
		"pts_time:not-number",
		"pts_time:10.000",
	}, "\n"))
	got := parseShowinfoTimes(raw)
	want := []float64{1, 3.25, 5.5, 10}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFetchYouTubeMetadataWithFakeCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess contract")
	}
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	script := filepath.Join(dir, "yt-dlp")
	content := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_PATH\"\nprintf '%s' '{\"title\":\"Video\",\"channel\":\"Channel\",\"duration\":65,\"upload_date\":\"20260711\",\"view_count\":42,\"description\":\"Desc\"}'\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARGS_PATH", argsPath)
	t.Setenv("DENEB_YTDLP_PLAYER_CLIENT", "web")
	got, err := fetchYouTubeMetadata(context.Background(), script, "https://youtu.be/abcdefghijk")
	if err != nil {
		t.Fatal(err)
	}
	want := &ytMetadata{Title: "Video", Channel: "Channel", Duration: 65, UploadDate: "20260711", ViewCount: 42, Description: "Desc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata = %+v, want %+v", got, want)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, wantArg := range []string{"--dump-json", "--no-download", "--no-warnings", "--no-playlist", "--extractor-args", "youtube:player_client=web", "https://youtu.be/abcdefghijk"} {
		if !strings.Contains(string(args), wantArg) {
			t.Errorf("args missing %q: %s", wantArg, args)
		}
	}
}

func TestFetchYouTubeMetadataFailureContract(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess contract")
	}
	dir := t.TempDir()
	failing := filepath.Join(dir, "fail")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\nexit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := fetchYouTubeMetadata(context.Background(), failing, "url"); err == nil || !strings.Contains(err.Error(), "--dump-json failed") {
		t.Fatalf("failure error = %v", err)
	}
	badJSON := filepath.Join(dir, "bad-json")
	if err := os.WriteFile(badJSON, []byte("#!/bin/sh\nprintf 'not-json'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := fetchYouTubeMetadata(context.Background(), badJSON, "url"); err == nil || !strings.Contains(err.Error(), "parse metadata") {
		t.Fatalf("JSON error = %v", err)
	}
}

func TestRunSubsAttemptWithFakeCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess contract")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "write-subs")
	content := "#!/bin/sh\nprintf '%b' 'WEBVTT\\n\\n00:00:00.000 --> 00:00:01.000\\nHello world\\n' > \"$SUB_PATH\"\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUB_PATH", filepath.Join(dir, "subs.en.vtt"))
	text, limited := runSubsAttempt(context.Background(), script, nil, dir)
	if text != "Hello world" || limited {
		t.Fatalf("result = %q/%v", text, limited)
	}

	limitedScript := filepath.Join(dir, "limited")
	if err := os.WriteFile(limitedScript, []byte("#!/bin/sh\necho 'HTTP Error 429: Too Many Requests' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	text, limited = runSubsAttempt(context.Background(), limitedScript, nil, t.TempDir())
	if text != "" || !limited {
		t.Fatalf("limited result = %q/%v", text, limited)
	}

	ordinary := filepath.Join(dir, "ordinary")
	if err := os.WriteFile(ordinary, []byte("#!/bin/sh\necho 'no captions' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	text, limited = runSubsAttempt(context.Background(), ordinary, nil, t.TempDir())
	if text != "" || limited {
		t.Fatalf("ordinary result = %q/%v", text, limited)
	}
}

func TestExtractTranscriptNativeRejectsBadIDWithoutNetwork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	for _, input := range []string{"", "not a video", "https://example.com", "short"} {
		if got := extractTranscriptNative(ctx, input); got != nil {
			t.Errorf("input %q returned %+v", input, got)
		}
	}
}

func TestExtractTranscriptNativeBoundedReturnsNilWithoutNetworkOnTightASRBudget(t *testing.T) {
	oldTranscriber, oldReady := AudioTranscriber, AudioTranscriberReady
	t.Cleanup(func() {
		AudioTranscriber = oldTranscriber
		AudioTranscriberReady = oldReady
	})
	AudioTranscriber = func(context.Context, string) (string, error) { return "text", nil }
	AudioTranscriberReady = func(context.Context) bool { return true }
	ctx, cancel := context.WithTimeout(context.Background(), asrReserveBudget+minSubtitleBudget+minNativeBudget-time.Second)
	defer cancel()
	start := time.Now()
	if got := extractTranscriptNativeBounded(ctx, "https://youtu.be/abcdefghijk"); got != nil {
		t.Fatalf("tight budget result = %+v", got)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("tight budget performed network work: %s", elapsed)
	}
}

func TestTranscriptViaASRSkipsBeforeDownloadWhenUnavailable(t *testing.T) {
	oldTranscriber, oldReady := AudioTranscriber, AudioTranscriberReady
	t.Cleanup(func() {
		AudioTranscriber = oldTranscriber
		AudioTranscriberReady = oldReady
	})
	for _, tt := range []struct {
		name        string
		transcriber func(context.Context, string) (string, error)
		ready       func(context.Context) bool
	}{
		{name: "unwired"},
		{name: "not ready", transcriber: func(context.Context, string) (string, error) { return "text", nil }, ready: func(context.Context) bool { return false }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			AudioTranscriber = tt.transcriber
			AudioTranscriberReady = tt.ready
			text, lang := transcriptViaASR(context.Background(), "/definitely/missing/yt-dlp", "url", t.TempDir(), 0, 0, 100)
			if text != "" || lang != "" {
				t.Fatalf("result = %q/%q", text, lang)
			}
		})
	}
}

func TestTranscriptViaASRReturnsEmptyWithoutCallingTranscriberOnTightDeadline(t *testing.T) {
	oldTranscriber, oldReady := AudioTranscriber, AudioTranscriberReady
	t.Cleanup(func() {
		AudioTranscriber = oldTranscriber
		AudioTranscriberReady = oldReady
	})
	called := false
	AudioTranscriber = func(context.Context, string) (string, error) {
		called = true
		return "text", nil
	}
	AudioTranscriberReady = func(context.Context) bool { return true }
	ctx, cancel := context.WithTimeout(context.Background(), asrOverheadBudget+time.Second)
	defer cancel()
	text, lang := transcriptViaASR(ctx, "/missing", "url", t.TempDir(), 0, 0, 100)
	if text != "" || lang != "" || called {
		t.Fatalf("result = %q/%q called=%v", text, lang, called)
	}
}

func TestErrorSentinelAssumptions(t *testing.T) {
	if !errors.Is(errors.Join(errors.New("x"), context.Canceled), context.Canceled) {
		t.Fatal("context cancellation assumption changed")
	}
}

func TestYouTubeJSONShapeContract(t *testing.T) {
	result := YouTubeResult{
		Title:       "Video",
		DurationSec: 65,
		ChannelID:   "UC1",
		Chapters:    []YouTubeChapter{{StartSec: 0, Title: "Intro"}},
		Segments:    []TranscriptSegment{{StartSec: 0, Text: "Hello"}},
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"title":"Video"`, `"duration_sec":65`, `"channel_id":"UC1"`, `"chapters"`, `"segments"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("JSON missing %s: %s", key, raw)
		}
	}
}
