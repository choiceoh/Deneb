// mail_backfill.go — recovers mail the poll window never saw, and counts what
// the pipeline is still missing.
//
// Two facts about the poll path make loss possible: the query is
// `is:unread newer_than:1h`, and Run() returns early outside weekday business
// hours. Mail arriving at 20:00 has left the window before the next poll looks.
// The 2026-08-30 audit measured the result in the live work state — of the
// messages registered more than 60 days ago, 41.7% (96 of 230) had never been
// analyzed, 87 of them human-sent, including 견적 요청 and 배치도 송부 for live
// projects. They were invisible to recall, and nothing in the system was
// counting.
//
// Recent cohorts are 0–2%, so this is a backlog to drain rather than a leak to
// plug — LMTP intake now covers arrival independently of the Gmail poll. The
// task stays registered anyway: it is also the "later pass" that
// MarkAnalysisFailed's comment has been promising since before there was one,
// and its counters are what would notice if the rate climbed again.
package server

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
)

const (
	// mailBackfillInterval is slow on purpose: the queue is a fixed backlog, and
	// draining it is worth less than never competing with live analysis.
	mailBackfillInterval = 3 * time.Hour
	// mailBackfillPerCycle bounds LLM spend per cycle.
	mailBackfillPerCycle = 4
	// mailBackfillMinAge keeps the task off mail that is merely still in flight.
	mailBackfillMinAge = 12 * time.Hour
)

// mailBackfillTask drains messages the poll window missed.
type mailBackfillTask struct{ s *Server }

func (t mailBackfillTask) Name() string            { return "mail-backfill" }
func (t mailBackfillTask) Interval() time.Duration { return mailBackfillInterval }

func (t mailBackfillTask) Run(ctx context.Context) error { return t.s.runMailBackfill(ctx) }

// registerMailBackfillTask wires the backfill into the autonomous service. It
// is production-gated like every other task that mutates shared state: a
// dev/live-test instance must never spend LLM budget re-analyzing the operator's
// mail or write pages into the production wiki.
func (s *Server) registerMailBackfillTask(homeDir string) {
	if s.autonomousSvc == nil || s.gmailPollSvc == nil || s.mailStore == nil {
		return
	}
	if _, ok := s.productionStateDir(homeDir); !ok {
		return
	}
	s.autonomousSvc.RegisterTask(mailBackfillTask{s: s})
}

// runMailBackfill analyzes one bounded batch of never-analyzed mail and reports
// how much remains.
func (s *Server) runMailBackfill(ctx context.Context) error {
	workStore := s.mailWorkStatePathStore()
	pending := workStore.PendingAnalysis(mailBackfillPerCycle, mailBackfillMinAge)
	remaining := len(workStore.PendingAnalysis(mailBackfillBacklogCap, mailBackfillMinAge))
	if len(pending) == 0 {
		s.logger.Debug("mail backfill: 대기 없음")
		return nil
	}

	var msgs []*gmail.MessageDetail
	for _, ms := range pending {
		msg, ok := s.archivedMessageDetail(ms)
		if !ok {
			// The body was never mirrored locally and the poll window is long
			// gone. Park it in review rather than leaving it pending: a
			// candidate that can never be fetched would otherwise occupy the
			// bounded batch on every cycle and starve the ones that can.
			if _, err := workStore.MarkAnalysisReview(messageInputOf(ms), "본문 없음 — 백필 불가"); err != nil {
				s.logger.Warn("mail backfill: review 표시 실패", "id", ms.ID, "error", err)
			}
			continue
		}
		msgs = append(msgs, msg)
	}
	if len(msgs) == 0 {
		s.logger.Info("mail backfill: 본문 있는 대기 없음", "남은 대기", remaining)
		return nil
	}

	done, err := s.gmailPollSvc.AnalyzeArchived(ctx, msgs)
	if err != nil {
		s.logger.Warn("mail backfill: 분석 실패", "count", len(msgs), "error", err)
		return err
	}
	analyzed := make(map[string]struct{}, len(done))
	for _, id := range done {
		analyzed[id] = struct{}{}
	}
	for _, ms := range pending {
		if _, ok := analyzed[ms.ID]; !ok {
			continue
		}
		if _, err := workStore.MarkAnalysisDone(mailwork.AnalysisInput{
			MessageInput: messageInputOf(ms),
		}); err != nil {
			s.logger.Warn("mail backfill: done 표시 실패", "id", ms.ID, "error", err)
		}
	}
	s.logger.Info("mail backfill: 과거 메일 분석",
		"analyzed", len(done), "attempted", len(msgs), "남은 대기", remaining-len(done))
	return nil
}

// mailWorkStatePath is the one place the workflow-state filename is spelled;
// mailWorkStatePathStore opens a store on it. Five call sites used to repeat the
// literal.
func (s *Server) mailWorkStatePath() string {
	return filepath.Join(s.denebDir, "mail_work_state.json")
}

func (s *Server) mailWorkStatePathStore() *mailwork.Store {
	return mailwork.New(s.mailWorkStatePath())
}

// mailBackfillBacklogCap bounds the "how much is left" count so a pathological
// backlog cannot turn the report into a full-store scan cost every cycle.
const mailBackfillBacklogCap = 500

// archivedMessageDetail rebuilds an analyzable message from the local archive.
// Returns false when the body was never mirrored — an analysis of a subject line
// alone is the kind of thin page the corpus audit spent months removing.
func (s *Server) archivedMessageDetail(ms mailwork.MessageState) (*gmail.MessageDetail, bool) {
	if s.mailStore == nil || strings.TrimSpace(ms.ID) == "" {
		return nil, false
	}
	cm, ok := s.mailStore.Read(ms.ID, "", nil)
	if !ok || strings.TrimSpace(cm.Body) == "" {
		return nil, false
	}
	return &gmail.MessageDetail{
		ID:          ms.ID,
		ThreadID:    ms.ThreadID,
		From:        firstNonEmpty(cm.From, ms.From),
		To:          cm.To,
		CC:          cm.CC,
		Subject:     firstNonEmpty(cm.Subject, ms.Subject),
		Date:        firstNonEmpty(cm.Date, ms.Date),
		Body:        cm.Body,
		Attachments: cm.Attachments,
	}, true
}

func messageInputOf(ms mailwork.MessageState) mailwork.MessageInput {
	return mailwork.MessageInput{
		ID:              ms.ID,
		ThreadID:        ms.ThreadID,
		From:            ms.From,
		Subject:         ms.Subject,
		Date:            ms.Date,
		Mailbox:         ms.Mailbox,
		HasAttachment:   ms.HasAttachment,
		AttachmentCount: ms.AttachmentCount,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
