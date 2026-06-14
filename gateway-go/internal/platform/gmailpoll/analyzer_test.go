package gmailpoll

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func TestLoadPrompt_Default(t *testing.T) {
	prompt := loadPrompt("")
	if prompt != DefaultPrompt {
		t.Errorf("empty path should return default prompt")
	}
}

func TestLoadPrompt_CustomFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-prompt.md")
	custom := "커스텀 분석 프롬프트입니다."
	if err := os.WriteFile(path, []byte(custom), 0600); err != nil {
		t.Fatal(err)
	}

	prompt := loadPrompt(path)
	if prompt != custom {
		t.Errorf("loadPrompt = %q, want %q", prompt, custom)
	}
}

func TestFormatEmailForAnalysis(t *testing.T) {
	msg := &gmail.MessageDetail{
		From:    "sender@example.com",
		To:      "me@example.com",
		Subject: "Test Subject",
		Date:    "Mon, 1 Jan 2024 00:00:00 +0900",
		Body:    "Hello, this is the email body.",
	}

	result := FormatEmailForAnalysis(msg)

	if !strings.Contains(result, "sender@example.com") {
		t.Error("should contain From address")
	}
	if !strings.Contains(result, "Test Subject") {
		t.Error("should contain Subject")
	}
	if !strings.Contains(result, "Hello, this is the email body.") {
		t.Error("should contain body")
	}
}

func TestFormatEmailForAnalysis_LongBody(t *testing.T) {
	longBody := strings.Repeat("x", 10000)
	msg := &gmail.MessageDetail{
		From:    "a@b.com",
		To:      "c@d.com",
		Subject: "Long",
		Body:    longBody,
	}

	result := FormatEmailForAnalysis(msg)
	if !strings.Contains(result, "본문 생략") {
		t.Error("long body should be truncated with notice")
	}
	// Body in result should be capped.
	if strings.Contains(result, longBody) {
		t.Error("full long body should not appear in result")
	}
}
