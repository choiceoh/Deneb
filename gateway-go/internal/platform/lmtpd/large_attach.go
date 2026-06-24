// large_attach.go — extract "large attachment" (대용량첨부) download links from
// an HTML mail body. Korean groupware (e.g. tsgw.topsolar.kr) does not attach
// big files as MIME bytes; it uploads them to its own file server and embeds a
// download widget (largeUpDownLoader) with one <a href> per file. The flattened
// body the analysis sees keeps only the human-readable metadata ("대용량 파일첨부
// 3개", filenames, sizes) — the actual URLs live only in the discarded HTML
// part. This file surfaces those URLs so the ingest path can fetch + archive
// them like ordinary attachments.
//
// This is intentionally permissive: every absolute http(s) anchor in a
// large-attachment body becomes a candidate. The SSRF gate (which hosts may be
// fetched) lives at the download boundary (gmailpoll), not here — a parser must
// not make network-policy decisions.
package lmtpd

import (
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// largeAttachWidgetMarker is the container class Korean groupware webmail emits
// for its 대용량첨부 widget. Its presence is the trigger: normal mail has no such
// widget, so extractLargeAttachmentLinks returns nil and is a no-op for it.
const largeAttachWidgetMarker = "largeUpDownLoader"

// maxLargeAttachLinks bounds how many candidate links one message can carry, so
// a pathological body can't balloon the MessageDetail.
const maxLargeAttachLinks = 30

var (
	largeAttachAnchorRE   = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a\s*>`)
	largeAttachHrefRE     = regexp.MustCompile(`(?i)href\s*=\s*"([^"]+)"`)
	largeAttachInnerTagRE = regexp.MustCompile(`(?s)<[^>]+>`)
)

// extractLargeAttachmentLinks pulls candidate large-attachment download links
// from an HTML mail body. It returns nil unless the body carries a
// large-attachment widget, so it never touches normal mail. Only absolute
// http(s) anchor hrefs are returned (HTML entities decoded); the anchor text is
// captured as a best-effort filename hint. Duplicate URLs are dropped.
func extractLargeAttachmentLinks(htmlBody string) []gmail.LargeAttachmentRef {
	if !strings.Contains(htmlBody, largeAttachWidgetMarker) {
		return nil
	}
	var out []gmail.LargeAttachmentRef
	seen := map[string]bool{}
	for _, m := range largeAttachAnchorRE.FindAllStringSubmatch(htmlBody, -1) {
		attrs, inner := m[1], m[2]
		hm := largeAttachHrefRE.FindStringSubmatch(attrs)
		if hm == nil {
			continue
		}
		href := strings.TrimSpace(html.UnescapeString(hm[1]))
		u, err := url.Parse(href)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			continue // mailto:, in-page #frag, relative, malformed — never a download
		}
		if seen[href] {
			continue
		}
		seen[href] = true
		// Anchor text is typically "filename\n (size)"; keep just the filename
		// (first non-empty line). Content-Disposition is authoritative at download,
		// so this hint is only a fallback — but a clean value keeps a newline (or a
		// "(22.2 MB)" suffix) from ever reaching a filesystem path.
		name := strings.TrimSpace(html.UnescapeString(largeAttachInnerTagRE.ReplaceAllString(inner, "")))
		if nl := strings.IndexByte(name, '\n'); nl >= 0 {
			name = strings.TrimSpace(name[:nl])
		}
		out = append(out, gmail.LargeAttachmentRef{URL: href, Filename: name})
		if len(out) >= maxLargeAttachLinks {
			break
		}
	}
	return out
}
