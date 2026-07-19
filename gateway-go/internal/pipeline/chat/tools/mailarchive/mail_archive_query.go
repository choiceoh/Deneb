package mailtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
)

// mailArchiveArgs is the wire input for ToolMailArchive. Invalid JSON keeps the
// historical zero-value behavior: the request is treated as the default list.
type mailArchiveArgs struct {
	Action      string `json:"action"`
	Mailbox     string `json:"mailbox"`
	Days        int    `json:"days"`
	Query       string `json:"query"`
	MessageID   string `json:"message_id"`
	Attachment  string `json:"attachment"`
	Limit       int    `json:"limit"`
	IncludeBody bool   `json:"include_body"`
	AsJSON      bool   `json:"as_json"`
}

func parseMailArchiveArgs(input json.RawMessage) mailArchiveArgs {
	var args mailArchiveArgs
	_ = json.Unmarshal(input, &args)
	return args
}

// mailArchiveQuery carries the immutable request/configuration plus the timing
// observations updated by query modes for the caller's deferred log.
type mailArchiveQuery struct {
	deps       MailArchiveDeps
	args       mailArchiveArgs
	mailboxes  []string
	cfg        mailarchive.Config
	opts       mailarchive.ContextOptions
	storeReady bool
	imapReady  bool
	usedIMAP   *bool
	storeHits  *int
}

func (q mailArchiveQuery) search(ctx context.Context) (string, error) {
	if strings.TrimSpace(q.args.Query) == "" {
		return "", fmt.Errorf("search에는 query가 필요합니다")
	}
	opts := q.opts
	// Honor days on search too — it was silently ignored here (only
	// project_history bounded by date), so "지난달 메일만" forced the model to
	// over-fetch and eyeball dates.
	if q.args.Days > 0 {
		opts.Since = time.Now().AddDate(0, 0, -(q.args.Days - 1))
	}

	var msgs []mailarchive.ContextMessage
	widened := false
	if q.storeReady {
		// The local mirror is authoritative: a store miss is trusted, never
		// re-queried over IMAP. The archive IMAP is a smaller rolling buffer
		// (measured: mirror 3,320 msgs vs live IMAP ~1,217) whose Dovecot has no CJK
		// full-text index, so the old text-search fallback searched FEWER messages,
		// slower (~11s scanning the large Gmail mailbox), and CJK-blind — pure
		// latency, zero recall. widenStoreSearch already relaxes a bounded miss to
		// all-time within the mirror, which is the real recall lever.
		msgs = q.deps.Store.SearchContext(ctx, q.mailboxes, q.args.Query, opts.Since, opts.Limit)
		*q.storeHits = len(msgs)
		msgs, widened = q.widenStoreSearch(ctx, msgs, opts)
	} else if q.imapReady {
		// No local mirror (legacy IMAP-only mode): IMAP is the primary source here,
		// not a fallback.
		*q.usedIMAP = true
		var err error
		msgs, err = mailarchive.SearchContextMessages(ctx, q.cfg, q.args.Query, opts)
		if err != nil {
			return "", fmt.Errorf("아카이브 검색 실패: %w", err)
		}
	}
	return q.formatSearch(ctx, msgs, widened)
}

func (q mailArchiveQuery) widenStoreSearch(ctx context.Context, msgs []mailarchive.ContextMessage, opts mailarchive.ContextOptions) ([]mailarchive.ContextMessage, bool) {
	// A bounded store miss is widened before paying for an IMAP request with the
	// same narrow window. This keeps historical-topic searches on the fast path.
	if len(msgs) != 0 || opts.Since.IsZero() {
		return msgs, false
	}
	wider := q.deps.Store.SearchContext(ctx, q.mailboxes, q.args.Query, time.Time{}, opts.Limit)
	if len(wider) == 0 {
		return msgs, false
	}
	*q.storeHits = len(wider)
	return wider, true
}

