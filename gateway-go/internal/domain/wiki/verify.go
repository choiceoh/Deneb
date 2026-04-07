// verify.go — Wiki verification: duplicate entity detection + misclassification check.
// Called as Phase 5 of the WikiDreamer cycle. Detection only — no auto-fix.
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// VerifyFinding represents a single verification issue found.
type VerifyFinding struct {
	Type   string `json:"type"`            // "duplicate" or "misclassified"
	Detail string `json:"detail"`          // human-readable description (Korean)
	PageA  string `json:"pageA"`           // primary page path
	PageB  string `json:"pageB,omitempty"` // secondary page path (for duplicates)
}

// verifyPages runs verification on existing wiki pages:
//  1. Duplicate detection via Levenshtein distance on titles/IDs (no LLM)
//  2. Misclassification detection via single LLM call
func (wd *WikiDreamer) verifyPages(ctx context.Context) []VerifyFinding {
	idx := wd.store.Index()
	if len(idx.Entries) < 2 {
		return nil
	}

	var findings []VerifyFinding

	// 5a: Duplicate detection (pure computation).
	findings = append(findings, detectDuplicates(idx)...)

	// 5b: Misclassification detection (single LLM call).
	if wd.client != nil {
		findings = append(findings, wd.detectMisclassifications(ctx, idx)...)
	}

	return findings
}

type pageRef struct {
	path  string
	title string
	id    string
}

// detectDuplicates finds pages with identical or very similar titles/IDs.
func detectDuplicates(idx *Index) []VerifyFinding {
	pages := make([]pageRef, 0, len(idx.Entries))
	for path, entry := range idx.Entries {
		pages = append(pages, pageRef{path: path, title: entry.Title, id: entry.ID})
	}

	var findings []VerifyFinding
	seen := map[string]struct{}{}

	for i := 0; i < len(pages); i++ {
		for j := i + 1; j < len(pages); j++ {
			a, b := pages[i], pages[j]
			key := a.path + "|" + b.path

			if _, ok := seen[key]; ok {
				continue
			}

			// Compare titles.
			if a.title != "" && b.title != "" && isSimilar(a.title, b.title) {
				dist := levenshtein(a.title, b.title)
				if dist == 0 {
					findings = append(findings, VerifyFinding{
						Type:   "duplicate",
						Detail: fmt.Sprintf("동일한 제목: \"%s\"", a.title),
						PageA:  a.path, PageB: b.path,
					})
				} else {
					findings = append(findings, VerifyFinding{
						Type:   "duplicate",
						Detail: fmt.Sprintf("유사한 제목: \"%s\" ~ \"%s\" (거리 %d)", a.title, b.title, dist),
						PageA:  a.path, PageB: b.path,
					})
				}
				seen[key] = struct{}{}
				continue
			}

			// Compare IDs.
			if _, dup := seen[key]; a.id != "" && b.id != "" && !dup && isSimilar(a.id, b.id) {
				dist := levenshtein(a.id, b.id)
				if dist == 0 {
					findings = append(findings, VerifyFinding{
						Type:   "duplicate",
						Detail: fmt.Sprintf("동일한 ID: \"%s\"", a.id),
						PageA:  a.path, PageB: b.path,
					})
				} else {
					findings = append(findings, VerifyFinding{
						Type:   "duplicate",
						Detail: fmt.Sprintf("유사한 ID: \"%s\" ~ \"%s\" (거리 %d)", a.id, b.id, dist),
						PageA:  a.path, PageB: b.path,
					})
				}
				seen[key] = struct{}{}
			}
		}
	}

	return findings
}

// isSimilar checks if two strings are similar enough to be potential duplicates.
func isSimilar(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	dist := levenshtein(a, b)
	if dist == 0 {
		return true
	}
	maxLen := max(utf8.RuneCountInString(a), utf8.RuneCountInString(b))
	if maxLen <= 5 {
		return dist <= 1
	}
	return dist <= 2 && float64(dist)/float64(maxLen) < 0.3
}

// misclassificationResult is the LLM response format for category errors.
type misclassificationResult struct {
	Path            string `json:"path"`
	CurrentCategory string `json:"currentCategory"`
	CorrectCategory string `json:"correctCategory"`
	Reason          string `json:"reason"`
}

// detectMisclassifications sends page list to LLM to find category errors.
func (wd *WikiDreamer) detectMisclassifications(ctx context.Context, idx *Index) []VerifyFinding {
	var lines []string
	for path, entry := range idx.Entries {
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s",
			path, entry.Title, entry.Category, entry.Summary))
	}
	if len(lines) == 0 {
		return nil
	}

	prompt := fmt.Sprintf(`아래는 위키 페이지 목록입니다 (경로, 제목, 카테고리, 요약).
잘못 분류된 항목을 찾아 JSON 배열로 반환하세요.

카테고리 목록: %s

## 페이지 목록
%s

## 규칙
- 제목/요약을 보고 카테고리가 명백히 틀린 것만 지적
- 애매한 경우는 무시 (현재 분류 유지)
- 예: 호수/산/건물 이름이 "사람"으로 분류됨 → 지적
- 예: 사람 이름이 "기술"로 분류됨 → 지적
- 문제 없으면 빈 배열 [] 반환

JSON 배열만 반환. 다른 텍스트 없이.
형식: [{"path":"...", "currentCategory":"...", "correctCategory":"...", "reason":"..."}]`,
		strings.Join(Categories, ", "), strings.Join(lines, "\n"))

	systemJSON, _ := json.Marshal("You are a wiki category validator. Respond only with a JSON array.")
	resp, err := wd.client.Complete(ctx, llm.ChatRequest{
		Model:     wd.model,
		System:    systemJSON,
		Messages:  []llm.Message{llm.NewTextMessage("user", prompt)},
		MaxTokens: 2048,
	})
	if err != nil {
		wd.logger.Warn("wiki-verify: LLM misclassification check failed", "error", err)
		return nil
	}

	text := strings.TrimSpace(resp)
	text = stripCodeFences(text)

	var results []misclassificationResult
	if err := json.Unmarshal([]byte(text), &results); err != nil {
		wd.logger.Warn("wiki-verify: failed to parse LLM response",
			"error", err, "raw", truncate(text, 200))
		return nil
	}

	var findings []VerifyFinding
	for _, r := range results {
		findings = append(findings, VerifyFinding{
			Type:   "misclassified",
			Detail: fmt.Sprintf("%s → %s (%s)", r.CurrentCategory, r.CorrectCategory, r.Reason),
			PageA:  r.Path,
		})
	}

	return findings
}

// levenshtein computes the edit distance between two strings (rune-level).
func levenshtein(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	la, lb := len(ra), len(rb)

	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr := make([]int, lb+1)
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev = curr
	}
	return prev[lb]
}

// stripCodeFences removes markdown code block wrappers from LLM responses.
func stripCodeFences(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	if idx := strings.Index(text[3:], "\n"); idx >= 0 {
		text = text[3+idx+1:]
	}
	if strings.HasSuffix(text, "```") {
		text = text[:len(text)-3]
	}
	return strings.TrimSpace(text)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
