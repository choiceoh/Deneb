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

// stripMailChrome handles a representative subset of the noise we see on
// real newsletters/auto-mails. Each case is shaped as the rendered text
// the operator would see after htmlToText.
func TestStripMailChrome(t *testing.T) {
	// "Lorem" body padded out so the result clears the 200-byte safety
	// gate that disables chrome stripping for short bodies (one-line
	// replies, OTPs, alerts).
	body := strings.Repeat("이번 주 출시 노트의 주요 변경 사항입니다. ", 8)

	cases := []struct {
		name     string
		in       string
		contains []string // substrings that must survive the strip
		absent   []string // substrings that must be cut
	}{
		{
			name:     "view-in-browser banner removed (en)",
			in:       "Not displaying correctly? View in browser.\n\n" + body + "\n",
			contains: []string{"이번 주 출시"},
			absent:   []string{"View in browser"},
		},
		{
			name:     "view-in-browser banner removed (ko)",
			in:       "이메일이 잘 보이지 않으시나요? 웹에서 보기\n\n" + body + "\n",
			contains: []string{"이번 주 출시"},
			absent:   []string{"잘 보이지 않", "웹에서 보기"},
		},
		{
			name:     "unsubscribe footer cut",
			in:       body + "\n\n구독 해지하기\n회사 주소: 서울특별시 강남구\n© 2026 Acme",
			contains: []string{"이번 주 출시"},
			absent:   []string{"구독 해지", "© 2026"},
		},
		{
			name:     "copyright footer cut",
			in:       body + "\n\nCopyright © 2026 Acme Corp. All rights reserved.\nSomeone@example.com",
			contains: []string{"이번 주 출시"},
			absent:   []string{"All rights reserved", "Copyright"},
		},
		{
			name:     "english unsubscribe footer cut",
			in:       body + "\n\nUnsubscribe | Email preferences\nAcme HQ, 1 Market St",
			contains: []string{"이번 주 출시"},
			absent:   []string{"Unsubscribe", "Acme HQ"},
		},
		{
			name:     "separator-only lines collapsed",
			in:       body + "\n──────────────\nSee you next week!\n==========\n" + body,
			contains: []string{"See you next week"},
			absent:   []string{"──────────────", "=========="},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripMailChrome(tc.in)
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("expected %q to survive; got:\n%s", want, got)
				}
			}
			for _, gone := range tc.absent {
				if strings.Contains(got, gone) {
					t.Errorf("expected %q to be stripped; got:\n%s", gone, got)
				}
			}
		})
	}
}

// Short bodies must not be touched — OTP/alert mails are tiny and any
// pattern misfire there would discard the entire content.
func TestStripMailChrome_ShortBodyUntouched(t *testing.T) {
	in := "OTP: 123456 — 5분 후 만료됩니다.\nUnsubscribe"
	got := stripMailChrome(in)
	if got != in {
		t.Errorf("short body modified: got %q, want %q", got, in)
	}
}

// Safety abort: if heuristics would carve away >75% of the body, return
// the input unchanged. Built from a synthetic mail whose "real" content
// is dwarfed by a fake "view in browser" header followed by a single
// short visible line.
func TestStripMailChrome_AbortsOnOveraggressiveCut(t *testing.T) {
	chrome := strings.Repeat("View in browser. ", 30) // 510 bytes of preamble noise
	visible := "한 줄."                                 // ≤25% of chrome length
	in := chrome + "\n" + visible
	got := stripMailChrome(in)
	if got != in {
		t.Errorf("expected abort (return input), got %q", got)
	}
}

