package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
)

type MailArchiveDeps struct {
	Wiki     *wiki.Store
	Calendar *toolctx.CalendarDeps
}

// ToolMailArchive reads the on-box mail archive (the deneb-mailarchive IMAP store)
// so the agent can review received mail locally. Archive credentials
// come from env (DENEB_ARCHIVE_IMAP_USER/PASS; addr default 127.0.0.1:1143);
// without them the tool reports that it is unconfigured rather than erroring.
func ToolMailArchive(optional ...MailArchiveDeps) func(ctx context.Context, input json.RawMessage) (string, error) {
	deps := MailArchiveDeps{}
	if len(optional) > 0 {
		deps = optional[0]
	}
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var args struct {
			Action      string `json:"action"`
			Mailbox     string `json:"mailbox"`
			Days        int    `json:"days"`
			Query       string `json:"query"`
			MessageID   string `json:"message_id"`
			Limit       int    `json:"limit"`
			IncludeBody bool   `json:"include_body"`
			AsJSON      bool   `json:"as_json"`
		}
		_ = json.Unmarshal(input, &args)

		configuredMailboxes := mailarchive.ParseMailboxList(os.Getenv("DENEB_ARCHIVE_IMAP_MAILBOXES"))
		mailboxes := mailarchive.SelectMailboxes(args.Mailbox, configuredMailboxes)
		cfg := mailarchive.Config{
			Addr:      mailArchiveAddr(),
			User:      os.Getenv("DENEB_ARCHIVE_IMAP_USER"),
			Pass:      os.Getenv("DENEB_ARCHIVE_IMAP_PASS"),
			Mailboxes: mailboxes,
		}
		if cfg.User == "" || cfg.Pass == "" {
			return "메일 아카이브가 설정되지 않았습니다 (DENEB_ARCHIVE_IMAP_USER/PASS 미설정).", nil
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 50
		}
		opts := mailarchive.ContextOptions{
			Mailboxes: mailboxes,
			Limit:     limit,
			BodyRunes: mailArchiveBodyRunes(args.IncludeBody),
		}

		switch args.Action {
		case "search":
			if strings.TrimSpace(args.Query) == "" {
				return "", fmt.Errorf("search에는 query가 필요합니다")
			}
			msgs, err := mailarchive.SearchContextMessages(ctx, cfg, args.Query, opts)
			if err != nil {
				return "", fmt.Errorf("아카이브 검색 실패: %w", err)
			}
			if args.AsJSON {
				return marshalMailArchiveResponse(mailArchiveResponse{
					Action:    "search",
					Mailboxes: mailboxes,
					Count:     len(msgs),
					Messages:  enrichArchiveMessages(ctx, deps, msgs, args.IncludeBody),
				})
			}
			return formatArchiveMessages(fmt.Sprintf("'%s' 검색 결과 (%s)", args.Query, mailArchiveMailboxLabel(mailboxes)), msgs, args.IncludeBody), nil
		case "read":
			msg, err := mailarchive.ReadContextMessage(ctx, cfg, args.MessageID, args.Query, opts)
			if err != nil {
				return "", fmt.Errorf("아카이브 메일 열기 실패: %w", err)
			}
			enriched := enrichArchiveMessage(ctx, deps, msg, true)
			if args.AsJSON {
				return marshalMailArchiveResponse(mailArchiveResponse{
					Action:    "read",
					Mailboxes: mailboxes,
					Count:     1,
					Message:   &enriched,
				})
			}
			out := formatArchiveRead(msg)
			if related := formatMailArchiveRelated(enriched); related != "" {
				out += "\n\n" + related
			}
			return out, nil
		case "thread":
			msgs, err := mailarchive.ThreadContext(ctx, cfg, args.MessageID, args.Query, opts)
			if err != nil {
				return "", fmt.Errorf("아카이브 스레드 조회 실패: %w", err)
			}
			enriched := enrichArchiveMessages(ctx, deps, msgs, true)
			if args.AsJSON {
				return marshalMailArchiveResponse(mailArchiveResponse{
					Action:    "thread",
					Mailboxes: mailboxes,
					Count:     len(enriched),
					Messages:  enriched,
				})
			}
			out := formatArchiveThread(msgs)
			if related := formatMailArchiveRelatedSummary(enriched); related != "" {
				out += "\n\n" + related
			}
			return out, nil
		case "project_history", "history":
			if strings.TrimSpace(args.Query) == "" {
				return "", fmt.Errorf("project_history에는 query가 필요합니다")
			}
			days := args.Days
			if days > 0 {
				opts.Since = time.Now().AddDate(0, 0, -(days - 1))
			}
			history, err := mailarchive.ProjectHistoryContext(ctx, cfg, args.Query, opts)
			if err != nil {
				return "", fmt.Errorf("프로젝트 히스토리 조회 실패: %w", err)
			}
			enriched := enrichProjectHistory(ctx, deps, history, args.IncludeBody)
			if args.AsJSON {
				return marshalMailArchiveResponse(mailArchiveResponse{
					Action:    "project_history",
					Mailboxes: mailboxes,
					Count:     len(enriched.History.Messages),
					History:   &enriched.History,
				})
			}
			out := formatProjectHistory(history, args.IncludeBody)
			if related := formatMailArchiveRelatedSummary(enriched.History.Messages); related != "" {
				out += "\n\n" + related
			}
			return out, nil
		case "list", "":
			days := args.Days
			if days <= 0 {
				days = 1
			}
			since := time.Now().AddDate(0, 0, -(days - 1))
			msgs, err := mailarchive.ListContextMessages(ctx, cfg, since, opts)
			if days == 1 {
				if err != nil {
					return "", fmt.Errorf("아카이브 목록 조회 실패: %w", err)
				}
				if args.AsJSON {
					return marshalMailArchiveResponse(mailArchiveResponse{
						Action:    "list",
						Mailboxes: mailboxes,
						Count:     len(msgs),
						Messages:  enrichArchiveMessages(ctx, deps, msgs, args.IncludeBody),
					})
				}
				return formatArchiveMessages(fmt.Sprintf("오늘 수신 메일 (%s)", mailArchiveMailboxLabel(mailboxes)), msgs, args.IncludeBody), nil
			} else {
				if err != nil {
					return "", fmt.Errorf("아카이브 목록 조회 실패: %w", err)
				}
				if args.AsJSON {
					return marshalMailArchiveResponse(mailArchiveResponse{
						Action:    "list",
						Mailboxes: mailboxes,
						Count:     len(msgs),
						Messages:  enrichArchiveMessages(ctx, deps, msgs, args.IncludeBody),
					})
				}
				return formatArchiveMessages(fmt.Sprintf("최근 %d일 메일 (%s)", days, mailArchiveMailboxLabel(mailboxes)), msgs, args.IncludeBody), nil
			}
		default:
			return "", fmt.Errorf("알 수 없는 action %q (list|search|read|thread|project_history)", args.Action)
		}
	}
}

