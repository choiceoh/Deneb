package autoreply

import (
	"strings"
	"testing"
)

func TestIsAudioAttachment(t *testing.T) {
	tests := []struct {
		a    MediaAttachment
		want bool
	}{
		{MediaAttachment{MimeType: "audio/mp3"}, true},
		{MediaAttachment{MimeType: "audio/ogg"}, true},
		{MediaAttachment{Name: "voice.ogg"}, true},
		{MediaAttachment{Path: "/tmp/recording.m4a"}, true},
		{MediaAttachment{MimeType: "image/png"}, false},
		{MediaAttachment{Name: "photo.jpg"}, false},
	}
	for _, tt := range tests {
		got := IsAudioAttachment(tt.a)
		if got != tt.want {
			t.Errorf("IsAudioAttachment(%+v) = %v, want %v", tt.a, got, tt.want)
		}
	}
}

func TestBuildInboundMediaNote(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := BuildInboundMediaNote(nil, MediaNoteOptions{})
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("single image", func(t *testing.T) {
		got := BuildInboundMediaNote([]MediaAttachment{
			{MimeType: "image/png", Name: "photo.png"},
		}, MediaNoteOptions{})
		if !strings.Contains(got, "image") || !strings.Contains(got, "photo.png") {
			t.Errorf("unexpected note: %q", got)
		}
	})

	t.Run("multiple mixed", func(t *testing.T) {
		got := BuildInboundMediaNote([]MediaAttachment{
			{MimeType: "image/png", Name: "a.png"},
			{MimeType: "audio/mp3", Name: "b.mp3"},
		}, MediaNoteOptions{})
		if !strings.Contains(got, "media attached") {
			t.Errorf("expected multi-media note, got %q", got)
		}
	})

	t.Run("strip transcribed audio", func(t *testing.T) {
		got := BuildInboundMediaNote([]MediaAttachment{
			{MimeType: "audio/ogg", Name: "voice.ogg"},
		}, MediaNoteOptions{StripTranscribedAudio: true})
		if got != "" {
			t.Errorf("expected empty after stripping audio, got %q", got)
		}
	})

	t.Run("suppress understanding", func(t *testing.T) {
		got := BuildInboundMediaNote([]MediaAttachment{
			{MimeType: "image/png"},
		}, MediaNoteOptions{SuppressMediaUnderstanding: true})
		if got != "[1 media attached]" {
			t.Errorf("expected count-only note, got %q", got)
		}
	})
}
