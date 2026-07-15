package gmailops

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
)

// rowPriority resolves one row's glanceable marker, layered: the per-mail
// LLM analysis verdict is authoritative when cached (it read the full body
// and thread context), the cheap heuristic covers everything not yet — or
// never — analyzed. An analyzed-routine verdict suppresses the heuristic so
// a body-aware "FYI" beats a subject-level keyword match.
func rowPriority(deps GmailDeps, msgID, from, subject, snippet string) (string, string) {
	if rec, err := deps.AnalysisCache.Load(msgID); err == nil && rec != nil {
		switch rec.Importance {
		case "urgent", "attention":
			return rec.Importance, "분석 판정"
		case "routine":
			return "", ""
		}
		// "" (v2 record without a parseable tag): fall through to heuristic.
	}
	if deps.Priority == nil {
		return "", ""
	}
	return deps.Priority(from, subject, snippet)
}

func nonNilLabels(labels []string) []string {
	if labels == nil {
		return []string{}
	}
	return labels
}

type mailWorkFilter string

const (
	mailWorkFilterAnalysisFailed    mailWorkFilter = "analysis_failed"
	mailWorkFilterFeedMissing       mailWorkFilter = "feed_missing"
	mailWorkFilterCalendarCandidate mailWorkFilter = "calendar_candidate"
	mailWorkFilterTodo              mailWorkFilter = "todo"
)

func parseMailWorkQuery(query string) (string, mailWorkFilter) {
	var filter mailWorkFilter
	fields := strings.Fields(strings.TrimSpace(query))
	kept := make([]string, 0, len(fields))
	for _, f := range fields {
		if !strings.HasPrefix(f, "deneb:") {
			kept = append(kept, f)
			continue
		}
		switch strings.TrimPrefix(f, "deneb:") {
		case string(mailWorkFilterAnalysisFailed):
			filter = mailWorkFilterAnalysisFailed
		case string(mailWorkFilterFeedMissing):
			filter = mailWorkFilterFeedMissing
		case string(mailWorkFilterCalendarCandidate):
			filter = mailWorkFilterCalendarCandidate
		case string(mailWorkFilterTodo):
			filter = mailWorkFilterTodo
		default:
			kept = append(kept, f)
		}
	}
	return strings.Join(kept, " "), filter
}

func (f mailWorkFilter) matches(st mailwork.MessageState) bool {
	switch f {
	case "":
		return true
	case mailWorkFilterAnalysisFailed:
		return st.AnalysisStatus == mailwork.AnalysisFailed
	case mailWorkFilterFeedMissing:
		return st.AnalysisStatus == mailwork.AnalysisDone && st.FeedStatus != mailwork.FeedCreated
	case mailWorkFilterCalendarCandidate:
		return st.CalendarProposalCount > 0
	case mailWorkFilterTodo:
		return st.TodoCount > 0
	default:
		return true
	}
}

func messageInputFromSummary(m gmail.MessageSummary) mailwork.MessageInput {
	return mailwork.MessageInput{
		ID:              m.ID,
		ThreadID:        m.ThreadID,
		From:            m.From,
		Subject:         m.Subject,
		Date:            NormalizeDate(m.Date),
		Mailbox:         m.Mailbox,
		HasAttachment:   m.HasAttachment || m.AttachmentCount > 0,
		AttachmentCount: m.AttachmentCount,
	}
}

// MessageInputFromDetail adapts a *gmail.MessageDetail into the shared
// mailwork.MessageInput shape. Exported so the sibling analyzebind
// package's analyze/ask handlers can seed the same workflow-state
// bookkeeping this package uses for list rows.
func MessageInputFromDetail(msg *gmail.MessageDetail) mailwork.MessageInput {
	if msg == nil {
		return mailwork.MessageInput{}
	}
	return mailwork.MessageInput{
		ID:              msg.ID,
		ThreadID:        msg.ThreadID,
		From:            msg.From,
		Subject:         msg.Subject,
		Date:            NormalizeDate(msg.Date),
		HasAttachment:   len(msg.Attachments) > 0,
		AttachmentCount: len(msg.Attachments),
	}
}

