// Package lmtpd is a minimal LMTP (RFC 2033) server that lets a local mail
// server (e.g. a Docker mail service running Postfix) PUSH email into Deneb,
// replacing the previous IMAP poll. A received message is parsed into the same
// gmail.MessageDetail the Gmail poller produces, so the existing analysis +
// delivery pipeline (AnalyzeEmailPipeline → cache/wiki → proactive chat) is reused
// verbatim. This file is the RFC822/MIME → MessageDetail parser.
package lmtpd

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/unicode"
)

// Bound a single delivery: refuse messages whose DATA exceeds this, and cap the
// flattened body fed to analysis. Korean business mail with embedded images can
// be large, but the analysis only needs text — attachments are kept as metadata.
const (
	maxBodyRunes  = 200_000  // flattened body cap (analysis input)
	maxLeafBytes  = 32 << 20 // per-part decoded attachment cap (defensive)
	maxPartsCount = 200      // defensive cap on multipart parts walked
)

// Message is a parsed LMTP delivery: the MessageDetail fed to analysis, the raw
// attachment bytes (keyed by AttachmentInfo.AttachmentID, for Dropbox archiving),
// and a stable dedup key derived from the Message-ID header (so a re-delivery of
// the same mail isn't analyzed — or wiki-paged — twice).
type Message struct {
	Detail          *gmail.MessageDetail
	AttachmentBytes map[string][]byte
	DedupKey        string
	Raw             []byte
}

// parseMessage turns a raw RFC822 message into a Message. Headers are
// RFC2047-decoded (so EUC-KR/UTF-8 encoded subjects read correctly); the body is
// the flattened text (text/plain preferred, else HTML flattened the same way the
// Gmail path does); attachments carry filename/mime/size plus their bytes for
// archiving. The MessageDetail.ID is the sanitized Message-ID when present (a
// stable cache/wiki key across re-delivery), else a fresh unique id.
func parseMessage(raw []byte, fallbackID string) (*Message, error) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("lmtp: parse message: %w", err)
	}
	hdr := m.Header

	key := sanitizeID(hdr.Get("Message-ID"))
	if key == "" {
		key = fallbackID // no Message-ID → unique per delivery (can't dedup)
	}

	detail := &gmail.MessageDetail{
		ID:              key,
		From:            decodeHeader(hdr.Get("From")),
		To:              decodeHeader(hdr.Get("To")),
		CC:              decodeHeader(hdr.Get("Cc")),
		Subject:         decodeHeader(hdr.Get("Subject")),
		Date:            hdr.Get("Date"),
		MessageIDHeader: strings.TrimSpace(hdr.Get("Message-ID")),
		References:      parseRefIDs(hdr.Get("References"), hdr.Get("In-Reply-To")),
	}

	ctype := hdr.Get("Content-Type")
	if ctype == "" {
		ctype = "text/plain"
	}
	mediaType, params, mtErr := mime.ParseMediaType(ctype)
	if mtErr != nil {
		// A malformed Content-Type (e.g. "multipart/mixed; boundary=" with an
		// empty boundary) leaves mediaType empty — the body then neither splits as
		// multipart nor reads as text, so it silently vanishes and analysis sees an
		// empty mail. Degrade to plain text so the content is preserved (the
		// subpart walk already defaults empty subtypes to text/plain).
		mediaType, params = "text/plain", nil
	}
	acc := mimeAccumulator{attBytes: map[string][]byte{}}
	acc.walk(part{
		mediaType: mediaType,
		params:    params,
		cte:       hdr.Get("Content-Transfer-Encoding"),
		body:      m.Body,
	}, 0)

	text := acc.plain
	if strings.TrimSpace(text) == "" && acc.html != "" {
		text = gmail.HTMLToText(acc.html)
	}
	detail.Body = clampRunes(strings.TrimSpace(text), maxBodyRunes)
	detail.Attachments = acc.atts
	return &Message{Detail: detail, AttachmentBytes: acc.attBytes, DedupKey: key}, nil
}

