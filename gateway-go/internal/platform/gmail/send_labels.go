// send_labels.go — Gmail write operations: send/reply MIME composition and
// the label/trash mutations (ListLabels, Trash, ModifyLabels, label name
// resolution). Split from operations.go (pure move).
package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

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
