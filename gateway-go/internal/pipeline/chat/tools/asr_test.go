package tools

import (
	"encoding/json"
	"testing"
)

func TestSegmentSpeakerFlexible(t *testing.T) {
	// The model emits speaker as a string label OR a bare integer index OR null.
	var num asrSegment
	if err := json.Unmarshal([]byte(`{"start":0,"end":1,"speaker":2,"content":"hi"}`), &num); err != nil {
		t.Fatalf("numeric speaker: %v", err)
	}
	if string(num.Speaker) != "2" {
		t.Errorf("numeric speaker = %q, want %q", num.Speaker, "2")
	}
	var str asrSegment
	if err := json.Unmarshal([]byte(`{"speaker":"SPEAKER_01","content":"hi"}`), &str); err != nil {
		t.Fatalf("string speaker: %v", err)
	}
	if string(str.Speaker) != "SPEAKER_01" {
		t.Errorf("string speaker = %q", str.Speaker)
	}
	var null asrSegment
	if err := json.Unmarshal([]byte(`{"speaker":null,"content":"hi"}`), &null); err != nil {
		t.Fatalf("null speaker: %v", err)
	}
	if string(null.Speaker) != "" {
		t.Errorf("null speaker = %q, want empty", null.Speaker)
	}
}

func TestDisplaySpeaker(t *testing.T) {
	cases := map[string]string{
		"":           "화자",
		"0":          "화자1",
		"3":          "화자4",
		"SPEAKER_01": "SPEAKER_01",
		"민수":         "민수",
	}
	for in, want := range cases {
		if got := displaySpeaker(in); got != want {
			t.Errorf("displaySpeaker(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMmss(t *testing.T) {
	cases := map[float64]string{
		0:    "00:00",
		5:    "00:05",
		65:   "01:05",
		600:  "10:00",
		3661: "1:01:01",
		-3:   "00:00",
	}
	for in, want := range cases {
		if got := mmss(in); got != want {
			t.Errorf("mmss(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatTranscript(t *testing.T) {
	// Segments render diarized + timestamped, sorted by start; blanks dropped.
	r := &asrResponse{
		Segments: []asrSegment{
			{Start: 5, End: 8, Speaker: "SPEAKER_01", Content: "반갑습니다"},
			{Start: 0, End: 4, Speaker: "SPEAKER_00", Content: "안녕하세요"},
			{Start: 9, End: 10, Speaker: "", Content: "  "},
		},
		Transcription: "flat fallback",
	}
	got := formatTranscript(r)
	want := "[00:00 SPEAKER_00] 안녕하세요\n[00:05 SPEAKER_01] 반갑습니다"
	if got != want {
		t.Errorf("formatTranscript segments =\n%q\nwant\n%q", got, want)
	}

	// No usable segments -> flat transcription (trimmed).
	if flat := formatTranscript(&asrResponse{Transcription: "  just text  "}); flat != "just text" {
		t.Errorf("formatTranscript flat = %q, want %q", flat, "just text")
	}

	if formatTranscript(nil) != "" {
		t.Error("formatTranscript(nil) should be empty")
	}
}

func TestAudioFilename(t *testing.T) {
	cases := map[string]string{
		"audio/mp4":     "audio.m4a",
		"audio/x-m4a":   "audio.m4a",
		"audio/ogg":     "audio.oga",
		"audio/opus":    "audio.oga",
		"audio/mpeg":    "audio.mp3",
		"audio/wav":     "audio.wav",
		"audio/webm":    "audio.webm",
		"audio/flac":    "audio.flac",
		"":              "audio",
		"application/x": "audio",
	}
	for in, want := range cases {
		if got := audioFilename(in); got != want {
			t.Errorf("audioFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