func mailArchiveAddr() string {
	if v := strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_IMAP_ADDR")); v != "" {
		return v
	}
	return "127.0.0.1:1143"
}

func mailArchiveMailboxLabel(mailboxes []string) string {
	if len(mailboxes) == 0 {
		return "all"
	}
	return strings.Join(mailboxes, "+")
}

func mailArchiveBodyRunes(includeBody bool) int {
	if includeBody {
		return 6000
	}
	return 2400
}

type mailArchiveResponse struct {
	Action    string                  `json:"action"`
	Mailboxes []string                `json:"mailboxes"`
	Count     int                     `json:"count"`
	Message   *mailArchiveMessageOut  `json:"message,omitempty"`
	Messages  []mailArchiveMessageOut `json:"messages,omitempty"`
	History   *mailArchiveHistoryOut  `json:"history,omitempty"`
}

type mailArchiveHistoryOut struct {
	Query     string                      `json:"query"`
	IndexUsed bool                        `json:"index_used,omitempty"`
	Threads   []mailarchive.ProjectThread `json:"threads"`
	Messages  []mailArchiveMessageOut     `json:"messages"`
}

type mailArchiveMessageOut struct {
	mailarchive.ContextMessage
	RelatedWiki   []mailArchiveWikiHit  `json:"related_wiki,omitempty"`
	RelatedEvents []mailArchiveEventHit `json:"related_events,omitempty"`
}

type mailArchiveWikiHit struct {
	Path    string  `json:"path"`
	Snippet string  `json:"snippet,omitempty"`
	Score   float64 `json:"score,omitempty"`
}

type mailArchiveEventHit struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Source      string `json:"source,omitempty"`
	SourceLabel string `json:"source_label,omitempty"`
}

