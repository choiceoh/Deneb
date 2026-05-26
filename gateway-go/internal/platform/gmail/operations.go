package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// Gmail API JSON response types (internal).

type apiMessageList struct {
	Messages []struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	} `json:"messages"`
	NextPageToken string `json:"nextPageToken"`
	ResultSizeEst int    `json:"resultSizeEstimate"`
}

type apiMessage struct {
	ID       string      `json:"id"`
	ThreadID string      `json:"threadId"`
	LabelIDs []string    `json:"labelIds"`
	Snippet  string      `json:"snippet"`
	Payload  *apiPayload `json:"payload"`
}

type apiThread struct {
	ID       string       `json:"id"`
	Messages []apiMessage `json:"messages"`
}

type apiPayload struct {
	Headers  []apiHeader  `json:"headers"`
	Body     *apiBody     `json:"body"`
	Parts    []apiPayload `json:"parts"`
	MimeType string       `json:"mimeType"`
	Filename string       `json:"filename"`
}

type apiHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type apiBody struct {
	Size         int    `json:"size"`
	Data         string `json:"data"`
	AttachmentID string `json:"attachmentId"`
}

type apiLabelList struct {
	Labels []apiLabel `json:"labels"`
}

type apiLabel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Search lists messages matching a Gmail query. Equivalent to
// SearchPage with an empty pageToken; the next page token is discarded.
// Callers that want pagination (the Mini App inbox view) should use
// SearchPage directly.
func (c *Client) Search(ctx context.Context, query string, maxResults int) ([]MessageSummary, error) {
	summaries, _, err := c.SearchPage(ctx, query, "", maxResults)
	return summaries, err
}

// SearchPage lists messages matching a Gmail query and returns the
// next-page token alongside. An empty pageToken starts from the most
// recent message; an empty returned nextPageToken means no more pages.
//
// Per-message metadata fetches are capped at maxMetadataConcurrency
// in-flight goroutines (semaphore on a buffered channel) to stay under
// Gmail's per-user-per-second quota even for the max limit=100 case;
// without this a single burst of 100 metadata.get calls (~5 quota units
// each) can trip 429 RESOURCE_EXHAUSTED.
func (c *Client) SearchPage(ctx context.Context, query, pageToken string, maxResults int) ([]MessageSummary, string, error) {
	if maxResults <= 0 {
		maxResults = 10
	}

	params := url.Values{
		"q":          {query},
		"maxResults": {fmt.Sprintf("%d", maxResults)},
	}
	if pageToken != "" {
		params.Set("pageToken", pageToken)
	}
	path := "/messages?" + params.Encode()

	var list apiMessageList
	if err := c.readJSON(ctx, path, &list); err != nil {
		return nil, "", err
	}

	if len(list.Messages) == 0 {
		return nil, list.NextPageToken, nil
	}

	// Fetch metadata for each message in parallel, capped by a
	// semaphore. fetchMessageMetadata threads ctx through readJSON, so
	// a cancelled ctx short-circuits each in-flight call quickly.
	type indexedResult struct {
		idx int
		msg MessageSummary
		err error
	}
	results := make([]MessageSummary, len(list.Messages))
	ch := make(chan indexedResult, len(list.Messages))
	sem := make(chan struct{}, maxMetadataConcurrency)
	var wg sync.WaitGroup

	for i, m := range list.Messages {
		wg.Add(1)
		go func(idx int, id, threadID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			summary, err := c.fetchMessageMetadata(ctx, id, threadID)
			ch <- indexedResult{idx, summary, err}
		}(i, m.ID, m.ThreadID)
	}
	wg.Wait()
	close(ch)

	for r := range ch {
		if r.err != nil {
			// Skip failed fetches; use a stub.
			results[r.idx] = MessageSummary{ID: list.Messages[r.idx].ID, Subject: "(로드 실패)"}
			continue
		}
		results[r.idx] = r.msg
	}

	return results, list.NextPageToken, nil
}

