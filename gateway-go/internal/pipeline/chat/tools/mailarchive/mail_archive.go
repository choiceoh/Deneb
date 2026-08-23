package mailtool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailstore"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// MailArchiveDeps supplies archive readers and optional enrichment services.
type MailArchiveDeps struct {
	// Wiki is an optional knowledge adapter used to attach related wiki hits to
	// archive messages. Prefer knowledge.NewWikiAdapter(store) at the wire site
	// so this tool package does not import the wiki domain.
	Wiki     knowledge.Adapter
	Calendar *tooldeps.CalendarDeps
	// Store is the local file-backed mail mirror and the authoritative corpus:
	// when populated it answers list/search/project_history entirely from memory,
	// and a miss is trusted — NOT re-queried over IMAP (the archive IMAP is a
	// smaller, slower, CJK-blind rolling buffer). read/thread still fall through to
	// IMAP on a miss (a cheap Message-ID/header lookup) and attachment always uses
	// IMAP for bytes. An empty/nil store = IMAP-only legacy mode.
	Store *mailstore.Store
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
		args := parseMailArchiveArgs(input)

		configuredMailboxes := mailarchive.ParseMailboxList(os.Getenv("DENEB_ARCHIVE_IMAP_MAILBOXES"))
		mailboxes := mailarchive.SelectMailboxes(args.Mailbox, configuredMailboxes)
		cfg := mailarchive.Config{
			Addr:      mailarchive.AddressFromEnv(),
			User:      os.Getenv("DENEB_ARCHIVE_IMAP_USER"),
			Pass:      os.Getenv("DENEB_ARCHIVE_IMAP_PASS"),
			Mailboxes: mailboxes,
		}
		// The local store answers on its own; IMAP serves only read/thread misses,
		// attachment bytes, and the no-mirror legacy mode. So the tool is usable when
		// EITHER is ready — requiring IMAP creds even with a populated store would
		// defeat the whole point (no per-call IMAP dependency).
		storeReady := deps.Store != nil && deps.Store.Len() > 0
		imapReady := cfg.User != "" && cfg.Pass != ""
		if !storeReady && !imapReady {
			return "메일 아카이브가 설정되지 않았습니다 (로컬 저장소 미백필 + DENEB_ARCHIVE_IMAP_USER/PASS 미설정).", nil
		}

		// Phase timing: attribute where a call's time went so "mail_archive is
		// slow" is diagnosable straight from the log. usedIMAP flips true only for
		// the actions that still touch IMAP — read/thread on a store miss (a cheap
		// Message-ID/header lookup) and attachment (always IMAP fetch + OCR), plus
		// the legacy no-mirror mode. search / project_history are mirror-only now: a
		// store miss is NOT re-run over IMAP (the text-search fallback was removed —
		// the archive IMAP is a smaller, slower, CJK-blind corpus than the mirror).
		// EVERY call logs at Info, store hits included, so the log shows the full
		// picture: a "path=store" line is a served-from-mirror call, and a search
		// miss reads as path=store with storeHits=0. durationMs at Info follows
		// logging.md §5 (latency belongs in an Info field, not Debug).
		start := time.Now()
		loggedAction := args.Action
		if loggedAction == "" {
			loggedAction = "list"
		}
		usedIMAP := false
		// storeHits records what the local store returned for a search before it
		// returned (-1 = the action never queried the store). storeHits=0 with
		// path=store is a genuine mirror miss — there is no IMAP fallback for search.
		storeHits := -1
		defer func() {
			path := mailArchivePath(loggedAction, usedIMAP)
			durMs := time.Since(start).Milliseconds()
			storeLen := 0
			if deps.Store != nil {
				storeLen = deps.Store.Len()
			}
			slog.Info("mail_archive", "action", loggedAction, "path", path, "durationMs", durMs,
				"storeReady", storeReady, "storeLen", storeLen, "storeHits", storeHits,
				"query", textutil.TruncateRunes(args.Query, 80, "\n... (이하 생략)"))
		}()

		limit := args.Limit
		if limit <= 0 {
			limit = 50
		}
		opts := mailarchive.ContextOptions{
			Mailboxes: mailboxes,
			Limit:     limit,
			BodyRunes: mailArchiveBodyRunes(args.IncludeBody),
		}
		query := mailArchiveQuery{
			deps:       deps,
			args:       args,
			mailboxes:  mailboxes,
			cfg:        cfg,
			opts:       opts,
			storeReady: storeReady,
			imapReady:  imapReady,
			usedIMAP:   &usedIMAP,
			storeHits:  &storeHits,
		}

		switch args.Action {
		case "search":
			return query.search(ctx)
		case "read":
			return query.read(ctx)
		case "thread":
			return query.thread(ctx)
		case "project_history", "history":
			return query.projectHistory(ctx)
		case "list", "":
			return query.list(ctx)
		case "attachment":
			return query.attachment(ctx)
		default:
			return "", fmt.Errorf("알 수 없는 action %q (list|search|read|thread|project_history|attachment)", args.Action)
		}
	}
}

