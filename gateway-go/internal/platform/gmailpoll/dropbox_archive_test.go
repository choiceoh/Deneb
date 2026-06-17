package gmailpoll

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func TestIsArchivableSkipsTruncatedAttachment(t *testing.T) {
	att := gmail.AttachmentInfo{
		Filename:  "quote.pdf",
		MimeType:  "application/pdf",
		Size:      minArchiveSize + 1,
		Truncated: true,
	}
	if isArchivable(att) {
		t.Fatal("truncated attachments must not be archived as complete documents")
	}
}