// maxMetadataConcurrency caps the parallel metadata.get fan-out in
// SearchPage. 8 keeps a limit=100 page well under Gmail's per-user
// quota (~250 units/sec; metadata.get = 5 units) while still
// completing a typical inbox refresh in well under a second.
const maxMetadataConcurrency = 8

// fetchMessageMetadata fetches a single message with metadata format.
func (c *Client) fetchMessageMetadata(ctx context.Context, id, _ string) (MessageSummary, error) {
	path := "/messages/" + id + "?format=metadata&metadataHeaders=From&metadataHeaders=Subject&metadataHeaders=Date"

	var msg apiMessage
	if err := c.readJSON(ctx, path, &msg); err != nil {
		return MessageSummary{}, err
	}

	s := MessageSummary{
		ID:       msg.ID,
		ThreadID: msg.ThreadID,
		Snippet:  msg.Snippet,
		Labels:   msg.LabelIDs,
	}
	if msg.Payload != nil {
		for _, h := range msg.Payload.Headers {
			switch strings.ToLower(h.Name) {
			case "from":
				s.From = h.Value
			case "subject":
				s.Subject = h.Value
			case "date":
				s.Date = h.Value
			}
		}
	}
	return s, nil
}

// GetMessage fetches the full content of a message.
func (c *Client) GetMessage(ctx context.Context, messageID string) (*MessageDetail, error) {
	path := "/messages/" + messageID + "?format=full"

	var msg apiMessage
	if err := c.readJSON(ctx, path, &msg); err != nil {
		return nil, err
	}
	return messageToDetail(&msg), nil
}

// GetThread fetches every message in a conversation thread, oldest first.
func (c *Client) GetThread(ctx context.Context, threadID string) ([]*MessageDetail, error) {
	path := "/threads/" + threadID + "?format=full"

	var thread apiThread
	if err := c.readJSON(ctx, path, &thread); err != nil {
		return nil, err
	}

	details := make([]*MessageDetail, 0, len(thread.Messages))
	for i := range thread.Messages {
		details = append(details, messageToDetail(&thread.Messages[i]))
	}
	return details, nil
}

// messageToDetail maps a full-format API message into a MessageDetail.
func messageToDetail(msg *apiMessage) *MessageDetail {
	detail := &MessageDetail{
		ID:       msg.ID,
		ThreadID: msg.ThreadID,
		Labels:   msg.LabelIDs,
	}
	if msg.Payload != nil {
		for _, h := range msg.Payload.Headers {
			switch strings.ToLower(h.Name) {
			case "from":
				detail.From = h.Value
			case "to":
				detail.To = h.Value
			case "cc":
				detail.CC = h.Value
			case "subject":
				detail.Subject = h.Value
			case "date":
				detail.Date = h.Value
			}
		}
		detail.Body = extractBody(msg.Payload)
		collectAttachments(msg.Payload, &detail.Attachments)
	}
	return detail
}

// collectAttachments walks a message payload and records every part that has a
// filename (i.e. a real attachment), recursing into multipart containers.
func collectAttachments(p *apiPayload, out *[]AttachmentInfo) {
	if p == nil {
		return
	}
	if p.Filename != "" {
		info := AttachmentInfo{
			Filename: p.Filename,
			MimeType: p.MimeType,
		}
		if p.Body != nil {
			info.AttachmentID = p.Body.AttachmentID
			info.Size = p.Body.Size
		}
		*out = append(*out, info)
	}
	for i := range p.Parts {
		collectAttachments(&p.Parts[i], out)
	}
}

// GetAttachment fetches and decodes a single message attachment by its ID.
func (c *Client) GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error) {
	path := "/messages/" + messageID + "/attachments/" + attachmentID

	var att struct {
		Size int    `json:"size"`
		Data string `json:"data"`
	}
	if err := c.readJSON(ctx, path, &att); err != nil {
		return nil, err
	}

	data, ok := decodeBase64URLBytes(att.Data)
	if !ok {
		return nil, fmt.Errorf("첨부파일 base64 디코딩 실패")
	}
	return data, nil
}

