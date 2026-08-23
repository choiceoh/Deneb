// dreamer_critique.go — the offline self-critique pass (P3 verifier 공진화).
//
// Synthesis is one shot; the deterministic apply guards catch only structural
// violations, not "this proposal restates a fact the index already has" or
// "this is trivia not worth a page." An online chat turn cannot afford a second
// model round-trip to judge that, but the dream cycle is offline with a 10-minute
// budget — so it can. This is a PRECISION FILTER, not a gate: it fails open on
// any LLM/parse error (never zero a cycle) and removes only proposals the critic
// explicitly rejects. Merge/rewrite is intentionally out of scope — retargeting
// a write to the wrong page is worse than an extra page, and findExistingPage +
// the verify duplicate pass already converge near-dups at apply time.
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	// critiqueMinUpdates is the size at which every batch is reviewed.
	// Smaller batches still run when they create a page or write 기타
	// (see critiqueNeeded) — those used to skip the filter (2026-08-23).
	critiqueMinUpdates = 3
	// critiqueMaxTokens budgets the verdict array (short: index + verdict +
	// reason per proposal). llmRequest's headroom mode scales it on reasoning
	// models with no thinking off-switch.
	critiqueMaxTokens = 1500
	// critiqueTimeout bounds the critique call so a wedged backend cannot eat the
	// rest of the cycle budget (same discipline as wikiDreamSynthesisTimeout).
	critiqueTimeout = 3 * time.Minute
	// critiqueDemandLimit bounds how many demand terms the critique prompt
	// carries — the topics the wiki could not answer, which the critic must not
	// drop proposals addressing.
	critiqueDemandLimit = 8
)

const critiqueSystem = "You are a wiki knowledge-base editor reviewing proposed changes. Respond only with a JSON array."

// critiqueVerdict is one LLM judgment on a proposed update, keyed by the
// proposal's position in the list shown to the critic.
type critiqueVerdict struct {
	Index   int    `json:"index"`
	Verdict string `json:"verdict"` // "keep" | "drop"
	Reason  string `json:"reason"`
}

// critiqueUpdates precision-filters freshly synthesized proposals against the
// current index. Pending user corrections (5.7) ride along as 반증 evidence:
// a proposal that restates a fact the operator just corrected is drop material.
// Returns the surviving updates and the number dropped. Fail-open
// on every error path — the proposals pass through unfiltered rather than risk
// losing a good cycle to a flaky critic.
func (wd *WikiDreamer) critiqueUpdates(ctx context.Context, updates []wikiUpdate, corrections []DreamCorrection) ([]wikiUpdate, int) {
	if wd.client == nil || !critiqueNeeded(updates) {
		return updates, 0
	}
	ctx, cancel := context.WithTimeout(ctx, critiqueTimeout)
	defer cancel()

	indexContent := wd.store.SnapshotIndex().Render()
	demand := wd.store.RecallDemandTerms(time.Now(), critiqueDemandLimit)
	prompt := buildCritiquePrompt(updates, indexContent, corrections, demand)
	resp, err := wd.client.Complete(ctx, wd.llmRequest(critiqueSystem, prompt, critiqueMaxTokens))
	if err != nil {
		wd.logger.Warn("wiki-dream: critique call failed; keeping all proposals", "error", err)
		return updates, 0
	}
	drop := parseCritiqueDrops(resp, len(updates), wd.logger)
	if len(drop) == 0 {
		return updates, 0
	}
	kept := make([]wikiUpdate, 0, len(updates))
	dropped := 0
	for i, u := range updates {
		if drop[i] {
			dropped++
			continue
		}
		kept = append(kept, u)
	}
	if dropped > 0 {
		wd.logger.Info("wiki-dream: self-critique dropped proposals", "dropped", dropped, "kept", len(kept))
	}
	return kept, dropped
}

