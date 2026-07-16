package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
)

const (
	approvalAnalysisMaxTokens   = 2048
	approvalAnalysisBodyMaxRune = 24000
)

// completeApprovalAnalysis runs the LLM over one 전자결재 document (메일
// stage-2 / main role). Importance is left empty so the handler can parse
// IMPORTANCE: from the analysis body.
//
// Price-memory loop (결재 단가 기억): the ledger's matching price history is
// injected into the prompt BEFORE the LLM sees the document, and after the
// analysis the document's own cost is extracted (quote-gated), diffed against
// the ledger, and filed — so the NEXT approval of the same 품목/경비 sees this
// one. The exact deltas are appended below the analysis so the card carries
// deterministic figures regardless of the model's narrative.
func (s *Server) completeApprovalAnalysis(ctx context.Context, docID, title, date, body string) (string, string, error) {
	client, model, _, _ := s.mailAnalysisModels()
	if client == nil || strings.TrimSpace(model) == "" {
		return "", "", fmt.Errorf("main-role model unavailable")
	}
	user := fmt.Sprintf("제목: %s\n\n본문:\n%s", strings.TrimSpace(title), truncateRunes(body, approvalAnalysisBodyMaxRune))
	if s.wikiStore != nil {
		if hist := s.wikiStore.PriceHistoryContext(title + "\n" + body); hist != "" {
			user += "\n\n## 과거 단가·경비 이력 (사내 원장 · 공급가액 기준)\n" + hist
		}
	}
	out, err := client.Complete(ctx, llm.ChatRequest{
		Model:     model,
		System:    llm.SystemString(handlerminiapp.ApprovalAnalyzeSystemPrompt()),
		Messages:  []llm.Message{llm.NewTextMessage("user", user)},
		MaxTokens: approvalAnalysisMaxTokens,
		Thinking:  &llm.ThinkingConfig{Type: "disabled"},
	})
	if err != nil {
		return "", "", err
	}
	analysis := strings.TrimSpace(out)
	if deltas := s.fileApprovalCost(ctx, docID, title, date, body); len(deltas) > 0 {
		analysis += "\n\n**단가 이력 대조 (자동)**\n- " + strings.Join(deltas, "\n- ")
	}
	return analysis, "", nil
}

