// large_attachment.go — resolve 대용량첨부 (large-file download links) into real
// attachment bytes. Korean groupware webmail uploads big files to its own
// server and embeds only a download URL in the body (see lmtpd.extractLarge-
// AttachmentLinks). This file fetches the allowlisted ones and merges them into
// the message's inline attachments, so the existing OCR gate + archiver handle
// them exactly like a normal MIME attachment — no other code path changes.
//
// Security: a mail body is untrusted input, and an autonomous poller following
// links in it is an SSRF/tracking hazard. The host allowlist here is the gate —
// only links whose host (and optional path fragment) match a rule are ever
// fetched. The default allows just the operator's own groupware download
// endpoint; extend via DENEB_LARGE_ATTACH_HOSTS.
package gmailpoll

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

const (
	// maxLargeAttachmentBytes caps a single download. 대용량첨부 files run tens of
	// MB; this is a generous ceiling that still bounds a hostile/huge response.
	maxLargeAttachmentBytes = 60 << 20 // 60MB
	// maxLargeAttachmentsPerMsg bounds how many links one message may fetch.
	maxLargeAttachmentsPerMsg = 10
	// largeAttachmentTimeout bounds one download (a 20MB file over a slow link).
	largeAttachmentTimeout = 120 * time.Second
)

// largeAttachRule allows a download host, optionally constrained to a path
// fragment that marks a real attachment download (not, e.g., an inline
// thumbnail endpoint on the same host).
type largeAttachRule struct {
	host         string // exact host (case-insensitive), no port
	pathFragment string // substring the request path/query must contain; "" = any
}

// defaultLargeAttachRules is the built-in allowlist: only the operator's own
// groupware download endpoint. The `mail002A31` fragment is the download path;
// it excludes `mail002A30` (inline image thumbnails) on the same host. Extend
// via DENEB_LARGE_ATTACH_HOSTS (comma-separated "host" or "host|pathFragment").
var defaultLargeAttachRules = []largeAttachRule{
	{host: "tsgw.topsolar.kr", pathFragment: "mail002A31"},
}

func largeAttachRules() []largeAttachRule {
	raw := strings.TrimSpace(os.Getenv("DENEB_LARGE_ATTACH_HOSTS"))
	if raw == "" {
		return defaultLargeAttachRules
	}
	var rules []largeAttachRule
	for _, tok := range strings.Split(raw, ",") {
		host, frag, _ := strings.Cut(strings.TrimSpace(tok), "|")
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		rules = append(rules, largeAttachRule{host: host, pathFragment: strings.TrimSpace(frag)})
	}
	if len(rules) == 0 {
		return defaultLargeAttachRules
	}
	return rules
}

// largeAttachAllowed reports whether rawURL is an allowlisted large-attachment
// download link. This is the SSRF gate — a link is fetched only when it matches.
func largeAttachAllowed(rawURL string, rules []largeAttachRule) bool {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, r := range rules {
		if host != strings.ToLower(r.host) {
			continue
		}
		if r.pathFragment == "" || strings.Contains(u.RequestURI(), r.pathFragment) {
			return true
		}
	}
	return false
}

// largeAttachHostAllowed is the host-only gate used to vet redirect hops: a
// redirect must stay on an allowlisted host (the path may legitimately change,
// e.g. a download endpoint 302-ing to a signed URL on the same host), so a
// compromised/misconfigured server can't bounce the fetch to an internal URL.
func largeAttachHostAllowed(u *url.URL, rules []largeAttachRule) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, r := range rules {
		if host == strings.ToLower(r.host) {
			return true
		}
	}
	return false
}

// fetchLargeAttachmentsInto resolves a message's 대용량첨부 links into attachment
// bytes, appending each fetched file to msg.Attachments and attBytes so the
// downstream OCR gate and archiver treat it like an ordinary inline attachment.
// Best-effort: a non-allowlisted host or a fetch failure is logged and skipped;
// it never fails the analysis. Must run before the attachment gate so the files
// are read into the analysis and archived.
func (s *Service) fetchLargeAttachmentsInto(ctx context.Context, msg *gmail.MessageDetail, attBytes map[string][]byte) {
	if msg == nil || len(msg.LargeAttachments) == 0 || attBytes == nil {
		return
	}
	rules := largeAttachRules()
	client := httputil.NewClient(largeAttachmentTimeout)
	// Defense in depth: keep every redirect hop on an allowlisted host so a
	// download endpoint can't bounce the fetch to an arbitrary (internal) URL.
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("대용량첨부: too many redirects")
		}
		if !largeAttachHostAllowed(req.URL, rules) {
			return fmt.Errorf("대용량첨부: redirect to non-allowlisted host %q", req.URL.Hostname())
		}
		return nil
	}
	fetched := 0
	for _, ref := range msg.LargeAttachments {
		if fetched >= maxLargeAttachmentsPerMsg {
			s.log.Warn("대용량첨부 개수 상한 초과 — 나머지 스킵", "limit", maxLargeAttachmentsPerMsg, "msg", msg.ID)
			break
		}
		if !largeAttachAllowed(ref.URL, rules) {
			s.log.Warn("대용량첨부 호스트 미허용 — 스킵", "host", hostOf(ref.URL), "msg", msg.ID)
			continue
		}
		data, filename, err := downloadLargeAttachment(ctx, client, ref)
		if err != nil {
			// User-observable: a real attachment failed to archive. Surface it.
			s.log.Error("대용량첨부 다운로드 실패", "file", ref.Filename, "host", hostOf(ref.URL), "error", err, "msg", msg.ID)
			continue
		}
		if len(data) == 0 {
			continue
		}
		attID := fmt.Sprintf("large-%d", len(msg.Attachments))
		msg.Attachments = append(msg.Attachments, gmail.AttachmentInfo{
			Filename:     filename,
			MimeType:     mimeForName(filename),
			AttachmentID: attID,
			Size:         len(data),
		})
		attBytes[attID] = data
		fetched++
		s.log.Info("대용량첨부 다운로드 완료", "file", filename, "bytes", len(data), "msg", msg.ID)
	}
}

// downloadLargeAttachment GETs one allowlisted link, returning the bytes and the
// resolved filename (Content-Disposition wins; else the body hint; else a
// generic name). The caller has already host-gated ref.URL.
func downloadLargeAttachment(ctx context.Context, client *http.Client, ref gmail.LargeAttachmentRef) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref.URL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Deneb-mail-archiver")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxLargeAttachmentBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxLargeAttachmentBytes {
		return nil, "", fmt.Errorf("exceeds %d-byte cap", maxLargeAttachmentBytes)
	}
	filename := dispositionFilename(resp.Header.Get("Content-Disposition"))
	if filename == "" {
		filename = strings.TrimSpace(ref.Filename)
	}
	if filename == "" {
		filename = "대용량첨부"
	}
	return data, filename, nil
}

// dispositionFilename pulls the filename from a Content-Disposition header
// (handling RFC 2231 filename* automatically via mime.ParseMediaType).
func dispositionFilename(cd string) string {
	if cd == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(cd)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(params["filename"])
}

// mimeForName maps a filename extension to a MIME type for the attachment gate;
// falls back to octet-stream (the gate keys on extension anyway).
func mimeForName(name string) string {
	if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

func hostOf(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		return u.Hostname()
	}
	return ""
}