// extractBody extracts the text body from a message payload,
// preferring text/plain over text/html. HTML bodies are flattened to a
// plain-text approximation so the Mini App (which renders the body as
// text inside a <pre>) doesn't end up showing raw <table>/<div> markup
// on HTML-only newsletters.
func extractBody(p *apiPayload) string {
	if p == nil {
		return ""
	}

	// Single-part message.
	if p.Body != nil && p.Body.Data != "" && len(p.Parts) == 0 {
		decoded := decodeBase64URL(p.Body.Data)
		if strings.EqualFold(p.MimeType, "text/html") {
			return htmlToText(decoded)
		}
		return decoded
	}

	// Multipart: search for text/plain first, then text/html.
	var plainText, htmlText string
	findBody(p, &plainText, &htmlText)

	if plainText != "" {
		return plainText
	}
	if htmlText != "" {
		return htmlToText(htmlText)
	}
	return ""
}

func findBody(p *apiPayload, plain, html *string) {
	if p.MimeType == "text/plain" && p.Body != nil && p.Body.Data != "" {
		*plain = decodeBase64URL(p.Body.Data)
	}
	if p.MimeType == "text/html" && p.Body != nil && p.Body.Data != "" && *html == "" {
		*html = decodeBase64URL(p.Body.Data)
	}
	for i := range p.Parts {
		findBody(&p.Parts[i], plain, html)
	}
}

// HTML cleanup regexes (compiled once).
//
//	htmlDropREs  — <script>/<style>/<head> blocks including their content.
//	               RE2 has no backreferences, so each tag gets its own pattern.
//	htmlParaRE   — paragraph-level boundaries that become a blank line so
//	               paragraphs stay visually separated.
//	htmlLineRE   — line-level boundaries that become a single newline.
//	htmlAnyTagRE — any remaining tag (stripped without leaving artifacts).
//	htmlBlankRE  — collapse runs of 3+ newlines into a single blank line.
var (
	htmlDropREs = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`),
		regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style\s*>`),
		regexp.MustCompile(`(?is)<head\b[^>]*>.*?</head\s*>`),
	}
	htmlParaRE   = regexp.MustCompile(`(?i)</(?:p|div|h[1-6]|blockquote)\s*>`)
	htmlLineRE   = regexp.MustCompile(`(?i)<(?:br\s*/?|hr\s*/?|/li|/tr)\s*[^>]*>`)
	htmlAnyTagRE = regexp.MustCompile(`(?s)<[^>]+>`)
	htmlBlankRE  = regexp.MustCompile(`\n{3,}`)
)

// htmlToText turns an HTML email body into a readable plain-text
// approximation for the Mini App's <pre>-based body view. It is regex-
// based on purpose: Gmail HTML is usually well-formed enough, and a
// perfect HTML→text render isn't the goal — we just need to keep
// raw <table>/<div style="..."> markup from leaking into the UI.
func htmlToText(s string) string {
	if s == "" {
		return s
	}
	for _, re := range htmlDropREs {
		s = re.ReplaceAllString(s, "")
	}
	s = htmlParaRE.ReplaceAllString(s, "\n\n")
	s = htmlLineRE.ReplaceAllString(s, "\n")
	s = htmlAnyTagRE.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	// &nbsp; decodes to U+00A0 which renders as a space but trips up any
	// downstream splitter that expects ASCII whitespace. Normalize.
	s = strings.ReplaceAll(s, " ", " ")

	// Trim trailing whitespace per line, then collapse runs of blank
	// lines so newsletter templates don't render as a tall column of
	// empty lines.
	var b strings.Builder
	b.Grow(len(s))
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(line, " \t\r"))
	}
	out := htmlBlankRE.ReplaceAllString(b.String(), "\n\n")
	return strings.TrimSpace(out)
}

func decodeBase64URL(s string) string {
	if data, ok := decodeBase64URLBytes(s); ok {
		return string(data)
	}
	return s
}