func (q mailArchiveQuery) formatSearch(ctx context.Context, msgs []mailarchive.ContextMessage, widened bool) (string, error) {
	if q.args.AsJSON {
		return marshalMailArchiveResponse(mailArchiveResponse{
			Action:      "search",
			Mailboxes:   q.mailboxes,
			Count:       len(msgs),
			WidenedDays: mailArchiveWidenedDays(widened, q.args.Days),
			Messages:    enrichArchiveMessages(ctx, q.deps, msgs, q.args.IncludeBody),
		})
	}
	title := fmt.Sprintf("'%s' 검색 결과 (%s)", q.args.Query, mailArchiveMailboxLabel(q.mailboxes))
	if widened {
		// Make the relaxed date boundary explicit so callers do not present older
		// results as if they fell within the requested window.
		title = fmt.Sprintf("'%s' 검색 결과 (%s · 최근 %d일 내 없음 → 전체 기간)", q.args.Query, mailArchiveMailboxLabel(q.mailboxes), q.args.Days)
	}
	return formatArchiveMessages(title, msgs, q.args.IncludeBody), nil
}

func (q mailArchiveQuery) read(ctx context.Context) (string, error) {
	var msg mailarchive.ContextMessage
	var found bool
	if q.storeReady {
		msg, found = q.deps.Store.Read(q.args.MessageID, q.args.Query, q.mailboxes)
	}
	if !found && q.imapReady {
		*q.usedIMAP = true
		var err error
		msg, err = mailarchive.ReadContextMessage(ctx, q.cfg, q.args.MessageID, q.args.Query, q.opts)
		if err != nil {
			if errors.Is(err, mailarchive.ErrArchiveNotFound) {
				return "해당 메일을 아카이브에서 찾지 못했습니다 — Locator가 오래됐을 수 있습니다(재색인 후 흔함). action=search에 제목 키워드로 다시 찾아 새 Locator로 여세요.", nil
			}
			return "", fmt.Errorf("아카이브 메일 열기 실패: %w", err)
		}
	}
	return q.formatRead(ctx, msg)
}

func (q mailArchiveQuery) formatRead(ctx context.Context, msg mailarchive.ContextMessage) (string, error) {
	enriched := enrichArchiveMessage(ctx, q.deps, msg, true)
	if q.args.AsJSON {
		return marshalMailArchiveResponse(mailArchiveResponse{
			Action:    "read",
			Mailboxes: q.mailboxes,
			Count:     1,
			Message:   &enriched,
		})
	}
	out := formatArchiveRead(msg)
	if related := formatMailArchiveRelated(enriched); related != "" {
		out += "\n\n" + related
	}
	return out, nil
}

func (q mailArchiveQuery) thread(ctx context.Context) (string, error) {
	var msgs []mailarchive.ContextMessage
	var found bool
	if q.storeReady {
		msgs, found = q.deps.Store.Thread(q.args.MessageID, q.args.Query, q.mailboxes, q.opts.Limit)
	}
	if (!found || len(msgs) == 0) && q.imapReady {
		*q.usedIMAP = true
		var err error
		msgs, err = mailarchive.ThreadContext(ctx, q.cfg, q.args.MessageID, q.args.Query, q.opts)
		if err != nil {
			if errors.Is(err, mailarchive.ErrArchiveNotFound) {
				return "스레드 기준 메일을 아카이브에서 찾지 못했습니다 — Locator가 오래됐을 수 있습니다. action=search에 제목 키워드로 다시 찾아 새 Locator로 시도하세요.", nil
			}
			return "", fmt.Errorf("아카이브 스레드 조회 실패: %w", err)
		}
	}
	return q.formatThread(ctx, msgs)
}

func (q mailArchiveQuery) formatThread(ctx context.Context, msgs []mailarchive.ContextMessage) (string, error) {
	enriched := enrichArchiveMessages(ctx, q.deps, msgs, true)
	if q.args.AsJSON {
		return marshalMailArchiveResponse(mailArchiveResponse{
			Action:    "thread",
			Mailboxes: q.mailboxes,
			Count:     len(enriched),
			Messages:  enriched,
		})
	}
	out := formatArchiveThread(msgs)
	if related := formatMailArchiveRelatedSummary(enriched); related != "" {
		out += "\n\n" + related
	}
	return out, nil
}

