package media

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

func TestHasMedia(t *testing.T) {
	tests := []struct {
		name string
		msg  *telegram.Message
		want bool
	}{
		{
			name: "nil message",
			msg:  nil,
			want: false,
		},
		{
			name: "text only",
			msg:  &telegram.Message{Text: "hello"},
			want: false,
		},
		{
			name: "photo message",
			msg: &telegram.Message{
				Photo: []telegram.PhotoSize{{FileID: "abc", Width: 100, Height: 100}},
			},
			want: true,
		},
		{
			name: "video message",
			msg: &telegram.Message{
				Video: &telegram.Video{FileID: "vid1", Duration: 10},
			},
			want: true,
		},
		{
			name: "animation message",
			msg: &telegram.Message{
				Animation: &telegram.Animation{FileID: "gif1"},
			},
			want: true,
		},
		{
			name: "image document",
			msg: &telegram.Message{
				Document: &telegram.Document{FileID: "doc1", MimeType: "image/png"},
			},
			want: true,
		},
		{
			name: "non-image document (PDF, parseable by liteparse)",
			msg: &telegram.Message{
				Document: &telegram.Document{FileID: "doc2", MimeType: "application/pdf"},
			},
			want: true, // liteparse supports PDF extraction
		},
		{
			name: "audio only (not processed)",
			msg: &telegram.Message{
				Audio: &telegram.Audio{FileID: "aud1"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasMedia(tt.msg); got != tt.want {
				t.Errorf("HasMedia() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessageText(t *testing.T) {
	tests := []struct {
		name string
		msg  *telegram.Message
		want string
	}{
		{
			name: "nil message",
			msg:  nil,
			want: "",
		},
		{
			name: "text message",
			msg:  &telegram.Message{Text: "hello"},
			want: "hello",
		},
		{
			name: "caption on media",
			msg: &telegram.Message{
				Caption: "my photo",
				Photo:   []telegram.PhotoSize{{FileID: "abc"}},
			},
			want: "my photo",
		},
		{
			name: "text takes priority over caption",
			msg: &telegram.Message{
				Text:    "text body",
				Caption: "caption",
			},
			want: "text body",
		},
		{
			name: "empty message",
			msg:  &telegram.Message{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MessageText(tt.msg); got != tt.want {
				t.Errorf("MessageText() = %q, want %q", got, tt.want)
			}
		})
	}
}
