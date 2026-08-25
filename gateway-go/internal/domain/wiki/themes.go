// themes.go — cross-source recurring-signal ledger (업무/반복신호.md).
//
// Adopted from OpenWiki's /themes.md concept (2026-07-20 review): recall and
// the proactive briefs had no place where a signal that keeps coming back —
// a rising price, a counterparty issue, a recurring request — is visible AS
// recurring. Individual observations live on project/업무 pages; this ledger
// aggregates them across days into terse table rows.
//
// Like open_loops.go, extraction is a separate focused LLM call so contract
// drift can never cost a synthesis cycle. Unlike prose pages, the page BODY
// is deterministic: Go parses the existing table, merges extracted signals by
// stable key (dates, counts, stage promotion, dormancy), and rewrites the
// whole table. The LLM never edits rows — the append-merge duplication
// problem that rules out prompt-maintained tables cannot corrupt it, and the
// synthesis path is blocked from touching the page (prepareDreamUpdate).
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

// ThemePagePath is the ledger's fixed wiki slot. The dreamer synthesis path
// must not write it (deterministically enforced in prepareDreamUpdate).
const ThemePagePath = "업무/반복신호.md"

// themeMaxPerCycle caps extraction output; a cycle that "finds" a dozen
// recurring signals in one diary batch is hallucinating, not observing.
const themeMaxPerCycle = 6

// themeMaxTokens bounds the extraction response.
const themeMaxTokens = 1024

// themeTimeout bounds the extraction LLM call so a wedged backend costs the
// theme pass, not the remaining dream-cycle budget.
const themeTimeout = 2 * time.Minute

// themeDormantAfterDays marks a row 휴면 when its last observation is older.
// Re-observation flips it back to 활성.
const themeDormantAfterDays = 45

// themeMaxRows caps the ledger; dormant rows are pruned first beyond it.
const themeMaxRows = 60

// ThemeSignal is one recurring-signal candidate extracted from cycle input.
type ThemeSignal struct {
	Key      string `json:"key"`      // stable short key, reused across cycles
	Signal   string `json:"signal"`   // one-line Korean description
	Evidence string `json:"evidence"` // one-line source context
}

// themeRow is one ledger row. 단계 (관찰/반복/정착) is derived from Count at
// render time, so it carries no state of its own.
type themeRow struct {
	Key      string
	Signal   string
	First    string // YYYY-MM-DD of first observation
	Last     string // YYYY-MM-DD of latest observation
	Count    int    // distinct observation days
	Status   string // 활성 | 휴면
	Evidence string // latest one-line evidence
}

// extractThemeSignals runs the focused extraction pass over the cycle input.
// knownKeys steers the model to reuse existing ledger keys so the
// deterministic merge can match re-observations instead of minting variants.
func (wd *WikiDreamer) extractThemeSignals(ctx context.Context, content string, knownKeys []string) ([]ThemeSignal, error) {
	if wd.client == nil || strings.TrimSpace(content) == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, themeTimeout)
	defer cancel()

	known := "(없음)"
	if len(knownKeys) > 0 {
		known = strings.Join(knownKeys, ", ")
	}
	prompt := fmt.Sprintf(`아래 일지/메모에서 **앞으로도 반복될 가능성이 있는 신호**(추세·리스크·반복 이슈·반복 요청)만 추출하세요.

## 추출 기준
- 오늘 하루로 끝나는 단발 사건·잡담·인사·일정 공지는 제외
- 같은 주제가 재등장할 때 알아볼 수 있는 짧은 신호키(kebab-case 한국어/영문)를 부여
- 기존 신호키 목록에 같은 신호가 이미 있으면 **반드시 그 키를 재사용**: %s
- 최대 %d개, 확신 있는 것만. 없으면 무리하게 만들지 마세요

## 출력 (JSON 배열만, 다른 텍스트 없이)
[{"key":"신호키","signal":"신호 한 줄(한국어)","evidence":"근거 한 줄"}]
신호가 없으면 [] 를 반환하세요.

## 일지/메모
%s`, known, themeMaxPerCycle, content)

	resp, err := wd.client.Complete(ctx,
		wd.llmRequest("You extract recurring signals. Respond only with a JSON array.", prompt, themeMaxTokens))
	if err != nil {
		return nil, fmt.Errorf("theme LLM call: %w", err)
	}
	return parseThemeSignals(resp)
}

