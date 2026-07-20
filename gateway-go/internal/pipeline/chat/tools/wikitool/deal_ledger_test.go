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
