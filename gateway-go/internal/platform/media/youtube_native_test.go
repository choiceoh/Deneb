package media

import (
	"encoding/xml"
	"testing"
)

func TestTimedTextSegments(t *testing.T) {
	t.Run("legacy text shape", func(t *testing.T) {
		raw := `<?xml version="1.0"?><transcript>` +
			`<text start="0" dur="2">Hello &amp;amp; welcome</text>` +
			`<text start="2.5" dur="3">It&amp;#39;s great</text>` +
			`<text start="6"></text></transcript>`
		var doc timedTextDoc
		if err := xml.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		segs := doc.segments()
		if len(segs) != 2 {
			t.Fatalf("got %d segs, want 2: %+v", len(segs), segs)
		}
		if segs[0].StartSec != 0 || segs[0].Text != "Hello & welcome" {
			t.Errorf("seg0 = %+v, want {0 Hello & welcome}", segs[0])
		}
		if segs[1].StartSec != 2 || segs[1].Text != "It's great" {
			t.Errorf("seg1 = %+v, want {2 It's great}", segs[1])
		}
	})

	t.Run("srv3 body/p/s shape", func(t *testing.T) {
		raw := `<?xml version="1.0"?><timedtext format="3"><body>` +
			`<p t="0" d="2000"><s>Hello </s><s>world</s></p>` +
			`<p t="2500" d="3000">single chunk</p>` +
			`<p t="6000"></p></body></timedtext>`
		var doc timedTextDoc
		if err := xml.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		segs := doc.segments()
		if len(segs) != 2 {
			t.Fatalf("got %d segs, want 2: %+v", len(segs), segs)
		}
		if segs[0].StartSec != 0 || segs[0].Text != "Hello world" {
			t.Errorf("seg0 = %+v, want {0 Hello world}", segs[0])
		}
		if segs[1].StartSec != 2 || segs[1].Text != "single chunk" {
			t.Errorf("seg1 = %+v, want {2 single chunk}", segs[1])
		}
	})
}