// parseThemeSignals decodes the extraction response: fences stripped, capped,
// empty/keyless entries dropped, free text redacted, keys normalized.
func parseThemeSignals(text string) ([]ThemeSignal, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text[3:], "\n"); idx >= 0 {
			text = text[3+idx+1:]
		}
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}
	if text == "" {
		return nil, nil
	}
	var signals []ThemeSignal
	if err := json.Unmarshal([]byte(text), &signals); err != nil {
		return nil, fmt.Errorf("parse theme signals: %w (raw: %.200s)", err, text)
	}
	out := signals[:0]
	for _, s := range signals {
		s.Key = normalizeThemeKey(s.Key)
		s.Signal = themeCell(redact.String(s.Signal))
		s.Evidence = themeCell(redact.String(s.Evidence))
		if s.Key == "" || s.Signal == "" {
			continue
		}
		out = append(out, s)
		if len(out) >= themeMaxPerCycle {
			break
		}
	}
	return out, nil
}

// normalizeThemeKey canonicalizes an extraction key so the same signal merges
// across cycles despite spelling drift: lowercase, spaces/underscores to
// hyphens, table-breaking characters removed.
func normalizeThemeKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer(" ", "-", "_", "-", "|", "", "\n", "").Replace(key)
	return strings.Trim(key, "-")
}

// themeCell makes free text safe as a single Markdown table cell.
func themeCell(s string) string {
	s = strings.NewReplacer("|", "¦", "\r", " ", "\n", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// themeSignalMinOverlap is the character-bigram Jaccard floor at which an
// incoming signal counts as the same signal as the stored one. Set low on
// purpose: rewording is expected and explicitly allowed ("latest phrasing
// wins"), so the floor only has to separate a rephrasing — which keeps the
// subject nouns and scores well above it — from a topic swap, which shares
// almost no bigrams and scores near zero.
const themeSignalMinOverlap = 0.25

// sameThemeSignal reports whether two signal descriptions are the same signal.
// Character bigrams rather than words: Korean inflection changes suffixes
// between cycles ("반복됨"/"반복된다"), so word equality understates continuity
// while bigrams survive it.
func sameThemeSignal(stored, incoming string) bool {
	a, b := themeBigrams(stored), themeBigrams(incoming)
	if len(a) == 0 || len(b) == 0 {
		return true // nothing to compare on — fall back to the key's own claim
	}
	inter := 0
	for g := range a {
		if b[g] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return true
	}
	return float64(inter)/float64(union) >= themeSignalMinOverlap
}

func themeBigrams(s string) map[string]bool {
	r := []rune(strings.ToLower(strings.Join(strings.Fields(s), "")))
	if len(r) < 2 {
		return nil
	}
	out := make(map[string]bool, len(r)-1)
	for i := 0; i+1 < len(r); i++ {
		out[string(r[i:i+2])] = true
	}
	return out
}

// freeThemeKey returns key with the lowest numeric suffix not already in the
// ledger, so a diverging signal gets its own row instead of overwriting one.
func freeThemeKey(byKey map[string]int, key string) string {
	for n := 2; ; n++ {
		cand := key + "-" + strconv.Itoa(n)
		if _, taken := byKey[cand]; !taken {
			return cand
		}
	}
}

// mergeThemeRows merges extracted signals into the ledger rows by key and
// applies the lifecycle rules: one count per distinct observation day, latest
// phrasing/evidence wins, dormancy after themeDormantAfterDays without
// re-observation, active-first ordering, dormant-first pruning at the cap.
// Idempotent for a same-day re-run of the same signals.
func mergeThemeRows(rows []themeRow, signals []ThemeSignal, today string) []themeRow {
	byKey := make(map[string]int, len(rows))
	for i, r := range rows {
		byKey[r.Key] = i
	}
	for _, s := range signals {
		if i, ok := byKey[s.Key]; ok {
			// A key match is only a recurrence if it is the same signal. The
			// extractor is handed the whole keyspace and told to reuse a
			// matching key, so it sometimes reaches for a key that merely reads
			// close — srv2-vllm-endpoint-setup came back carrying 현대차 출고센터 EPC
			// content. Overwriting Signal in place would graft that row's First
			// date and Count (and so its 정착 stage) onto a first-ever
			// observation: the ledger would assert a months-old recurring
			// pattern for something seen once, today.
			//
			// Divergence is recorded as its own row instead. The established
			// row keeps the history it earned; the new observation starts at
			// Count 1 where it belongs. Neither is destroyed, because we cannot
			// tell which one the next cycle will legitimately continue.
			if sameThemeSignal(rows[i].Signal, s.Signal) {
				if rows[i].Last != today {
					rows[i].Count++
					rows[i].Last = today
				}
				rows[i].Signal = s.Signal
				if s.Evidence != "" {
					rows[i].Evidence = s.Evidence
				}
				continue
			}
			s.Key = freeThemeKey(byKey, s.Key)
		}
		byKey[s.Key] = len(rows)
		rows = append(rows, themeRow{
			Key: s.Key, Signal: s.Signal, First: today, Last: today,
			Count: 1, Status: "활성", Evidence: s.Evidence,
		})
	}
	for i := range rows {
		rows[i].Status = themeStatus(rows[i].Last, today)
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].Last != rows[b].Last {
			return rows[a].Last > rows[b].Last
		}
		if rows[a].Count != rows[b].Count {
			return rows[a].Count > rows[b].Count
		}
		return rows[a].Key < rows[b].Key
	})
	return pruneThemeRows(rows)
}

