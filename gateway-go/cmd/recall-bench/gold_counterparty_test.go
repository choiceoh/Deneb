package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeProject lays down a project folder with a client: frontmatter and a body.
func writeProject(t *testing.T, wiki, code, client, body string) {
	t.Helper()
	dir := filepath.Join(wiki, "프로젝트", code)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	page := "---\nid: " + code + "\nclient: " + client + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "대표.md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
}

func counterpartyFixture(t *testing.T) string {
	t.Helper()
	wiki := t.TempDir()
	writeProject(t, wiki, "pl2-kia-epc-002", "기아", "기아 광주공장 5MW 태양광. 케이블 발주 진행.")
	writeProject(t, wiki, "pl2-kum-epc-001", "금호타이어", "금호타이어 곡성 농공단지 리스사업.")
	writeProject(t, wiki, "nde-joc-cbl-001", "JOCA", "비금도 154kV 케이블. ZTT 공급분 포함.")
	writeProject(t, wiki, "pl1-mrp-dev-001", "무림피앤피", "무림 해상풍력 개발.")
	return wiki
}

// The defect this guard exists for: a mail about counterparty A labeled with a
// project whose client is B, where A appears nowhere under it. Live 2026-07-25:
// an-mail-3 (금호타이어 mail → 기아 project, 0 mentions across 6 pages).
func TestCounterpartyMismatchFlagsAbsentCounterparty(t *testing.T) {
	wiki := counterpartyFixture(t)
	cases := []goldCase{{
		ID:        "an-mail-3",
		Question:  "[탑솔라(주)] 금호타이어 광주공장 사업계획변경 승인 신청서 송부의 件",
		GoldPaths: []string{"프로젝트/pl2-kia-epc-002"},
	}}
	suspect, sample := counterpartyMismatchCases(wiki, cases)
	if len(suspect) != 1 || suspect[0] != "an-mail-3" {
		t.Fatalf("suspect = %v, want [an-mail-3]", suspect)
	}
	if len(sample) != 1 {
		t.Fatalf("sample = %v, want one entry", sample)
	}
}

// The second stage is what keeps the guard from crying wolf: a mail can name a
// counterparty the project is not and still belong there. A ZTT cable mail
// filed under a JOCA project is correct — the project page names ZTT.
func TestCounterpartyMismatchAllowsPresentCounterparty(t *testing.T) {
	wiki := counterpartyFixture(t)
	cases := []goldCase{{
		ID:        "an-mail-263",
		Question:  "Beigeum 154kV Project - ZTT Cable 납품 일정",
		GoldPaths: []string{"프로젝트/nde-joc-cbl-001"},
	}}
	if suspect, _ := counterpartyMismatchCases(wiki, cases); len(suspect) != 0 {
		t.Fatalf("multi-party mail must not flag, got %v", suspect)
	}
}

// Spelling variants are one counterparty. Comparing them raw reported 76
// mismatches on the live set where 9 were real.
func TestCounterpartyMismatchFoldsNameVariants(t *testing.T) {
	wiki := counterpartyFixture(t)
	for _, q := range []string{
		"[탑솔라(주)] 무림페이퍼 환경공단 지원사업 내역서 송부의 件",
		"[탑솔라(주)] 무림P&P 풍력발전 관련 내용 전달의 件",
		"[탑솔라(주)] 무림피엔피 검토 내용 송부의 件",
	} {
		cases := []goldCase{{ID: "v", Question: q, GoldPaths: []string{"프로젝트/pl1-mrp-dev-001"}}}
		if suspect, _ := counterpartyMismatchCases(wiki, cases); len(suspect) != 0 {
			t.Errorf("variant %q must fold to the same counterparty, got %v", q, suspect)
		}
	}
}

// The operator's own company prefixes nearly every mail subject and is also a
// client on internal projects. Leaving it in the vocabulary made the check fire
// on almost every case.
func TestCounterpartyMismatchIgnoresSelfCompany(t *testing.T) {
	wiki := counterpartyFixture(t)
	writeProject(t, wiki, "internal-001", selfCounterparty, "내부 과제.")
	cases := []goldCase{{
		ID:        "self",
		Question:  "[탑솔라(주)] 기아 광주 공사 예상일정표 송부의 件",
		GoldPaths: []string{"프로젝트/pl2-kia-epc-002"},
	}}
	if suspect, _ := counterpartyMismatchCases(wiki, cases); len(suspect) != 0 {
		t.Fatalf("the self company must not drive a mismatch, got %v", suspect)
	}
}

// must_contain cases belong to mismatchedGoldCases; judging them here too would
// double-report the same row under two different warnings.
func TestCounterpartyMismatchSkipsMustContainCases(t *testing.T) {
	wiki := counterpartyFixture(t)
	cases := []goldCase{{
		ID:          "owned-by-sibling-guard",
		Question:    "금호타이어 광주공장 승인 신청서",
		GoldPaths:   []string{"프로젝트/pl2-kia-epc-002"},
		MustContain: []string{"케이블"},
	}}
	if suspect, _ := counterpartyMismatchCases(wiki, cases); len(suspect) != 0 {
		t.Fatalf("must_contain case is the sibling guard's, got %v", suspect)
	}
}

// No frontmatter to judge against, or a gold project with no declared client,
// must stay silent rather than cry wolf — the same posture the sibling guards
// take on an unwalkable tree.
func TestCounterpartyMismatchStaysSilentWithoutAuthority(t *testing.T) {
	cases := []goldCase{{
		ID:        "x",
		Question:  "금호타이어 광주공장 승인 신청서",
		GoldPaths: []string{"프로젝트/pl2-kia-epc-002"},
	}}
	if suspect, _ := counterpartyMismatchCases(t.TempDir(), cases); len(suspect) != 0 {
		t.Fatalf("empty wiki must stay silent, got %v", suspect)
	}

	wiki := t.TempDir()
	dir := filepath.Join(wiki, "프로젝트", "pl2-kia-epc-002")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "대표.md"), []byte("---\nid: x\n---\n본문"), 0o644); err != nil {
		t.Fatal(err)
	}
	if suspect, _ := counterpartyMismatchCases(wiki, cases); len(suspect) != 0 {
		t.Fatalf("a project with no client: must stay silent, got %v", suspect)
	}
}

func TestFrontmatterFieldReadsFirstScalar(t *testing.T) {
	body := "---\nid: a\nclient: 기아, 남도에코\ntitle: 광주\n---\n본문 client: 무시"
	if got := frontmatterField(body, "client"); got != "기아" {
		t.Fatalf("client = %q, want 기아 (first value, comma-split)", got)
	}
	if got := frontmatterField(body, "missing"); got != "" {
		t.Fatalf("missing field = %q, want empty", got)
	}
}