func marshalMailArchiveResponse(resp mailArchiveResponse) (string, error) {
	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func enrichProjectHistory(ctx context.Context, deps MailArchiveDeps, history mailarchive.ProjectHistory, includeBody bool) struct {
	History mailArchiveHistoryOut
} {
	return struct {
		History mailArchiveHistoryOut
	}{
		History: mailArchiveHistoryOut{
			Query:     history.Query,
			IndexUsed: history.IndexUsed,
			Threads:   history.Threads,
			Messages:  enrichArchiveMessages(ctx, deps, history.Messages, includeBody),
		},
	}
}

func enrichArchiveMessages(ctx context.Context, deps MailArchiveDeps, msgs []mailarchive.ContextMessage, includeBody bool) []mailArchiveMessageOut {
	out := make([]mailArchiveMessageOut, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, enrichArchiveMessage(ctx, deps, msg, includeBody))
	}
	return out
}

func enrichArchiveMessage(ctx context.Context, deps MailArchiveDeps, msg mailarchive.ContextMessage, includeBody bool) mailArchiveMessageOut {
	if !includeBody {
		msg.Body = ""
	}
	return mailArchiveMessageOut{
		ContextMessage: msg,
		RelatedWiki:    relatedArchiveWiki(ctx, deps.Wiki, msg),
		RelatedEvents:  relatedArchiveEvents(deps.Calendar, msg),
	}
}

func relatedArchiveWiki(ctx context.Context, store *wiki.Store, msg mailarchive.ContextMessage) []mailArchiveWikiHit {
	if store == nil {
		return nil
	}
	query := archiveRelatedQuery(msg)
	if query == "" {
		return nil
	}
	hits, err := store.Search(ctx, query, 3)
	if err != nil || len(hits) == 0 {
		return nil
	}
	out := make([]mailArchiveWikiHit, 0, len(hits))
	for _, hit := range hits {
		out = append(out, mailArchiveWikiHit{Path: hit.Path, Snippet: hit.Content, Score: hit.Score})
	}
	return out
}