// themeStatus derives 활성/휴면 from the last observation date. An unparsable
// date stays 활성 — never dormant a row on bad data.
func themeStatus(last, today string) string {
	lastT, err1 := time.Parse("2006-01-02", last)
	todayT, err2 := time.Parse("2006-01-02", today)
	if err1 != nil || err2 != nil {
		return "활성"
	}
	if todayT.Sub(lastT) > themeDormantAfterDays*24*time.Hour {
		return "휴면"
	}
	return "활성"
}

// pruneThemeRows enforces themeMaxRows, dropping dormant rows (already sorted
// oldest-last) before ever touching active ones.
func pruneThemeRows(rows []themeRow) []themeRow {
	if len(rows) <= themeMaxRows {
		return rows
	}
	kept := make([]themeRow, 0, themeMaxRows)
	overflow := len(rows) - themeMaxRows
	for i := len(rows) - 1; i >= 0; i-- {
		if overflow > 0 && rows[i].Status == "휴면" {
			overflow--
			continue
		}
		kept = append(kept, rows[i])
	}
	for l, r := 0, len(kept)-1; l < r; l, r = l+1, r-1 {
		kept[l], kept[r] = kept[r], kept[l]
	}
	if len(kept) > themeMaxRows {
		kept = kept[:themeMaxRows]
	}
	return kept
}

// themeStage derives the 단계 label from the observation-day count.
func themeStage(count int) string {
	switch {
	case count <= 1:
		return "관찰"
	case count <= 3:
		return "반복"
	default:
		return "정착"
	}
}

// parseThemeRows recovers ledger rows from the page body. Unrecognized lines
// are ignored, so a manually damaged table degrades to fewer rows instead of
// failing the pass.
func parseThemeRows(body string) []themeRow {
	var rows []themeRow
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 8 {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if cells[0] == "신호키" || strings.HasPrefix(cells[0], "---") {
			continue
		}
		count, err := strconv.Atoi(cells[4])
		if err != nil || count < 1 {
			count = 1
		}
		rows = append(rows, themeRow{
			Key: normalizeThemeKey(cells[0]), Signal: cells[1],
			First: cells[2], Last: cells[3], Count: count,
			Status: cells[6], Evidence: cells[7],
		})
	}
	return rows
}