// buildCritiquePrompt renders the numbered proposal list + the current index and
// asks for a keep/drop verdict per index. Only the fields that decide value are
// shown (action/path/title/summary + a content snippet) to keep the call cheap.
// Pending user corrections, when present, add a 반증 block — the critic drops
// proposals that conflict with a correction the operator just made.
func buildCritiquePrompt(updates []wikiUpdate, indexContent string, corrections []DreamCorrection, demand []string) string {
	var sb strings.Builder
	for i, u := range updates {
		snippet := strings.TrimSpace(u.Content)
		if len(snippet) > 240 {
			snippet = snippet[:240]
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		fmt.Fprintf(&sb, "[%d] action=%s path=%s title=%q summary=%q\n    content: %s\n",
			i, u.Action, u.Path, u.Title, u.Summary, snippet)
	}
	demandSection := ""
	if len(demand) > 0 {
		demandSection = fmt.Sprintf(`
## 미충족 수요 (최근 답하지 못한 질문 주제)
%s
이 주제를 다루는 제안은 기존 페이지와 일부 겹치더라도 "drop"하지 마세요 — 실제로 사용자가 찾는 지식 구멍을 메우는 제안입니다.`, strings.Join(demand, ", "))
	}
	correctionBlock := ""
	if rendered := RenderDreamCorrections(corrections, 10); rendered != "" {
		correctionBlock = "\n## 사용자 반증 (운영자가 최근 정정한 사실 — 이와 충돌하거나 정정된 내용을 재진술하는 제안은 drop)\n" + rendered + "\n"
	}
	return fmt.Sprintf(`아래는 위키에 적용 예정인 제안 목록입니다. 각 제안이 지식베이스에 실제 가치를 더하는지 현재 인덱스와 대조해 판정하세요.

다음에 해당하면 verdict="drop":
- 이미 인덱스에 같은 사실이 있어 중복인 것 (새 사실 추가 없이 기존 내용 재진술)
- 일시적·잡담성이라 위키에 남길 가치가 없는 것
- 근거가 불충분한 추측 (출처 없이 단정)
- 사용자 반증과 충돌하는 것 (정정된 사실을 되살리거나 위반)
그 외 실제로 새 지식을 더하면 verdict="keep".

보수적으로: 애매하면 "keep". 명백히 가치 없는 것만 "drop".
%s
## 현재 위키 인덱스
%s
%s
## 제안 목록
%s

각 제안에 대해 JSON 배열로만 응답: [{"index":0,"verdict":"keep|drop","reason":"짧은 근거"}]
다른 텍스트 없이 배열만.`, correctionBlock, indexContent, demandSection, sb.String())
}

// parseCritiqueDrops decodes the verdict array into a drop set indexed by
// proposal position. Lenient like parseWikiUpdates: strips code fences, salvages
// a damaged-prefix array, ignores out-of-range indices, and treats anything but
// an explicit "drop" as keep. Returns an empty map (keep all) on total failure.
func parseCritiqueDrops(resp string, n int, logger *slog.Logger) map[int]bool {
	text := strings.TrimSpace(resp)
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text[3:], "\n"); idx >= 0 {
			text = text[3+idx+1:]
		}
		text = strings.TrimSpace(strings.TrimSuffix(text, "```"))
	}

	var verdicts []critiqueVerdict
	if err := json.Unmarshal([]byte(text), &verdicts); err != nil {
		// Salvage complete objects off a truncated/garbled array prefix.
		raw, _ := salvageJSONArrayPrefix(text)
		if len(raw) == 0 {
			if logger != nil {
				logger.Warn("wiki-dream: critique response unparseable; keeping all", "error", err)
			}
			return nil
		}
		for _, item := range raw {
			var v critiqueVerdict
			if json.Unmarshal(item, &v) == nil {
				verdicts = append(verdicts, v)
			}
		}
	}

	drop := make(map[int]bool)
	for _, v := range verdicts {
		if v.Index < 0 || v.Index >= n {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(v.Verdict), "drop") {
			drop[v.Index] = true
		}
	}
	return drop
}