// A footer cue that lands in the *top* half of the body must not cut
// the message — operators sometimes quote "copyright" wording inside
// the actual content (e.g., legal review threads).
func TestStripMailChrome_FooterCueInTopHalfIgnored(t *testing.T) {
	body := "Copyright © 2025 holder of record.\n" +
		strings.Repeat("이 메일은 카피라이트 문구의 적법성에 대한 토론입니다. ", 12)
	got := stripMailChrome(body)
	if !strings.Contains(got, "Copyright © 2025") {
		t.Errorf("Copyright mention in top half was wrongly cut; got:\n%s", got)
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
		{
			name: "anchor keeps text and href",
			in:   `Visit <a href="https://example.com/path">our site</a> today`,
			want: "Visit our site (https://example.com/path) today",
		},
		{
			name: "anchor with text equal to href emits one copy",
			in:   `<a href="https://example.com">https://example.com</a>`,
			want: "https://example.com",
		},
		{
			name: "anchor with empty text falls back to href",
			in:   `<a href="https://example.com"></a>`,
			want: "https://example.com",
		},
		{
			name: "javascript anchor drops href, keeps text",
			in:   `<a href="javascript:void(0)">click me</a>`,
			want: "click me",
		},
		{
			name: "fragment-only anchor drops href, keeps text",
			in:   `<a href="#section">Jump</a>`,
			want: "Jump",
		},
		{
			name: "mailto anchor keeps scheme",
			in:   `<a href="mailto:a@b.com">Contact us</a>`,
			want: "Contact us (mailto:a@b.com)",
		},
		{
			name: "anchor with inner span keeps visible label",
			in:   `<a href="https://x.com"><span>Click</span></a>`,
			want: "Click (https://x.com)",
		},
		{
			name: "img with alt becomes marker",
			in:   `<img src="https://x.com/logo.png" alt="Company Logo">`,
			want: "[이미지: Company Logo]",
		},
		{
			name: "img without alt is dropped (likely tracking pixel)",
			in:   `Hello<img src="https://t.example.com/p.gif" width="1" height="1">World`,
			want: "HelloWorld",
		},
		{
			name: "img inside anchor: alt becomes link label",
			in:   `<a href="https://x.com"><img src="x.png" alt="Logo"></a>`,
			want: "[이미지: Logo] (https://x.com)",
		},
		{
			name: "single quote href",
			in:   `<a href='https://x.com/q'>q</a>`,
			want: "q (https://x.com/q)",
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
		{ID: "abc123", From: "Alice <alice@example.com>", Subject: "Hello", Date: "Mon, 1 Jan 2026", Snippet: "Hi there", Labels: []string{"INBOX", "UNREAD"}},
		{ID: "def456", From: "Bob <bob@example.com>", Subject: "Meeting", Date: "Tue, 2 Jan 2026", Labels: []string{"INBOX"}},
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
	if strings.Contains(result, "🔵") || strings.Contains(result, "⚪") {
		t.Error("legacy color emoji markers should be gone")
	}
	if strings.Contains(result, "(안 읽음)") {
		t.Error("legacy 안 읽음 label should be gone")
	}
	for _, line := range strings.Split(result, "\n") {
		if strings.HasPrefix(line, "> ") || strings.HasPrefix(line, ">  ") {
			t.Errorf("legacy blockquote prefix should be gone, got line %q", line)
		}
	}
	if !strings.Contains(result, "○ **Alice <alice@example.com>**") {
		t.Error("unread item should be prefixed with `○ ` and bold sender")
	}
	if strings.Contains(result, "○ Bob") || strings.Contains(result, "**Bob <bob@example.com>**") {
		t.Error("read item should have no marker and no bold sender")
	}
}

func TestFormatSearchResults_AllRead(t *testing.T) {
	msgs := []MessageSummary{
		{ID: "x1", From: "Carol", Subject: "FYI", Date: "Wed", Labels: []string{"INBOX"}},
	}
	result := FormatSearchResults(msgs)
	if strings.Contains(result, "○") {
		t.Error("read-only list should contain no `○` marker")
	}
	if strings.Contains(result, "**Carol**") {
		t.Error("read sender should not be bold")
	}
}

func TestFormatSearchResults_AllUnread(t *testing.T) {
	msgs := []MessageSummary{
		{ID: "u1", From: "Dan", Subject: "Ping", Date: "Thu", Labels: []string{"INBOX", "UNREAD"}},
		{ID: "u2", From: "Eve", Subject: "Pong", Date: "Fri", Labels: []string{"INBOX", "UNREAD"}},
	}
	result := FormatSearchResults(msgs)
	if strings.Count(result, "○") != 2 {
		t.Errorf("expected one `○` per unread item, got %d in %q", strings.Count(result, "○"), result)
	}
}

func TestHasUnreadLabel(t *testing.T) {
	if !hasUnreadLabel([]string{"INBOX", "UNREAD"}) {
		t.Error("expected true when UNREAD label present")
	}
	if hasUnreadLabel([]string{"INBOX"}) {
		t.Error("expected false without UNREAD label")
	}
	if hasUnreadLabel(nil) {
		t.Error("expected false for nil labels")
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
