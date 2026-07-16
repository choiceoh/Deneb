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
func (s *Server) completeApprovalAnalysis(ctx context.Context, title, body string) (string, string, error) {
	client, model, _, _ := s.mailAnalysisModels()
	if client == nil || strings.TrimSpace(model) == "" {
		return "", "", fmt.Errorf("main-role model unavailable")
	}
	user := fmt.Sprintf("제목: %s\n\n본문:\n%s", strings.TrimSpace(title), truncateRunes(body, approvalAnalysisBodyMaxRune))
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
	return strings.TrimSpace(out), "", nil
}

// analyzeApprovalBestEffort reads + analyzes a pending approval after the
// radar feed card is durable. Cache hits and LLM/reader failures are silent —
// the card must stay notified.
func (s *Server) analyzeApprovalBestEffort(ctx context.Context, doc groupware.ApprovalSummary) {
	docID := strings.TrimSpace(doc.DocID)
	if docID == "" || s.denebDir == "" {
		return
	}
	cache := groupware.NewApprovalAnalysisStore(filepath.Join(s.denebDir, "cache", "approval_analysis"))
	if rec, err := cache.Load(docID); err == nil && rec != nil {
		return
	}
	cfg, ok := groupware.FromEnv()
	if !ok {
		return
	}
	body, err := groupware.ReadApprovalByDocIDIn(ctx, cfg, docID, doc.Folder)
	if err != nil || strings.TrimSpace(body) == "" {
		if s.logger != nil {
			s.logger.Warn("groupware radar approval analysis skipped", "docId", docID, "err", err)
		}
		return
	}
	// Prewarm the body cache — the card's detail open right after the
	// notification is the hottest path.
	_ = groupware.NewApprovalBodyStore(filepath.Join(s.denebDir, "cache", "approval_body")).Save(docID, body)
	title := strings.TrimSpace(doc.Title)
	if title == "" {
		title = firstNonEmptyLine(body)
	}
	start := time.Now()
	analysis, _, err := s.completeApprovalAnalysis(ctx, title, body)
	if err != nil || strings.TrimSpace(analysis) == "" {
		if s.logger != nil {
			s.logger.Warn("groupware radar approval analysis failed", "docId", docID, "err", err)
		}
		return
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
	_ = cache.Save(rec)
	// 결재 gist joins the project memory trail (로그.md `결재` op) — best-effort.
	s.logApprovalAnalysisToWiki(rec)
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