// ParseMessage turns raw RFC822 bytes into a Message. It is exported for the
// durable LMTP queue worker, which re-parses queued raw deliveries after ACK.
func ParseMessage(raw []byte, fallbackID string) (*Message, error) {
	return parseMessage(raw, fallbackID)
}

// ParseDetail parses a raw RFC822 message into a gmail.MessageDetail (headers
// decoded, body flattened to text), discarding attachment bytes. It is the
// read-only counterpart to the LMTP ingest parse, used to turn archive-fetched
// messages into thread context.
func ParseDetail(raw []byte) (*gmail.MessageDetail, error) {
	m, err := parseMessage(raw, "")
	if err != nil {
		return nil, err
	}
	return m.Detail, nil
}

// refIDRe matches RFC 5322 msg-id tokens "<...>" in References / In-Reply-To.
var refIDRe = regexp.MustCompile(`<[^<>\s]+>`)

// parseRefIDs collects referenced Message-IDs from the References and In-Reply-To
// headers (raw "<...>" form), de-duplicated, In-Reply-To first (the most direct
// parent), then the References chain. These drive the archive thread lookup.
func parseRefIDs(references, inReplyTo string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(h string) {
		for _, id := range refIDRe.FindAllString(h, -1) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	add(inReplyTo)
	add(references)
	return out
}

// sanitizeID turns a Message-ID header into a safe, stable key usable as a wiki
// page / cache id: strips angle brackets and path separators. "" if absent.
func sanitizeID(messageID string) string {
	s := strings.Trim(strings.TrimSpace(messageID), "<>")
	if s == "" {
		return ""
	}
	// Percent-encode the path/space chars rather than collapsing them all to "_".
	// Distinct Message-IDs MUST map to distinct keys: this value is both the dedup
	// key and the wiki/cache filename, so a collision lets one crafted id suppress
	// another's analysis (MarkIfNew → false, dropped as a "duplicate") and clobber
	// its wiki page. Control chars are dropped (invalid in a Message-ID).
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f:
			// drop control chars / NUL
		case r == '/' || r == '\\' || r == ' ':
			fmt.Fprintf(&b, "%%%02X", r)
		default:
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), ".") // avoid "." / ".." filenames
}

// part is one node of the MIME tree.
type part struct {
	mediaType  string
	params     map[string]string
	cte        string // Content-Transfer-Encoding
	filename   string // from Content-Disposition / name=
	attachment bool   // Content-Disposition: attachment
	body       io.Reader
}

// mimeAccumulator collects the flattened text bodies, attachment metadata, and
// attachment bytes (keyed by the synthetic AttachmentID) as the tree is walked.
type mimeAccumulator struct {
	plain    string
	html     string
	atts     []gmail.AttachmentInfo
	attBytes map[string][]byte
	parts    int
}

func (a *mimeAccumulator) walk(p part, depth int) {
	if depth > 20 || a.parts > maxPartsCount { // defensive against pathological nesting
		return
	}
	a.parts++

	if strings.HasPrefix(p.mediaType, "multipart/") {
		boundary := p.params["boundary"]
		if boundary == "" {
			return
		}
		mr := multipart.NewReader(p.body, boundary)
		for {
			sub, err := mr.NextPart()
			if err != nil {
				break
			}
			subType, subParams, _ := mime.ParseMediaType(sub.Header.Get("Content-Type"))
			if subType == "" {
				subType = "text/plain"
			}
			disp, dispParams, _ := mime.ParseMediaType(sub.Header.Get("Content-Disposition"))
			filename := dispParams["filename"]
			if filename == "" {
				filename = subParams["name"]
			}
			a.walk(part{
				mediaType:  subType,
				params:     subParams,
				cte:        sub.Header.Get("Content-Transfer-Encoding"),
				filename:   decodeHeader(filename),
				attachment: strings.EqualFold(disp, "attachment"),
				body:       sub,
			}, depth+1)
			_ = sub.Close()
		}
		return // multipart node fully consumed — must not fall through to leaf
	}

	// Leaf part.
	decoded, truncated := readCapped(transferDecode(p.cte, p.body), maxLeafBytes)

	isText := strings.HasPrefix(p.mediaType, "text/")
	if p.attachment || (p.filename != "" && !isText) {
		attID := fmt.Sprintf("att-%d", len(a.atts))
		a.atts = append(a.atts, gmail.AttachmentInfo{
			Filename:     p.filename,
			MimeType:     p.mediaType,
			AttachmentID: attID,
			Size:         len(decoded),
			Truncated:    truncated,
		})
		if a.attBytes != nil {
			a.attBytes[attID] = decoded
		}
		return
	}
	if !isText {
		return
	}

	text := charsetDecode(decoded, p.params["charset"])
	switch {
	case strings.HasPrefix(p.mediaType, "text/html"):
		a.html += text
	default: // text/plain and unknown text/*
		a.plain += text
	}
}