func TestExtractVideoID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://youtu.be/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/shorts/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/live/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"이 영상 https://youtu.be/dQw4w9WgXcQ 봐", "dQw4w9WgXcQ"},
		{"dQw4w9WgXcQ", "dQw4w9WgXcQ"}, // bare ID
		{"https://example.com", ""},
		{"not a url", ""},
		{"", ""},
		{"tooShort", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := extractVideoID(tt.input); got != tt.want {
				t.Errorf("extractVideoID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeUploadDate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2024-01-15", "20240115"},
		{"2024-01-15T00:00:00-07:00", "20240115"},
		{"20240115", "20240115"},
		{"", ""},
		{"2024", ""}, // too few digits
		{"garbage", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeUploadDate(tt.input); got != tt.want {
				t.Errorf("normalizeUploadDate(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSelectCaptionTrack(t *testing.T) {
	manualKo := captionTrack{BaseURL: "u-ko", LanguageCode: "ko"}
	manualEn := captionTrack{BaseURL: "u-en", LanguageCode: "en"}
	manualEnUS := captionTrack{BaseURL: "u-en-us", LanguageCode: "en-US"}
	manualJa := captionTrack{BaseURL: "u-ja", LanguageCode: "ja"}
	autoKo := captionTrack{BaseURL: "u-ko-auto", LanguageCode: "ko", Kind: "asr"}
	autoEn := captionTrack{BaseURL: "u-en-auto", LanguageCode: "en", Kind: "asr"}
	autoJa := captionTrack{BaseURL: "u-ja-auto", LanguageCode: "ja", Kind: "asr"}

	tests := []struct {
		name      string
		tracks    []captionTrack
		wantURL   string
		wantLabel string
	}{
		{"prefers ko manual", []captionTrack{autoKo, manualEn, manualKo}, "u-ko", "ko"},
		{"en manual over auto", []captionTrack{autoKo, manualEn}, "u-en", "en"},
		{"regional en-US manual matches en, beats auto", []captionTrack{autoKo, manualEnUS}, "u-en-us", "en-US"},
		{"ko auto when no manual", []captionTrack{autoEn, autoKo}, "u-ko-auto", "ko (auto)"},
		{"en auto next", []captionTrack{autoEn, autoJa}, "u-en-auto", "en (auto)"},
		{"any manual before any auto", []captionTrack{autoJa, manualJa}, "u-ja", "ja"},
		{"any auto last", []captionTrack{autoJa}, "u-ja-auto", "ja (auto)"},
		{"empty baseURL skipped", []captionTrack{{LanguageCode: "ko"}, manualEn}, "u-en", "en"},
		{"none", nil, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotLabel := selectCaptionTrack(tt.tracks)
			if gotURL != tt.wantURL || gotLabel != tt.wantLabel {
				t.Errorf("selectCaptionTrack() = (%q, %q), want (%q, %q)", gotURL, gotLabel, tt.wantURL, tt.wantLabel)
			}
		})
	}
}

func TestParseTimestampToSec(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"0:00", 0},
		{"1:23", 83},
		{"12:34", 754},
		{"1:02:03", 3723},
		{"10:00:00", 36000},
	}
	for _, tt := range tests {
		if got := parseTimestampToSec(tt.in); got != tt.want {
			t.Errorf("parseTimestampToSec(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseChaptersFromDescription(t *testing.T) {
	t.Run("valid chapters", func(t *testing.T) {
		desc := "Welcome!\n\n0:00 Intro\n1:30 - First topic\n5:45 Second topic\n12:00 Wrap up\n\nThanks for watching"
		ch := parseChaptersFromDescription(desc)
		if len(ch) != 4 {
			t.Fatalf("got %d chapters, want 4: %+v", len(ch), ch)
		}
		if ch[0].StartSec != 0 || ch[0].Title != "Intro" {
			t.Errorf("ch[0] = %+v, want {0 Intro}", ch[0])
		}
		if ch[1].StartSec != 90 || ch[1].Title != "First topic" {
			t.Errorf("ch[1] = %+v, want {90 First topic}", ch[1])
		}
		if ch[3].StartSec != 720 {
			t.Errorf("ch[3].StartSec = %d, want 720", ch[3].StartSec)
		}
	})
	t.Run("rejects when first not at 0:00", func(t *testing.T) {
		desc := "1:30 A\n2:00 B\n3:00 C"
		if ch := parseChaptersFromDescription(desc); ch != nil {
			t.Errorf("expected nil (no 0:00 start), got %+v", ch)
		}
	})
	t.Run("rejects fewer than 3", func(t *testing.T) {
		desc := "0:00 A\n1:00 B"
		if ch := parseChaptersFromDescription(desc); ch != nil {
			t.Errorf("expected nil (<3 markers), got %+v", ch)
		}
	})
	t.Run("rejects non-monotonic", func(t *testing.T) {
		desc := "0:00 A\n5:00 B\n2:00 C"
		if ch := parseChaptersFromDescription(desc); ch != nil {
			t.Errorf("expected nil (non-monotonic), got %+v", ch)
		}
	})
	t.Run("no timestamps", func(t *testing.T) {
		if ch := parseChaptersFromDescription("just some text"); ch != nil {
			t.Errorf("expected nil, got %+v", ch)
		}
	})
}

func TestSegmentsToPlainText(t *testing.T) {
	segs := []TranscriptSegment{
		{StartSec: 0, Text: "Hello world"},
		{StartSec: 2, Text: "Hello world"}, // consecutive dup
		{StartSec: 4, Text: "second line"},
		{StartSec: 6, Text: ""},
	}
	got := segmentsToPlainText(segs)
	want := "Hello world\nsecond line"
	if got != want {
		t.Errorf("segmentsToPlainText() = %q, want %q", got, want)
	}
}

func TestFormatTimestampedTranscript(t *testing.T) {
	segs := []TranscriptSegment{
		{StartSec: 0, Text: "intro line"},
		{StartSec: 10, Text: "still intro"},  // same 30s bucket → no new marker
		{StartSec: 35, Text: "next section"}, // new bucket → marker
		{StartSec: 40, Text: "next section"}, // dup → skipped
	}
	got := formatTimestampedTranscript(segs)
	want := "[0:00] intro line still intro\n[0:35] next section"
	if got != want {
		t.Errorf("formatTimestampedTranscript() =\n%q\nwant\n%q", got, want)
	}
	if formatTimestampedTranscript(nil) != "" {
		t.Errorf("expected empty for nil segments")
	}
}

func TestFormatChapteredTranscript(t *testing.T) {
	chapters := []YouTubeChapter{
		{StartSec: 0, Title: "Intro"},
		{StartSec: 60, Title: "Main"},
		{StartSec: 120, Title: "Outro"},
	}
	segs := []TranscriptSegment{
		{StartSec: 0, Text: "hello"},
		{StartSec: 30, Text: "welcome"},
		{StartSec: 65, Text: "main point"},
		{StartSec: 95, Text: "more detail"},
		{StartSec: 125, Text: "thanks"},
	}
	got := formatChapteredTranscript(chapters, segs)
	want := "#### [0:00] Intro\n[0:00] hello\n[0:30] welcome\n\n" +
		"#### [1:00] Main\n[1:05] main point\n[1:35] more detail\n\n" +
		"#### [2:00] Outro\n[2:05] thanks"
	if got != want {
		t.Errorf("formatChapteredTranscript() =\n%q\nwant\n%q", got, want)
	}

	// Empty when no chapters or no segments.
	if formatChapteredTranscript(nil, segs) != "" {
		t.Errorf("expected empty for no chapters")
	}
	if formatChapteredTranscript(chapters, nil) != "" {
		t.Errorf("expected empty for no segments")
	}

	// A chapter with no segments in its span is skipped (no empty header).
	sparse := []TranscriptSegment{{StartSec: 0, Text: "only intro"}, {StartSec: 125, Text: "only outro"}}
	gotSparse := formatChapteredTranscript(chapters, sparse)
	want2 := "#### [0:00] Intro\n[0:00] only intro\n\n#### [2:00] Outro\n[2:05] only outro"
	if gotSparse != want2 {
		t.Errorf("sparse =\n%q\nwant\n%q", gotSparse, want2)
	}
}

func TestAvailableCaptionLabels(t *testing.T) {
	tracks := []captionTrack{
		{LanguageCode: "ko"},
		{LanguageCode: "en", Kind: "asr"},
		{LanguageCode: ""},
	}
	got := availableCaptionLabels(tracks)
	want := []string{"ko", "en (auto)", "unknown"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("label[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if availableCaptionLabels(nil) != nil {
		t.Errorf("expected nil for no tracks")
	}
}

func TestChannelURL(t *testing.T) {
	tests := []struct {
		channelID  string
		profileURL string
		want       string
	}{
		{"UC123", "", "https://www.youtube.com/channel/UC123"},
		{"UC123", "http://www.youtube.com/@handle", "http://www.youtube.com/@handle"},
		{"UC123", "/@handle", "https://www.youtube.com/@handle"},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := channelURL(tt.channelID, tt.profileURL); got != tt.want {
			t.Errorf("channelURL(%q,%q) = %q, want %q", tt.channelID, tt.profileURL, got, tt.want)
		}
	}
}

func TestBestThumbnail(t *testing.T) {
	type thumb = struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	thumbs := []thumb{
		{URL: "small", Width: 120, Height: 90},
		{URL: "big", Width: 1280, Height: 720},
		{URL: "med", Width: 480, Height: 360},
	}
	if got := bestThumbnail(thumbs); got != "big" {
		t.Errorf("bestThumbnail() = %q, want %q", got, "big")
	}
	if got := bestThumbnail(nil); got != "" {
		t.Errorf("bestThumbnail(nil) = %q, want empty", got)
	}
}