// mailArchivePath classifies which data path a mail_archive call took, for the
// per-call phase-timing log: "store" = in-memory mirror hit or miss (~ms; search
// and project_history are mirror-only, so a miss is still "store"); "attachment"
// = always IMAP fetch + OCR; "imap-fallback" = an IMAP round-trip — read/thread on
// a store miss (a cheap Message-ID/header lookup) or the legacy no-mirror mode.
// The slow ~11s text-search fallback was removed. Pure so the classification is
// unit-testable.
func mailArchivePath(action string, usedIMAP bool) string {
	switch {
	case action == "attachment":
		return "attachment"
	case usedIMAP:
		return "imap-fallback"
	default:
		return "store"
	}
}

// formatArchiveAttachments adapts archive records to the format-neutral
// document extraction boundary.
func formatArchiveAttachments(ctx context.Context, atts []mailarchive.ArchivedAttachment) string {
	documents := make([]document.Attachment, len(atts))
	for i := range atts {
		documents[i] = document.Attachment{
			Filename: atts[i].Filename,
			MimeType: atts[i].MimeType,
			Bytes:    atts[i].Bytes,
		}
	}
	return document.ExtractAttachments(ctx, documents)
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
	Action    string   `json:"action"`
	Mailboxes []string `json:"mailboxes"`
	Count     int      `json:"count"`
	// WidenedDays is set when a days-bounded search found nothing in the window
	// and was widened to all-time — the results are OLDER than the requested
	// days, so the model must not present them as recent.
	WidenedDays int                     `json:"widened_days,omitempty"`
	Message     *mailArchiveMessageOut  `json:"message,omitempty"`
	Messages    []mailArchiveMessageOut `json:"messages,omitempty"`
	History     *mailArchiveHistoryOut  `json:"history,omitempty"`
}