func readCapped(r io.Reader, max int64) ([]byte, bool) {
	if max <= 0 {
		return nil, true
	}
	data, _ := io.ReadAll(io.LimitReader(r, max+1))
	if int64(len(data)) <= max {
		return data, false
	}
	return data[:max], true
}

// transferDecode wraps the reader to undo the Content-Transfer-Encoding.
func transferDecode(cte string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &base64Sanitizer{r: r})
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	default: // 7bit, 8bit, binary, "" — passthrough
		return r
	}
}

// base64Sanitizer strips CR/LF/space from a base64 stream so the std decoder
// (which rejects whitespace) accepts mail-wrapped base64.
type base64Sanitizer struct {
	r io.Reader
}

func (s *base64Sanitizer) Read(p []byte) (int, error) {
	for {
		tmp := make([]byte, len(p))
		n, err := s.r.Read(tmp)
		w := 0
		for _, b := range tmp[:n] {
			if b == '\r' || b == '\n' || b == ' ' || b == '\t' {
				continue
			}
			p[w] = b
			w++
		}
		// Never return (0, nil): an all-whitespace chunk with no error reads
		// again (the underlying reader makes forward progress, so this ends).
		if w > 0 || err != nil {
			return w, err
		}
	}
}

// charsetDecode converts a body part to UTF-8 from its declared charset.
// UTF-8/ASCII pass through; EUC-KR/CP949 (common in Korean mail) and KS_C_5601
// are decoded via x/text. Unknown charsets are returned as-is (best-effort).
func charsetDecode(b []byte, charset string) string {
	dec := decoderFor(charset)
	if dec == nil {
		return string(b)
	}
	out, err := dec.Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

func decoderFor(charset string) *encoding.Decoder {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return nil // already UTF-8/ASCII
	case "euc-kr", "euckr", "cp949", "ks_c_5601-1987", "ksc5601", "ks_c_5601":
		return korean.EUCKR.NewDecoder()
	case "utf-16", "utf-16le":
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()
	case "utf-16be":
		return unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder()
	default:
		return nil
	}
}

// wordDecoder decodes RFC2047 encoded-words in headers, adding EUC-KR on top of
// the stdlib's built-in UTF-8/ISO-8859-1 support.
var wordDecoder = mime.WordDecoder{
	CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
		if dec := decoderFor(charset); dec != nil {
			return dec.Reader(input), nil
		}
		return input, nil
	},
}

// decodeHeader RFC2047-decodes a header value (subject/from/filename), falling
// back to the raw value if it isn't an encoded-word or can't be decoded.
func decodeHeader(v string) string {
	if v == "" {
		return ""
	}
	out, err := wordDecoder.DecodeHeader(v)
	if err != nil {
		return v
	}
	return out
}

// clampRunes caps a string to n runes (CJK-safe, not bytes), marking truncation.
func clampRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n\n[본문이 길어 일부 생략됨]"
}