func (q mailArchiveQuery) projectHistory(ctx context.Context) (string, error) {
	if strings.TrimSpace(q.args.Query) == "" {
		return "", fmt.Errorf("project_history에는 query가 필요합니다")
	}
	opts := q.opts
	if q.args.Days > 0 {
		opts.Since = time.Now().AddDate(0, 0, -(q.args.Days - 1))
	}
	var history mailarchive.ProjectHistory
	if q.storeReady {
		// Mirror-authoritative, same as search: a store miss is not re-run over the
		// smaller/slower/CJK-blind IMAP archive.
		history, _ = q.deps.Store.ProjectHistoryContext(ctx, q.args.Query, opts.Since, opts.Limit, opts.IndexLimit)
	} else if q.imapReady {
		// Legacy IMAP-only mode (no mirror): IMAP is the primary source.
		*q.usedIMAP = true
		var err error
		history, err = mailarchive.ProjectHistoryContext(ctx, q.cfg, q.args.Query, opts)
		if err != nil {
			return "", fmt.Errorf("프로젝트 히스토리 조회 실패: %w", err)
		}
	}
	return q.formatProjectHistory(ctx, history)
}

func (q mailArchiveQuery) formatProjectHistory(ctx context.Context, history mailarchive.ProjectHistory) (string, error) {
	enriched := enrichProjectHistory(ctx, q.deps, history, q.args.IncludeBody)
	if q.args.AsJSON {
		return marshalMailArchiveResponse(mailArchiveResponse{
			Action:    "project_history",
			Mailboxes: q.mailboxes,
			Count:     len(enriched.History.Messages),
			History:   &enriched.History,
		})
	}
	out := formatProjectHistory(history, q.args.IncludeBody)
	if related := formatMailArchiveRelatedSummary(enriched.History.Messages); related != "" {
		out += "\n\n" + related
	}
	return out, nil
}

func (q mailArchiveQuery) list(ctx context.Context) (string, error) {
	days := q.args.Days
	if days <= 0 {
		days = 1
	}
	since := time.Now().AddDate(0, 0, -(days - 1))
	var msgs []mailarchive.ContextMessage
	var err error
	if q.storeReady {
		msgs = q.deps.Store.List(q.mailboxes, since, q.opts.Limit)
	} else if q.imapReady {
		*q.usedIMAP = true
		msgs, err = mailarchive.ListContextMessages(ctx, q.cfg, since, q.opts)
	}
	if err != nil {
		return "", fmt.Errorf("아카이브 목록 조회 실패: %w", err)
	}
	return q.formatList(ctx, msgs, days)
}

func (q mailArchiveQuery) formatList(ctx context.Context, msgs []mailarchive.ContextMessage, days int) (string, error) {
	if q.args.AsJSON {
		return marshalMailArchiveResponse(mailArchiveResponse{
			Action:    "list",
			Mailboxes: q.mailboxes,
			Count:     len(msgs),
			Messages:  enrichArchiveMessages(ctx, q.deps, msgs, q.args.IncludeBody),
		})
	}
	if days == 1 {
		return formatArchiveMessages(fmt.Sprintf("오늘 수신 메일 (%s)", mailArchiveMailboxLabel(q.mailboxes)), msgs, q.args.IncludeBody), nil
	}
	return formatArchiveMessages(fmt.Sprintf("최근 %d일 메일 (%s)", days, mailArchiveMailboxLabel(q.mailboxes)), msgs, q.args.IncludeBody), nil
}

func (q mailArchiveQuery) attachment(ctx context.Context) (string, error) {
	// Attachment bytes aren't mirrored into the local store (only the cleaned
	// text is), so this action always needs IMAP.
	if !q.imapReady {
		return "첨부 원문은 IMAP 아카이브에서만 제공됩니다 — DENEB_ARCHIVE_IMAP_USER/PASS 설정이 필요합니다.", nil
	}
	atts, err := mailarchive.ReadAttachment(ctx, q.cfg, q.args.MessageID, q.args.Query, q.args.Attachment, q.opts)
	if err != nil {
		if errors.Is(err, mailarchive.ErrArchiveNotFound) {
			return "해당 메일을 아카이브에서 찾지 못했습니다. message_id(Locator) 또는 query를 확인하세요.", nil
		}
		return "", fmt.Errorf("첨부 읽기 실패: %w", err)
	}
	if len(atts) == 0 {
		return "선택한 조건에 맞는 첨부가 없습니다. action=read로 첨부 목록을 먼저 확인하세요.", nil
	}
	return formatArchiveAttachments(ctx, atts), nil
}