func relatedArchiveEvents(deps *toolctx.CalendarDeps, msg mailarchive.ContextMessage) []mailArchiveEventHit {
	if deps == nil || deps.Local == nil {
		return nil
	}
	center := parseArchiveToolDate(msg.Date)
	if center.IsZero() {
		center = time.Now()
	}
	events := deps.Local.ListRange(center.AddDate(-1, 0, 0), center.AddDate(1, 0, 0))
	out := make([]mailArchiveEventHit, 0, 5)
	for _, ev := range events {
		if !archiveEventRelated(ev.Source, ev.SourceLabel, ev.Summary, ev.Description, msg) {
			continue
		}
		out = append(out, mailArchiveEventHit{
			ID:          ev.ID,
			Title:       ev.Summary,
			Start:       formatMailArchiveEventTime(ev.Start),
			End:         formatMailArchiveEventTime(ev.End),
			Kind:        ev.Kind,
			Source:      ev.Source,
			SourceLabel: ev.SourceLabel,
		})
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func formatMailArchiveEventTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func archiveEventRelated(source, sourceLabel, title, description string, msg mailarchive.ContextMessage) bool {
	hay := strings.ToLower(strings.Join([]string{source, sourceLabel, title, description}, "\n"))
	for _, needle := range []string{msg.ID, msg.Locator, msg.MessageID, strings.Trim(msg.MessageID, "<>")} {
		needle = strings.ToLower(strings.TrimSpace(needle))
		if needle != "" && strings.Contains(hay, needle) {
			return true
		}
	}
	subject := strings.ToLower(strings.Join(strings.Fields(msg.Subject), " "))
	if subject == "" {
		return false
	}
	if strings.Contains(hay, subject) {
		return true
	}
	terms := archiveRelatedTerms(subject)
	matches := 0
	for _, term := range terms {
		if strings.Contains(hay, term) {
			matches++
		}
	}
	return len(terms) > 0 && matches >= minArchiveRelatedMatches(len(terms))
}

func archiveRelatedQuery(msg mailarchive.ContextMessage) string {
	subject := strings.TrimSpace(msg.Subject)
	for {
		lower := strings.ToLower(subject)
		next := subject
		for _, prefix := range []string{"re:", "fw:", "fwd:", "re：", "fw：", "[외부메일]", "[외부 메일]", "[external]"} {
			if strings.HasPrefix(lower, strings.ToLower(prefix)) {
				next = strings.TrimSpace(subject[len(prefix):])
				break
			}
		}
		if next == subject {
			break
		}
		subject = next
	}
	if subject != "" {
		return subject
	}
	if addr, err := mail.ParseAddress(msg.From); err == nil && addr.Address != "" {
		return addr.Address
	}
	return msg.From
}

func archiveRelatedTerms(s string) []string {
	fields := strings.Fields(strings.ToLower(s))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.Trim(f, `"'()[]{}<>:;,.!?`)
		if len([]rune(f)) < 2 {
			continue
		}
		out = append(out, f)
	}
	return out
}

func minArchiveRelatedMatches(n int) int {
	if n <= 2 {
		return n
	}
	return 2
}

func parseArchiveToolDate(s string) time.Time {
	if t, err := mail.ParseDate(s); err == nil {
		return t
	}
	return time.Time{}
}

func formatArchiveMessages(header string, msgs []mailarchive.ContextMessage, includeBody bool) string {
	if len(msgs) == 0 {
		return header + ": 해당하는 메일이 없습니다."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %d건\n", header, len(msgs))
	for i, msg := range msgs {
		fmt.Fprintf(&b, "\n[%d] %s\n  발신: %s\n  일시: %s\n  ID: %s\n  Locator: %s\n  %s\n",
			i+1, oneLine(msg.Subject), oneLine(msg.From), msg.Date, msg.ID, msg.Locator, oneLine(msg.Snippet))
		if includeBody && strings.TrimSpace(msg.Body) != "" {
			fmt.Fprintf(&b, "\n%s\n", msg.Body)
		}
	}
	b.WriteString("\n다음 단계: 특정 메일은 action=read + message_id=Locator, 전체 대화는 action=thread + message_id=Locator로 여세요.")
	return b.String()
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func formatArchiveRead(msg mailarchive.ContextMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## 메일 원문\n\n")
	writeArchiveMessageHeader(&b, msg)
	if len(msg.Attachments) > 0 {
		names := make([]string, 0, len(msg.Attachments))
		for _, att := range msg.Attachments {
			names = append(names, att.Filename)
		}
		fmt.Fprintf(&b, "**첨부:** %s\n", strings.Join(names, ", "))
	}
	b.WriteString("\n")
	if strings.TrimSpace(msg.Body) == "" {
		b.WriteString("(표시할 본문이 없습니다. 첨부 메타/서명/히스토리만 있었을 수 있습니다.)")
	} else {
		b.WriteString(msg.Body)
	}
	return b.String()
}

func formatArchiveThread(msgs []mailarchive.ContextMessage) string {
	if len(msgs) == 0 {
		return "스레드에 메시지가 없습니다."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## 전체 메일 스레드 (%d개, 오래된 순)\n", len(msgs))
	for i, msg := range msgs {
		fmt.Fprintf(&b, "\n---\n\n### [%d] %s\n", i+1, oneLine(msg.Subject))
		writeArchiveMessageHeader(&b, msg)
		b.WriteString("\n")
		if strings.TrimSpace(msg.Body) == "" {
			b.WriteString("(본문 없음)\n")
		} else {
			b.WriteString(msg.Body)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func formatProjectHistory(history mailarchive.ProjectHistory, includeBody bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## 프로젝트 메일 히스토리: %s\n\n", history.Query)
	if len(history.Messages) == 0 {
		b.WriteString("해당하는 메일이 없습니다.")
		return b.String()
	}
	fmt.Fprintf(&b, "총 %d건, 관련 스레드 후보 %d개\n", len(history.Messages), len(history.Threads))
	if history.IndexUsed {
		b.WriteString("로컬 FTS 인덱스로 최근 아카이브 후보를 넓게 잡은 뒤 업무 신호로 랭킹했습니다.\n")
	}
	if len(history.Threads) > 0 {
		b.WriteString("\n### 스레드 후보\n")
		for i, th := range history.Threads {
			fmt.Fprintf(&b, "%d. %s — %d건, %s → %s\n", i+1, oneLine(th.Subject), th.Count, th.FirstDate, th.LastDate)
			if len(th.Participants) > 0 {
				fmt.Fprintf(&b, "   참여자: %s\n", strings.Join(th.Participants, ", "))
			}
			if len(th.Locators) > 0 {
				fmt.Fprintf(&b, "   대표 Locator: %s\n", th.Locators[len(th.Locators)-1])
			}
		}
	}
	b.WriteString("\n### 시간선\n")
	for i, msg := range history.Messages {
		fmt.Fprintf(&b, "\n[%d] %s — %s\n", i+1, msg.Date, oneLine(msg.Subject))
		fmt.Fprintf(&b, "  발신: %s\n  ID: %s\n  Locator: %s\n  %s\n", oneLine(msg.From), msg.ID, msg.Locator, oneLine(msg.Snippet))
		if includeBody && strings.TrimSpace(msg.Body) != "" {
			fmt.Fprintf(&b, "\n%s\n", msg.Body)
		}
	}
	b.WriteString("\n특정 흐름을 깊게 볼 때는 대표 Locator로 action=thread를 호출하세요.")
	return b.String()
}

func writeArchiveMessageHeader(b *strings.Builder, msg mailarchive.ContextMessage) {
	fmt.Fprintf(b, "**From:** %s\n", msg.From)
	fmt.Fprintf(b, "**To:** %s\n", msg.To)
	if msg.CC != "" {
		fmt.Fprintf(b, "**CC:** %s\n", msg.CC)
	}
	fmt.Fprintf(b, "**Subject:** %s\n", msg.Subject)
	fmt.Fprintf(b, "**Date:** %s\n", msg.Date)
	fmt.Fprintf(b, "**ID:** %s\n", msg.ID)
	fmt.Fprintf(b, "**Locator:** %s\n", msg.Locator)
	if msg.MessageID != "" {
		fmt.Fprintf(b, "**Message-ID:** %s\n", msg.MessageID)
	}
	if msg.Score > 0 {
		fmt.Fprintf(b, "**Score:** %.2f", msg.Score)
		if len(msg.RankReasons) > 0 {
			fmt.Fprintf(b, " (%s)", strings.Join(msg.RankReasons, ", "))
		}
		b.WriteString("\n")
	}
}

func formatMailArchiveRelated(msg mailArchiveMessageOut) string {
	if len(msg.RelatedWiki) == 0 && len(msg.RelatedEvents) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 연결된 맥락\n")
	if len(msg.RelatedWiki) > 0 {
		b.WriteString("\n### 위키\n")
		for _, hit := range msg.RelatedWiki {
			fmt.Fprintf(&b, "- %s", hit.Path)
			if hit.Snippet != "" {
				fmt.Fprintf(&b, " — %s", oneLine(hit.Snippet))
			}
			b.WriteString("\n")
		}
	}
	if len(msg.RelatedEvents) > 0 {
		b.WriteString("\n### 일정\n")
		for _, ev := range msg.RelatedEvents {
			fmt.Fprintf(&b, "- %s — %s", oneLine(ev.Title), ev.Start)
			if ev.Kind != "" {
				fmt.Fprintf(&b, " (%s)", ev.Kind)
			}
			if ev.SourceLabel != "" {
				fmt.Fprintf(&b, " · %s", oneLine(ev.SourceLabel))
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func formatMailArchiveRelatedSummary(msgs []mailArchiveMessageOut) string {
	if len(msgs) == 0 {
		return ""
	}
	var wikiHits []mailArchiveWikiHit
	var events []mailArchiveEventHit
	seenWiki := map[string]bool{}
	seenEvent := map[string]bool{}
	for _, msg := range msgs {
		for _, hit := range msg.RelatedWiki {
			if hit.Path == "" || seenWiki[hit.Path] {
				continue
			}
			seenWiki[hit.Path] = true
			wikiHits = append(wikiHits, hit)
			if len(wikiHits) >= 5 {
				break
			}
		}
		for _, ev := range msg.RelatedEvents {
			key := ev.ID
			if key == "" {
				key = ev.Title + ev.Start
			}
			if key == "" || seenEvent[key] {
				continue
			}
			seenEvent[key] = true
			events = append(events, ev)
			if len(events) >= 5 {
				break
			}
		}
		if len(wikiHits) >= 5 && len(events) >= 5 {
			break
		}
	}
	return formatMailArchiveRelated(mailArchiveMessageOut{RelatedWiki: wikiHits, RelatedEvents: events})
}