// decodeBase64URLBytes decodes Gmail web-safe base64 into raw bytes. Gmail may
// send it with or without "=" padding, using the url-safe (-_) or standard
// (+/) alphabet, and MIME parts can wrap it across lines — the old strict
// decoder (URLEncoding + NoPadding) rejected padded or wrapped input and
// silently returned the raw base64. Normalize whitespace and padding, then try
// each alphabet. Returns ok=false only when the input is genuinely not base64.
func decodeBase64URLBytes(s string) ([]byte, bool) {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', ' ', '\t':
			return -1
		}
		return r
	}, s)
	s = strings.TrimRight(s, "=")
	if data, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return data, true
	}
	if data, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return data, true
	}
	return nil, false
}

// Send composes and sends an email. Returns the sent message ID.
func (c *Client) Send(ctx context.Context, to, cc, bcc, subject, body string, html bool) (string, error) {
	raw := buildMIME(to, cc, bcc, subject, "", body, html)
	return c.sendRaw(ctx, raw, "")
}

// Reply sends a reply to an existing message. Returns the sent message ID.
func (c *Client) Reply(ctx context.Context, messageID, to, body string, html bool) (string, error) {
	// Fetch original to get threadId, Message-ID, Subject.
	orig, err := c.getMessageHeaders(ctx, messageID)
	if err != nil {
		return "", fmt.Errorf("원본 메시지 조회 실패: %w", err)
	}

	replySubject := orig.subject
	if !strings.HasPrefix(strings.ToLower(replySubject), "re:") {
		replySubject = "Re: " + replySubject
	}

	// If no explicit to, reply to the original sender.
	if to == "" {
		to = orig.from
	}

	raw := buildMIME(to, "", "", replySubject, orig.messageIDHeader, body, html)
	return c.sendRaw(ctx, raw, orig.threadID)
}

type origHeaders struct {
	threadID        string
	from            string
	subject         string
	messageIDHeader string // the Message-ID header value
}

func (c *Client) getMessageHeaders(ctx context.Context, id string) (*origHeaders, error) {
	path := "/messages/" + id + "?format=metadata&metadataHeaders=From&metadataHeaders=Subject&metadataHeaders=Message-ID"

	var msg apiMessage
	if err := c.readJSON(ctx, path, &msg); err != nil {
		return nil, err
	}

	h := &origHeaders{threadID: msg.ThreadID}
	if msg.Payload != nil {
		for _, hdr := range msg.Payload.Headers {
			switch strings.ToLower(hdr.Name) {
			case "from":
				h.from = hdr.Value
			case "subject":
				h.subject = hdr.Value
			case "message-id":
				h.messageIDHeader = hdr.Value
			}
		}
	}
	return h, nil
}

// buildMIME constructs an RFC 2822 message.
func buildMIME(to, cc, bcc, subject, inReplyTo, body string, html bool) string {
	var sb strings.Builder
	sb.WriteString("To: " + to + "\r\n")
	if cc != "" {
		sb.WriteString("Cc: " + cc + "\r\n")
	}
	if bcc != "" {
		sb.WriteString("Bcc: " + bcc + "\r\n")
	}
	sb.WriteString("Subject: " + subject + "\r\n")
	if inReplyTo != "" {
		sb.WriteString("In-Reply-To: " + inReplyTo + "\r\n")
		sb.WriteString("References: " + inReplyTo + "\r\n")
	}

	if html {
		sb.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	} else {
		sb.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	}
	sb.WriteString("\r\n")
	sb.WriteString(body)

	return sb.String()
}

// sendRaw sends a pre-built RFC 2822 message.
func (c *Client) sendRaw(ctx context.Context, rawMessage, threadID string) (string, error) {
	encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(rawMessage))

	payload := map[string]string{"raw": encoded}
	if threadID != "" {
		payload["threadId"] = threadID
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := c.postJSON(ctx, "/messages/send", payload, &result); err != nil {
		return "", err
	}
	return result.ID, nil
}

