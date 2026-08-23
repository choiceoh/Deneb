package mailarchive

import (
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive/overlay"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailbody"
)

func detailToSummary(detail *gmail.MessageDetail, mailbox string, st overlay.MessageState, evidenceQuery string) gmail.MessageSummary {
	return gmail.MessageSummary{
		ID:              detail.ID,
		ThreadID:        detail.ThreadID,
		From:            detail.From,
		Subject:         detail.Subject,
		Date:            detail.Date,
		Snippet:         snippetFromBodyAroundQuery(detail.Body, evidenceQuery),
		Labels:          labelsForArchiveMessage(mailbox, st),
		Mailbox:         mailbox,
		HasAttachment:   len(detail.Attachments) > 0,
		AttachmentCount: len(detail.Attachments),
	}
}

func (r *Repository) applyStateToDetail(detail *gmail.MessageDetail, st overlay.MessageState) {
	if detail == nil {
		return
	}
	mailbox := st.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}
	detail.Labels = labelsForArchiveMessage(mailbox, st)
}

func labelsForArchiveMessage(mailbox string, st overlay.MessageState) []string {
	if st.Trashed {
		return []string{"TRASH"}
	}
	var labels []string
	if strings.EqualFold(strings.TrimSpace(mailbox), "INBOX") && !st.Archived {
		labels = append(labels, "INBOX")
		if !st.Read {
			labels = append(labels, "UNREAD")
		}
	}
	if labels == nil {
		return []string{}
	}
	return labels
}

func cloneDetail(detail *gmail.MessageDetail) *gmail.MessageDetail {
	if detail == nil {
		return nil
	}
	cp := *detail
	cp.Labels = append([]string(nil), detail.Labels...)
	cp.Attachments = append([]gmail.AttachmentInfo(nil), detail.Attachments...)
	cp.References = append([]string(nil), detail.References...)
	return &cp
}

func snippetFromBody(body string) string {
	return snippetFromBodyAroundQuery(body, "")
}

func snippetFromBodyAroundQuery(body, query string) string {
	if cleaned := strings.TrimSpace(mailbody.CleanForDisplay(body).Body); cleaned != "" {
		body = cleaned
	}
	body = strings.TrimSpace(strings.Join(strings.Fields(body), " "))
	const maxRunes = 360
	runes := []rune(body)
	if len(runes) <= maxRunes {
		return body
	}
	query = strings.TrimSpace(strings.Join(strings.Fields(query), " "))
	if query == "" {
		return string(runes[:maxRunes]) + "..."
	}
	lowerBody := strings.ToLower(body)
	byteIndex := strings.Index(lowerBody, strings.ToLower(query))
	if byteIndex < 0 {
		return string(runes[:maxRunes]) + "..."
	}
	matchStart := utf8.RuneCountInString(lowerBody[:byteIndex])
	start := max(0, matchStart-120)
	end := min(len(runes), start+maxRunes)
	if end-start < maxRunes {
		start = max(0, end-maxRunes)
	}
	snippet := string(runes[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(runes) {
		snippet += "..."
	}
	return snippet
}

func archiveLocator(mailbox, uid string) string {
	return archiveLocatorPrefix + url.QueryEscape(mailbox) + "|" + url.QueryEscape(uid)
}

func archiveLocatorParts(id string) (string, string, bool) {
	if !strings.HasPrefix(id, archiveLocatorPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(id, archiveLocatorPrefix)
	parts := strings.Split(rest, "|")
	if len(parts) != 2 {
		return "", "", false
	}
	mailbox, err1 := url.QueryUnescape(parts[0])
	uid, err2 := url.QueryUnescape(parts[1])
	if err1 != nil || err2 != nil || mailbox == "" || uid == "" {
		return "", "", false
	}
	return mailbox, uid, true
}

func tailStrings(in []string, n int) []string {
	if n <= 0 || len(in) <= n {
		return append([]string(nil), in...)
	}
	return append([]string(nil), in[len(in)-n:]...)
}

func reverseStrings(in []string) {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
}

func parseUID(uid string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(uid))
	return n
}

func latestUID(uids []string) string {
	if len(uids) == 0 {
		return ""
	}
	latest := strings.TrimSpace(uids[0])
	latestN := parseUID(latest)
	for _, uid := range uids[1:] {
		n := parseUID(uid)
		if n > latestN {
			latest = strings.TrimSpace(uid)
			latestN = n
		}
	}
	return latest
}

func nativeOverlayStatus(snapshot map[string]overlay.MessageState) NativeOverlayStatus {
	var out NativeOverlayStatus
	out.Messages = len(snapshot)
	for _, st := range snapshot {
		if st.Read {
			out.Read++
		}
		if st.Archived {
			out.Archived++
		}
		if st.Trashed {
			out.Trashed++
		}
	}
	return out
}
