package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
)

// ToolMailArchive reads the on-box mail archive (the deneb-mailarchive IMAP store)
// so the agent can review received mail locally — the daily-digest cron uses this
// instead of the Gmail tool, completing the move off Gmail. Archive credentials
// come from env (DENEB_ARCHIVE_IMAP_USER/PASS; addr default 127.0.0.1:1143);
// without them the tool reports that it is unconfigured rather than erroring.
func ToolMailArchive() func(ctx context.Context, input json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var args struct {
			Action  string `json:"action"`
			Mailbox string `json:"mailbox"`
			Days    int    `json:"days"`
			Query   string `json:"query"`
			Limit   int    `json:"limit"`
		}
		_ = json.Unmarshal(input, &args)

		cfg := mailarchive.Config{
			Addr: mailArchiveAddr(),
			User: os.Getenv("DENEB_ARCHIVE_IMAP_USER"),
			Pass: os.Getenv("DENEB_ARCHIVE_IMAP_PASS"),
		}
		if cfg.User == "" || cfg.Pass == "" {
			return "메일 아카이브가 설정되지 않았습니다 (DENEB_ARCHIVE_IMAP_USER/PASS 미설정).", nil
		}
		mailbox := args.Mailbox
		if mailbox == "" {
			mailbox = "INBOX"
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 50
		}

		var (
			summaries []mailarchive.Summary
			err       error
			header    string
		)
		switch args.Action {
		case "search":
			if strings.TrimSpace(args.Query) == "" {
				return "", fmt.Errorf("search에는 query가 필요합니다")
			}
			summaries, err = mailarchive.Search(ctx, cfg, mailbox, args.Query, limit)
			header = fmt.Sprintf("'%s' 검색 결과 (%s)", args.Query, mailbox)
		case "list", "":
			days := args.Days
			if days <= 0 {
				days = 1
			}
			since := time.Now().AddDate(0, 0, -(days - 1))
			summaries, err = mailarchive.ListSince(ctx, cfg, mailbox, since, limit)
			if days == 1 {
				header = fmt.Sprintf("오늘 수신 메일 (%s)", mailbox)
			} else {
				header = fmt.Sprintf("최근 %d일 메일 (%s)", days, mailbox)
			}
		default:
			return "", fmt.Errorf("알 수 없는 action %q (list|search)", args.Action)
		}
		if err != nil {
			return "", fmt.Errorf("아카이브 조회 실패: %w", err)
		}
		return formatMailSummaries(header, summaries), nil
	}
}

func mailArchiveAddr() string {
	if v := strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_IMAP_ADDR")); v != "" {
		return v
	}
	return "127.0.0.1:1143"
}

func formatMailSummaries(header string, ss []mailarchive.Summary) string {
	if len(ss) == 0 {
		return header + ": 해당하는 메일이 없습니다."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %d건\n", header, len(ss))
	for i, s := range ss {
		fmt.Fprintf(&b, "\n[%d] %s\n  발신: %s\n  일시: %s\n  %s\n",
			i+1, oneLine(s.Subject), oneLine(s.From), s.Date, oneLine(s.Snippet))
	}
	return b.String()
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