// mailArchiveWidenedDays reports the days window that was widened away, for the
// JSON response — 0 (omitted) unless a bounded search actually widened.
func mailArchiveWidenedDays(widened bool, days int) int {
	if widened {
		return days
	}
	return 0
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
	// Conflict is set when a related 인물 page's emails disagree with this
	// message's From. Both sides stay; the server does not pick a winner.
	Conflict string `json:"conflict,omitempty"`
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

const (
	// maxEnrichConcurrency bounds the parallel per-message enrichment. Each
	// enriched message costs a wiki semantic recall (embed + fusion) and a
	// calendar scan; running a result list sequentially made mail_archive
	// latency track the recall latency × N (observed 5–40s on busy turns when
	// recall spiked). A small pool overlaps them without flooding the shared
	// embedding sidecar (BGE served 4 parallel contexts; the Nemotron vLLM
	// backend batches internally).
	maxEnrichConcurrency = 6
	// maxEnrichedMessages caps the related-wiki/events decoration to the head of
	// the result list. Beyond it the message is returned plain: the decoration is
	// supplementary and the caller acts on the top ranked/recent hits, so paying
	// a recall for every one of a 50-row list is wasted latency.
	maxEnrichedMessages = 12
)

// enrichArchiveMessages attaches related wiki/calendar context to each message,
// bounded-parallel and capped so a long list does not serialize N wiki recalls.
// Order is preserved (out[i] ↔ msgs[i]); read serves a single message and calls
// enrichArchiveMessage directly.
func enrichArchiveMessages(ctx context.Context, deps MailArchiveDeps, msgs []mailarchive.ContextMessage, includeBody bool) []mailArchiveMessageOut {
	out := make([]mailArchiveMessageOut, len(msgs))
	plain := func(msg mailarchive.ContextMessage) mailArchiveMessageOut {
		if !includeBody {
			msg.Body = ""
		}
		return mailArchiveMessageOut{ContextMessage: msg}
	}
	sem := make(chan struct{}, maxEnrichConcurrency)
	var wg sync.WaitGroup
	for i := range msgs {
		if i >= maxEnrichedMessages {
			out[i] = plain(msgs[i])
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				// One message's enrichment must never panic the turn; fall back plain.
				if r := recover(); r != nil {
					out[idx] = plain(msgs[idx])
					slog.Error("panic enriching archive message", "index", idx, "panic", r)
				}
			}()
			out[idx] = enrichArchiveMessage(ctx, deps, msgs[idx], includeBody)
		}(i)
	}
	wg.Wait()
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

func relatedArchiveWiki(ctx context.Context, adapter knowledge.Adapter, msg mailarchive.ContextMessage) []mailArchiveWikiHit {
	if adapter == nil {
		return nil
	}
	query := archiveRelatedQuery(msg)
	if query == "" {
		return nil
	}
	hits, err := adapter.Recall(ctx, query, 3)
	if err != nil || len(hits) == 0 {
		return nil
	}
	out := make([]mailArchiveWikiHit, 0, len(hits))
	for _, hit := range hits {
		row := mailArchiveWikiHit{Path: hit.Ref.ID, Snippet: hit.Snippet, Score: hit.Score}
		if c := archiveWikiMailConflict(hit.Ref.ID, hit.Meta, hit.Snippet, msg.From); c != "" {
			row.Conflict = c
		}
		out = append(out, row)
	}
	return out
}

func relatedArchiveEvents(deps *tooldeps.CalendarDeps, msg mailarchive.ContextMessage) []mailArchiveEventHit {
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

// archiveWikiMailConflict reports a 담당자 mismatch when a related 인물
// page's emails disagree with this message's From. Empty when they agree,
// when the hit is not a person page, or when either side has no address.
// The server never drops either side — Conflict is display-only.
func archiveWikiMailConflict(path string, meta map[string]string, snippet, from string) string {
	if !strings.HasPrefix(strings.ReplaceAll(path, "\\", "/"), "인물/") {
		return ""
	}
	addr := ""
	if parsed, err := mail.ParseAddress(from); err == nil && parsed.Address != "" {
		addr = strings.ToLower(strings.TrimSpace(parsed.Address))
	}
	if addr == "" {
		return ""
	}
	var wikiEmails []string
	if meta != nil {
		for _, e := range strings.Split(meta["emails"], ",") {
			if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
				wikiEmails = append(wikiEmails, e)
			}
		}
	}
	if len(wikiEmails) == 0 {
		for _, e := range archiveEmailRe.FindAllString(snippet, -1) {
			wikiEmails = append(wikiEmails, strings.ToLower(e))
		}
	}
	if len(wikiEmails) == 0 {
		return ""
	}
	wikiDom := map[string]bool{}
	for _, e := range wikiEmails {
		if e == addr {
			return ""
		}
		if at := strings.LastIndex(e, "@"); at >= 0 && at+1 < len(e) {
			d := e[at+1:]
			if !archiveFreemail[d] {
				wikiDom[d] = true
			}
		}
	}
	if at := strings.LastIndex(addr, "@"); at >= 0 && at+1 < len(addr) && wikiDom[addr[at+1:]] {
		return ""
	}
	return "불일치: 위키 " + strings.Join(wikiEmails, ", ") + " · 메일 " + addr + " — 둘 다 근거. 서버는 중재하지 않음"
}

var archiveEmailRe = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)

var archiveFreemail = map[string]bool{
	"gmail.com": true, "naver.com": true, "daum.net": true, "hanmail.net": true,
	"outlook.com": true, "hotmail.com": true, "icloud.com": true, "yahoo.com": true,
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
