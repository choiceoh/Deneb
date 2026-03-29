package gmailpoll

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/gmail"
)

func TestLoadPrompt_Default(t *testing.T) {
	prompt := loadPrompt("")
	if prompt != DefaultPrompt {
		t.Errorf("empty path should return default prompt")
	}
}

func TestLoadPrompt_MissingFile(t *testing.T) {
	prompt := loadPrompt("/nonexistent/path/prompt.md")
	if prompt != DefaultPrompt {
		t.Errorf("missing file should return default prompt")
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

func TestLoadPrompt_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(path, []byte("  \n  "), 0600); err != nil {
		t.Fatal(err)
	}

	prompt := loadPrompt(path)
	if prompt != DefaultPrompt {
		t.Errorf("empty file should return default prompt")
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

func TestFormatReport_HTML(t *testing.T) {
	msg := &gmail.MessageDetail{
		From:    "sender@test.com",
		Subject: "Important Email",
	}
	analysis := "중요도: 높음. 결제 확인 필요."

	report := formatReport(msg, analysis)

	if !strings.Contains(report, "📬") {
		t.Error("report should contain emoji header")
	}
	if !strings.Contains(report, "<b>") {
		t.Error("report should use HTML bold tags")
	}
	if !strings.Contains(report, "sender@test.com") {
		t.Error("report should contain sender")
	}
	if !strings.Contains(report, "Important Email") {
		t.Error("report should contain subject")
	}
	if !strings.Contains(report, analysis) {
		t.Error("report should contain analysis")
	}
}

func TestFormatReport_EscapesHTML(t *testing.T) {
	msg := &gmail.MessageDetail{
		From:    "test <user@test.com>",
		Subject: "Subject with <tag>",
	}
	analysis := "Analysis with <script>alert('xss')</script>"

	report := formatReport(msg, analysis)

	if strings.Contains(report, "<script>") {
		t.Error("report should escape HTML entities in content")
	}
	if !strings.Contains(report, "&lt;script&gt;") {
		t.Error("report should contain escaped HTML")
	}
}
