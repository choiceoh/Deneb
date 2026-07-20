// deal_ledger.go — chat surface over the typed deal ledger
// (domain/wiki/deal_records.go).
//
// Until now the ledger's only consumers were the heartbeat deadline signals and
// code-mode; the chat agent answered 합계/건수 questions by eyeballing prose and
// re-summing figures itself (measured drift: wiki-qa pipeline-h2 turned a
// documented ~1,068MW into "~1,070MW"). This tool makes those questions
// deterministic: filtering and per-currency sums happen in code, unparsed
// amounts are surfaced instead of guessed.
package wikitool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// metricDefsPage is the operator-curated metric-definition page — the semantic
// layer over the ledger: which docTypes count toward 수주/파이프라인/매출, how
// MW totals are read, how 기타/미파싱 rows are treated. The wiki page (not
// code) owns the definitions, matching the 모닝레터-발송-규칙 control model;
// it is re-read on every call so edits apply without a rebuild.
const metricDefsPage = "시스템/지표-정의.md"

// metricDefsMaxRunes caps the appended definitions block so a grown page can
// never crowd out the ledger rows it annotates.
const metricDefsMaxRunes = 1500

// metricDefinitionsFooter renders the curated definitions page as a footer
// block. A missing/empty page yields "" so the ledger behaves exactly as
// before the semantic layer existed.
func metricDefinitionsFooter(store *wiki.Store) string {
	page, err := store.ReadPage(metricDefsPage)
	if err != nil || page == nil {
		return ""
	}
	body := strings.TrimSpace(page.Body)
	if body == "" {
		return ""
	}
	if runes := []rune(body); len(runes) > metricDefsMaxRunes {
		body = strings.TrimSpace(string(runes[:metricDefsMaxRunes])) + "\n… (잘림 — 전체는 시스템/지표-정의 페이지 참조)"
	}
	return "지표 정의 (시스템/지표-정의 — 집계 해석은 이 기준을 따를 것):\n" + body
}

// ToolDealLedger returns the deal_ledger tool over the wiki store's typed
// ledger. list = filtered rows (newest first) + totals footer; sum = totals
// only. When the operator-curated 지표-정의 page exists, every result carries
// it as a footer so aggregate answers use one shared set of definitions.
func ToolDealLedger(store *wiki.Store) toolport.ToolFunc {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		var args struct {
			Action       string `json:"action"`
			Counterparty string `json:"counterparty"`
			Project      string `json:"project"`
			DocType      string `json:"doc_type"`
			Since        string `json:"since"`
			Until        string `json:"until"`
			Limit        int    `json:"limit"`
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("deal_ledger: invalid arguments: %w", err)
			}
		}
		recs, err := store.QueryDealRecords(wiki.DealRecordFilter{
			Counterparty: args.Counterparty,
			Project:      args.Project,
			DocType:      args.DocType,
			Since:        args.Since,
			Until:        args.Until,
		})
		if err != nil {
			return "", fmt.Errorf("deal_ledger: %w", err)
		}
		if len(recs) == 0 {
			// Definitions matter most here: an empty result is often a
			// mis-chosen doc_type filter, and the page names the valid ones.
			out := "거래 원장 기록 없음 (필터: " + filterDesc(args.Counterparty, args.Project, args.DocType, args.Since, args.Until) + ")"
			if defs := metricDefinitionsFooter(store); defs != "" {
				out += "\n\n" + defs
			}
			return out, nil
		}
		totals := wiki.SumDealRecords(recs)

		var b strings.Builder
		fmt.Fprintf(&b, "거래 원장 %d건 (필터: %s)\n", len(recs),
			filterDesc(args.Counterparty, args.Project, args.DocType, args.Since, args.Until))

		if strings.TrimSpace(args.Action) != "sum" {
			limit := args.Limit
			if limit <= 0 {
				limit = 20
			}
			shown := recs
			if len(shown) > limit {
				shown = shown[:limit]
			}
			for _, r := range shown {
				b.WriteString(renderDealRecordLine(r))
				b.WriteByte('\n')
			}
			if len(recs) > len(shown) {
				fmt.Fprintf(&b, "…외 %d건 (limit=%d — 합계는 전체 기준)\n", len(recs)-len(shown), limit)
			}
		}

		b.WriteString(renderDealTotals(totals))
		// The tool advertises 거래처별 합계: when the result spans several
		// counterparties, append the deterministic per-counterparty breakdown
		// (always over the FULL filtered set, independent of the list limit).
		if breakdown := renderCounterpartyBreakdown(recs); breakdown != "" {
			b.WriteByte('\n')
			b.WriteString(breakdown)
		}
		if defs := metricDefinitionsFooter(store); defs != "" {
			b.WriteString("\n\n")
			b.WriteString(defs)
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
}

