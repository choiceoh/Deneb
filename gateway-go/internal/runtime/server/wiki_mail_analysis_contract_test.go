package server

import (
	"strings"
	"testing"

	handlermail "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail"
)

func TestBuildMailAnalysisPageCarriesStableProvenance(t *testing.T) {
	page := buildMailAnalysisPage(handlermail.WikiAnalysisInput{
		MsgID:           "msg-123\nforged",
		ThreadID:        "thread-456",
		MessageIDHeader: "<rfc-789@example.test>",
		Subject:         "견적 회신",
		From:            "Vendor <sales@example.test>\n> Injected: yes",
		Date:            "Thu, 16 Jul 2026 10:00:00 +0900\n> Fake: row",
		Analysis:        "## 분석\n내용",
	})
	if page.Meta.Resource != "mail:msg-123 forged" {
		t.Fatalf("resource = %q", page.Meta.Resource)
	}
	for _, want := range []string{
		"> Source: `mail:msg-123 forged`",
		"[Gmail 원문](https://mail.google.com/mail/u/0/#all/thread-456)",
		"> Thread ID: `thread-456`",
		"> RFC Message-ID: `<rfc-789@example.test>`",
		"## 분석\n내용",
	} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("body missing %q:\n%s", want, page.Body)
		}
	}
	if strings.Contains(page.Body, "\n> Injected:") || strings.Contains(page.Body, "\n> Fake:") {
		t.Fatalf("header injected metadata rows:\n%s", page.Body)
	}
}

func TestGmailThreadLinkEmptyAndEscaped(t *testing.T) {
	if gmailThreadLink(" \n ") != "" {
		t.Fatal("blank thread produced a link")
	}
	if got := gmailThreadLink("thread id"); got != "https://mail.google.com/mail/u/0/#all/thread%20id" {
		t.Fatalf("escaped link = %q", got)
	}
}
