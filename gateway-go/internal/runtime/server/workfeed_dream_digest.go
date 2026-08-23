package server

// Weekly memory digest (docs/research/improvement-ideas.md 4.9). The dreamer
// is the most influential autonomous component — it mutates long-term memory
// (wiki) in the background with zero operator visibility beyond per-cycle
// cards. The digest rolls the week up into one card the operator can skim and
// answer: nothing wrong (확인) or something learned is wrong (틀린 기억 알리기
// routes a correction turn into the home conversation, where the agent can fix
// the wiki and the next dream verify pass re-checks the fact).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

const (
	dreamDigestSource = "dream-digest"
	// dreamDigestCheckInterval: how often the rollup is inspected. The digest
	// itself fires at most once per dreamDigestMinInterval.
	dreamDigestCheckInterval = 24 * time.Hour
	dreamDigestMinInterval   = 7 * 24 * time.Hour
)

// DreamDigestTask posts the weekly memory digest card. Registered on the
// autonomous scheduler like the genesis watches; its only state is the
// last-digest timestamp next to the rollup ledger.
type DreamDigestTask struct {
	Server *Server
}

func (t *DreamDigestTask) Name() string { return "dream-digest" }

func (t *DreamDigestTask) Interval() time.Duration { return dreamDigestCheckInterval }

type dreamDigestState struct {
	LastDigestAtMs int64 `json:"lastDigestAtMs"`
}

func dreamDigestStatePath() string {
	return filepath.Join(filepath.Dir(dreamReportsPath()), "dream_digest_state.json")
}

// Run posts one digest when new dream reports accumulated since the last
// digest AND the minimum weekly gap elapsed. The first run (no state file)
// baselines silently so months of standing reports never flood a card.
func (t *DreamDigestTask) Run(_ context.Context) error {
	s := t.Server
	if s == nil {
		return nil
	}
	raw, err := os.ReadFile(dreamDigestStatePath())
	if err != nil {
		// First run (no state file) or unreadable state: record the baseline
		// timestamp and stay silent — never flood months of standing reports.
		writeDreamDigestState(time.Now().UnixMilli())
		return nil //nolint:nilerr // baseline is the intended success path
	}
	var state dreamDigestState
	if err := json.Unmarshal(raw, &state); err != nil {
		// Corrupt state: re-baseline.
		writeDreamDigestState(time.Now().UnixMilli())
		return nil //nolint:nilerr // re-baseline is the intended success path
	}
	now := time.Now().UnixMilli()
	if now-state.LastDigestAtMs < dreamDigestMinInterval.Milliseconds() {
		return nil
	}
	entries, err := loadDreamReportsSince(state.LastDigestAtMs)
	if err != nil || len(entries) == 0 {
		return err // no fresh cycles → nothing to digest
	}
	stats := aggregateDreamReports(entries, state.LastDigestAtMs, now)
	if err := s.postDreamDigestCard(stats); err != nil {
		s.logger.Warn("dream digest card post failed", "error", err)
		return nil // retry next check; state not bumped
	}
	writeDreamDigestState(now)
	return nil
}

