package gmail

import (
	"strings"
	"testing"
)

func TestBuildMIME_PlainText(t *testing.T) {
	raw := buildMIME("alice@example.com", "", "", "Hello", "", "Hi Alice", false)

	if !strings.Contains(raw, "To: alice@example.com\r\n") {
		t.Error("missing To header")
	}
	if !strings.Contains(raw, "Subject: Hello\r\n") {
		t.Error("missing Subject header")
	}
	if !strings.Contains(raw, "Content-Type: text/plain; charset=\"UTF-8\"\r\n") {
		t.Error("missing plain text Content-Type")
	}
	if strings.Contains(raw, "In-Reply-To:") {
		t.Error("unexpected In-Reply-To header")
	}
	if !strings.HasSuffix(raw, "Hi Alice") {
		t.Error("body not at end of message")
	}
}

func TestBuildMIME_HTML(t *testing.T) {
	raw := buildMIME("bob@example.com", "", "", "Hi", "", "<p>Hello</p>", true)

	if !strings.Contains(raw, "Content-Type: text/html; charset=\"UTF-8\"\r\n") {
		t.Error("missing HTML Content-Type")
	}
}

func TestBuildMIME_WithCC_BCC(t *testing.T) {
	raw := buildMIME("alice@example.com", "cc@example.com", "bcc@example.com", "Test", "", "Body", false)

	if !strings.Contains(raw, "Cc: cc@example.com\r\n") {
		t.Error("missing Cc header")
	}
	if !strings.Contains(raw, "Bcc: bcc@example.com\r\n") {
		t.Error("missing Bcc header")
	}
}

func TestBuildMIME_Reply(t *testing.T) {
	raw := buildMIME("alice@example.com", "", "", "Re: Hello", "<msg-123@mail.gmail.com>", "Thanks", false)

	if !strings.Contains(raw, "In-Reply-To: <msg-123@mail.gmail.com>\r\n") {
		t.Error("missing In-Reply-To header")
	}
	if !strings.Contains(raw, "References: <msg-123@mail.gmail.com>\r\n") {
		t.Error("missing References header")
	}
}

func TestDecodeBase64URL(t *testing.T) {
	// "Hello, World!" base64url-encoded (no padding).
	encoded := "SGVsbG8sIFdvcmxkIQ"
	decoded := decodeBase64URL(encoded)
	if decoded != "Hello, World!" {
		t.Errorf("decoded = %q, want %q", decoded, "Hello, World!")
	}
}

func TestDecodeBase64URL_Invalid(t *testing.T) {
	// Invalid base64 should return the original string.
	result := decodeBase64URL("not-valid-base64!!!")
	if result != "not-valid-base64!!!" {
		t.Errorf("got %q, want original string for invalid base64", result)
	}
}

func TestDecodeBase64URL_Padded(t *testing.T) {
	// "Hello, World!" with "=" padding — the old strict decoder rejected this
	// and silently returned the raw base64.
	if got := decodeBase64URL("SGVsbG8sIFdvcmxkIQ=="); got != "Hello, World!" {
		t.Errorf("padded decode = %q, want %q", got, "Hello, World!")
	}
}

func TestDecodeBase64URL_Wrapped(t *testing.T) {
	// MIME part data can arrive wrapped across lines.
	if got := decodeBase64URL("SGVsbG8s\r\nIFdvcmxk\nIQ"); got != "Hello, World!" {
		t.Errorf("wrapped decode = %q, want %q", got, "Hello, World!")
	}
}

func TestCollectAttachments(t *testing.T) {
	payload := &apiPayload{
		MimeType: "multipart/mixed",
		Parts: []apiPayload{
			{MimeType: "text/plain", Body: &apiBody{Data: "SGk"}},
			{MimeType: "application/pdf", Filename: "contract.pdf", Body: &apiBody{AttachmentID: "att-1", Size: 2048}},
		},
	}
	var atts []AttachmentInfo
	collectAttachments(payload, &atts)
	if len(atts) != 1 {
		t.Fatalf("got %d attachments, want 1", len(atts))
	}
	if atts[0].Filename != "contract.pdf" || atts[0].AttachmentID != "att-1" || atts[0].Size != 2048 {
		t.Errorf("attachment = %+v", atts[0])
	}
}

func TestExtractBody_SinglePart(t *testing.T) {
	p := &apiPayload{
		MimeType: "text/plain",
		Body:     &apiBody{Data: "SGVsbG8"}, // "Hello"
	}
	body := extractBody(p)
	if body != "Hello" {
		t.Errorf("body = %q, want Hello", body)
	}
}

func TestExtractBody_Multipart_PrefersPlain(t *testing.T) {
	p := &apiPayload{
		MimeType: "multipart/alternative",
		Parts: []apiPayload{
			{
				MimeType: "text/plain",
				Body:     &apiBody{Data: "UGxhaW4gdGV4dA"}, // "Plain text"
			},
			{
				MimeType: "text/html",
				Body:     &apiBody{Data: "PFBIVE1MIGJvZHk"}, // some HTML
			},
		},
	}
	body := extractBody(p)
	if body != "Plain text" {
		t.Errorf("body = %q, want Plain text", body)
	}
}

