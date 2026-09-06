package wikitool

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

func TestToolDealLedgerReturnsLedgerSummary(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)
	for _, in := range []wiki.DealPageInput{
		{Counterparty: "JA Solar", DocType: "견적서", Amount: "$1,200,000", Date: "2026-06-22", SourceRef: "mail:ja1"},
		{Counterparty: "JA Solar", DocType: "견적서", Amount: "$800,000.50", Date: "2026-06-24", SourceRef: "mail:ja2"},
		{Counterparty: "대한전선", DocType: "견적서", Amount: "오백만원", Date: "2026-06-01", SourceRef: "mail:dh1"},
	} {
		if _, _, err := store.UpsertDealPage(in, now); err != nil {
			t.Fatalf("UpsertDealPage: %v", err)
		}
	}

	tool := ToolDealLedger(store)

	out, err := tool(context.Background(), json.RawMessage(`{"counterparty":"ja solar"}`))
	if err != nil {
		t.Fatalf("deal_ledger: %v", err)
	}
	if !strings.Contains(out, "거래 원장 2건") {
		t.Errorf("row count missing:\n%s", out)
	}
	// Deterministic per-currency total with thousands separators and cents.
	if !strings.Contains(out, "USD 2,000,000.5 (2건)") {
		t.Errorf("USD total missing:\n%s", out)
	}
	// Newest first in the listing.
	if strings.Index(out, "2026-06-24") > strings.Index(out, "2026-06-22") {
		t.Errorf("not newest-first:\n%s", out)
	}

	// sum action: totals only, unparsed amounts surfaced with their raw text.
	out, err = tool(context.Background(), json.RawMessage(`{"action":"sum","counterparty":"대한전선"}`))
	if err != nil {
		t.Fatalf("deal_ledger sum: %v", err)
	}
	if strings.Contains(out, "- 2026-06-01") {
		t.Errorf("sum action should not list rows:\n%s", out)
	}
	if !strings.Contains(out, "미파싱 1건") || !strings.Contains(out, "오백만원") {
		t.Errorf("unparsed reporting missing:\n%s", out)
	}

	// Empty result is a clear message, not an error.
	out, err = tool(context.Background(), json.RawMessage(`{"counterparty":"없는회사"}`))
	if err != nil || !strings.Contains(out, "기록 없음") {
		t.Errorf("empty result = %q, %v", out, err)
	}
}

func TestToolDealLedgerMetricDefinitionsFooter(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)
	in := wiki.DealPageInput{Counterparty: "JA Solar", DocType: "계약서", Amount: "$1,000", Date: "2026-06-22", SourceRef: "mail:ja1"}
	if _, _, err := store.UpsertDealPage(in, now); err != nil {
		t.Fatalf("UpsertDealPage: %v", err)
	}
	tool := ToolDealLedger(store)

	// Without the page, results carry no footer (pre-semantic-layer behavior).
	out, err := tool(context.Background(), nil)
	if err != nil || strings.Contains(out, "지표 정의") {
		t.Errorf("footer without defs page: %q, %v", out, err)
	}

	page := wiki.NewPage("지표 정의", "시스템", nil)
	page.Body = "# 지표 정의\n\n- 수주액 = 계약서+발주서 합계"
	if err := store.WritePage("시스템/지표-정의.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	// List path: definitions appended after totals.
	out, err = tool(context.Background(), nil)
	if err != nil {
		t.Fatalf("deal_ledger: %v", err)
	}
	if !strings.Contains(out, "지표 정의 (시스템/지표-정의") || !strings.Contains(out, "수주액 = 계약서+발주서 합계") {
		t.Errorf("definitions footer missing:\n%s", out)
	}
	if strings.Index(out, "합계(금액 파싱분)") > strings.Index(out, "지표 정의 (") {
		t.Errorf("footer must follow totals:\n%s", out)
	}

	// Empty-result path (mis-chosen filters) also carries the definitions.
	out, err = tool(context.Background(), json.RawMessage(`{"counterparty":"없는회사"}`))
	if err != nil || !strings.Contains(out, "기록 없음") || !strings.Contains(out, "수주액 = 계약서+발주서 합계") {
		t.Errorf("empty-result footer missing: %q, %v", out, err)
	}

	// A grown page is capped with a visible truncation marker.
	page.Body = strings.Repeat("정의 항목 줄\n", 400)
	if err := store.WritePage("시스템/지표-정의.md", page); err != nil {
		t.Fatalf("WritePage long: %v", err)
	}
	out, err = tool(context.Background(), json.RawMessage(`{"action":"sum"}`))
	if err != nil {
		t.Fatalf("deal_ledger sum: %v", err)
	}
	if !strings.Contains(out, "잘림 — 전체는 시스템/지표-정의 페이지 참조") {
		t.Errorf("truncation marker missing:\n%s", out)
	}
	if got := len([]rune(out)); got > metricDefsMaxRunes+800 {
		t.Errorf("footer not capped: %d runes", got)
	}
}