// renderCounterpartyBreakdown renders per-counterparty totals when more than
// one counterparty is present, most rows first, capped at 12 lines.
func renderCounterpartyBreakdown(recs []wiki.DealRecord) string {
	byCP := map[string][]wiki.DealRecord{}
	for _, r := range recs {
		byCP[r.Counterparty] = append(byCP[r.Counterparty], r)
	}
	if len(byCP) <= 1 {
		return ""
	}
	names := make([]string, 0, len(byCP))
	for n := range byCP {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if len(byCP[names[i]]) != len(byCP[names[j]]) {
			return len(byCP[names[i]]) > len(byCP[names[j]])
		}
		return names[i] < names[j]
	})
	var b strings.Builder
	b.WriteString("거래처별:")
	for i, n := range names {
		if i >= 12 {
			fmt.Fprintf(&b, "\n- …외 %d개 거래처 (counterparty 필터로 좁혀서 조회)", len(names)-i)
			break
		}
		b.WriteString("\n- " + n + ": " + renderDealTotals(wiki.SumDealRecords(byCP[n])))
	}
	return b.String()
}

func filterDesc(cp, pj, dt, since, until string) string {
	var parts []string
	if cp = strings.TrimSpace(cp); cp != "" {
		parts = append(parts, "거래처="+cp)
	}
	if pj = strings.TrimSpace(pj); pj != "" {
		parts = append(parts, "프로젝트="+pj)
	}
	if dt = strings.TrimSpace(dt); dt != "" {
		parts = append(parts, "문서="+dt)
	}
	if since != "" || until != "" {
		parts = append(parts, strings.TrimSpace(since+"~"+until))
	}
	if len(parts) == 0 {
		return "전체"
	}
	return strings.Join(parts, ", ")
}

func renderDealRecordLine(r wiki.DealRecord) string {
	var parts []string
	if r.Date != "" {
		parts = append(parts, r.Date)
	}
	parts = append(parts, r.Counterparty)
	if r.DocType != "" {
		parts = append(parts, r.DocType)
	}
	if r.AmountRaw != "" {
		amt := r.AmountRaw
		if !r.AmountParsed {
			amt += " (미파싱)"
		}
		parts = append(parts, amt)
	}
	// Quote-verified terms (fact layer) — compact; quotes stay on the ledger row.
	if t := r.Terms; t != nil {
		for _, term := range []struct{ label, v string }{
			{"물량", t.Capacity.Value},
			{"단가", t.UnitPrice.Value},
			{"지급", t.Payment.Value},
			{"하자", t.Warranty.Value},
			{"지체상금", t.DelayPenalty.Value},
		} {
			if term.v != "" {
				parts = append(parts, term.label+" "+term.v)
			}
		}
	}
	line := "- " + strings.Join(parts, " · ")
	if r.Summary != "" {
		line += " — " + r.Summary
	}
	if len(r.Projects) > 0 {
		line += " [" + strings.Join(r.Projects, ", ") + "]"
	}
	if r.DueDate != "" {
		line += " (기한 " + r.DueDate + ")"
	}
	return line
}

func renderDealTotals(t wiki.DealTotals) string {
	currencies := make([]string, 0, len(t.SumByCurrency))
	for c := range t.SumByCurrency {
		currencies = append(currencies, c)
	}
	sort.Strings(currencies)
	var parts []string
	for _, c := range currencies {
		label := c
		if c == "?" {
			label = "통화미상"
		}
		parts = append(parts, fmt.Sprintf("%s %s (%d건)", label, fmtDealAmount(t.SumByCurrency[c]), t.CountByCurrency[c]))
	}
	line := "합계(금액 파싱분): "
	if len(parts) == 0 {
		line += "없음"
	} else {
		line += strings.Join(parts, " · ")
	}
	if t.UnparsedCount > 0 {
		line += fmt.Sprintf(" · 미파싱 %d건", t.UnparsedCount)
		if len(t.UnparsedSamples) > 0 {
			line += " (예: " + strings.Join(t.UnparsedSamples, ", ") + ")"
		}
		line += " — 합계 미포함, 원문 확인 필요"
	}
	if t.NoAmountCount > 0 {
		line += fmt.Sprintf(" · 금액 없음 %d건 (합계 미포함)", t.NoAmountCount)
	}
	if t.CapacityCount > 0 {
		line += fmt.Sprintf(" · 물량 합계 %sMW (%d건 — 물량 확인분만)", fmtDealAmount(t.CapacityMWSum), t.CapacityCount)
	}
	return line
}

// fmtDealAmount renders a sum with thousands separators, keeping cents only
// when they exist ("564,963.44", "5,000,000").
func fmtDealAmount(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	intPart, frac, _ := strings.Cut(s, ".")
	neg := strings.HasPrefix(intPart, "-")
	intPart = strings.TrimPrefix(intPart, "-")
	var b strings.Builder
	for i, ch := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	out := b.String()
	if neg {
		out = "-" + out
	}
	if frac != "" {
		out += "." + frac
	}
	return out
}