func TestExtractBody_Multipart_FallsBackToHTML(t *testing.T) {
	p := &apiPayload{
		MimeType: "multipart/alternative",
		Parts: []apiPayload{
			{
				MimeType: "text/html",
				Body:     &apiBody{Data: "PHA-SFRNTDWVCD4"}, // some HTML
			},
		},
	}
	body := extractBody(p)
	if body == "" {
		t.Error("got empty, want HTML fallback body")
	}
}

func TestExtractBody_Nil(t *testing.T) {
	if body := extractBody(nil); body != "" {
		t.Errorf("got %q, want empty for nil payload", body)
	}
}

// HTML-only newsletters used to leak raw markup into the Mini App's <pre>
// body view. extractBody should flatten the HTML on both the single-part
// and the multipart fallback paths.
func TestExtractBody_SinglePart_HTML_FlattenedToText(t *testing.T) {
	// "<html><body><p>Hello <b>world</b></p><br><div>Line two</div><script>alert(1)</script></body></html>"
	p := &apiPayload{
		MimeType: "text/html",
		Body:     &apiBody{Data: "PGh0bWw-PGJvZHk-PHA-SGVsbG8gPGI-d29ybGQ8L2I-PC9wPjxicj48ZGl2PkxpbmUgdHdvPC9kaXY-PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0PjwvYm9keT48L2h0bWw-"},
	}
	body := extractBody(p)
	if strings.Contains(body, "<") || strings.Contains(body, ">") {
		t.Errorf("body still contains HTML markup: %q", body)
	}
	if !strings.Contains(body, "Hello world") {
		t.Errorf("body lost visible text: %q", body)
	}
	if strings.Contains(body, "alert(1)") {
		t.Errorf("<script> content leaked into body: %q", body)
	}
}

func TestHTMLToText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty",
			in:   "",
			want: "",
		},
		{
			name: "tags stripped, paragraphs broken on block boundaries",
			in:   "<p>Hello <b>world</b></p><p>Second</p>",
			want: "Hello world\n\nSecond",
		},
		{
			name: "br becomes newline",
			in:   "Line one<br>Line two<br/>Line three",
			want: "Line one\nLine two\nLine three",
		},
		{
			name: "entities decoded",
			in:   "Tom &amp; Jerry &lt;3 &nbsp;spaces",
			want: "Tom & Jerry <3  spaces",
		},
		{
			name: "script and style dropped including content",
			in:   "<style>.x{color:red}</style>Visible<script>alert(1)</script>",
			want: "Visible",
		},
		{
			name: "blank line runs collapse",
			in:   "<p>A</p><p></p><p></p><p>B</p>",
			want: "A\n\nB",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := htmlToText(tc.in)
			if got != tc.want {
				t.Errorf("htmlToText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatSearchResults(t *testing.T) {
	msgs := []MessageSummary{
		{ID: "abc123", From: "Alice <alice@example.com>", Subject: "Hello", Date: "Mon, 1 Jan 2026", Snippet: "Hi there"},
		{ID: "def456", From: "Bob <bob@example.com>", Subject: "Meeting", Date: "Tue, 2 Jan 2026"},
	}

	result := FormatSearchResults(msgs)
	if !strings.Contains(result, "Alice") {
		t.Error("missing Alice in output")
	}
	if !strings.Contains(result, "abc123") {
		t.Error("missing message ID")
	}
	if !strings.Contains(result, "Meeting") {
		t.Error("missing second message subject")
	}
}

func TestFormatSearchResults_Empty(t *testing.T) {
	if result := FormatSearchResults(nil); result != "" {
		t.Errorf("got %q, want empty", result)
	}
}

func TestFormatMessage(t *testing.T) {
	m := &MessageDetail{
		ID:      "msg1",
		From:    "Alice <alice@example.com>",
		To:      "Bob <bob@example.com>",
		CC:      "Carol <carol@example.com>",
		Subject: "Test",
		Date:    "2026-01-01",
		Body:    "Hello Bob",
	}

	result := FormatMessage(m)
	if !strings.Contains(result, "**From:** Alice") {
		t.Error("missing From")
	}
	if !strings.Contains(result, "**CC:** Carol") {
		t.Error("missing CC")
	}
	if !strings.Contains(result, "Hello Bob") {
		t.Error("missing body")
	}
}

func TestFormatMessage_NoCC(t *testing.T) {
	m := &MessageDetail{From: "A", To: "B", Subject: "S", Date: "D", ID: "1"}
	result := FormatMessage(m)
	if strings.Contains(result, "**CC:**") {
		t.Error("CC should not appear when empty")
	}
}

func TestFormatLabels(t *testing.T) {
	labels := []LabelInfo{
		{ID: "INBOX", Name: "INBOX", Type: "system"},
		{ID: "Label_1", Name: "Work", Type: "user"},
	}

	result := FormatLabels(labels)
	if !strings.Contains(result, "INBOX (시스템)") {
		t.Error("missing system label marker")
	}
	if !strings.Contains(result, "- Work\n") {
		t.Error("missing user label")
	}
}

func TestFormatLabels_Empty(t *testing.T) {
	if result := FormatLabels(nil); result != "" {
		t.Errorf("got %q, want empty", result)
	}
}
