// operations.go — Gmail read operations: API JSON response types, message
// search/pagination with parallel metadata fan-out, and message/thread/
// attachment fetch. Send/label mutations live in send_labels.go, body
// extraction in message_body.go, and transcript rendering in format.go.
package gmail

import (
	"context"
	"fmt"
	"html"
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
// Per-message metadata fetches fan out in parallel, sized by
// metadataConcurrency: a normal screenful goes out in a single round,
// while larger pages stay capped so a burst of metadata.get calls
// (~5 quota units each) can't trip 429 RESOURCE_EXHAUSTED on the max
// limit=100 case.
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
	// Scale the fan-out to the page size: a single screenful (the default
	// limit=20) fires all its metadata.get calls in one round instead of
	// being serialized across multiple semaphore rounds, which is the
	// dominant cost of a cold inbox load (each round is a full API
	// round-trip). Larger custom pages stay throttled for quota safety.
	sem := make(chan struct{}, metadataConcurrency(len(list.Messages)))
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

// Gmail allows ~250 quota units per user per second; a metadata.get
// costs 5 units, so ~50 gets/sec is the sustained-safe rate. Each
// round-trip to the Gmail API is ~330ms in practice, so the real cost
// of a cold inbox load is the *number of rounds* the fan-out is split
// into, not raw quota.
//
//   - metadataFanoutDefault lets the default page (limit=20) fire every
//     metadata.get in a single round: ~20 gets complete in ~660ms
//     (~30 gets/sec), comfortably under quota.
//   - metadataFanoutLarge throttles larger custom pages (up to limit=100)
//     so a 100-message page can't burst past the per-second quota —
//     16 in flight ≈ 215 units/sec sustained across its rounds.
const (
	metadataFanoutDefault = 20
	metadataFanoutLarge   = 16
)

// metadataConcurrency picks the parallel fan-out for fetching n message
// summaries: never more slots than messages, a single round for a normal
// screenful, and a quota-safe ceiling once a custom page exceeds it.
func metadataConcurrency(n int) int {
	if n > metadataFanoutDefault {
		return metadataFanoutLarge
	}
	if n < 1 {
		return 1
	}
	return n
}

// decodeMailEntities undoes the HTML entities that leak into otherwise-plain mail
// text — both Gmail snippets and text/plain bodies. Gmail double-encodes snippets
// (&#39; &quot; &amp; …), and some senders (Korean mail clients especially) put
// literal &nbsp; / &amp; in the text/plain *body* part instead of real characters,
// so "주소 :&nbsp;경기" would otherwise show the raw entity. Some signatures are
// double-encoded (&amp;nbsp;), so after the single UnescapeString a literal
// "&nbsp;" remains — collapse those to spaces. A second blanket UnescapeString is
// avoided because it would corrupt intentionally escaped text like "&amp;lt;".
func decodeMailEntities(s string) string {
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "&nbsp;", " ") // leftover from a double-encoded &amp;nbsp;
	s = strings.ReplaceAll(s, "\u00A0", " ") // NBSP from a single &nbsp; → a regular space
	// Webmail composers pad "blank" lines with a zero-width space (U+200B); it is not
	// whitespace, so it survives as a non-empty line and renders as a phantom gap. Drop it.
	s = strings.ReplaceAll(s, "\u200B", "")
	return s
}

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
		Snippet:  decodeMailEntities(msg.Snippet),
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
