package media

import (
	"testing"
)

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
		{"ko auto when no manual", []captionTrack{autoEn, autoKo}, "u-ko-auto", "ko (auto)"},
		{"en auto next", []captionTrack{autoEn, autoJa}, "u-en-auto", "en (auto)"},
		{"any auto last", []captionTrack{autoJa}, "u-ja-auto", "auto"},
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