// renderThemePage renders the full deterministic ledger body.
func renderThemePage(rows []themeRow) string {
	var sb strings.Builder
	sb.WriteString("# 반복 신호\n\n")
	sb.WriteString("여러 날짜에 걸쳐 재관측된 신호의 원장. 행은 드림 사이클이 결정적으로 갱신한다 — ")
	sb.WriteString("직접 편집하지 말고, 상세 설명은 해당 프로젝트/업무 페이지에 남겨 [[링크]]로 잇는다.\n\n")
	sb.WriteString("| 신호키 | 신호 | 최초 | 최근 | 근거수 | 단계 | 상태 | 최근 근거 |\n")
	sb.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "| %s | %s | %s | %s | %d | %s | %s | %s |\n",
			themeCell(r.Key), themeCell(r.Signal), themeCell(r.First), themeCell(r.Last),
			r.Count, themeStage(r.Count), themeCell(r.Status), themeCell(r.Evidence))
	}
	return sb.String()
}

// captureDreamThemes runs the theme pass for one dream cycle: extract signal
// candidates from the cycle input, merge them into the ledger, persist only
// when the table actually changed. Failures cost the pass, never the cycle.
func (wd *WikiDreamer) captureDreamThemes(ctx context.Context, cycle *dreamCycle) {
	if wd.client == nil || auxInputTooSmall(cycle.synthInput) {
		return
	}
	existing := wd.loadThemeRows()
	keys := make([]string, 0, len(existing))
	for _, r := range existing {
		keys = append(keys, r.Key)
	}
	signals, err := wd.extractThemeSignals(ctx, cycle.synthInput, keys)
	if err != nil {
		cycle.addPhaseError("themes: %v", err)
		return
	}
	if len(signals) == 0 {
		return
	}
	today := dentime.Now().Format("2006-01-02")
	merged := mergeThemeRows(existing, signals, today)
	body := renderThemePage(merged)
	changed, err := wd.saveThemePage(body, today)
	if err != nil {
		cycle.addPhaseError("themes-save: %v", err)
		return
	}
	if changed {
		wd.logger.Info("wiki-dream: theme signals merged",
			"extracted", len(signals), "rows", len(merged))
	}
}

// loadThemeRows reads the current ledger; a missing or unreadable page is an
// empty ledger.
func (wd *WikiDreamer) loadThemeRows() []themeRow {
	page, err := wd.store.ReadPage(ThemePagePath)
	if err != nil || page == nil {
		return nil
	}
	return parseThemeRows(page.Body)
}

// saveThemePage persists the rendered ledger, reporting whether anything
// changed (an unchanged body is a no-op so scheduled cycles do not churn
// updated: metadata — OpenWiki's content-snapshot discipline).
func (wd *WikiDreamer) saveThemePage(body, today string) (bool, error) {
	changed := false
	err := wd.store.UpdatePage(ThemePagePath, func(existing *Page) (*Page, error) {
		if existing != nil && existing.Body == body {
			return existing, nil
		}
		changed = true
		if existing == nil {
			existing = NewPage("반복 신호", "업무", []string{"반복신호", "트렌드"})
			existing.Meta.ID = "recurring-signals"
			existing.Meta.Type = "log"
			existing.Meta.Summary = "여러 날짜에 걸쳐 재관측된 신호의 결정적 원장 (드림 사이클 자동 관리)"
			existing.Meta.Cues = []string{"테마", "반복 이슈", "요즘 자주 나오는"}
			existing.Meta.Importance = 0.6
		}
		existing.Body = body
		existing.Meta.Updated = today
		return existing, nil
	})
	return changed, err
}
