package chat

import "testing"

func TestDecodeMessage_SlashCommand(t *testing.T) {
	dm := DecodeMessage("/reset", nil)
	if !dm.IsSlashCommand {
		t.Fatal("expected slash command detection")
	}
	if !dm.IsShort {
		t.Fatal("expected short message")
	}
}

func TestDecodeMessage_Greeting(t *testing.T) {
	cases := []struct {
		text     string
		greeting bool
	}{
		{"안녕하세요", true},
		{"hi", true},
		{"ㅋㅋㅋ", true},
		{"프로젝트 빌드 실패 원인 분석해줘", false},
	}
	for _, tc := range cases {
		dm := DecodeMessage(tc.text, nil)
		if dm.IsGreeting != tc.greeting {
			t.Errorf("DecodeMessage(%q).IsGreeting = %v, want %v", tc.text, dm.IsGreeting, tc.greeting)
		}
	}
}

func TestDecodeMessage_Attachments(t *testing.T) {
	atts := []ChatAttachment{
		{Type: "image", MimeType: "image/png"},
	}
	dm := DecodeMessage("이 이미지 분석해줘", atts)
	if !dm.HasAttachments {
		t.Fatal("expected HasAttachments")
	}
	if !dm.HasImageAttachment {
		t.Fatal("expected HasImageAttachment")
	}
}

func TestDecodeMessage_KeywordHints(t *testing.T) {
	dm := DecodeMessage("Rust 컴파일 에러 해결 방법 알려줘", nil)
	if dm.IsShort {
		t.Fatal("expected not short")
	}
	if len(dm.KeywordHints) == 0 {
		t.Fatal("expected keyword hints for long message")
	}
	// Should include "Rust", "컴파일", "에러" etc, but not particles.
	found := false
	for _, h := range dm.KeywordHints {
		if h == "Rust" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'Rust' in hints, got %v", dm.KeywordHints)
	}
}

func TestDecodeMessage_ShortSkipsKeywords(t *testing.T) {
	dm := DecodeMessage("ㅇㅇ", nil)
	if len(dm.KeywordHints) > 0 {
		t.Fatal("short messages should not extract keywords")
	}
}