// prepareApprovalBeforeFeed reads + analyzes a pending approval before the
// radar posts a work-feed card. Cache hits return the stored record. Reader/LLM
// failures return an error so onPending stays retryable (no bare notification
// card). Meaningful analyses (urgent/attention) append to the project wiki log.
func (s *Server) prepareApprovalBeforeFeed(ctx context.Context, doc groupware.ApprovalSummary) (*groupware.ApprovalAnalysisRecord, error) {
	docID := strings.TrimSpace(doc.DocID)
	if docID == "" {
		return nil, fmt.Errorf("groupware approval analysis missing docId")
	}
	if s.denebDir == "" {
		return nil, fmt.Errorf("groupware approval analysis cache unavailable")
	}
	cache := groupware.NewApprovalAnalysisStore(filepath.Join(s.denebDir, "cache", "approval_analysis"))
	bodyStore := groupware.NewApprovalBodyStore(filepath.Join(s.denebDir, "cache", "approval_body"))
	if rec, err := cache.Load(docID); err == nil && rec != nil {
		// Analysis may outlive the body TTL — rewarm so the feed-card → detail
		// hop still skips Playwright when the operator opens later.
		if bodyStore.Load(docID) == "" {
			if cfg, ok := groupware.FromEnv(); ok {
				if body, rerr := groupware.ReadApprovalByDocIDIn(ctx, cfg, docID, doc.Folder); rerr == nil && strings.TrimSpace(body) != "" {
					_ = bodyStore.Save(docID, body)
				} else if s.logger != nil && rerr != nil {
					s.logger.Warn("groupware radar approval body rewarm failed", "docId", docID, "err", rerr)
				}
			}
		}
		if approvalAnalysisMeaningfulForWiki(rec.Importance) {
			s.logApprovalAnalysisToWiki(rec)
		}
		return rec, nil
	}
	cfg, ok := groupware.FromEnv()
	if !ok {
		return nil, fmt.Errorf("groupware credentials unset")
	}
	body, err := groupware.ReadApprovalByDocIDIn(ctx, cfg, docID, doc.Folder)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("groupware radar approval read failed", "docId", docID, "err", err)
		}
		return nil, fmt.Errorf("groupware approval %s read failed: %w", docID, err)
	}
	if strings.TrimSpace(body) == "" {
		if s.logger != nil {
			s.logger.Warn("groupware radar approval body empty", "docId", docID)
		}
		return nil, fmt.Errorf("groupware approval %s body empty", docID)
	}
	// Prewarm the body cache — the card's detail open right after the
	// notification is the hottest path.
	_ = bodyStore.Save(docID, body)
	title := strings.TrimSpace(doc.Title)
	if title == "" {
		title = firstNonEmptyLine(body)
	}
	start := time.Now()
	analysis, _, err := s.completeApprovalAnalysis(ctx, docID, title, doc.Date, body)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("groupware radar approval analysis failed", "docId", docID, "err", err)
		}
		return nil, fmt.Errorf("groupware approval %s analysis failed: %w", docID, err)
	}
	if strings.TrimSpace(analysis) == "" {
		if s.logger != nil {
			s.logger.Warn("groupware radar approval analysis empty", "docId", docID)
		}
		return nil, fmt.Errorf("groupware approval %s analysis empty", docID)
	}
	importance := normalizeApprovalImportance(analysis)
	rec := &groupware.ApprovalAnalysisRecord{
		DocID:         docID,
		Title:         title,
		Drafter:       strings.TrimSpace(doc.Drafter),
		Date:          strings.TrimSpace(doc.Date),
		Analysis:      analysis,
		Importance:    importance,
		DurationMs:    time.Since(start).Milliseconds(),
		PromptVersion: groupware.ApprovalAnalysisPromptVersion,
		CreatedAt:     time.Now().UTC(),
	}
	if err := cache.Save(rec); err != nil {
		if s.logger != nil {
			s.logger.Warn("groupware radar approval analysis cache save failed", "docId", docID, "err", err)
		}
		return nil, fmt.Errorf("groupware approval %s analysis cache save failed: %w", docID, err)
	}
	if approvalAnalysisMeaningfulForWiki(importance) {
		s.logApprovalAnalysisToWiki(rec)
	}
	return rec, nil
}

// approvalAnalysisMeaningfulForWiki reports whether an analysis is worth a
// project 로그.md trail. routine noise stays in the approval cache only;
// UniqueProjectInText still gates the write inside logApprovalAnalysisToWiki.
func approvalAnalysisMeaningfulForWiki(importance string) bool {
	switch strings.TrimSpace(strings.ToLower(importance)) {
	case "urgent", "attention":
		return true
	default:
		return false
	}
}

// stripApprovalImportanceMarker drops the machine IMPORTANCE: trailer so feed
// and wiki bodies stay human-readable (중요도는 meta/필드에 따로 실린다).
func stripApprovalImportanceMarker(analysis string) string {
	var kept []string
	for _, line := range strings.Split(analysis, "\n") {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "IMPORTANCE:") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "\n…(truncated)"
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			if utf8.RuneCountInString(t) > 80 {
				return string([]rune(t)[:80])
			}
			return t
		}
	}
	return ""
}

func normalizeApprovalImportance(analysis string) string {
	for _, line := range strings.Split(analysis, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "IMPORTANCE:") {
			part := strings.ToLower(strings.TrimSpace(line[len("IMPORTANCE:"):]))
			switch {
			case strings.HasPrefix(part, "urgent"):
				return "urgent"
			case strings.HasPrefix(part, "attention"):
				return "attention"
			case strings.HasPrefix(part, "routine"):
				return "routine"
			}
		}
	}
	return "attention"
}
