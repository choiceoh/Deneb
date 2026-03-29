package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// Gmail API JSON response types (internal).

type apiMessageList struct {
	Messages []struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	} `json:"messages"`
	NextPageToken  string `json:"nextPageToken"`
	ResultSizeEst  int    `json:"resultSizeEstimate"`
}

type apiMessage struct {
	ID       string       `json:"id"`
	ThreadID string       `json:"threadId"`
	LabelIDs []string     `json:"labelIds"`
	Snippet  string       `json:"snippet"`
	Payload  *apiPayload  `json:"payload"`
}

type apiPayload struct {
	Headers []apiHeader  `json:"headers"`
	Body    *apiBody     `json:"body"`
	Parts   []apiPayload `json:"parts"`
	MimeType string      `json:"mimeType"`
}

type apiHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type apiBody struct {
	Size int    `json:"size"`
	Data string `json:"data"`
}

type apiLabelList struct {
	Labels []apiLabel `json:"labels"`
}

type apiLabel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Search lists messages matching a Gmail query.
func (c *Client) Search(ctx context.Context, query string, maxResults int) ([]MessageSummary, error) {
	if maxResults <= 0 {
		maxResults = 10
	}

	params := url.Values{
		"q":          {query},
		"maxResults": {fmt.Sprintf("%d", maxResults)},
	}
	path := "/messages?" + params.Encode()

	var list apiMessageList
	if err := c.readJSON(ctx, path, &list); err != nil {
		return nil, err
	}

	if len(list.Messages) == 0 {
		return nil, nil
	}

	// Fetch metadata for each message in parallel.
	type indexedResult struct {
		idx int
		msg MessageSummary
		err error
	}
	results := make([]MessageSummary, len(list.Messages))
	ch := make(chan indexedResult, len(list.Messages))
	var wg sync.WaitGroup

	for i, m := range list.Messages {
		wg.Add(1)
		go func(idx int, id, threadID string) {
			defer wg.Done()
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

	return results, nil
}

// fetchMessageMetadata fetches a single message with metadata format.
func (c *Client) fetchMessageMetadata(ctx context.Context, id, threadID string) (MessageSummary, error) {
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
	}

	return detail, nil
}

// extractBody extracts the text body from a message payload,
// preferring text/plain over text/html.
func extractBody(p *apiPayload) string {
	if p == nil {
		return ""
	}

	// Single-part message.
	if p.Body != nil && p.Body.Data != "" && len(p.Parts) == 0 {
		decoded := decodeBase64URL(p.Body.Data)
		return decoded
	}

	// Multipart: search for text/plain first, then text/html.
	var plainText, htmlText string
	findBody(p, &plainText, &htmlText)

	if plainText != "" {
		return plainText
	}
	return htmlText
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

func decodeBase64URL(s string) string {
	data, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(s)
	if err != nil {
		return s
	}
	return string(data)
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
		labels[i] = LabelInfo{
			ID:   l.ID,
			Name: l.Name,
			Type: l.Type,
		}
	}
	return labels, nil
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
	fmt.Fprintf(&sb, "**ID:** %s\n\n", m.ID)
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