func writeDreamDigestState(atMs int64) {
	raw, err := json.Marshal(dreamDigestState{LastDigestAtMs: atMs})
	if err != nil {
		return
	}
	path := dreamDigestStatePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// dreamDigestStats aggregates one digest window of the rollup.
type dreamDigestStats struct {
	Cycles           int     `json:"cycles"`
	FactsLearned     int     `json:"factsLearned"`
	FactsMerged      int     `json:"factsMerged"`
	FactsExpired     int     `json:"factsExpired"`
	FactsMoved       int     `json:"factsMoved"`
	WikiPagesCreated int     `json:"wikiPagesCreated"`
	WikiPagesUpdated int     `json:"wikiPagesUpdated"`
	UserModelUpdated int     `json:"userModelUpdated"`
	RecallHitPages   int     `json:"recallHitPages"`
	QualityAvg       float64 `json:"qualityAvg"`
	// QualityTrend carries the window's per-cycle quality scores (scored
	// cycles only, entry order) for the body's sparkline — the average hides
	// a slide that a trend makes obvious.
	QualityTrend []float64 `json:"qualityTrend,omitempty"`
	// RecallDemandTerms: the topics recent turns asked about that the wiki
	// could not answer (deduplicated, most-seen first) — what memory should
	// learn next.
	RecallDemandTerms []string `json:"recallDemandTerms,omitempty"`
	SinceMs           int64    `json:"sinceMs"`
	UntilMs           int64    `json:"untilMs"`
}

func aggregateDreamReports(entries []dreamReportEntry, sinceMs, untilMs int64) dreamDigestStats {
	stats := dreamDigestStats{SinceMs: sinceMs, UntilMs: untilMs}
	var qualitySum float64
	var qualityCount int
	demandCounts := map[string]int{}
	var demandOrder []string
	for _, e := range entries {
		r := e.Report
		stats.Cycles++
		stats.FactsLearned += r.FactsLearned
		stats.FactsMerged += r.FactsMerged
		stats.FactsExpired += r.FactsExpired
		stats.FactsMoved += r.FactsMoved
		stats.WikiPagesCreated += r.WikiPagesCreated
		stats.WikiPagesUpdated += r.WikiPagesUpdated
		stats.UserModelUpdated += r.UserModelUpdated
		stats.RecallHitPages += r.RecallHitPages
		if r.QualityScore > 0 {
			qualitySum += r.QualityScore
			qualityCount++
			if len(stats.QualityTrend) >= 12 {
				stats.QualityTrend = stats.QualityTrend[1:]
			}
			stats.QualityTrend = append(stats.QualityTrend, r.QualityScore)
		}
		for _, term := range r.RecallDemandTerms {
			term = strings.TrimSpace(term)
			if term == "" {
				continue
			}
			if demandCounts[term] == 0 {
				demandOrder = append(demandOrder, term)
			}
			demandCounts[term]++
		}
	}
	if qualityCount > 0 {
		stats.QualityAvg = qualitySum / float64(qualityCount)
	}
	// Sort demand terms by count (stable on first-seen order), keep top 5.
	for i := 1; i < len(demandOrder); i++ {
		for j := i; j > 0 && demandCounts[demandOrder[j]] > demandCounts[demandOrder[j-1]]; j-- {
			demandOrder[j], demandOrder[j-1] = demandOrder[j-1], demandOrder[j]
		}
	}
	if len(demandOrder) > 5 {
		demandOrder = demandOrder[:5]
	}
	stats.RecallDemandTerms = demandOrder
	return stats
}

// postDreamDigestCard surfaces one weekly digest. The feedback action's
// prompt lands in the home conversation (client:main) — the agent then fixes
// the wrong memory with its normal wiki tools and the next dream verify pass
// re-checks the corrected fact.
func (s *Server) postDreamDigestCard(stats dreamDigestStats) error {
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return fmt.Errorf("native work feed unavailable")
	}
	days := int(time.Duration(stats.UntilMs-stats.SinceMs).Hours() / 24)
	var b strings.Builder
	fmt.Fprintf(&b, "지난 %d일간 백그라운드 기억 통합(드림)이 위키 장기기억을 정리했습니다.\n\n", days)
	fmt.Fprintf(&b, "- 드림 사이클: %d회\n", stats.Cycles)
	fmt.Fprintf(&b, "- 사실: 학습 %d · 병합 %d · 만료 %d", stats.FactsLearned, stats.FactsMerged, stats.FactsExpired)
	if stats.FactsMoved > 0 {
		fmt.Fprintf(&b, " · 재분류 %d", stats.FactsMoved)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "- 위키: 생성 %d · 갱신 %d (사용자 모델 %d)\n", stats.WikiPagesCreated, stats.WikiPagesUpdated, stats.UserModelUpdated)
	if stats.RecallHitPages > 0 {
		fmt.Fprintf(&b, "- 회상 활용: %d면\n", stats.RecallHitPages)
	}
	if stats.QualityAvg > 0 {
		fmt.Fprintf(&b, "- 평균 품질: %.0f/100\n", stats.QualityAvg)
	}
	if len(stats.QualityTrend) >= 2 {
		first := stats.QualityTrend[0]
		last := stats.QualityTrend[len(stats.QualityTrend)-1]
		fmt.Fprintf(&b, "- 품질 추이: %s (%.0f→%.0f)\n", sparklineBars(stats.QualityTrend), first, last)
	}
	if len(stats.RecallDemandTerms) > 0 {
		fmt.Fprintf(&b, "- 기억 공백(자주 물은 주제): %s\n", strings.Join(stats.RecallDemandTerms, ", "))
	}
	b.WriteString("\n학습된 기억 중 사실과 다른 것이 있으면 '틀린 기억 알리기'로 알려주세요 — 정정은 다음 검증 주기에 반영됩니다.")
	item := workfeed.Item{
		Source:     dreamDigestSource,
		Title:      fmt.Sprintf("주간 기억 다이제스트: 학습 %d · 병합 %d · 만료 %d", stats.FactsLearned, stats.FactsMerged, stats.FactsExpired),
		Summary:    fmt.Sprintf("드림 %d사이클 · 위키 %d생성 %d갱신", stats.Cycles, stats.WikiPagesCreated, stats.WikiPagesUpdated),
		Body:       b.String(),
		Status:     workfeed.StatusUnread,
		SessionKey: "client:main",
		RefType:    "dream-digest",
		Actions: []workfeed.Action{
			{ID: "dream-digest:ack", Kind: workfeed.ActionAck, Label: "확인"},
			{
				ID: "dream-digest:feedback", Kind: workfeed.ActionFollowUp, Label: "틀린 기억 알리기",
				Prompt: "주간 기억 다이제스트를 확인했어. 학습된 기억 중 사실과 다른 것이 있으면 알려줘 — 말해주면 위키/기억을 정정하고 다음 드림 검증 주기에 반증 증거로 반영하겠어.\n\n## 다이제스트 요약\n" + b.String(),
			},
		},
	}
	if _, err := nf.Append(item); err != nil {
		return fmt.Errorf("post dream digest card: %w", err)
	}
	return nil
}

// sparklineBars renders 0–100 scores as unicode block bars (▁▂▃▄▅▆▇█), scaled
// to the series' own min/max so a flat series reads flat rather than pinned to
// the floor. Text-only by design: the digest body is markdown, and a text
// sparkline survives every renderer (native, desktop, notification preview).
func sparklineBars(series []float64) string {
	if len(series) == 0 {
		return ""
	}
	const levels = "▁▂▃▄▅▆▇█"
	minV, maxV := series[0], series[0]
	for _, v := range series[1:] {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	levelRunes := []rune(levels)
	bars := make([]rune, 0, len(series)*2)
	for _, v := range series {
		idx := len(levelRunes) / 2 // flat series → mid bar
		if maxV > minV {
			idx = int((v - minV) / (maxV - minV) * float64(len(levelRunes)-1))
			if idx < 0 {
				idx = 0
			}
			if idx > len(levelRunes)-1 {
				idx = len(levelRunes) - 1
			}
		}
		bars = append(bars, levelRunes[idx], ' ')
	}
	return strings.TrimSpace(string(bars))
}