// TestToolDealLedgerMonthlyBucketsAreExact is the contract 월별 매출/발주 추이
// depends on: a month filter returns that month and nothing else, including for
// documents whose date was written in a Korean shorthand, and it says out loud
// how many rows carry no date at all rather than quietly counting them in every
// month. Dates here are the shapes the production ledger actually holds.
func TestToolDealLedgerMonthlyBucketsAreExact(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	for _, in := range []wiki.DealPageInput{
		{Counterparty: "연우철강", DocType: "발주품의", Amount: "505,000원", Date: "26.08.26", SourceRef: "m:1"},
		{Counterparty: "우림건설", DocType: "지출결의", Amount: "1,000,000원", Date: "2026-08-31", SourceRef: "m:2"},
		{Counterparty: "금비전자", DocType: "발주품의", Amount: "2,000,000원", Date: "2026-07-16 (목)", SourceRef: "m:3"},
		{Counterparty: "SKB", DocType: "기타", Amount: "3,000,000원", Date: "2026년 6월 29일", SourceRef: "m:4"},
		{Counterparty: "남제주빛드림", DocType: "계약서", Amount: "4,000,000원", Date: "2026. 8.", SourceRef: "m:5"},
	} {
		if _, _, err := store.UpsertDealPage(in, now); err != nil {
			t.Fatalf("UpsertDealPage(%s): %v", in.Counterparty, err)
		}
	}
	tool := ToolDealLedger(store)

	// August holds exactly the two August documents — the 26.08.26 shorthand
	// lands in its real month, and the month-only "2026. 8." row does not.
	out, err := tool(context.Background(), json.RawMessage(`{"since":"2026-08-01","until":"2026-08-31"}`))
	if err != nil {
		t.Fatalf("deal_ledger: %v", err)
	}
	if !strings.Contains(out, "거래 원장 2건") {
		t.Errorf("August bucket is not exactly 2 rows:\n%s", out)
	}
	for _, want := range []string{"연우철강", "우림건설"} {
		if !strings.Contains(out, want) {
			t.Errorf("August bucket missing %s:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"금비전자", "SKB", "남제주빛드림"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("August bucket leaked %s (wrong month or undated):\n%s", unwanted, out)
		}
	}
	// The undated row is reported, not dropped in silence.
	if !strings.Contains(out, "날짜 미상 1건은 기간 필터에서 제외됨") {
		t.Errorf("undated row was excluded without saying so:\n%s", out)
	}

	// Neighbouring months are equally exact.
	for _, tc := range []struct{ since, until, want, counterparty string }{
		{"2026-07-01", "2026-07-31", "거래 원장 1건", "금비전자"},
		{"2026-06-01", "2026-06-30", "거래 원장 1건", "SKB"},
	} {
		got, err := tool(context.Background(), json.RawMessage(`{"since":"`+tc.since+`","until":"`+tc.until+`"}`))
		if err != nil {
			t.Fatalf("deal_ledger %s: %v", tc.since, err)
		}
		if !strings.Contains(got, tc.want) || !strings.Contains(got, tc.counterparty) {
			t.Errorf("%s bucket = wrong contents:\n%s", tc.since, got)
		}
	}

	// Without a date bound nothing is hidden, and nothing is reported hidden.
	all, err := tool(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("deal_ledger unbounded: %v", err)
	}
	if !strings.Contains(all, "거래 원장 5건") || strings.Contains(all, "날짜 미상") {
		t.Errorf("unbounded query should show all 5 rows and no exclusion notice:\n%s", all)
	}
}

// TestToolDealLedgerCountsOneContractOnce is the read-out half of the deal
// collapse: the same contract filed from several mails must be listed as the
// documents it is, but summed once — and the line has to SAY it merged, or the
// gap between "원장 4건" and a one-deal total reads as a bug.
func TestToolDealLedgerCountsOneContractOnce(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	// One EPC contract, carried by four different mails — the production shape.
	for _, ref := range []string{"mail:a", "mail:b", "mail:c", "mail:d"} {
		in := wiki.DealPageInput{
			Counterparty: "주식회사 해봄에너지",
			DocType:      "계약서",
			Amount:       "114,705,800,000원",
			Date:         "2026-08-13",
			SourceRef:    ref,
		}
		if _, _, err := store.UpsertDealPage(in, now); err != nil {
			t.Fatalf("UpsertDealPage: %v", err)
		}
	}

	out, err := ToolDealLedger(store)(context.Background(), json.RawMessage(`{"counterparty":"해봄"}`))
	if err != nil {
		t.Fatalf("deal_ledger: %v", err)
	}
	// Every document stays visible — the ledger is evidence.
	if !strings.Contains(out, "거래 원장 4건") {
		t.Errorf("document rows must still be listed:\n%s", out)
	}
	// But the money is counted once.
	if !strings.Contains(out, "KRW 114,705,800,000 (1건)") {
		t.Errorf("contract must be summed once:\n%s", out)
	}
	if !strings.Contains(out, "같은 거래 중복 3행 병합") {
		t.Errorf("the merge must be stated, not silent:\n%s", out)
	}
	if !strings.Contains(out, "×4행") {
		t.Errorf("the merged group must be named:\n%s", out)
	}
}
