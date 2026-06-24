package gmailpoll

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func TestLargeAttachAllowed(t *testing.T) {
	rules := defaultLargeAttachRules
	cases := []struct {
		url  string
		want bool
	}{
		{"https://tsgw.topsolar.kr/mail/mail002A31?&key=x", true},
		{"https://tsgw.topsolar.kr/mail/mail002A30?key=x", false}, // inline thumbnail endpoint
		{"https://tsgw.topsolar.kr/", false},                      // right host, wrong path
		{"https://www.topsolar.kr/mail/mail002A31", false},        // wrong host
		{"https://evil.com/mail002A31", false},
		{"http://tr.qiye.163.com/datacapture/mailreport", false}, // tracking pixel host
		{"mailto:x@y.com", false},
		{"not a url at all", false},
	}
	for _, c := range cases {
		if got := largeAttachAllowed(c.url, rules); got != c.want {
			t.Errorf("largeAttachAllowed(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestLargeAttachRulesEnvOverride(t *testing.T) {
	t.Setenv("DENEB_LARGE_ATTACH_HOSTS", "mail.example.com|download , files.example.org")
	rules := largeAttachRules()
	if !largeAttachAllowed("https://mail.example.com/download/1", rules) {
		t.Error("host|fragment should allow a matching path")
	}
	if largeAttachAllowed("https://mail.example.com/other", rules) {
		t.Error("path fragment should constrain the host")
	}
	if !largeAttachAllowed("https://files.example.org/anything", rules) {
		t.Error("no fragment = any path on host")
	}
	if largeAttachAllowed("https://tsgw.topsolar.kr/mail/mail002A31", rules) {
		t.Error("env override should replace the default, not extend it")
	}
}

func TestFetchLargeAttachmentsInto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="quote.pdf"`)
		_, _ = w.Write([]byte("%PDF-1.7 fake pdf bytes"))
	}))
	defer srv.Close()
	t.Setenv("DENEB_LARGE_ATTACH_HOSTS", mustHost(t, srv.URL)) // allow the test host, any path

	s := &Service{log: slog.Default()}
	msg := &gmail.MessageDetail{
		ID: "m1",
		LargeAttachments: []gmail.LargeAttachmentRef{
			{URL: srv.URL + "/dl", Filename: "견적서.pdf"},
			{URL: "https://blocked.example.com/x", Filename: "blocked.pdf"}, // not allowlisted → never fetched
		},
	}
	attBytes := map[string][]byte{}
	s.fetchLargeAttachmentsInto(context.Background(), msg, attBytes)

	if len(msg.Attachments) != 1 {
		t.Fatalf("want 1 merged attachment (blocked host skipped), got %d", len(msg.Attachments))
	}
	att := msg.Attachments[0]
	if att.Filename != "quote.pdf" { // Content-Disposition wins over the body hint
		t.Errorf("filename = %q, want quote.pdf (Content-Disposition)", att.Filename)
	}
	if b, ok := attBytes[att.AttachmentID]; !ok || string(b) != "%PDF-1.7 fake pdf bytes" {
		t.Errorf("bytes not merged under AttachmentID %q", att.AttachmentID)
	}
}

func TestFetchLargeAttachments_RedirectBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example.com/x", http.StatusFound)
	}))
	defer srv.Close()
	t.Setenv("DENEB_LARGE_ATTACH_HOSTS", mustHost(t, srv.URL)) // initial host allowed, any path

	s := &Service{log: slog.Default()}
	msg := &gmail.MessageDetail{
		ID:               "m1",
		LargeAttachments: []gmail.LargeAttachmentRef{{URL: srv.URL + "/dl", Filename: "x.pdf"}},
	}
	attBytes := map[string][]byte{}
	s.fetchLargeAttachmentsInto(context.Background(), msg, attBytes)
	if len(msg.Attachments) != 0 || len(attBytes) != 0 {
		t.Fatalf("redirect to a non-allowlisted host must be blocked, got %d attachments", len(msg.Attachments))
	}
}

func TestFetchLargeAttachmentsInto_NoLinks(t *testing.T) {
	s := &Service{log: slog.Default()}
	msg := &gmail.MessageDetail{ID: "m1"}
	attBytes := map[string][]byte{}
	s.fetchLargeAttachmentsInto(context.Background(), msg, attBytes)
	if len(msg.Attachments) != 0 || len(attBytes) != 0 {
		t.Error("no-op expected when the message has no large attachments")
	}
}

func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname()
}