func mailWorkStateForSummary(deps GmailDeps, m gmail.MessageSummary) mailwork.MessageState {
	if deps.WorkState == nil {
		return mailwork.MessageState{}
	}
	st, _ := deps.WorkState.RememberMessage(messageInputFromSummary(m))
	if st.AnalysisStatus != "" {
		return st
	}
	if deps.AnalysisCache != nil {
		if rec, err := deps.AnalysisCache.Load(m.ID); err == nil && rec != nil {
			st, _ = deps.WorkState.MarkAnalysisDone(mailwork.AnalysisInput{
				MessageInput: messageInputFromSummary(m),
				Quality:      rec.Importance,
				DurationMs:   rec.DurationMs,
			})
		}
	}
	return st
}

func mailWorkStateForDetail(deps GmailDeps, msg *gmail.MessageDetail) mailwork.MessageState {
	if deps.WorkState == nil || msg == nil {
		return mailwork.MessageState{}
	}
	st, _ := deps.WorkState.RememberMessage(MessageInputFromDetail(msg))
	if st.AnalysisStatus != "" {
		return st
	}
	if deps.AnalysisCache != nil {
		if rec, err := deps.AnalysisCache.Load(msg.ID); err == nil && rec != nil {
			st, _ = deps.WorkState.MarkAnalysisDone(mailwork.AnalysisInput{
				MessageInput: MessageInputFromDetail(msg),
				Quality:      rec.Importance,
				DurationMs:   rec.DurationMs,
			})
		}
	}
	return st
}

func mailWorkCacheRevision(deps GmailDeps) int64 {
	if deps.WorkState == nil {
		return 0
	}
	return deps.WorkState.Summary().UpdatedAtMs
}

func applyMailWorkRow(row *mailRowOut, st mailwork.MessageState) {
	if row == nil {
		return
	}
	row.AnalysisStatus = st.AnalysisStatus
	row.AnalysisQuality = st.AnalysisQuality
	row.FeedStatus = st.FeedStatus
	row.CalendarProposalCount = st.CalendarProposalCount
	row.TodoCount = st.TodoCount
	row.WorkStateHint = st.Hint()
}

func applyMailWorkMessage(out *mailMessageOut, st mailwork.MessageState) {
	if out == nil {
		return
	}
	out.AnalysisStatus = st.AnalysisStatus
	out.AnalysisQuality = st.AnalysisQuality
	out.FeedStatus = st.FeedStatus
	out.CalendarProposalCount = st.CalendarProposalCount
	out.TodoCount = st.TodoCount
	out.WorkStateHint = st.Hint()
}

func mailRelatedProjects(deps GmailDeps, msgID string) []ProjectRef {
	if deps.AnalysisCache == nil {
		return nil
	}
	rec, err := deps.AnalysisCache.Load(msgID)
	if err != nil || rec == nil || len(rec.RelatedProjects) == 0 {
		return nil
	}
	refs := make([]ProjectRef, 0, len(rec.RelatedProjects))
	for _, path := range rec.RelatedProjects {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		refs = append(refs, ProjectRef{Path: path})
	}
	return refs
}

func appendMailRows(out []mailRowOut, deps GmailDeps, results []gmail.MessageSummary, workFilter mailWorkFilter, limit int) []mailRowOut {
	for _, m := range results {
		state := mailWorkStateForSummary(deps, m)
		if !workFilter.matches(state) {
			continue
		}
		snippet := mailSnippetForDisplay(m.Snippet)
		row := mailRowOut{
			ID:              m.ID,
			ThreadID:        m.ThreadID,
			From:            m.From,
			Subject:         m.Subject,
			Snippet:         snippet,
			Date:            NormalizeDate(m.Date),
			IsUnread:        hasUnreadLabel(m.Labels),
			Labels:          nonNilLabels(m.Labels),
			Mailbox:         m.Mailbox,
			HasAttachment:   m.HasAttachment || m.AttachmentCount > 0,
			AttachmentCount: m.AttachmentCount,
			RelatedProjects: mailRelatedProjects(deps, m.ID),
		}
		row.Priority, row.PriorityHint = rowPriority(deps, m.ID, m.From, m.Subject, snippet)
		applyMailWorkRow(&row, state)
		out = append(out, row)
		if len(out) >= limit {
			break
		}
	}
	return out
}
