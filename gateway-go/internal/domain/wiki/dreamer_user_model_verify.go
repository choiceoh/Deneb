// dreamer_user_model_verify.go — cross-axis consistency check on the 사용자 model.
//
// The synthesis rules keep the 사용자 category as one page per axis (소통, 업무
// 리듬, 도구·포맷 취향, 판단·결정 성향, 개인 컨텍스트) and mandate "current policy,
// not history" — a preference flip replaces the old value in its OWN page. But
// that flip is rarely reflected across the OTHER axis pages that restate the
// same preference (e.g., a "간결 선호" in the 소통 axis vs a stale "상세를 좋아함"
// in the 업무 리듬 axis). The deterministic verify pass checks structure,
// duplicates and archive status — never semantic contradiction. This pass reads
// only the 사용자 pages and asks a cheap model to flag cross-axis contradictions
// as advisory findings. Detection only: the user model is current policy, so a
// rewrite stays the operator's decision (no auto-fix).
//
// Fail-open by contract: an LLM/parse error yields no findings — a flaky pass
// must never masquerade as a clean user model or block the cycle.
package wiki

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

const (
	// userModelVerifyMaxTokens budgets the contradiction JSON array.
	userModelVerifyMaxTokens = 4096
	// userModelVerifyTimeout bounds the call.
	userModelVerifyTimeout = 3 * time.Minute
	// userModelBodyMaxRunes caps how much of each 사용자 page body the pass reads —
	// enough for contradiction detection, not the whole (possibly long) policy.
	userModelBodyMaxRunes = 1200
	// userModelVerifyMinPages: below this there is no cross-axis surface to
	// contradict (one axis page cannot contradict another).
	userModelVerifyMinPages = 2
)

// userModelConflict is one cross-axis contradiction the LLM reports.
type userModelConflict struct {
	PageA         string `json:"pageA"`
	PageB         string `json:"pageB"`
	Contradiction string `json:"contradiction"`
}

// detectUserModelContradictions flags semantic contradictions across 사용자
// category axis pages. Advisory only (no Fix). Returns nil when there are fewer
// than two live 사용자 pages, no client, or any LLM/parse error (fail-open).
func (wd *WikiDreamer) detectUserModelContradictions(ctx context.Context) []verifyFinding {
	if wd.client == nil {
		return nil
	}
	relPaths, err := wd.store.ListPages("사용자")
	if err != nil || len(relPaths) < userModelVerifyMinPages {
		return nil
	}

	type pageSnippet struct {
		path  string
		title string
		body  string
	}
	var pages []pageSnippet
	for _, rp := range relPaths {
		rp = filepath.ToSlash(rp) // ListPages walks with the OS separator
		page, rerr := wd.store.ReadPage(rp)
		if rerr != nil || !isCurrentOrdinaryPage(rp, page) {
			continue
		}
		pages = append(pages, pageSnippet{path: rp, title: page.Meta.Title, body: clipRunes(page.Body, userModelBodyMaxRunes)})
	}
	if len(pages) < userModelVerifyMinPages {
		return nil
	}

	var lines []string
	for _, p := range pages {
		title := p.title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(p.path), ".md")
		}
		lines = append(lines, fmt.Sprintf("## [%s] %s\n%s", p.path, title, p.body))
	}

	prompt := fmt.Sprintf(`아래는 사용자 모델 페이지입니다 (축별 현행 정책). 축 간 **서로 모순되는** 내용을 찾아 JSON 배열로 반환하세요.

## 사용자 모델 페이지
%s

## 규칙
- 같은 사실이 두 페이지에서 서로 다르게 서술된 것만 지적 (예: 소통 축 "간결 선호" vs 업무리듬 축 "상세를 좋아함")
- 한 페이지 안의 모순이 아니라 **서로 다른 두 페이지 사이**의 모순만
- 애매하면 생략. 실제로 충돌할 때만
- 문제 없으면 빈 배열 [] 반환

JSON 배열만 반환. 다른 텍스트 없이.
형식: [{"pageA":"...", "pageB":"...", "contradiction":"..."}]`,
		strings.Join(lines, "\n\n"))

	req := wd.llmRequest("You check a user-model knowledge base for contradictions. Respond only with a JSON array.", prompt, userModelVerifyMaxTokens)
	// Strict-JSON one-shot: same thinking-off shaping as the misclassification
	// pass, so the output budget goes to the verdict array, not chain-of-thought.
	if !hasTemplateThinkingOff(wd.llmExtraBody) {
		req.Thinking = &llm.ThinkingConfig{Type: "disabled"}
	}
	cctx, cancel := context.WithTimeout(ctx, userModelVerifyTimeout)
	defer cancel()
	resp, err := wd.client.Complete(cctx, req)
	if err != nil {
		wd.logger.Warn("wiki-verify: user-model contradiction check failed", "error", err)
		return nil
	}
	conflicts, err := jsonutil.UnmarshalLLMArray[userModelConflict](resp)
	if err != nil {
		wd.logger.Warn("wiki-verify: failed to parse user-model contradiction response",
			"error", err, "raw", truncate(strings.TrimSpace(resp), 200))
		return nil
	}

	var findings []verifyFinding
	for _, c := range conflicts {
		if strings.TrimSpace(c.PageA) == "" || strings.TrimSpace(c.PageB) == "" || strings.TrimSpace(c.Contradiction) == "" {
			continue
		}
		findings = append(findings, verifyFinding{
			Type:   "user_model_contradiction",
			Detail: fmt.Sprintf("사용자 모델 축 간 모순: %s", c.Contradiction),
			PageA:  c.PageA,
			PageB:  c.PageB,
		})
	}
	return findings
}
