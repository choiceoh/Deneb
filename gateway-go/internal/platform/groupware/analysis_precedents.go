package groupware

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Precedent recall: past cached analyses that resemble a new document (same
// drafter, overlapping title tokens). Injected into the analysis prompt so the
// model can judge against 전례 — repeat patterns, amount drift, consistency
// with earlier recommendations — instead of each approval in isolation.

// Recent returns up to limit cached analyses, newest first. Unlike Load this
// accepts any prompt version: precedents only need title/drafter/gist, not a
// fresh full body under the current contract.
func (s *ApprovalAnalysisStore) Recent(limit int) []*ApprovalAnalysisRecord {
	if s == nil || s.dir == "" || limit <= 0 {
		return nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	out := make([]*ApprovalAnalysisRecord, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var rec ApprovalAnalysisRecord
		if err := json.Unmarshal(data, &rec); err != nil || rec.DocID == "" {
			continue
		}
		out = append(out, &rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// SelectApprovalPrecedents picks records relevant to the new document: title
// token overlap (거래처·프로젝트·품목이 제목에 실리는 Amaranth 관행) plus a
// same-drafter boost. Newest-first within equal scores; the document itself is
// excluded. Returns nil when nothing scores.
func SelectApprovalPrecedents(records []*ApprovalAnalysisRecord, docID, title, bodyHint string, limit int) []*ApprovalAnalysisRecord {
	if limit <= 0 {
		return nil
	}
	want := approvalTitleTokens(title)
	type scored struct {
		rec   *ApprovalAnalysisRecord
		score int
	}
	picks := make([]scored, 0, len(records))
	for _, rec := range records {
		if rec == nil || rec.DocID == docID {
			continue
		}
		score := 0
		for tok := range approvalTitleTokens(rec.Title) {
			if _, ok := want[tok]; ok {
				score++
			}
		}
		if rec.Drafter != "" && bodyHint != "" && strings.Contains(bodyHint, rec.Drafter) {
			score++
		}
		if score > 0 {
			picks = append(picks, scored{rec, score})
		}
	}
	sort.SliceStable(picks, func(i, j int) bool {
		if picks[i].score != picks[j].score {
			return picks[i].score > picks[j].score
		}
		return picks[i].rec.CreatedAt.After(picks[j].rec.CreatedAt)
	})
	if len(picks) == 0 {
		return nil
	}
	if len(picks) > limit {
		picks = picks[:limit]
	}
	out := make([]*ApprovalAnalysisRecord, len(picks))
	for i, p := range picks {
		out[i] = p.rec
	}
	return out
}

// FormatApprovalPrecedents renders precedents as prompt bullets: date · title ·
// drafter · importance · one-line gist (truncated).
func FormatApprovalPrecedents(recs []*ApprovalAnalysisRecord) string {
	lines := make([]string, 0, len(recs))
	for _, rec := range recs {
		bits := []string{}
		if rec.Date != "" {
			bits = append(bits, rec.Date)
		}
		bits = append(bits, strings.TrimSpace(rec.Title))
		if rec.Drafter != "" {
			bits = append(bits, "기안 "+rec.Drafter)
		}
		if rec.Importance != "" {
			bits = append(bits, rec.Importance)
		}
		line := "- " + strings.Join(bits, " · ")
		if gist := truncatePrecedentGist(ApprovalAnalysisGistLine(rec.Analysis), 90); gist != "" {
			line += fmt.Sprintf(" — %s", gist)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// approvalTitleTokens tokenizes a title into content-bearing tokens: length ≥2,
// generic approval boilerplate (건·기안·품의·요청·관련…) dropped.
func approvalTitleTokens(title string) map[string]struct{} {
	drop := map[string]struct{}{
		"건": {}, "기안": {}, "품의": {}, "요청": {}, "관련": {}, "문서": {},
		"전자결재": {}, "승인": {}, "발주": {}, "구매": {}, "지출": {}, "대한": {},
	}
	out := map[string]struct{}{}
	for _, tok := range strings.FieldsFunc(title, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("()[]{}_-—·.,/\\\"'", r)
	}) {
		tok = strings.TrimSpace(tok)
		if len([]rune(tok)) < 2 {
			continue
		}
		if _, skip := drop[tok]; skip {
			continue
		}
		out[tok] = struct{}{}
	}
	return out
}

func truncatePrecedentGist(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}