// ListLabels returns all labels for the user.
func (c *Client) ListLabels(ctx context.Context) ([]LabelInfo, error) {
	var list apiLabelList
	if err := c.readJSON(ctx, "/labels", &list); err != nil {
		return nil, err
	}

	labels := make([]LabelInfo, len(list.Labels))
	for i, l := range list.Labels {
		labels[i] = LabelInfo(l)
	}
	return labels, nil
}

// Trash moves a message to Gmail's Trash folder. Recoverable from the
// user's Trash UI for ~30 days; uses the dedicated /trash endpoint so
// we don't have to resolve the TRASH label ID via ListLabels.
func (c *Client) Trash(ctx context.Context, messageID string) error {
	return c.postJSON(ctx, "/messages/"+messageID+"/trash", struct{}{}, nil)
}

// ModifyLabels adds and/or removes labels on a message.
// Label names are resolved to IDs automatically.
func (c *Client) ModifyLabels(ctx context.Context, messageID string, addNames, removeNames []string) error {
	addIDs, err := c.resolveLabels(ctx, addNames)
	if err != nil {
		return err
	}
	removeIDs, err := c.resolveLabels(ctx, removeNames)
	if err != nil {
		return err
	}

	payload := map[string][]string{}
	if len(addIDs) > 0 {
		payload["addLabelIds"] = addIDs
	}
	if len(removeIDs) > 0 {
		payload["removeLabelIds"] = removeIDs
	}

	return c.postJSON(ctx, "/messages/"+messageID+"/modify", payload, nil)
}

// resolveLabels maps label names to their IDs.
func (c *Client) resolveLabels(ctx context.Context, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}

	labels, err := c.ListLabels(ctx)
	if err != nil {
		return nil, err
	}

	// Build name→ID map (case-insensitive).
	nameMap := make(map[string]string, len(labels))
	for _, l := range labels {
		nameMap[strings.ToLower(l.Name)] = l.ID
	}

	ids := make([]string, 0, len(names))
	for _, name := range names {
		id, ok := nameMap[strings.ToLower(name)]
		if !ok {
			return nil, fmt.Errorf("라벨을 찾을 수 없음: %q", name)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// FormatSearchResults formats message summaries into a readable string.
func FormatSearchResults(msgs []MessageSummary) string {
	if len(msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, m := range msgs {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "**%s** — %s\n", m.From, m.Date)
		fmt.Fprintf(&sb, "  %s\n", m.Subject)
		if m.Snippet != "" {
			fmt.Fprintf(&sb, "  %s\n", m.Snippet)
		}
		fmt.Fprintf(&sb, "  ID: %s", m.ID)
	}
	return sb.String()
}

// FormatMessage formats a full message detail into a readable string.
func FormatMessage(m *MessageDetail) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**From:** %s\n", m.From)
	fmt.Fprintf(&sb, "**To:** %s\n", m.To)
	if m.CC != "" {
		fmt.Fprintf(&sb, "**CC:** %s\n", m.CC)
	}
	fmt.Fprintf(&sb, "**Subject:** %s\n", m.Subject)
	fmt.Fprintf(&sb, "**Date:** %s\n", m.Date)
	fmt.Fprintf(&sb, "**ID:** %s\n", m.ID)
	if len(m.Attachments) > 0 {
		names := make([]string, len(m.Attachments))
		for i, a := range m.Attachments {
			names[i] = a.Filename
		}
		fmt.Fprintf(&sb, "**첨부:** %s  (gmail attachment 액션으로 내용 확인)\n", strings.Join(names, ", "))
	}
	sb.WriteString("\n")
	sb.WriteString(m.Body)
	return sb.String()
}

// FormatLabels formats label info into a readable list.
func FormatLabels(labels []LabelInfo) string {
	if len(labels) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, l := range labels {
		ltype := ""
		if l.Type == "system" {
			ltype = " (시스템)"
		}
		fmt.Fprintf(&sb, "- %s%s\n", l.Name, ltype)
	}
	return sb.String()
}
