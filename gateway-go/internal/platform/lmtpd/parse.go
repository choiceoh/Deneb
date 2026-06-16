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
	maxBodyRunes  = 200_000 // flattened body cap (analysis input)
	maxLeafBytes  = 8 << 20 // per-part decode cap (defensive)
	maxPartsCount = 200     // defensive cap on multipart parts walked
)

// parseMessage turns a raw RFC822 message into a gmail.MessageDetail. Headers are
// RFC2047-decoded (so EUC-KR/UTF-8 encoded subjects read correctly); the body is
// the flattened text (text/plain preferred, else HTML flattened the same way the
// Gmail path does); attachments carry filename/mime/size only (the analysis works
// from metadata + body, like the Gmail path before any attachment fetch).
func parseMessage(raw []byte, id string) (*gmail.MessageDetail, error) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("lmtp: parse message: %w", err)
	}
	hdr := m.Header

	detail := &gmail.MessageDetail{
		ID:      id,
		From:    decodeHeader(hdr.Get("From")),
		To:      decodeHeader(hdr.Get("To")),
		CC:      decodeHeader(hdr.Get("Cc")),
		Subject: decodeHeader(hdr.Get("Subject")),
		Date:    hdr.Get("Date"),
	}

	ctype := hdr.Get("Content-Type")
	if ctype == "" {
		ctype = "text/plain"
	}
	mediaType, params, _ := mime.ParseMediaType(ctype)
	leaf := part{
		mediaType: mediaType,
		params:    params,
		cte:       hdr.Get("Content-Transfer-Encoding"),
		body:      m.Body,
	}
	var acc mimeAccumulator
	acc.walk(leaf, 0)

	text := acc.plain
	if strings.TrimSpace(text) == "" && acc.html != "" {
		text = gmail.HTMLToText(acc.html)
	}
	detail.Body = clampRunes(strings.TrimSpace(text), maxBodyRunes)
	detail.Attachments = acc.atts
	return detail, nil
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

// mimeAccumulator collects the flattened text bodies and attachment metadata as
// the tree is walked.
type mimeAccumulator struct {
	plain string
	html  string
	atts  []gmail.AttachmentInfo
	parts int
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
				return
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
	}

	// Leaf part.
	decoded, _ := io.ReadAll(io.LimitReader(transferDecode(p.cte, p.body), maxLeafBytes))

	isText := strings.HasPrefix(p.mediaType, "text/")
	if p.attachment || (p.filename != "" && !isText) {
		a.atts = append(a.atts, gmail.AttachmentInfo{
			Filename: p.filename,
			MimeType: p.mediaType,
			Size:     len(decoded),
		})
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
	return w, err
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
